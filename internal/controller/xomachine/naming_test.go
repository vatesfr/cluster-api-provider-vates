package xomachine

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/vatesfr/xenorchestra-go-sdk/pkg/payloads"

	infrastructurev1beta2 "github.com/vatesfr/cluster-api-provider-vates/api/v1beta2"
)

var _ = Describe("MachineID", func() {
	It("returns the random suffix of a control plane Machine name", func() {
		Expect(MachineID("my-cluster-x8kz2-7n62g")).To(Equal("7n62g"))
	})

	It("returns the random suffix of a MachineDeployment Machine name", func() {
		Expect(MachineID("my-cluster-md-0-hdvzv-5bxt9-48gx5")).To(Equal("48gx5"))
	})

	It("returns the random suffix of a name Cluster API cut to 63 characters", func() {
		// Over 63 characters, Cluster API keeps 58 and appends 5 random
		// characters WITHOUT a dash: the last dash segment is then longer
		// than the suffix, the last 5 characters are still the suffix.
		name := strings.Repeat("a", 51) + "-md-0-h" + "k2pq7"
		Expect(name).To(HaveLen(63))
		Expect(MachineID(name)).To(Equal("k2pq7"))
	})

	It("uses the whole name when it does not end with a random suffix", func() {
		// A control plane provider may name its Machines <cluster>-<index>:
		// nothing in the Cluster API contract constrains the Machine name.
		Expect(MachineID("vmn-0")).To(Equal("vmn-0"))
		Expect(MachineID("my-cluster-cp-1")).To(Equal("my-cluster-cp-1"))
		Expect(MachineID("cp1")).To(Equal("cp1"))
	})

	It("uses the whole name when the last characters are not from the random alphabet", func() {
		// "node1" holds vowels and a 1: not a generated suffix, and another
		// Machine (b-node1) may end with it too.
		Expect(MachineID("a-node1")).To(Equal("a-node1"))
		Expect(MachineID("my-cluster-CP-ABCDE")).To(Equal("my-cluster-CP-ABCDE"))
	})

	It("uses the whole name when the suffix is neither after a dash nor in a cut name", func() {
		Expect(MachineID("controlplanekwqns")).To(Equal("controlplanekwqns"))
	})
})

var _ = Describe("VMNameFromNaming", func() {
	naming := &infrastructurev1beta2.VMNamingSpec{
		ControlPlanePrefix: "CAPI-CP-",
		WorkerPrefix:       "CAPI-WORKER-",
	}

	It("names a control plane VM from the control plane prefix", func() {
		Expect(VMNameFromNaming(naming, true, "my-cluster-x8kz2-7n62g")).
			To(Equal("CAPI-CP-7n62g"))
	})

	It("names a worker VM from the worker prefix", func() {
		Expect(VMNameFromNaming(naming, false, "my-cluster-md-0-hdvzv-5bxt9-48gx5")).
			To(Equal("CAPI-WORKER-48gx5"))
	})

	It("takes the prefix verbatim, whatever the convention", func() {
		other := &infrastructurev1beta2.VMNamingSpec{ControlPlanePrefix: "cp.paris_", WorkerPrefix: "w "}
		Expect(VMNameFromNaming(other, true, "c-7n62g")).To(Equal("cp.paris_7n62g"))
		Expect(VMNameFromNaming(other, false, "c-7n62g")).To(Equal("w 7n62g"))
	})

	It("uses the whole Machine name when it has no random suffix", func() {
		Expect(VMNameFromNaming(naming, true, "vmn-0")).To(Equal("CAPI-CP-vmn-0"))
	})
})

var _ = Describe("nameLabelFilter", func() {
	It("quotes the name", func() {
		Expect(nameLabelFilter("CAPI-CP-7n62g")).
			To(Equal(`name_label:"CAPI-CP-7n62g"`))
	})

	It("escapes quotes and backslashes", func() {
		Expect(nameLabelFilter(`a "b" \c`)).To(Equal(`name_label:"a \"b\" \\c"`))
	})
})

var _ = Describe("foreignVMCount", func() {
	const marker = "vates-capi-machine:uid-1"

	It("counts the VMs with exactly the name that are not ours", func() {
		vms := []*payloads.VM{
			{NameLabel: "vm-7n62g", NameDescription: "another machine"},
			{NameLabel: "vm-7n62g"},
			{NameLabel: "vm-7n62g", NameDescription: marker},
			{NameLabel: "vm-7n62g-old"},
			nil,
		}
		Expect(foreignVMCount(vms, "vm-7n62g", marker)).To(Equal(2))
	})

	It("counts every VM with the name without a marker", func() {
		vms := []*payloads.VM{{NameLabel: "vm-7n62g", NameDescription: marker}}
		Expect(foreignVMCount(vms, "vm-7n62g", "")).To(Equal(1))
	})
})

var _ = Describe("DefaultVMName", func() {
	It("derives the distribution from the bootstrap configuration kind", func() {
		Expect(DefaultVMName("TalosConfig", "my-cluster", true, "my-cluster-cp-4hgx7")).
			To(Equal("TALOS-my-cluster-CP-4hgx7"))
		Expect(DefaultVMName("KubeadmConfig", "my-cluster", false, "my-cluster-md-0-hdvzv-5bxt9-48gx5")).
			To(Equal("KUBEADM-my-cluster-WORKER-48gx5"))
		Expect(DefaultVMName("VatesConfig", "my-cluster", true, "my-cluster-x8kz2-7n62g")).
			To(Equal("VATES-my-cluster-CP-7n62g"))
	})

	It("names any bootstrap provider without a list of known ones", func() {
		Expect(DefaultVMName("RKE2Config", "my-cluster", true, "my-cluster-x8kz2-7n62g")).
			To(Equal("RKE2-my-cluster-CP-7n62g"))
		Expect(DefaultVMName("CustomBootstrap", "my-cluster", false, "my-cluster-x8kz2-7n62g")).
			To(Equal("CUSTOMBOOTSTRAP-my-cluster-WORKER-7n62g"))
	})

	It("falls back to CAPI without a bootstrap configuration", func() {
		Expect(DefaultVMName("", "my-cluster", false, "my-cluster-x8kz2-7n62g")).
			To(Equal("CAPI-my-cluster-WORKER-7n62g"))
	})

	It("uses the whole Machine name when it has no random suffix", func() {
		Expect(DefaultVMName("VatesConfig", "demo", true, "vmn-0")).To(Equal("VATES-demo-CP-vmn-0"))
	})
})
