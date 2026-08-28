// Package bootstrap isolates the bootstrap-provider logic (kubeadm vs Talos)
// from the infrastructure-provider logic (VM lifecycle in Xen Orchestra).
//
// In CAPI terms:
//   - the infrastructure provider manages XOMachine/XOCluster and VMs;
//   - the bootstrap provider (CABPK for kubeadm, CABPT for Talos) produces the
//     bootstrap data secret. This package turns that secret into the
//     user-data / network-config payload that Xen Orchestra injects into the
//     VM config drive.
package bootstrap

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	xok8scommon "github.com/vatesfr/xenorchestra-k8s-common"

	infrastructurev1beta2 "github.com/vatesfr/cluster-api-provider-vates/api/v1beta2"
)

// Dependencies carries everything a bootstrap Provider needs to turn the
// bootstrap data secret into the VM user-data / network-config payload.
type Dependencies struct {
	// Client is the controller-runtime client used to read cluster objects
	// (XOCluster, owner Machine, ...).
	Client client.Client
	// XOClient is the Xen Orchestra client (used to read the XO user profile
	// for SSH key injection on kubeadm).
	XOClient *xok8scommon.XoClient
	// Machine is the owner CAPI Machine.
	Machine *clusterv1.Machine
	// XOMachine is the infrastructure machine being reconciled.
	XOMachine *infrastructurev1beta2.XOMachine
	// BootstrapData is the raw bootstrap data secret (cloud-init for kubeadm,
	// Talos machine config for Talos).
	BootstrapData []byte
}

// Provider is implemented by each bootstrap provider (kubeadm, talos). It
// turns the bootstrap data into the payloads injected into the VM.
type Provider interface {
	// Name returns the provider name ("kubeadm" or "talos").
	Name() string
	// BuildCloudConfig returns the user-data payload to inject into the VM
	// (cloud-init for kubeadm, verbatim Talos machine config for talos).
	BuildCloudConfig(ctx context.Context, deps Dependencies) (string, error)
	// NetworkConfig returns the network-config payload for the config drive,
	// or nil when none is required.
	NetworkConfig(deps Dependencies) *string
}

// GetProvider returns the Provider implementation for the given bootstrap
// provider name. Unknown names fall back to kubeadm (the default).
func GetProvider(name string) Provider {
	switch name {
	case talosProviderName:
		return talosProvider{}
	default:
		return kubeadmProvider{}
	}
}
