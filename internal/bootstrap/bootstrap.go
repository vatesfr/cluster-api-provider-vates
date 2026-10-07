// Package bootstrap turns the bootstrap data secret produced by a CAPI
// bootstrap provider (CABPK for kubeadm, CABPT for Talos, ...) into the
// user-data / network-config payload that Xen Orchestra injects into the VM.
//
// It is deliberately provider-agnostic: it never identifies the bootstrap
// provider. What to do with the payload is declared on the XOMachine (see
// Behavior), so adding a bootstrap provider needs no change here.
package bootstrap

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	xok8scommon "github.com/vatesfr/xenorchestra-k8s-common"

	infrastructurev1beta2 "github.com/vatesfr/cluster-api-provider-vates/api/v1beta2"
)

// Dependencies carries everything needed to turn the bootstrap data secret into
// the VM user-data / network-config payload.
type Dependencies struct {
	// Client is the controller-runtime client used to read cluster objects
	// (XOCluster, owner Machine, ...).
	Client client.Client
	// XOClient is the Xen Orchestra client (used to read the XO user profile
	// for SSH key injection on cloud-init).
	XOClient *xok8scommon.XoClient
	// Machine is the owner CAPI Machine.
	Machine *clusterv1.Machine
	// XOMachine is the infrastructure machine being reconciled.
	XOMachine *infrastructurev1beta2.XOMachine
	// BootstrapData is the raw bootstrap data secret.
	BootstrapData []byte
}

// Behavior declares what the infrastructure provider must do with the bootstrap
// payload. It describes what to do, never who produced the payload: there is no
// provider name and no per-provider code path. The zero value is a full
// pass-through.
type Behavior struct {
	// CloudInit reports that the payload is a cloud-init document, which may be
	// enriched (the XO user's SSH keys are merged in).
	CloudInit bool
	// KubeVIP reports that kube-vip may be injected into the payload. It only
	// applies to a kubeadm-style cloud-init control plane, so it is ignored
	// unless CloudInit is set.
	KubeVIP bool
}

// Result is the pair of payloads handed to Xen Orchestra when creating the VM.
type Result struct {
	// CloudConfig is the user-data payload.
	CloudConfig string
	// NetworkConfig is the config-drive network payload, or nil when none is
	// required.
	NetworkConfig *string
}

// Build turns the bootstrap data into the payloads injected into the VM,
// following the declared Behavior. An empty Behavior is a full pass-through.
//
// It is the single entry point for turning bootstrap data into VM payloads:
// there is no provider registry, no name-keyed lookup and no type per provider.
func Build(ctx context.Context, deps Dependencies, behavior Behavior) (Result, error) {
	cloudConfig := string(deps.BootstrapData)

	if behavior.CloudInit {
		injectSSHKeys := resolveInjectSSHKeys(ctx, deps.Client, deps.Machine, deps.XOMachine)
		var err error
		cloudConfig, err = BuildCloudInitWithSSHKeys(ctx, deps.XOClient, []byte(cloudConfig), injectSSHKeys)
		if err != nil {
			return Result{}, err
		}
		if behavior.KubeVIP {
			cloudConfig, err = injectKubeVIPIfNeeded(ctx, deps.Client, cloudConfig, deps.Machine, deps.XOMachine)
			if err != nil {
				return Result{}, err
			}
		}
	}

	return Result{CloudConfig: cloudConfig, NetworkConfig: networkConfig(deps)}, nil
}

// networkConfig returns the config-drive network payload: the guest config the
// user set on the XOMachine, or nil when none is required. Xen Orchestra
// creates the config drive from CloudConfig alone, so nothing else is written.
func networkConfig(deps Dependencies) *string {
	guestConfig := ""
	if deps.XOMachine.Spec.NetworkConfig != nil {
		guestConfig = deps.XOMachine.Spec.NetworkConfig.GuestConfig
	}
	if guestConfig != "" {
		return &guestConfig
	}
	return nil
}
