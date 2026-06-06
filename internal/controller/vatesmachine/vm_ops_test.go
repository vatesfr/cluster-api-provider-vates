package vatesmachine

import (
	"context"

	"github.com/gofrs/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"

	xoclient "github.com/vatesfr/xenorchestra-go-sdk/client"
	"github.com/vatesfr/xenorchestra-go-sdk/pkg/payloads"
	xok8scommon "github.com/vatesfr/xenorchestra-k8s-common"
	k8smocks "github.com/vatesfr/xenorchestra-k8s-common/mocks"

	infrastructurev1beta2 "github.com/vatesfr/cluster-api-provider-vates/api/v1beta2"
)

var _ = Describe("ManageVIFs", func() {
	var (
		ctrl    *gomock.Controller
		mockV1  *MockXOClient
		mockLib *k8smocks.MockLibrary
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockV1 = NewMockXOClient(ctrl)
		mockLib = k8smocks.NewMockLibrary(ctrl)
		mockLib.EXPECT().V1Client().Return(mockV1).AnyTimes()
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	It("does nothing when NetworkConfig is nil", func() {
		err := ManageVIFs(context.Background(), nil, &infrastructurev1beta2.VatesMachine{}, &payloads.VM{})
		Expect(err).NotTo(HaveOccurred())
	})

	It("does nothing when Networks is empty", func() {
		err := ManageVIFs(context.Background(), nil, &infrastructurev1beta2.VatesMachine{
			Spec: infrastructurev1beta2.VatesMachineSpec{
				NetworkConfig: &infrastructurev1beta2.NetworkConfig{},
			},
		}, &payloads.VM{})
		Expect(err).NotTo(HaveOccurred())
	})

	It("reconnects disconnected VIFs", func() {
		vmUUID := uuid.Must(uuid.NewV4())
		mockV1.EXPECT().
			GetVIFs(&xoclient.Vm{Id: vmUUID.String()}).
			Return([]xoclient.VIF{
				{Id: "vif-1", Attached: false, Network: "net-1"},
				{Id: "vif-2", Attached: true, Network: "net-2"},
			}, nil)

		mockV1.EXPECT().
			ConnectVIF(&xoclient.VIF{Id: "vif-1", Attached: false, Network: "net-1"}).
			Return(nil)

		err := ManageVIFs(context.Background(), &xok8scommon.XoClient{Client: mockLib},
			&infrastructurev1beta2.VatesMachine{
				Spec: infrastructurev1beta2.VatesMachineSpec{
					NetworkConfig: &infrastructurev1beta2.NetworkConfig{
						Networks: []infrastructurev1beta2.Network{{NetworkID: "net-1"}},
					},
				},
			}, &payloads.VM{ID: vmUUID})
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("ResolveVMIP", func() {
	var (
		ctrl    *gomock.Controller
		mockV1  *MockXOClient
		mockLib *k8smocks.MockLibrary
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockV1 = NewMockXOClient(ctrl)
		mockLib = k8smocks.NewMockLibrary(ctrl)
		mockLib.EXPECT().V1Client().Return(mockV1).AnyTimes()
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	It("returns MainIpAddress when it is an IPv4 address", func() {
		vm := &payloads.VM{ID: uuid.Must(uuid.NewV4()), MainIpAddress: "192.168.1.42"}
		ip := ResolveVMIP(context.Background(), &xok8scommon.XoClient{Client: mockLib}, vm)
		Expect(ip).To(Equal("192.168.1.42"))
	})

	It("falls back to V1 addresses when MainIpAddress is fe80:", func() {
		vmID := uuid.Must(uuid.NewV4())
		mockV1.EXPECT().
			GetVm(xoclient.Vm{Id: vmID.String()}).
			Return(&xoclient.Vm{Addresses: map[string]string{"0": "192.168.1.42"}}, nil)

		vm := &payloads.VM{ID: vmID, MainIpAddress: "fe80::1"}
		ip := ResolveVMIP(context.Background(), &xok8scommon.XoClient{Client: mockLib}, vm)
		Expect(ip).To(Equal("192.168.1.42"))
	})
})
