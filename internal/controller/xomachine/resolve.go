package xomachine

import (
	"context"
	"fmt"

	"github.com/gofrs/uuid"
	xoclient "github.com/vatesfr/xenorchestra-go-sdk/client"
	xok8scommon "github.com/vatesfr/xenorchestra-k8s-common"

	infrastructurev1beta2 "github.com/vatesfr/cluster-api-provider-vates/api/v1beta2"
)

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
		if v1 == nil {
			return uuid.Nil, fmt.Errorf("V1 client not available for template lookup")
		}
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
		if v1 == nil {
			return uuid.Nil, fmt.Errorf("V1 client not available for pool lookup")
		}
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
		if v1Client == nil {
			return "", fmt.Errorf("V1 client not available for network lookup")
		}
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
