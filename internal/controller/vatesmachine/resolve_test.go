package vatesmachine

import (
	"context"

	"github.com/gofrs/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	xoclient "github.com/vatesfr/xenorchestra-go-sdk/client"
	xok8scommon "github.com/vatesfr/xenorchestra-k8s-common"
	k8smocks "github.com/vatesfr/xenorchestra-k8s-common/mocks"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	infrastructurev1beta2 "git.vates.tech/patrice.ferlet/vates-capi/api/v1beta2"
)

var _ = Describe("ResolveBootstrapData", func() {
	var (
		scheme *runtime.Scheme
		ctx    context.Context
	)

	BeforeEach(func() {
		scheme = runtime.NewScheme()
		Expect(clusterv1.AddToScheme(scheme)).To(Succeed())
		Expect(infrastructurev1beta2.AddToScheme(scheme)).To(Succeed())
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		ctx = context.Background()
	})

	It("returns requeue when Machine exists but DataSecretName is nil", func() {
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
			&infrastructurev1beta2.VatesMachine{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			},
			&clusterv1.Machine{
				ObjectMeta: metav1.ObjectMeta{Name: "owner", Namespace: "default"},
				Spec: clusterv1.MachineSpec{
					Bootstrap: clusterv1.Bootstrap{DataSecretName: nil},
					InfrastructureRef: clusterv1.ContractVersionedObjectReference{
						Kind: "VatesMachine",
						Name: "test",
					},
				},
			},
		).Build()

		xm := &infrastructurev1beta2.VatesMachine{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"}}
		result, err := ResolveBootstrapData(ctx, fakeClient, xm)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Requeue).To(BeTrue())
		Expect(result.Machine).NotTo(BeNil())
	})

	It("returns inline data when no Machine", func() {
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		xm := &infrastructurev1beta2.VatesMachine{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec:       infrastructurev1beta2.VatesMachineSpec{BootstrapData: "inline-data"},
		}
		result, err := ResolveBootstrapData(ctx, fakeClient, xm)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Requeue).To(BeFalse())
		Expect(string(result.Data)).To(Equal("inline-data"))
	})

	It("returns requeue when no Machine and no inline data", func() {
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		xm := &infrastructurev1beta2.VatesMachine{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		}
		result, err := ResolveBootstrapData(ctx, fakeClient, xm)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Requeue).To(BeTrue())
	})
})

var _ = Describe("ResolveNetworkID", func() {
	var (
		ctrl *gomock.Controller
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	It("returns NetworkID directly when provided", func() {
		id, err := ResolveNetworkID(nil, infrastructurev1beta2.Network{NetworkID: "net-123"}, uuid.Nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(id).To(Equal("net-123"))
	})

	It("looks up by Name via V1.GetNetwork", func() {
		mockV1 := NewMockXOClient(ctrl)
		mockV1.EXPECT().
			GetNetwork(xoclient.Network{NameLabel: "my-network", PoolId: ""}).
			Return(&xoclient.Network{Id: "net-abc"}, nil)

		id, err := ResolveNetworkID(mockV1, infrastructurev1beta2.Network{Name: "my-network"}, uuid.Nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(id).To(Equal("net-abc"))
	})

	It("passes PoolId to GetNetwork when poolID is given", func() {
		poolUUID := uuid.Must(uuid.NewV4())
		mockV1 := NewMockXOClient(ctrl)
		mockV1.EXPECT().
			GetNetwork(xoclient.Network{NameLabel: "my-net", PoolId: poolUUID.String()}).
			Return(&xoclient.Network{Id: "net-xyz"}, nil)

		id, err := ResolveNetworkID(mockV1, infrastructurev1beta2.Network{Name: "my-net"}, poolUUID)
		Expect(err).NotTo(HaveOccurred())
		Expect(id).To(Equal("net-xyz"))
	})

	It("returns an error when neither NetworkID nor Name is set", func() {
		_, err := ResolveNetworkID(nil, infrastructurev1beta2.Network{}, uuid.Nil)
		Expect(err).To(HaveOccurred())
	})

	It("returns an error when the network is not found by Name", func() {
		mockV1 := NewMockXOClient(ctrl)
		mockV1.EXPECT().
			GetNetwork(gomock.Any()).
			Return(nil, xoclient.NotFound{})

		_, err := ResolveNetworkID(mockV1, infrastructurev1beta2.Network{Name: "missing"}, uuid.Nil)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("ResolvePoolID", func() {
	var (
		ctrl    *gomock.Controller
		ctx     context.Context
		mockV1  *MockXOClient
		mockLib *k8smocks.MockLibrary
		xo      *xok8scommon.XoClient
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		ctx = context.Background()
		mockV1 = NewMockXOClient(ctrl)
		mockLib = k8smocks.NewMockLibrary(ctrl)
		mockLib.EXPECT().V1Client().Return(mockV1).AnyTimes()
		xo = &xok8scommon.XoClient{Client: mockLib}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	It("returns PoolID directly when provided as a valid UUID", func() {
		poolUUID := uuid.Must(uuid.NewV4())
		id, err := ResolvePoolID(ctx, xo, poolUUID.String(), "")
		Expect(err).NotTo(HaveOccurred())
		Expect(id).To(Equal(poolUUID))
	})

	It("returns an error when PoolID is an invalid UUID", func() {
		_, err := ResolvePoolID(ctx, xo, "not-a-uuid", "")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("invalid pool ID"))
	})

	It("looks up by PoolName via V1.GetPoolByName", func() {
		poolUUID := uuid.Must(uuid.NewV4())
		mockV1.EXPECT().
			GetPoolByName("my-pool").
			Return([]xoclient.Pool{{Id: poolUUID.String()}}, nil)

		id, err := ResolvePoolID(ctx, xo, "", "my-pool")
		Expect(err).NotTo(HaveOccurred())
		Expect(id).To(Equal(poolUUID))
	})

	It("returns an error when the pool is not found by Name", func() {
		mockV1.EXPECT().
			GetPoolByName("missing").
			Return(nil, nil)

		_, err := ResolvePoolID(ctx, xo, "", "missing")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("not found"))
	})

	It("returns uuid.Nil when both PoolID and PoolName are empty", func() {
		id, err := ResolvePoolID(ctx, xo, "", "")
		Expect(err).NotTo(HaveOccurred())
		Expect(id).To(Equal(uuid.Nil))
	})
})

var _ = Describe("ResolveTemplateID", func() {
	var (
		ctrl    *gomock.Controller
		ctx     context.Context
		mockV1  *MockXOClient
		mockLib *k8smocks.MockLibrary
		xo      *xok8scommon.XoClient
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		ctx = context.Background()
		mockV1 = NewMockXOClient(ctrl)
		mockLib = k8smocks.NewMockLibrary(ctrl)
		mockLib.EXPECT().V1Client().Return(mockV1).AnyTimes()
		xo = &xok8scommon.XoClient{Client: mockLib}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	It("returns TemplateID directly when provided as a valid UUID", func() {
		templateUUID := uuid.Must(uuid.NewV4())
		id, err := ResolveTemplateID(ctx, xo, templateUUID.String(), "")
		Expect(err).NotTo(HaveOccurred())
		Expect(id).To(Equal(templateUUID))
	})

	It("returns an error when TemplateID is an invalid UUID", func() {
		_, err := ResolveTemplateID(ctx, xo, "not-a-uuid", "")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("invalid template ID"))
	})

	It("looks up by TemplateName via V1.GetTemplate", func() {
		templateUUID := uuid.Must(uuid.NewV4())
		mockV1.EXPECT().
			GetTemplate(xoclient.Template{NameLabel: "my-template"}).
			Return([]xoclient.Template{{Id: templateUUID.String()}}, nil)

		id, err := ResolveTemplateID(ctx, xo, "", "my-template")
		Expect(err).NotTo(HaveOccurred())
		Expect(id).To(Equal(templateUUID))
	})

	It("returns an error when the template is not found by Name", func() {
		mockV1.EXPECT().
			GetTemplate(gomock.Any()).
			Return(nil, nil)

		_, err := ResolveTemplateID(ctx, xo, "", "missing")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("not found"))
	})

	It("returns an error when both TemplateID and TemplateName are empty", func() {
		_, err := ResolveTemplateID(ctx, xo, "", "")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("must specify"))
	})
})
