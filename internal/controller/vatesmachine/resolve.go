package vatesmachine

import (
	"context"
	"fmt"

	"github.com/gofrs/uuid"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	xoclient "github.com/vatesfr/xenorchestra-go-sdk/client"
	xok8scommon "github.com/vatesfr/xenorchestra-k8s-common"

	infrastructurev1beta2 "git.vates.tech/patrice.ferlet/vates-capi/api/v1beta2"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

// ResolveBootstrapDataResult holds the result of resolving bootstrap data.
type ResolveBootstrapDataResult struct {
	Machine *clusterv1.Machine
	Data    []byte
	Requeue bool
}

// ResolveBootstrapData gets the owner Machine and the associated bootstrap data.
// Returns:
//   - (result with machine and data) when data is available
//   - (result with Requeue=true) when bootstrap is not yet ready
//   - error on failure
func ResolveBootstrapData(ctx context.Context, c client.Client, vatesMachine *infrastructurev1beta2.VatesMachine) (ResolveBootstrapDataResult, error) {
	logger := log.FromContext(ctx)

	machine, err := GetOwnerMachine(ctx, c, vatesMachine)
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
		return ResolveBootstrapDataResult{Machine: machine, Data: data}, nil
	}

	if vatesMachine.Spec.BootstrapData != "" {
		logger.Info("Using inline bootstrap data from spec")
		return ResolveBootstrapDataResult{Data: []byte(vatesMachine.Spec.BootstrapData)}, nil
	}

	logger.Info("No owner Machine yet and no inline bootstrap data, requeuing")
	return ResolveBootstrapDataResult{Requeue: true}, nil
}

// ResolveTemplateID resolves a template UUID from either a direct UUID string
// or a name label lookup via the V1 client.
func ResolveTemplateID(ctx context.Context, xoClient *xok8scommon.XoClient, templateIDStr string, templateName string) (uuid.UUID, error) {
	if templateIDStr != "" {
		parsedUUID, err := uuid.FromString(templateIDStr)
		if err != nil {
			return uuid.Nil, fmt.Errorf("invalid template ID %q: %w", templateIDStr, err)
		}
		return parsedUUID, nil
	}
	if templateName != "" {
		v1 := xoClient.Client.V1Client()
		templates, err := v1.GetTemplate(xoclient.Template{NameLabel: templateName})
		if err != nil || len(templates) == 0 {
			if err == nil {
				err = fmt.Errorf("template %q not found", templateName)
			}
			return uuid.Nil, fmt.Errorf("template lookup: %w", err)
		}
		parsedUUID, err := uuid.FromString(templates[0].Id)
		if err != nil {
			return uuid.Nil, fmt.Errorf("invalid template UUID from lookup: %w", err)
		}
		return parsedUUID, nil
	}
	return uuid.Nil, fmt.Errorf("template must specify either templateID or templateName")
}

// ResolvePoolID resolves a pool UUID from either a direct UUID string or a
// name lookup via the V1 client.
func ResolvePoolID(ctx context.Context, xoClient *xok8scommon.XoClient, poolIDStr string, poolName string) (uuid.UUID, error) {
	if poolIDStr != "" {
		u, err := uuid.FromString(poolIDStr)
		if err != nil {
			return uuid.Nil, fmt.Errorf("invalid pool ID %q: %w", poolIDStr, err)
		}
		return u, nil
	}
	if poolName != "" {
		v1 := xoClient.Client.V1Client()
		pools, err := v1.GetPoolByName(poolName)
		if err != nil || len(pools) == 0 {
			if err == nil {
				err = fmt.Errorf("pool %q not found", poolName)
			}
			return uuid.Nil, fmt.Errorf("pool lookup: %w", err)
		}
		u, err := uuid.FromString(pools[0].Id)
		if err != nil {
			return uuid.Nil, fmt.Errorf("invalid pool UUID from lookup: %w", err)
		}
		return u, nil
	}
	return uuid.Nil, nil
}

// ResolveNetworkID resolves a network identifier from either a direct ID or a
// name lookup via the V1 client.
func ResolveNetworkID(v1Client xoclient.XOClient, netConfig infrastructurev1beta2.Network, poolID uuid.UUID) (string, error) {
	if netConfig.NetworkID != "" {
		return netConfig.NetworkID, nil
	}
	if netConfig.Name != "" {
		netReq := xoclient.Network{
			NameLabel: netConfig.Name,
		}
		if !poolID.IsNil() {
			netReq.PoolId = poolID.String()
		}
		network, err := v1Client.GetNetwork(netReq)
		if err != nil {
			return "", fmt.Errorf("network %q not found: %w", netConfig.Name, err)
		}
		return network.Id, nil
	}
	return "", fmt.Errorf("network must specify either networkID or name")
}
