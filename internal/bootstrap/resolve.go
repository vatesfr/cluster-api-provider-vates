package bootstrap

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	infrastructurev1beta2 "github.com/vatesfr/cluster-api-provider-vates/api/v1beta2"
	"github.com/vatesfr/cluster-api-provider-vates/internal/controller/xomachine"
)

// ResolveBootstrapDataResult holds the result of resolving bootstrap data.
type ResolveBootstrapDataResult struct {
	Machine           *clusterv1.Machine
	Data              []byte
	Requeue           bool
	BootstrapProvider string
}

// ResolveBootstrapData gets the owner Machine and the associated bootstrap data.
// Returns:
//   - (result with machine and data) when data is available
//   - (result with Requeue=true) when bootstrap is not yet ready
//   - error on failure
func ResolveBootstrapData(ctx context.Context, c client.Client, xoMachine *infrastructurev1beta2.XOMachine) (ResolveBootstrapDataResult, error) {
	logger := log.FromContext(ctx)

	machine, err := xomachine.GetOwnerMachine(ctx, c, xoMachine)
	if err != nil {
		logger.Error(err, "Failed to get owner Machine")
		return ResolveBootstrapDataResult{}, err
	}

	if machine != nil {
		if machine.Spec.Bootstrap.DataSecretName == nil {
			logger.Info("Waiting for bootstrap data secret to be generated")
			return ResolveBootstrapDataResult{Machine: machine, Requeue: true}, nil
		}
		data, err := GetBootstrapData(ctx, c, machine)
		if err != nil {
			logger.Error(err, "Failed to get bootstrap data")
			return ResolveBootstrapDataResult{}, err
		}
		return ResolveBootstrapDataResult{
			Machine:           machine,
			Data:              data,
			BootstrapProvider: DetectBootstrapProvider(xoMachine.Spec, machine),
		}, nil
	}

	if xoMachine.Spec.BootstrapData != "" {
		logger.Info("Using inline bootstrap data from spec")
		return ResolveBootstrapDataResult{
			Data:              []byte(xoMachine.Spec.BootstrapData),
			BootstrapProvider: DetectBootstrapProvider(xoMachine.Spec, nil),
		}, nil
	}

	logger.Info("No owner Machine yet and no inline bootstrap data, requeuing")
	return ResolveBootstrapDataResult{
		Requeue:           true,
		BootstrapProvider: DetectBootstrapProvider(xoMachine.Spec, nil),
	}, nil
}

// DetectBootstrapProvider returns the effective bootstrap provider for a machine.
// Priority:
// 1. Explicit spec.BootstrapProvider
// 2. Auto-detect from the owner Machine's bootstrap configRef (TalosConfig/TalosConfigTemplate = talos)
// 3. Default "kubeadm"
func DetectBootstrapProvider(spec infrastructurev1beta2.XOMachineSpec, machine *clusterv1.Machine) string {
	if spec.BootstrapProvider != "" {
		return spec.BootstrapProvider
	}
	if machine != nil && machine.Spec.Bootstrap.ConfigRef.Name != "" {
		kind := machine.Spec.Bootstrap.ConfigRef.Kind
		if kind == "TalosConfig" || kind == "TalosConfigTemplate" {
			return talosProviderName
		}
	}
	return KubeadmProviderName
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
