package xomachine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/vatesfr/xenorchestra-go-sdk/pkg/payloads"
	xok8scommon "github.com/vatesfr/xenorchestra-k8s-common"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// isUnmarshalError returns true when the old XO API returns a bare string
// instead of the expected JSON object. The operation likely succeeded.
func isUnmarshalError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "unmarshal")
}

// IsNotFoundError returns true when XO confirmed the object does not exist
// (HTTP 404). A distinct check is needed because the SDK surfaces transient
// network errors and "not found" as plain errors; only the latter must be
// treated as an already-done operation (e.g. allow the deletion finalizer to
// be dropped, or skip an already removed VBD).
func IsNotFoundError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "404")
}

// ExtractBareTaskPath extracts a task ID string from an unmarshal error
// returned by the V2 REST API when the old XO returns a bare string instead
// of a JSON object. Returns the task path (e.g. "/rest/v0/tasks/<uuid>").
func ExtractBareTaskPath(err error) (string, error) {
	errStr := err.Error()
	idx := strings.LastIndex(errStr, "%!(EXTRA string=")
	if idx < 0 {
		return "", fmt.Errorf("cannot extract task ID from error: %w", err)
	}
	bodyPart := errStr[idx+len("%!(EXTRA string="):]
	before, _, ok := strings.Cut(bodyPart, ")")
	if !ok {
		return "", fmt.Errorf("cannot parse task ID from error: %w", err)
	}
	taskPath := strings.Trim(before, `"`)
	if taskPath == "" {
		return "", fmt.Errorf("empty task ID in error: %w", err)
	}
	return taskPath, nil
}

// handleV2BareTaskResponse handles the case where the V2 REST API successfully
// created a VM but returned a bare string task ID instead of the expected
// JSON object. This happens on older XO versions (e.g. 0.22.0).
func handleV2BareTaskResponse(ctx context.Context, xoClient *xok8scommon.XoClient, createErr error) (*payloads.VM, error) {
	logger := log.FromContext(ctx)

	taskPath, err := ExtractBareTaskPath(createErr)
	if err != nil {
		return nil, err
	}

	logger.Info("V2 API created VM, waiting for bare task", "task", taskPath)

	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	task, err := xoClient.Client.Task().Wait(waitCtx, taskPath)
	if err != nil {
		return nil, fmt.Errorf("wait for VM creation task: %w", err)
	}
	if task.Status != payloads.Success {
		return nil, fmt.Errorf("VM creation task failed: %s", task.Result.Message)
	}

	vm, err := xoClient.Client.VM().GetByID(ctx, task.Result.ID)
	if err != nil {
		return nil, fmt.Errorf("fetch VM after creation: %w", err)
	}

	logger.Info("VM created via bare task", "name", vm.NameLabel, "id", vm.ID.String())
	return vm, nil
}
