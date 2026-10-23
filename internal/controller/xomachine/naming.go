package xomachine

import (
	"fmt"
	"strings"

	"github.com/vatesfr/xenorchestra-go-sdk/pkg/payloads"

	infrastructurev1beta2 "github.com/vatesfr/cluster-api-provider-vates/api/v1beta2"
)

// machineSuffixLength is the length of the random suffix Cluster API puts at
// the end of the Machine names it generates.
const machineSuffixLength = 5

// maxMachineNameLength is the length Cluster API cuts a generated Machine name
// to, before appending the random suffix without a dash.
const maxMachineNameLength = 63

// randomAlphabet is the alphabet of the random strings Kubernetes and Cluster
// API generate (k8s.io/apimachinery/pkg/util/rand): no vowels, no 0, 1 or 3.
const randomAlphabet = "bcdfghjklmnpqrstvwxz2456789"

// MachineID returns the part of a Machine name that identifies its VM: the
// random suffix when the name ends with one, the whole name otherwise.
//
// Cluster API's controllers (KubeadmControlPlane, MachineSet) and the Talos
// control plane provider end the names they generate with 5 random characters:
// after a dash, or appended WITHOUT a dash to a name cut to 63 characters. The
// suffix is recognized only in those two shapes, and only when its characters
// all come from the random alphabet, so that a name such as "demo-node1" is
// not cut to a part another Machine may share.
//
// Nothing in the Cluster API contract constrains the Machine name, though: a
// control plane provider may name its Machines "<cluster>-<index>". Such a name
// is used whole. A Machine name never changes, so the ID is stable across
// reconciles, and two Machines of a namespace never share it.
func MachineID(machineName string) string {
	n := len(machineName)
	if n < machineSuffixLength {
		return machineName
	}
	suffix := machineName[n-machineSuffixLength:]
	for _, r := range suffix {
		if !strings.ContainsRune(randomAlphabet, r) {
			return machineName
		}
	}
	switch {
	case n == machineSuffixLength, n == maxMachineNameLength:
		return suffix
	case machineName[n-machineSuffixLength-1] == '-':
		return suffix
	default:
		return machineName
	}
}

// VMNameFromNaming returns the VM name set by the XOCluster vmNaming: the
// prefix of the machine's role followed by the Machine ID (see MachineID). The prefix is
// taken verbatim: no naming convention is imposed.
func VMNameFromNaming(naming *infrastructurev1beta2.VMNamingSpec, controlPlane bool, machineName string) (string, error) {
	prefix := naming.WorkerPrefix
	if controlPlane {
		prefix = naming.ControlPlanePrefix
	}
	if prefix == "" {
		return "", fmt.Errorf("vmNaming sets no prefix for this machine's role")
	}
	return prefix + MachineID(machineName), nil
}

// defaultDistribution names the distribution of a Machine without a bootstrap
// configuration, e.g. one that only sets a bootstrap data secret.
const defaultDistribution = "CAPI"

// distribution derives the distribution part of the default VM name from the
// kind of the Machine's bootstrap configuration: the kind without its "Config"
// suffix, upper-cased, e.g. "TalosConfig" -> "TALOS", "KubeadmConfig" ->
// "KUBEADM". A kind without the suffix is used whole. There is no list of
// known bootstrap providers: a new one is named without any change here.
func distribution(bootstrapKind string) string {
	name := strings.TrimSuffix(bootstrapKind, "Config")
	if name == "" {
		name = bootstrapKind
	}
	if name == "" {
		return defaultDistribution
	}
	return strings.ToUpper(name)
}

// DefaultVMName returns the VM name used when the XOCluster sets no vmNaming:
// <DISTRIBUTION>-<cluster>-<ROLE>-<ID>, e.g. "TALOS-my-cluster-CP-7n62g", where
// ID is the Machine ID (see MachineID).
// The distribution is derived from the kind of the Machine's bootstrap
// configuration (see distribution). It only names the VM: the bootstrap
// payload is handled the same whatever the kind.
func DefaultVMName(bootstrapKind, clusterName string, controlPlane bool, machineName string) string {
	role := "WORKER"
	if controlPlane {
		role = "CP"
	}
	return distribution(bootstrapKind) + "-" + clusterName + "-" + role + "-" + MachineID(machineName)
}

// nameLabelFilter returns the Xen Orchestra filter matching the VMs whose
// name contains vmName. The value is quoted, so a name holding a space or a
// character of the filter syntax (":", "|", "(", "!", ...) is searched as a
// plain string. Xen Orchestra matches the string anywhere in the name: callers
// compare the name exactly.
func nameLabelFilter(vmName string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(vmName)
	return `name_label:"` + escaped + `"`
}

// foreignVMCount returns how many of vms carry exactly vmName without being
// this machine's own VM, recognized by its marker. Xen Orchestra does not
// enforce unique names, so such a VM does not prevent the creation: the caller
// only warns about it.
func foreignVMCount(vms []*payloads.VM, vmName, marker string) int {
	count := 0
	for _, vm := range vms {
		if vm == nil || vm.NameLabel != vmName {
			continue
		}
		if marker != "" && vm.NameDescription == marker {
			continue
		}
		count++
	}
	return count
}
