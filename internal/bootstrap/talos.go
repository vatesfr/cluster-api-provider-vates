package bootstrap

import "context"

// talosProviderName is the Talos bootstrap provider name.
const talosProviderName = "talos"

type talosProvider struct{}

func (talosProvider) Name() string {
	return talosProviderName
}

func (talosProvider) BuildCloudConfig(_ context.Context, deps Dependencies) (string, error) {
	return BuildTalosCloudConfig(deps.BootstrapData), nil
}

func (talosProvider) NetworkConfig(deps Dependencies) *string {
	guestConfig := ""
	if deps.XOMachine.Spec.NetworkConfig != nil {
		guestConfig = deps.XOMachine.Spec.NetworkConfig.GuestConfig
	}
	return BuildTalosNetworkConfig(guestConfig)
}

// BuildTalosCloudConfig returns the Talos machine config as-is. Unlike
// kubeadm, the bootstrap data is used verbatim: it is a Talos machine config
// YAML, not a cloud-init payload.
func BuildTalosCloudConfig(data []byte) string {
	return string(data)
}

// BuildTalosNetworkConfig returns the network-config to write on the Talos
// config drive. A "#" comment placeholder mirrors what the XO UI does and
// ensures the config drive is generated with the cidata label Talos requires.
// It returns nil when a user-provided guest config is already set.
func BuildTalosNetworkConfig(guestConfig string) *string {
	if guestConfig != "" {
		return &guestConfig
	}
	placeholder := "#"
	return &placeholder
}
