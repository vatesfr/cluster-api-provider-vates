package vatesmachine

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	xoclient "github.com/vatesfr/xenorchestra-go-sdk/client"
	xok8scommon "github.com/vatesfr/xenorchestra-k8s-common"
	k8smocks "github.com/vatesfr/xenorchestra-k8s-common/mocks"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	infrastructurev1beta2 "github.com/vatesfr/cluster-api-provider-vates/api/v1beta2"
)

var _ = Describe("MergeSSHKeysIntoCloudConfig", func() {
	It("leaves cloud-config unchanged when no keys are given", func() {
		input := "#cloud-config\nssh_authorized_keys:\n  - existing-key\n"
		out, err := MergeSSHKeysIntoCloudConfig(input, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(Equal(input))
	})

	It("adds new keys to the cloud-config", func() {
		out, err := MergeSSHKeysIntoCloudConfig(
			"#cloud-config\nssh_authorized_keys:\n  - existing-key\n",
			[]string{"new-key"},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(ContainSubstring("new-key"))
		Expect(out).To(ContainSubstring("existing-key"))
	})

	It("deduplicates keys already present in the config", func() {
		out, err := MergeSSHKeysIntoCloudConfig(
			"#cloud-config\nssh_authorized_keys:\n  - dup-key\n",
			[]string{"dup-key", "new-key"},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(ContainSubstring("- dup-key"))
		count := 0
		for i := 0; i <= len(out)-len("- dup-key"); i++ {
			if out[i:i+len("- dup-key")] == "- dup-key" {
				count++
			}
		}
		Expect(count).To(Equal(1))
	})

	It("creates ssh_authorized_keys from scratch when missing", func() {
		out, err := MergeSSHKeysIntoCloudConfig("#cloud-config\n", []string{"key-a"})
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(ContainSubstring("key-a"))
	})

	It("returns an error on invalid YAML", func() {
		_, err := MergeSSHKeysIntoCloudConfig("{{{invalid", []string{"key"})
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("BuildCloudConfig", func() {
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

	It("merges SSH keys into the cloud-config", func() {
		mockV1.EXPECT().
			GetCurrentUser().
			Return(&xoclient.User{
				Preferences: xoclient.Preferences{
					SshKeys: []xoclient.SshKey{{Key: "ssh-rsa AAA... user@host"}},
				},
			}, nil)

		out, err := BuildCloudConfig(context.Background(),
			&xok8scommon.XoClient{Client: mockLib},
			[]byte("#cloud-config\n"))
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(ContainSubstring("ssh-rsa"))
	})

	It("generates a minimal cloud-config when no bootstrap data", func() {
		mockV1.EXPECT().
			GetCurrentUser().
			Return(&xoclient.User{
				Preferences: xoclient.Preferences{
					SshKeys: []xoclient.SshKey{{Key: "ssh-ed25519 abc... test@test"}},
				},
			}, nil)

		out, err := BuildCloudConfig(context.Background(),
			&xok8scommon.XoClient{Client: mockLib},
			nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(ContainSubstring("#cloud-config"))
		Expect(out).To(ContainSubstring("ssh-ed25519"))
	})

	It("returns an error when V1Client is nil", func() {
		mockLib2 := k8smocks.NewMockLibrary(ctrl)
		mockLib2.EXPECT().V1Client().Return(nil)

		_, err := BuildCloudConfig(context.Background(),
			&xok8scommon.XoClient{Client: mockLib2},
			nil)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("GetOwnerMachine", func() {
	var (
		scheme *runtime.Scheme
		ctx    context.Context
	)

	BeforeEach(func() {
		scheme = runtime.NewScheme()
		Expect(clusterv1.AddToScheme(scheme)).To(Succeed())
		Expect(infrastructurev1beta2.AddToScheme(scheme)).To(Succeed())
		ctx = context.Background()
	})

	It("returns the Machine referencing this VatesMachine", func() {
		machine := &clusterv1.Machine{
			ObjectMeta: metav1.ObjectMeta{Name: "my-machine", Namespace: "default", UID: "machine-uid"},
			Spec: clusterv1.MachineSpec{
				InfrastructureRef: clusterv1.ContractVersionedObjectReference{
					Kind: "VatesMachine",
					Name: "my-xo-machine",
				},
			},
		}
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
			machine,
		).Build()

		xm := &infrastructurev1beta2.VatesMachine{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-xo-machine",
				Namespace: "default",
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: clusterv1.GroupVersion.String(),
						Kind:       "Machine",
						Name:       "my-machine",
						UID:        "machine-uid",
						Controller: ptr.To(true),
					},
				},
			},
		}
		result, err := GetOwnerMachine(ctx, fakeClient, xm)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).NotTo(BeNil())
		Expect(result.Name).To(Equal("my-machine"))
	})

	It("returns nil when no Machine matches", func() {
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		xm := &infrastructurev1beta2.VatesMachine{
			ObjectMeta: metav1.ObjectMeta{Name: "no-match", Namespace: "default"},
		}
		machine, err := GetOwnerMachine(ctx, fakeClient, xm)
		Expect(err).NotTo(HaveOccurred())
		Expect(machine).To(BeNil())
	})
})

var _ = Describe("GetBootstrapData", func() {
	var (
		scheme *runtime.Scheme
		ctx    context.Context
	)

	BeforeEach(func() {
		scheme = runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(clusterv1.AddToScheme(scheme)).To(Succeed())
		ctx = context.Background()
	})

	It("returns bootstrap data from the secret", func() {
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "bootstrap-secret", Namespace: "default"},
				Data:       map[string][]byte{"value": []byte("my-bootstrap-data")},
			},
		).Build()

		data, err := GetBootstrapData(ctx, fakeClient, &clusterv1.Machine{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
			Spec: clusterv1.MachineSpec{
				Bootstrap: clusterv1.Bootstrap{DataSecretName: ptr.To("bootstrap-secret")},
			},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(string(data)).To(Equal("my-bootstrap-data"))
	})

	It("returns an error when the secret does not exist", func() {
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		_, err := GetBootstrapData(ctx, fakeClient, &clusterv1.Machine{
			Spec: clusterv1.MachineSpec{
				Bootstrap: clusterv1.Bootstrap{DataSecretName: ptr.To("does-not-exist")},
			},
		})
		Expect(err).To(HaveOccurred())
	})

	It("returns an error when DataSecretName is nil", func() {
		_, err := GetBootstrapData(ctx, nil, &clusterv1.Machine{
			Spec: clusterv1.MachineSpec{
				Bootstrap: clusterv1.Bootstrap{DataSecretName: nil},
			},
		})
		Expect(err).To(HaveOccurred())
	})
})
