package bootstrap

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"
)

var _ = Describe("Talos bootstrap helpers", func() {
	Describe("BuildTalosCloudConfig", func() {
		It("returns the bootstrap data verbatim", func() {
			data := []byte("version: v1alpha1\nmachine:\n  type: controlplane\n")
			Expect(BuildTalosCloudConfig(data)).To(Equal(string(data)))
		})
	})

	Describe("BuildTalosNetworkConfig", func() {
		It("returns the guest config when set", func() {
			Expect(BuildTalosNetworkConfig("network: {version: 2}")).To(Equal(ptr.To("network: {version: 2}")))
		})

		It("returns a '#' placeholder when no guest config is set", func() {
			Expect(BuildTalosNetworkConfig("")).To(Equal(ptr.To("#")))
		})
	})
})
