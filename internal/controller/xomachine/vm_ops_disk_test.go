package xomachine

import (
	"context"
	"fmt"

	"github.com/gofrs/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"

	xoclient "github.com/vatesfr/xenorchestra-go-sdk/client"
	"github.com/vatesfr/xenorchestra-go-sdk/pkg/payloads"
	xok8scommon "github.com/vatesfr/xenorchestra-k8s-common"
	k8smocks "github.com/vatesfr/xenorchestra-k8s-common/mocks"
)

func vbdUUID(seed string) string {
	return uuid.NewV5(uuid.NamespaceOID, seed).String()
}

func disk(id, name string, size int, bootable bool, position, device string) xoclient.Disk {
	return xoclient.Disk{
		VBD: xoclient.VBD{
			Id:       vbdUUID(id),
			Bootable: bootable,
			Position: position,
			Device:   device,
		},
		VDI: xoclient.VDI{
			VDIId:     "vdi-" + id,
			NameLabel: name,
			Size:      size,
		},
	}
}

var _ = Describe("findMainDisk", func() {
	It("returns nil when there are no disks", func() {
		Expect(findMainDisk(nil)).To(BeNil())
	})

	It("returns the only disk when a single disk is present", func() {
		only := disk("1", "main", 1024, false, "0", "xvda")
		Expect(findMainDisk([]xoclient.Disk{only})).To(Equal(&only))
	})

	It("returns the bootable disk even when it is not first in the slice", func() {
		cloudInit := disk("ci", "XO CloudConfigDrive", 100, false, "1", "xvdb")
		main := disk("main", "rhel-template", 2048, true, "0", "xvda")
		result := findMainDisk([]xoclient.Disk{cloudInit, main})
		Expect(result).NotTo(BeNil())
		Expect(result.VDIId).To(Equal("vdi-main"))
		Expect(result.Bootable).To(BeTrue())
	})

	It("ignores the cloud-init config drive even when it is bootable", func() {
		cloudInitBootable := disk("ci", "XO CloudConfigDrive", 12582912, true, "1", "xvdb")
		main := disk("main", "PFT-Talos-Nocloud", 4454350848, false, "0", "xvda")
		result := findMainDisk([]xoclient.Disk{cloudInitBootable, main})
		Expect(result).NotTo(BeNil())
		Expect(result.VDIId).To(Equal("vdi-main"))
		Expect(result.Bootable).To(BeFalse())
	})

	It("skips CD drives while looking for the bootable disk", func() {
		cd := xoclient.Disk{VBD: xoclient.VBD{Id: "vbd-cd", IsCdDrive: true, Bootable: true}}
		main := disk("main", "rhel-template", 2048, true, "0", "xvda")
		result := findMainDisk([]xoclient.Disk{cd, main})
		Expect(result).NotTo(BeNil())
		Expect(result.VDIId).To(Equal("vdi-main"))
	})

	It("falls back to the lowest position, ignoring the cloud-init drive, when no disk is bootable", func() {
		cloudInit := disk("ci", "XO CloudConfigDrive", 100, false, "1", "xvdb")
		secondary := disk("data", "data-disk", 500, false, "2", "xvdc")
		main := disk("main", "rhel-template", 1024, false, "0", "xvda")
		result := findMainDisk([]xoclient.Disk{cloudInit, secondary, main})
		Expect(result).NotTo(BeNil())
		Expect(result.VDIId).To(Equal("vdi-main"))
	})

	It("uses the device name as a tie-breaker when positions are missing", func() {
		cloudInit := disk("ci", "XO CloudConfigDrive", 100, false, "", "xvdb")
		main := disk("main", "rhel-template", 1024, false, "", "xvda")
		result := findMainDisk([]xoclient.Disk{cloudInit, main})
		Expect(result).NotTo(BeNil())
		Expect(result.VDIId).To(Equal("vdi-main"))
	})

	It("returns nil when all disks are cloud-init drives", func() {
		cloudInitA := disk("a", "XO CloudConfigDrive", 100, false, "0", "xvda")
		cloudInitB := disk("b", "XO CloudConfigDrive", 100, false, "1", "xvdb")
		result := findMainDisk([]xoclient.Disk{cloudInitA, cloudInitB})
		Expect(result).To(BeNil())
	})
})

var _ = Describe("DetachPersistentVolumes", func() {
	const pvFilter = "tags:/^k8s:volumeId:/"

	var (
		ctrl     *gomock.Controller
		mockLib  *k8smocks.MockLibrary
		mockVM   *k8smocks.MockVM
		mockVBD  *MockVBD
		mockTask *MockTask
		xo       *xok8scommon.XoClient
		vmID     uuid.UUID
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockLib = k8smocks.NewMockLibrary(ctrl)
		mockVM = k8smocks.NewMockVM(ctrl)
		mockVBD = NewMockVBD(ctrl)
		mockTask = NewMockTask(ctrl)
		mockLib.EXPECT().VM().Return(mockVM).AnyTimes()
		mockLib.EXPECT().VBD().Return(mockVBD).AnyTimes()
		mockLib.EXPECT().Task().Return(mockTask).AnyTimes()
		xo = &xok8scommon.XoClient{Client: mockLib}
		vmID = uuid.Must(uuid.NewV4())
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	It("removes the VBD of a tagged VDI and leaves the other disks alone", func() {
		rootVDI := uuid.Must(uuid.NewV4())
		pvVDI := uuid.Must(uuid.NewV4())
		rootVBD := uuid.Must(uuid.NewV4())
		pvVBD := uuid.Must(uuid.NewV4())

		mockVM.EXPECT().GetVDIs(gomock.Any(), vmID, 0, pvFilter).Return([]*payloads.VDI{{ID: pvVDI}}, nil)
		mockVBD.EXPECT().GetAll(gomock.Any(), 0, "VM:"+vmID.String()).Return([]*payloads.VBD{
			{ID: rootVBD, VDI: &rootVDI, VM: vmID, Attached: true},
			{ID: pvVBD, VDI: &pvVDI, VM: vmID, Attached: true},
		}, nil)
		mockVBD.EXPECT().Disconnect(gomock.Any(), pvVBD).Return("", nil)
		mockVBD.EXPECT().Delete(gomock.Any(), pvVBD).Return(nil)

		Expect(DetachPersistentVolumes(context.Background(), xo, vmID)).To(Succeed())
	})

	It("does nothing when the VM has no tagged VDI", func() {
		mockVM.EXPECT().GetVDIs(gomock.Any(), vmID, 0, pvFilter).Return(nil, nil)
		Expect(DetachPersistentVolumes(context.Background(), xo, vmID)).To(Succeed())
	})

	It("removes the VBD of a halted VM's tagged VDI without disconnecting it", func() {
		pvVDI := uuid.Must(uuid.NewV4())
		pvVBD := uuid.Must(uuid.NewV4())

		mockVM.EXPECT().GetVDIs(gomock.Any(), vmID, 0, pvFilter).Return([]*payloads.VDI{{ID: pvVDI}}, nil)
		mockVBD.EXPECT().GetAll(gomock.Any(), 0, "VM:"+vmID.String()).Return([]*payloads.VBD{
			{ID: pvVBD, VDI: &pvVDI, VM: vmID, Attached: false},
		}, nil)
		mockVBD.EXPECT().Delete(gomock.Any(), pvVBD).Return(nil)

		Expect(DetachPersistentVolumes(context.Background(), xo, vmID)).To(Succeed())
	})

	It("waits for the disconnect task when the disk is attached", func() {
		pvVDI := uuid.Must(uuid.NewV4())
		pvVBD := uuid.Must(uuid.NewV4())

		mockVM.EXPECT().GetVDIs(gomock.Any(), vmID, 0, pvFilter).Return([]*payloads.VDI{{ID: pvVDI}}, nil)
		mockVBD.EXPECT().GetAll(gomock.Any(), 0, "VM:"+vmID.String()).Return([]*payloads.VBD{
			{ID: pvVBD, VDI: &pvVDI, VM: vmID, Attached: true},
		}, nil)
		mockVBD.EXPECT().Disconnect(gomock.Any(), pvVBD).Return("task-disconnect", nil)
		mockTask.EXPECT().Wait(gomock.Any(), "task-disconnect").Return(&payloads.Task{Status: payloads.Success}, nil)
		mockVBD.EXPECT().Delete(gomock.Any(), pvVBD).Return(nil)

		Expect(DetachPersistentVolumes(context.Background(), xo, vmID)).To(Succeed())
	})

	It("treats a 404 from the VBD deletion as already detached", func() {
		pvVDI := uuid.Must(uuid.NewV4())
		pvVBD := uuid.Must(uuid.NewV4())

		mockVM.EXPECT().GetVDIs(gomock.Any(), vmID, 0, pvFilter).Return([]*payloads.VDI{{ID: pvVDI}}, nil)
		mockVBD.EXPECT().GetAll(gomock.Any(), 0, "VM:"+vmID.String()).Return([]*payloads.VBD{
			{ID: pvVBD, VDI: &pvVDI, VM: vmID, Attached: false},
		}, nil)
		mockVBD.EXPECT().Delete(gomock.Any(), pvVBD).Return(fmt.Errorf("API error: 404 Not Found - no such VBD"))

		Expect(DetachPersistentVolumes(context.Background(), xo, vmID)).To(Succeed())
	})

	It("returns an error when the VBD deletion fails", func() {
		pvVDI := uuid.Must(uuid.NewV4())
		pvVBD := uuid.Must(uuid.NewV4())

		mockVM.EXPECT().GetVDIs(gomock.Any(), vmID, 0, pvFilter).Return([]*payloads.VDI{{ID: pvVDI}}, nil)
		mockVBD.EXPECT().GetAll(gomock.Any(), 0, "VM:"+vmID.String()).Return([]*payloads.VBD{
			{ID: pvVBD, VDI: &pvVDI, VM: vmID, Attached: false},
		}, nil)
		mockVBD.EXPECT().Delete(gomock.Any(), pvVBD).Return(fmt.Errorf("API error: 500 Internal Server Error - boom"))

		Expect(DetachPersistentVolumes(context.Background(), xo, vmID)).To(HaveOccurred())
	})

	It("returns an error when the disconnect fails", func() {
		pvVDI := uuid.Must(uuid.NewV4())
		pvVBD := uuid.Must(uuid.NewV4())

		mockVM.EXPECT().GetVDIs(gomock.Any(), vmID, 0, pvFilter).Return([]*payloads.VDI{{ID: pvVDI}}, nil)
		mockVBD.EXPECT().GetAll(gomock.Any(), 0, "VM:"+vmID.String()).Return([]*payloads.VBD{
			{ID: pvVBD, VDI: &pvVDI, VM: vmID, Attached: true},
		}, nil)
		mockVBD.EXPECT().Disconnect(gomock.Any(), pvVBD).Return("", fmt.Errorf("API error: 500 Internal Server Error - boom"))

		Expect(DetachPersistentVolumes(context.Background(), xo, vmID)).To(HaveOccurred())
	})

	It("treats a 404 from the disconnect as already gone", func() {
		pvVDI := uuid.Must(uuid.NewV4())
		pvVBD := uuid.Must(uuid.NewV4())

		mockVM.EXPECT().GetVDIs(gomock.Any(), vmID, 0, pvFilter).Return([]*payloads.VDI{{ID: pvVDI}}, nil)
		mockVBD.EXPECT().GetAll(gomock.Any(), 0, "VM:"+vmID.String()).Return([]*payloads.VBD{
			{ID: pvVBD, VDI: &pvVDI, VM: vmID, Attached: true},
		}, nil)
		mockVBD.EXPECT().Disconnect(gomock.Any(), pvVBD).Return("", fmt.Errorf("API error: 404 Not Found - no such VBD"))

		Expect(DetachPersistentVolumes(context.Background(), xo, vmID)).To(Succeed())
	})

	It("is a no-op when the VM is already gone (404)", func() {
		mockVM.EXPECT().GetVDIs(gomock.Any(), vmID, 0, pvFilter).Return(nil, fmt.Errorf("API error: 404 Not Found"))

		Expect(DetachPersistentVolumes(context.Background(), xo, vmID)).To(Succeed())
	})

	It("returns an error when listing the VM's VDIs fails transiently", func() {
		mockVM.EXPECT().GetVDIs(gomock.Any(), vmID, 0, pvFilter).Return(nil, fmt.Errorf("API error: 500 Internal Server Error"))

		Expect(DetachPersistentVolumes(context.Background(), xo, vmID)).To(HaveOccurred())
	})
})
