package vatesmachine

import (
	"context"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	xok8scommon "github.com/vatesfr/xenorchestra-k8s-common"

	infrastructurev1beta2 "git.vates.tech/patrice.ferlet/vates-capi/api/v1beta2"
	"git.vates.tech/patrice.ferlet/vates-capi/internal/kubevip"
)

// BuildCloudConfig retrieves SSH keys from the XO user profile and merges
// them into the bootstrap data. If no bootstrap data is provided, a minimal
// cloud-config with only SSH authorized keys is generated.
func BuildCloudConfig(ctx context.Context, xoClient *xok8scommon.XoClient, bootstrapData []byte) (string, error) {
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

// MaybeInjectKubeVIP injects kube-vip scripts into the cloud-init if this
// machine is a control plane node and kube-vip is enabled on the VatesCluster.
func MaybeInjectKubeVIP(ctx context.Context, c client.Client, vatesMachine *infrastructurev1beta2.VatesMachine, machine *clusterv1.Machine, cloudConfig string) (string, error) {
	logger := log.FromContext(ctx)

	if machine == nil {
		return cloudConfig, nil
	}

	_, isCP := machine.Labels[clusterv1.MachineControlPlaneLabel]
	if !isCP {
		return cloudConfig, nil
	}

	vatesCluster, err := getVatesClusterForMachine(ctx, c, vatesMachine)
	if err != nil {
		logger.Error(err, "Failed to get VatesCluster for kube-vip injection")
		return cloudConfig, nil
	}
	if vatesCluster == nil {
		return cloudConfig, nil
	}

	if vatesCluster.Spec.KubeVIP == nil || !vatesCluster.Spec.KubeVIP.Enabled {
		return cloudConfig, nil
	}

	if vatesCluster.Spec.ControlPlaneEndpoint == nil || vatesCluster.Spec.ControlPlaneEndpoint.Host == "" {
		logger.Info("Kube-vip enabled but no control plane endpoint set, skipping injection")
		return cloudConfig, nil
	}

	vip := vatesCluster.Spec.ControlPlaneEndpoint.Host
	logger.Info("Injecting kube-vip scripts into cloud-init", "vip", vip)

	result, err := kubevip.Inject(cloudConfig, kubevip.Config{VIP: vip})
	if err != nil {
		logger.Error(err, "Failed to inject kube-vip scripts into cloud-init")
		return cloudConfig, UpdateCondition(ctx, c, vatesMachine, metav1.ConditionFalse, "KubeVIPInjectionFailed", err.Error())
	}

	return result, nil
}

func getVatesClusterForMachine(ctx context.Context, c client.Client, vatesMachine *infrastructurev1beta2.VatesMachine) (*infrastructurev1beta2.VatesCluster, error) {
	machine, err := GetOwnerMachine(ctx, c, vatesMachine)
	if err != nil || machine == nil {
		return nil, err
	}

	clusterName := machine.Labels["cluster.x-k8s.io/cluster-name"]
	if clusterName == "" {
		return nil, nil
	}

	return GetVatesCluster(ctx, c, vatesMachine.Namespace, clusterName)
}

// GetVatesCluster retrieves a VatesCluster by namespace and cluster name.
func GetVatesCluster(ctx context.Context, c client.Client, namespace, clusterName string) (*infrastructurev1beta2.VatesCluster, error) {
	cluster := &clusterv1.Cluster{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: clusterName}, cluster); err != nil {
		return nil, err
	}

	if cluster.Spec.InfrastructureRef.Kind != "VatesCluster" {
		return nil, nil
	}

	vatesCluster := &infrastructurev1beta2.VatesCluster{}
	if err := c.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      cluster.Spec.InfrastructureRef.Name,
	}, vatesCluster); err != nil {
		return nil, err
	}

	return vatesCluster, nil
}

// GetOwnerMachine returns the CAPI Machine that references this VatesMachine.
func GetOwnerMachine(ctx context.Context, c client.Client, vatesMachine *infrastructurev1beta2.VatesMachine) (*clusterv1.Machine, error) {
	machineList := &clusterv1.MachineList{}
	if err := c.List(ctx, machineList, client.InNamespace(vatesMachine.Namespace)); err != nil {
		return nil, err
	}

	for i := range machineList.Items {
		m := &machineList.Items[i]
		if m.Spec.InfrastructureRef.Kind == "VatesMachine" && m.Spec.InfrastructureRef.Name == vatesMachine.Name {
			return m, nil
		}
	}

	return nil, nil
}

// GetBootstrapData reads the bootstrap data secret referenced by the Machine.
func GetBootstrapData(ctx context.Context, c client.Client, machine *clusterv1.Machine) ([]byte, error) {
	if machine.Spec.Bootstrap.DataSecretName == nil {
		return nil, fmt.Errorf("bootstrap data secret name is not set")
	}

	secret := &corev1.Secret{}
	secretName := types.NamespacedName{
		Namespace: machine.Namespace,
		Name:      *machine.Spec.Bootstrap.DataSecretName,
	}
	if err := c.Get(ctx, secretName, secret); err != nil {
		return nil, fmt.Errorf("failed to get bootstrap data secret: %w", err)
	}

	data, ok := secret.Data["value"]
	if !ok {
		return nil, fmt.Errorf("bootstrap data secret does not contain key 'value'")
	}

	return data, nil
}
