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

var _ = Describe("isPersistentVolumeDisk", func() {
	It("detects a disk by its csi- name label prefix", func() {
		d := disk("1", "csi-pvc-data", 1024, false, "0", "xvda")
		Expect(isPersistentVolumeDisk(d)).To(BeTrue())
	})

	It("detects a disk by its k8s:volumeId tag, even without a csi- prefix", func() {
		d := disk("1", "renamed-volume", 1024, false, "0", "xvda")
		d.Tags = []string{"k8s:volumeId:vol-123"}
		Expect(isPersistentVolumeDisk(d)).To(BeTrue())
	})

	It("returns false for a non-CSI disk", func() {
		osDisk := disk("1", "rhel-template", 1024, true, "0", "xvda")
		Expect(isPersistentVolumeDisk(osDisk)).To(BeFalse())
	})

	It("returns false for a cloud-init config drive", func() {
		ci := disk("2", "XO CloudConfigDrive", 100, false, "1", "xvdb")
		Expect(isPersistentVolumeDisk(ci)).To(BeFalse())
	})

	It("returns false for a CD-ROM", func() {
		cd := xoclient.Disk{VBD: xoclient.VBD{Id: "vbd-cd", IsCdDrive: true}}
		Expect(isPersistentVolumeDisk(cd)).To(BeFalse())
	})
})

var _ = Describe("DetachPersistentVolumes", func() {
	var (
		ctrl    *gomock.Controller
		mockV1  *MockXOClient
		mockLib *k8smocks.MockLibrary
		mockVBD *MockVBD
		xo      *xok8scommon.XoClient
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockV1 = NewMockXOClient(ctrl)
		mockLib = k8smocks.NewMockLibrary(ctrl)
		mockVBD = NewMockVBD(ctrl)
		mockLib.EXPECT().V1Client().Return(mockV1).AnyTimes()
		mockLib.EXPECT().VBD().Return(mockVBD).AnyTimes()
		xo = &xok8scommon.XoClient{Client: mockLib}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	It("detaches attached CSI disks but never deletes them", func() {
		pv := disk("pv", "csi-pvc-data", 1024, false, "2", "xvdc")
		pv.Attached = true
		osDisk := disk("os", "rhel-template", 2048, true, "0", "xvda")
		osDisk.Attached = true
		ci := disk("ci", "XO CloudConfigDrive", 100, false, "1", "xvdb")
		ci.Attached = true

		mockV1.EXPECT().GetDisks(gomock.Any()).Return([]xoclient.Disk{pv, osDisk, ci}, nil)
		mockV1.EXPECT().DisconnectDisk(pv).Return(nil)
		mockVBD.EXPECT().Delete(gomock.Any(), gomock.Any()).Return(nil)

		err := DetachPersistentVolumes(context.Background(), xo, &payloads.VM{ID: uuid.Must(uuid.NewV4())})
		Expect(err).NotTo(HaveOccurred())
	})

	It("removes the VBD of a halted VM's CSI disk (not attached) so the VDI survives vm deletion", func() {
		pv := disk("pv", "csi-pvc-data", 1024, false, "2", "xvdc")
		pv.Attached = false
		osDisk := disk("os", "rhel-template", 2048, true, "0", "xvda")
		osDisk.Attached = false

		mockV1.EXPECT().GetDisks(gomock.Any()).Return([]xoclient.Disk{pv, osDisk}, nil)
		mockVBD.EXPECT().Delete(gomock.Any(), gomock.Any()).Return(nil)

		err := DetachPersistentVolumes(context.Background(), xo, &payloads.VM{ID: uuid.Must(uuid.NewV4())})
		Expect(err).NotTo(HaveOccurred())
	})

	It("disconnects before removing the VBD when the disk is attached", func() {
		pv := disk("pv", "csi-pvc-data", 1024, false, "2", "xvdc")
		pv.Attached = true

		mockV1.EXPECT().GetDisks(gomock.Any()).Return([]xoclient.Disk{pv}, nil)
		mockV1.EXPECT().DisconnectDisk(pv).Return(nil)
		mockVBD.EXPECT().Delete(gomock.Any(), gomock.Any()).Return(nil)

		err := DetachPersistentVolumes(context.Background(), xo, &payloads.VM{ID: uuid.Must(uuid.NewV4())})
		Expect(err).NotTo(HaveOccurred())
	})

	It("treats a 404 from VBD deletion as already detached and continues without error", func() {
		pv := disk("pv", "csi-pvc-data", 1024, false, "2", "xvdc")
		pv.Attached = false

		mockV1.EXPECT().GetDisks(gomock.Any()).Return([]xoclient.Disk{pv}, nil)
		mockVBD.EXPECT().Delete(gomock.Any(), gomock.Any()).Return(fmt.Errorf("API error: 404 Not Found - ..."))

		err := DetachPersistentVolumes(context.Background(), xo, &payloads.VM{ID: uuid.Must(uuid.NewV4())})
		Expect(err).NotTo(HaveOccurred())
	})

	It("returns an error when VBD deletion fails", func() {
		pv := disk("pv", "csi-pvc-data", 1024, false, "2", "xvdc")
		pv.Attached = false

		mockV1.EXPECT().GetDisks(gomock.Any()).Return([]xoclient.Disk{pv}, nil)
		mockVBD.EXPECT().Delete(gomock.Any(), gomock.Any()).Return(fmt.Errorf("API error: 500 Internal Server Error - boom"))

		err := DetachPersistentVolumes(context.Background(), xo, &payloads.VM{ID: uuid.Must(uuid.NewV4())})
		Expect(err).To(HaveOccurred())
	})

	It("returns an error when the V1 client is nil", func() {
		nilLib := k8smocks.NewMockLibrary(ctrl)
		nilLib.EXPECT().V1Client().Return(nil).AnyTimes()
		err := DetachPersistentVolumes(context.Background(), &xok8scommon.XoClient{Client: nilLib}, &payloads.VM{ID: uuid.Must(uuid.NewV4())})
		Expect(err).To(HaveOccurred())
	})
})
