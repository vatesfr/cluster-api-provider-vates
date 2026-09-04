package xomachine

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	infrastructurev1beta2 "github.com/vatesfr/cluster-api-provider-vates/api/v1beta2"
)

var _ = Describe("vmTags", func() {
	It("tags a control plane VM with cluster, machine and role", func() {
		vm := &infrastructurev1beta2.XOMachine{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "demo3-cp-7n62g",
				Labels: map[string]string{"cluster.x-k8s.io/cluster-name": "demo3", "cluster.x-k8s.io/control-plane": ""},
			},
		}
		Expect(vmTags(vm)).To(ConsistOf(
			"cluster-name:demo3",
			"machine:demo3-cp-7n62g",
			"role:control-plane",
		))
	})

	It("tags a worker VM with role worker", func() {
		vm := &infrastructurev1beta2.XOMachine{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "demo3-md-0-hdvzv-48gx5",
				Labels: map[string]string{"cluster.x-k8s.io/cluster-name": "demo3"},
			},
		}
		Expect(vmTags(vm)).To(ConsistOf(
			"cluster-name:demo3",
			"machine:demo3-md-0-hdvzv-48gx5",
			"role:worker",
		))
	})
})
