package bootstrap

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	infrastructurev1beta2 "github.com/vatesfr/cluster-api-provider-vates/api/v1beta2"
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
		machine := &clusterv1.Machine{
			ObjectMeta: metav1.ObjectMeta{Name: "owner", Namespace: "default", UID: "machine-uid"},
			Spec: clusterv1.MachineSpec{
				Bootstrap: clusterv1.Bootstrap{DataSecretName: nil},
				InfrastructureRef: clusterv1.ContractVersionedObjectReference{
					Kind: "XOMachine",
					Name: "test",
				},
			},
		}
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
			machine,
		).Build()

		xm := &infrastructurev1beta2.XOMachine{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: clusterv1.GroupVersion.String(),
						Kind:       "Machine",
						Name:       "owner",
						UID:        "machine-uid",
						Controller: ptr.To(true),
					},
				},
			},
		}
		result, err := ResolveBootstrapData(ctx, fakeClient, xm)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Requeue).To(BeTrue())
		Expect(result.Machine).NotTo(BeNil())
	})

	It("returns inline data when no Machine", func() {
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		xm := &infrastructurev1beta2.XOMachine{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec:       infrastructurev1beta2.XOMachineSpec{BootstrapData: "inline-data"},
		}
		result, err := ResolveBootstrapData(ctx, fakeClient, xm)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Requeue).To(BeFalse())
		Expect(string(result.Data)).To(Equal("inline-data"))
	})

	It("returns requeue when no Machine and no inline data", func() {
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		xm := &infrastructurev1beta2.XOMachine{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		}
		result, err := ResolveBootstrapData(ctx, fakeClient, xm)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Requeue).To(BeTrue())
	})
})

var _ = Describe("DetectBootstrapProvider", func() {
	It("returns explicit value when set", func() {
		spec := infrastructurev1beta2.XOMachineSpec{BootstrapProvider: "talos"}
		result := DetectBootstrapProvider(spec, nil)
		Expect(result).To(Equal("talos"))
	})

	It("returns kubeadm by default when empty", func() {
		spec := infrastructurev1beta2.XOMachineSpec{}
		result := DetectBootstrapProvider(spec, nil)
		Expect(result).To(Equal("kubeadm"))
	})

	It("detects talos from the owner Machine configRef", func() {
		spec := infrastructurev1beta2.XOMachineSpec{}
		machine := &clusterv1.Machine{
			Spec: clusterv1.MachineSpec{
				Bootstrap: clusterv1.Bootstrap{
					ConfigRef: clusterv1.ContractVersionedObjectReference{
						Kind: "TalosConfig",
						Name: "my-talos-config",
					},
				},
			},
		}
		result := DetectBootstrapProvider(spec, machine)
		Expect(result).To(Equal("talos"))
	})
})
