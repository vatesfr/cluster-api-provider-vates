// Package vatesmachine provides extracted helpers for the VatesMachine controller.
//
// Functions are organized by concern:
//   - status.go: condition helpers (SetCondition, UpdateCondition)
//   - cloud_config.go: cloud-init, SSH keys, kube-vip, bootstrap data resolution
//   - resolve.go: XO object ID lookups (template, pool, network)
//   - vm_ops.go: VM lifecycle operations (CreateVM, StartVM, WaitForVMReady, etc.)
//   - client.go: GetOrCreateXOClient
//
// All functions receive a context, a client.Client, and/or an *xok8scommon.XoClient
// as appropriate, and return errors (no direct reconciliation side effects).
package vatesmachine
