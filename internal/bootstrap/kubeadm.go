package bootstrap

import (
	"context"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	xok8scommon "github.com/vatesfr/xenorchestra-k8s-common"

	infrastructurev1beta2 "github.com/vatesfr/cluster-api-provider-vates/api/v1beta2"
	"github.com/vatesfr/cluster-api-provider-vates/internal/controller/xomachine"
	"github.com/vatesfr/cluster-api-provider-vates/internal/kubevip"
)

// KubeadmProviderName is the kubeadm bootstrap provider name (the default).
const KubeadmProviderName = "kubeadm"

type kubeadmProvider struct{}

func (kubeadmProvider) Name() string {
	return KubeadmProviderName
}

func (kubeadmProvider) BuildCloudConfig(ctx context.Context, deps Dependencies) (string, error) {
	injectSSHKeys := resolveInjectSSHKeys(ctx, deps.Client, deps.Machine, deps.XOMachine)
	cloudConfig, err := BuildKubeadmCloudConfig(ctx, deps.XOClient, deps.BootstrapData, injectSSHKeys)
	if err != nil {
		return "", err
	}

	cloudConfig, err = injectKubeVIPIfNeeded(ctx, deps.Client, cloudConfig, deps.Machine, deps.XOMachine)
	if err != nil {
		return "", err
	}
	return cloudConfig, nil
}

func (kubeadmProvider) NetworkConfig(deps Dependencies) *string {
	guestConfig := ""
	if deps.XOMachine.Spec.NetworkConfig != nil {
		guestConfig = deps.XOMachine.Spec.NetworkConfig.GuestConfig
	}
	return BuildKubeadmNetworkConfig(guestConfig)
}

// BuildKubeadmNetworkConfig returns the network-config for a kubeadm VM.
// kubeadm uses cloud-init, so the network config is the user-provided guest
// config (netplan) when present, otherwise nil (no config drive network file).
func BuildKubeadmNetworkConfig(guestConfig string) *string {
	if guestConfig != "" {
		return &guestConfig
	}
	return nil
}

// BuildKubeadmCloudConfig optionally injects SSH keys from the XO user profile
// into the bootstrap data. If injectSSHKeys is false, the bootstrap data
// is returned as-is. If no bootstrap data is provided and injection is
// enabled, a minimal cloud-config with only SSH authorized keys is generated.
func BuildKubeadmCloudConfig(ctx context.Context, xoClient *xok8scommon.XoClient, bootstrapData []byte, injectSSHKeys bool) (string, error) {
	if !injectSSHKeys {
		if len(bootstrapData) > 0 {
			return string(bootstrapData), nil
		}
		return "", nil
	}

	v1Client := xoClient.Client.V1Client()
	if v1Client == nil {
		return "", fmt.Errorf("v1 client not available")
	}

	currentUser, err := v1Client.GetCurrentUser()
	if err != nil {
		return "", fmt.Errorf("failed to get current XO user: %w", err)
	}

	var sshKeys []string
	for _, key := range currentUser.Preferences.SshKeys {
		sshKeys = append(sshKeys, key.Key)
	}

	if len(bootstrapData) > 0 {
		return MergeSSHKeysIntoCloudConfig(string(bootstrapData), sshKeys)
	}

	var cc strings.Builder
	cc.WriteString("#cloud-config\nssh_authorized_keys:\n")
	for _, key := range sshKeys {
		fmt.Fprintf(&cc, "  - %s\n", key)
	}
	return cc.String(), nil
}

// MergeSSHKeysIntoCloudConfig parses the given cloud-config YAML, appends
// any SSH keys not already present, and returns the updated YAML.
func MergeSSHKeysIntoCloudConfig(cloudConfig string, sshKeys []string) (string, error) {
	if len(sshKeys) == 0 {
		return cloudConfig, nil
	}

	var config map[string]any
	if err := yaml.Unmarshal([]byte(cloudConfig), &config); err != nil {
		return "", fmt.Errorf("failed to parse cloud-config: %w", err)
	}

	existing := make(map[string]struct{})
	if raw, ok := config["ssh_authorized_keys"]; ok {
		if list, ok := raw.([]any); ok {
			for _, item := range list {
				if s, ok := item.(string); ok {
					existing[s] = struct{}{}
				}
			}
		}
	}

	var merged []any
	if raw, ok := config["ssh_authorized_keys"]; ok {
		if list, ok := raw.([]any); ok {
			merged = list
		}
	}
	for _, key := range sshKeys {
		if _, found := existing[key]; !found {
			merged = append(merged, key)
			existing[key] = struct{}{}
		}
	}
	if config == nil {
		config = make(map[string]any)
	}
	config["ssh_authorized_keys"] = merged

	out, err := yaml.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("failed to marshal cloud-config: %w", err)
	}

	return "#cloud-config\n" + string(out), nil
}

// injectKubeVIPIfNeeded injects kube-vip scripts into the cloud-init for a
// control plane node when the XOCluster requests it.
func injectKubeVIPIfNeeded(ctx context.Context, c client.Client, cloudConfig string, machine *clusterv1.Machine, vatesMachine *infrastructurev1beta2.XOMachine) (string, error) {
	logger := log.FromContext(ctx)
	if machine == nil {
		return cloudConfig, nil
	}

	if _, ok := machine.Labels[clusterv1.MachineControlPlaneLabel]; !ok {
		return cloudConfig, nil
	}

	clusterName := machine.Labels["cluster.x-k8s.io/cluster-name"]
	if clusterName == "" {
		return cloudConfig, nil
	}

	vatesCluster, err := xomachine.GetXOCluster(ctx, c, vatesMachine.Namespace, clusterName)
	if err != nil {
		logger.Error(err, "Failed to get XOCluster for kube-vip injection")
		return cloudConfig, nil
	}

	if vatesCluster != nil && vatesCluster.Spec.ControlPlaneLB != nil && *vatesCluster.Spec.ControlPlaneLB == "kube-vip" {
		cloudConfig, err = InjectKubeVIP(ctx, c, vatesMachine, machine, vatesCluster, cloudConfig)
		if err != nil {
			return "", err
		}
	}
	return cloudConfig, nil
}

// resolveInjectSSHKeys reports whether the XOCluster for the machine requests
// SSH key injection.
func resolveInjectSSHKeys(ctx context.Context, c client.Client, machine *clusterv1.Machine, vatesMachine *infrastructurev1beta2.XOMachine) bool {
	if machine == nil {
		return false
	}
	clusterName := machine.Labels["cluster.x-k8s.io/cluster-name"]
	if clusterName == "" {
		return false
	}
	vatesCluster, err := xomachine.GetXOCluster(ctx, c, vatesMachine.Namespace, clusterName)
	if err != nil || vatesCluster == nil {
		return false
	}
	return vatesCluster.Spec.InjectSSHKeys
}

// InjectKubeVIP injects kube-vip scripts into the cloud-init for a control plane
// node. The caller is responsible for checking that kube-vip is enabled on the
// VatesCluster before calling this function.
func InjectKubeVIP(ctx context.Context, c client.Client, vatesMachine *infrastructurev1beta2.XOMachine, machine *clusterv1.Machine, vatesCluster *infrastructurev1beta2.XOCluster, cloudConfig string) (string, error) {
	logger := log.FromContext(ctx)

	if machine == nil {
		return cloudConfig, nil
	}

	_, isCP := machine.Labels[clusterv1.MachineControlPlaneLabel]
	if !isCP {
		return cloudConfig, nil
	}

	if vatesCluster.Spec.ControlPlaneEndpoint == nil || vatesCluster.Spec.ControlPlaneEndpoint.Host == "" {
		logger.Info("Kube-vip enabled but no control plane endpoint set, skipping injection")
		return cloudConfig, nil
	}

	endpoint := vatesCluster.Spec.ControlPlaneEndpoint
	logger.Info("Injecting kube-vip scripts into cloud-init", "vip", endpoint.Host, "subnet", endpoint.Subnet)

	result, err := kubevip.Inject(cloudConfig, kubevip.Config{VIP: endpoint.Host, Subnet: endpoint.Subnet})
	if err != nil {
		logger.Error(err, "Failed to inject kube-vip scripts into cloud-init")
		return cloudConfig, xomachine.UpdateCondition(ctx, c, vatesMachine, metav1.ConditionFalse, "KubeVIPInjectionFailed", err.Error())
	}

	return result, nil
}
