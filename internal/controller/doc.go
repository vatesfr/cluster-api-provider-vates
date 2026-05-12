//go:generate go run go.uber.org/mock/mockgen@v0.6.0 -destination=mock_v1client.go -package=controller -self_package=git.vates.tech/patrice.ferlet/vates-capi/internal/controller github.com/vatesfr/xenorchestra-go-sdk/client XOClient
//go:generate go run go.uber.org/mock/mockgen@v0.6.0 -destination=mock_task.go -package=controller -self_package=git.vates.tech/patrice.ferlet/vates-capi/internal/controller github.com/vatesfr/xenorchestra-go-sdk/pkg/services/library Task,TaskAction

// Package controller implements the reconciliation logic for infrastructure
// resources managed by the Vates CAPI provider.
//
// It contains the VatesMachine controller which manages the lifecycle of
// XenOrchestra VMs, including creation, deletion, VIF management, and
// cloud-config injection.
package controller
