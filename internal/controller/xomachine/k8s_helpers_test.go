package xomachine

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	infrastructurev1beta2 "github.com/vatesfr/cluster-api-provider-vates/api/v1beta2"
)

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

	It("returns the Machine referencing this XOMachine", func() {
		machine := &clusterv1.Machine{
			ObjectMeta: metav1.ObjectMeta{Name: "my-machine", Namespace: "default", UID: "machine-uid"},
			Spec: clusterv1.MachineSpec{
				InfrastructureRef: clusterv1.ContractVersionedObjectReference{
					Kind: "XOMachine",
					Name: "my-xo-machine",
				},
			},
		}
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
			machine,
		).Build()

		xm := &infrastructurev1beta2.XOMachine{
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
		xm := &infrastructurev1beta2.XOMachine{
			ObjectMeta: metav1.ObjectMeta{Name: "no-match", Namespace: "default"},
		}
		machine, err := GetOwnerMachine(ctx, fakeClient, xm)
		Expect(err).NotTo(HaveOccurred())
		Expect(machine).To(BeNil())
	})
})
