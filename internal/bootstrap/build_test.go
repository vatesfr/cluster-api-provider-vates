package bootstrap

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	xoclient "github.com/vatesfr/xenorchestra-go-sdk/client"
	xok8scommon "github.com/vatesfr/xenorchestra-k8s-common"
	k8smocks "github.com/vatesfr/xenorchestra-k8s-common/mocks"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	infrastructurev1beta2 "github.com/vatesfr/cluster-api-provider-vates/api/v1beta2"
)

var _ = Describe("Build", func() {
	It("passes the payload through and writes no network config by default", func() {
		deps := Dependencies{
			XOMachine:     &infrastructurev1beta2.XOMachine{},
			BootstrapData: []byte("version: v1alpha1\n"),
		}
		result, err := Build(context.Background(), deps, Behavior{})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.CloudConfig).To(Equal("version: v1alpha1\n"))
		Expect(result.NetworkConfig).To(BeNil())
	})

	It("returns the guest network config when one is set", func() {
		deps := Dependencies{XOMachine: &infrastructurev1beta2.XOMachine{
			Spec: infrastructurev1beta2.XOMachineSpec{
				NetworkConfig: &infrastructurev1beta2.NetworkConfig{GuestConfig: "network: {version: 2}"},
			},
		}}
		result, err := Build(context.Background(), deps, Behavior{})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.NetworkConfig).NotTo(BeNil())
		Expect(*result.NetworkConfig).To(Equal("network: {version: 2}"))
	})

	It("does not touch a non-cloud-init payload even when KubeVIP is requested", func() {
		deps := Dependencies{
			XOMachine:     &infrastructurev1beta2.XOMachine{},
			BootstrapData: []byte("version: v1alpha1\nmachine:\n  type: controlplane\n"),
		}
		result, err := Build(context.Background(), deps, Behavior{CloudInit: false, KubeVIP: true})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.CloudConfig).To(Equal("version: v1alpha1\nmachine:\n  type: controlplane\n"))
	})

	It("merges the XO user SSH keys for a cloud-init behavior when the cluster asks", func() {
		ctrl := gomock.NewController(GinkgoT())
		defer ctrl.Finish()

		mockV1 := NewMockXOClient(ctrl)
		mockLib := k8smocks.NewMockLibrary(ctrl)
		mockLib.EXPECT().V1Client().Return(mockV1).AnyTimes()

		scheme := runtime.NewScheme()
		Expect(clusterv1.AddToScheme(scheme)).To(Succeed())
		Expect(infrastructurev1beta2.AddToScheme(scheme)).To(Succeed())

		fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
			&clusterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "default"},
				Spec: clusterv1.ClusterSpec{
					InfrastructureRef: clusterv1.ContractVersionedObjectReference{
						Kind: "XOCluster",
						Name: "demo-xo",
					},
				},
			},
			&infrastructurev1beta2.XOCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "demo-xo", Namespace: "default"},
				Spec:       infrastructurev1beta2.XOClusterSpec{InjectSSHKeys: true},
			},
		).Build()

		mockV1.EXPECT().GetCurrentUser().Return(&xoclient.User{
			Preferences: xoclient.Preferences{
				SshKeys: []xoclient.SshKey{{Key: "ssh-rsa AAA test"}},
			},
		}, nil)

		deps := Dependencies{
			Client:        fakeClient,
			XOClient:      &xok8scommon.XoClient{Client: mockLib},
			BootstrapData: []byte("#cloud-config\n"),
			Machine: &clusterv1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "demo-cp",
					Namespace: "default",
					Labels:    map[string]string{"cluster.x-k8s.io/cluster-name": "demo"},
				},
			},
			XOMachine: &infrastructurev1beta2.XOMachine{
				ObjectMeta: metav1.ObjectMeta{Name: "demo-cp", Namespace: "default"},
			},
		}

		result, err := Build(context.Background(), deps, Behavior{CloudInit: true})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.CloudConfig).To(ContainSubstring("ssh-rsa AAA test"))
	})
})
