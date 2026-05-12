package vatesmachine

import (
	"context"
	"fmt"

	xok8scommon "github.com/vatesfr/xenorchestra-k8s-common"
)

// GetOrCreateXOClient returns the cached XO client if available, or creates
// a new one from the given credentials. Returns nil, nil when credentials
// are not configured (caller should skip XO operations).
func GetOrCreateXOClient(ctx context.Context, xoCreds *xok8scommon.XoConfig, currentXoClient *xok8scommon.XoClient) (*xok8scommon.XoClient, error) {
	if currentXoClient != nil {
		return currentXoClient, nil
	}
	if xoCreds == nil {
		return nil, nil
	}

	xoClient, err := xok8scommon.NewXOClient(xoCreds)
	if err != nil {
		return nil, fmt.Errorf("create XO client: %w", err)
	}

	if err := xoClient.CheckClient(ctx); err != nil {
		return nil, fmt.Errorf("connect to XO: %w", err)
	}

	return xoClient, nil
}
