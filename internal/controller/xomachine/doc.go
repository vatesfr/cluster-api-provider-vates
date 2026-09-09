// Package xomachine provides extracted helpers for the XOMachine controller.
//
// Functions are organized by concern:
//   - status.go: condition helpers (SetCondition, UpdateCondition, WithConditionUpdate)
//   - resolve.go: XO object ID lookups (template, pool, network)
//   - vm_ops.go: VM lifecycle operations (CreateVM, StartVM, WaitForVMReady, etc.)
//   - client.go: GetOrCreateXOClient
//   - k8s_helpers.go: owner object lookups (GetXOCluster, GetOwnerMachine)
//   - xo_compat.go: Xen Orchestra API compatibility helpers
//
// All functions receive a context, a client.Client, and/or an *xok8scommon.XoClient
// as appropriate, and return errors (no direct reconciliation side effects).
//
// Bootstrap data handling (cloud-init, SSH keys, kube-vip) lives in the
// internal/bootstrap package.
package xomachine
