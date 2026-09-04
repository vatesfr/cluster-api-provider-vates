package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	xok8scommon "github.com/vatesfr/xenorchestra-k8s-common"
)

// trueStr is the string representation of a boolean true used when parsing the
// identity secret and rendering the CCM manifest.
const trueStr = "true"

// ResolveXOConfig reads the Secret referenced by identityRef and returns an
// XoConfig. If identityRef is nil, returns fallback.
func ResolveXOConfig(ctx context.Context, c client.Client, namespace string, identityRef *corev1.LocalObjectReference, fallback *xok8scommon.XoConfig) (*xok8scommon.XoConfig, error) {
	if identityRef == nil {
		return fallback, nil
	}

	secret := &corev1.Secret{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: identityRef.Name}, secret); err != nil {
		return nil, fmt.Errorf("get identity secret %s/%s: %w", namespace, identityRef.Name, err)
	}

	cfg := &xok8scommon.XoConfig{}

	if v := secret.Data["url"]; len(v) > 0 {
		cfg.URL = string(v)
	} else {
		return nil, fmt.Errorf("identity secret %s/%s missing 'url' key", namespace, identityRef.Name)
	}

	if v := secret.Data["token"]; len(v) > 0 {
		cfg.Token = string(v)
	} else {
		return nil, fmt.Errorf("identity secret %s/%s missing 'token' key", namespace, identityRef.Name)
	}

	if v := secret.Data["insecure"]; len(v) > 0 {
		cfg.Insecure = string(v) == trueStr
	}

	return cfg, nil
}

// NewXOClientFromConfig creates an XO client from the given config and checks
// connectivity. Returns nil, nil when config is nil (caller should skip XO ops).
func NewXOClientFromConfig(ctx context.Context, cfg *xok8scommon.XoConfig) (*xok8scommon.XoClient, error) {
	if cfg == nil {
		return nil, nil
	}

	xoClient, err := xok8scommon.NewXOClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("create XO client: %w", err)
	}

	if err := xoClient.CheckClient(ctx); err != nil {
		return nil, fmt.Errorf("connect to XO: %w", err)
	}

	return xoClient, nil
}
