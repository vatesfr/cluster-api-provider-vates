# Architecture

A map of the codebase: where to look for what, and how a reconciliation actually
flows through the packages.

This page assumes the object graph from
[How they fit together](../concept/how-they-link.md): providers reference each
other through objects and reconcile what they watch. Everything below is the
concrete shape of that idea in this repository.

## Repository layout

```
.
├── api/v1beta2/          # CRD types + kubebuilder markers (zz_generated.*)
├── cmd/                  # manager entry point: flags, schemes, reconcilers
├── internal/
│   ├── bootstrap/        # bootstrap payload handling (provider-agnostic)
│   ├── controller/       # reconciliation logic
│   │   ├── xocluster_controller.go
│   │   ├── xomachine_controller.go
│   │   ├── xomachine_reconcile.go
│   │   ├── ccm-manifest.yaml / csi-manifest.yaml / cni-cilium.yaml
│   │   └── xomachine/    # XOMachine helpers (VM ops, IPAM, status, resolve)
│   └── kubevip/          # kube-vip static-pod injection into cloud-init
├── config/               # kustomize: CRDs, RBAC, manager Deployment
├── templates/            # cluster templates (kubeadm / Talos)
├── test/e2e/             # Ginkgo end-to-end suite (build tag e2e)
├── hack/                 # codegen helpers (Cilium manifest, Helm patch)
└── docs/                 # this documentation
```

| You want to… | Look in |
|---|---|
| Add or change an API field | `api/v1beta2/*_types.go` |
| Change cluster-level behaviour (endpoint, addons) | `internal/controller/xocluster_controller.go` |
| Change the VM lifecycle | `internal/controller/xomachine_reconcile.go`, `internal/controller/xomachine/` |
| Change what happens to bootstrap data | `internal/bootstrap/` |
| Change kube-vip injection | `internal/kubevip/inject.go` |
| Change generated CRDs / RBAC | markers in `api/` and `internal/controller/`, then `make manifests` |
| Change a cluster template | `templates/` |

## The entry point

`cmd/main.go` wires everything together. It registers the schemes the manager's
clients need — core Kubernetes, CAPI core (`clusterv1`), the addons API
(`addonsv1`, for `ClusterResourceSet`), the Cluster API IPAM API (`ipamv1`, for
`IPAddressClaim`), and this provider's `v1beta2` types — then starts one manager
with two reconcilers.

The flags that matter day to day:

| Flag | Default | Purpose |
|---|---|---|
| `--xoa-secret-name` | `xo-credentials` | Secret holding the XO `url` and `token` keys. |
| `--xoa-secret-namespace` | *(pod namespace)* | Where that Secret lives. |
| `--leader-elect` | `false` | Only one active manager; off for local runs. |
| `--metrics-bind-address` | `0` (disabled) | Metrics endpoint. |
| `--metrics-secure` | `true` | Serve metrics over HTTPS with authn/authz. |
| `--health-probe-bind-address` | `:8081` | Liveness/readiness probes. |
| `--enable-http2` | `false` | HTTP/2 for metrics and webhooks. |

At startup the manager loads XO credentials into the process environment: it
reads `url`, `token` and (optionally) `insecure` from the Secret, sets
`XOA_URL` / `XOA_TOKEN` / `XOA_INSECURE`, then calls the shared
`xenorchestra-k8s-common` loader. **If that fails, the manager exits** — which is
why the credentials secret must exist before the Deployment starts. A cluster or
machine can override the global credentials later with its own `identityRef`.

## The API types

All four resources live in one API group,
`vates.infrastructure.cluster.x-k8s.io/v1beta2`:

| File | Kind | Role |
|---|---|---|
| `api/v1beta2/xocluster_types.go` | `XOCluster` | Cluster-wide infrastructure settings. |
| `api/v1beta2/xomachine_types.go` | `XOMachine` | One VM, one node. |
| `api/v1beta2/xoclustertemplate_types.go` | `XOClusterTemplate` | Stencil for `XOCluster`. |
| `api/v1beta2/xomachinetemplate_types.go` | `XOMachineTemplate` | Stencil for `XOMachine`. |

The types carry the usual CAPI contract fields: `status.initialization.provisioned`
and `status.conditions`. The field-by-field reference is in
[Reference → Custom resources](../reference/crds/index.md).

## `XOCluster` reconciliation

`XOClusterReconciler` (`internal/controller/xocluster_controller.go`) watches
`XOCluster`, the owner `Cluster`, and control plane `Machine`s. Its
`reconcileNormal` runs in a fixed order:

1. **Add the finalizer** `vates.infrastructure.cluster.x-k8s.io/xocluster`.
2. **Find the owner `Cluster`** through the `OwnerReference`. Until it exists,
   nothing else can proceed, so it waits.
3. **Ensure the cluster-name label.** The `ClusterResourceSet` selector (below)
   matches on `cluster.x-k8s.io/cluster-name`; clusters built from flat templates
   may lack it, so the reconciler adds it when missing.
4. **Resolve the control plane endpoint.** `spec.controlPlaneEndpoint` wins if
   set. Otherwise the reconciler inspects control plane `Machine`s until it finds
   one whose `XOMachine` is `Ready` and has an `InternalIP` or `ExternalIP`, and
   uses that address on port `6443`. Until then it sets `Ready=False`
   (`WaitingForControlPlaneEndpoint`) and requeues after 10 seconds.
5. **Publish status.** With an endpoint in hand it sets `status.ready`,
   `status.initialization.provisioned`, and a `Ready=True` condition
   (`ClusterInfrastructureReady`).
6. **Resolve XO credentials** for the cluster (its `identityRef`, else the global
   fallback).
7. **Reconcile the addons** (see below). A failure here requeues after 30
   seconds rather than failing the object.

Deletion is deliberately trivial: the provider owns no external resource for a
cluster, so `reconcileDelete` just removes the finalizer.

## Addons via `ClusterResourceSet`

Addons are the one place the provider reaches *into* the workload cluster. It
does so indirectly, through Cluster API: for each addon it writes a `ConfigMap`
holding a rendered manifest and a `ClusterResourceSet` that selects only this
cluster.

The two objects live in the **`XOCluster`'s namespace** — a deliberate detail,
because Cluster API applies a `ClusterResourceSet` only to `Cluster`s in the
same namespace. Both are named after the cluster:

| Addon | ConfigMap | `ClusterResourceSet` | Strategy |
|---|---|---|---|
| CCM | `ccm-manifests-<cluster>` | `ccm-deployment-<cluster>` | `Reconcile` |
| CSI | `csi-manifests-<cluster>` | `csi-deployment-<cluster>` | `Reconcile` |
| CNI | `cni-manifests-<cluster>` | `cni-deployment-<cluster>` | `ApplyOnce` |

Each `ClusterResourceSet` selects on
`cluster.x-k8s.io/cluster-name: <cluster>`. The manifests are Go templates
embedded with `//go:embed`:

- **CCM** (`ccm-manifest.yaml`) is rendered with the XO URL, token and insecure
  flag, and a `--controllers` list. The base list is
  `cloud-node,cloud-node-lifecycle,cloud-node-label-sync`; the out-of-service
  controller (`cloud-node-out-of-service`) is appended unless
  `addons.nodeOutOfService.enabled` is `false`. The optional `syncPeriod` and
  `gracePeriod` are only rendered when set, so the CCM keeps its own defaults
  otherwise.
- **CSI** (`csi-manifest.yaml`) is rendered with the API server host and port,
  defaulting to `127.0.0.1:6443` when `spec.controlPlaneEndpoint` is unset.
- **CNI** (`cni-cilium.yaml`) is rendered for Cilium. The template is generated
  from the upstream Helm chart by `hack/cilium-manifest` (`make cilium-manifest`)
  and carries the current Cilium version. The reconciler detects a Talos cluster
  (the owner `Cluster`'s `controlPlaneRef.kind` is `TalosControlPlane`) and
  renders Talos-specific values; with kube-proxy replacement and no explicit
  endpoint it defaults to Talos **KubePrism** (`localhost:7445`).

Disabling an addon deletes its ConfigMap and `ClusterResourceSet`; whatever the
addon already installed in the workload cluster is left in place.

!!! note "`ApplyOnce` versus `Reconcile`"

    The CNI is applied once, because re-applying a CNI manifest on every
    reconcile is not what you want; the CCM and CSI are reconciled so the
    credentials and endpoint stay current. That is also why the API documents
    changing `addons.cilium` as not reconfiguring a running cluster.

## `XOMachine` reconciliation

`XOMachineReconciler` (`internal/controller/xomachine_controller.go` plus
`xomachine_reconcile.go`) is the heart of the provider. It watches `XOMachine`,
the owner `Machine`, and `Secret`s (so a bootstrap secret is delivered as soon as
it appears), and runs up to 5 reconciles concurrently.

`reconcileNormal` is a sequence of guards and one-time steps:

1. **Add the finalizer** `vates.infrastructure.cluster.x-k8s.io/xomachine`.
2. **Fast path.** If `status.providerID` is already set, the reconciler skips
   straight to the existing VM. A `Ready` machine is refreshed — tags are
   re-applied and the address is re-read from Xen Orchestra — then it returns.
   A not-yet-ready machine goes through `reconcileExistingVM`: start it, resolve
   its IP, manage VIFs, and mark it `Ready`.
3. **Resolve bootstrap data** (`internal/bootstrap`). The owner `Machine`'s
   `spec.bootstrap.dataSecretName` points at the payload secret; the provider
   reads its `value` key. Missing data means requeue after 3 seconds. Inline
   `spec.bootstrapData` is used when there is no owner `Machine`.
4. **Resolve credentials**, in priority order: the `XOMachine`'s `identityRef`,
   then the owner `XOCluster`'s `identityRef`, then the controller's global
   credentials.
5. **Build an XO client** and check connectivity. No credentials configured means
   the reconciler logs and stops without failing.
6. **Build the payload** (`bootstrap.Build`) from the declared behavior.
7. **Apply IPAM, if configured.** When `networkConfig.addressesFromPools` is set,
   the address from the pool replaces the machine's own network configuration as
   a netplan document. Until the pool answers, the controller sets
   `WaitingForAddress` and requeues after 5 seconds — it never creates the VM on
   a guessed address.
8. **Resolve the template and pool** IDs (ID fields first, name lookups second).
9. **Create or adopt the VM.** With an existing `providerID` it looks the VM up;
   otherwise it creates it. Creation is idempotent: before creating, it searches
   for a VM carrying this machine's unique marker (derived from the `XOMachine`
   UID and written into the VM's description) and adopts it instead of leaking a
   second VM. It then resizes the disk, sets CPUs and boot order, saves the
   `providerID`, and applies tags.
10. **Wait for readiness.** `WaitForVMReady` starts the VM if needed, resolves a
    usable IPv4 address (ignoring link-local addresses), connects VIFs, and sets
    `status.addresses`, `status.ready` and the `VmReady` condition. While the
    guest tools have not reported an address yet, it requeues after 3 seconds.

Deletion is careful about data. Before removing the VM it:

1. stops it if it is Paused or Suspended (XCP-ng refuses to detach VBDs in those
   states);
2. detaches CSI-managed persistent-volume disks, identified by their
   `k8s:volumeId:` VDI tag, so the VM's deletion cannot destroy the PV data;
3. hard-shuts it down and deletes it in Xen Orchestra;
4. removes the finalizer.

## Bootstrap payload handling

`internal/bootstrap` is the provider-agnostic seam described in
[Reference → Bootstrap payload](../reference/bootstrap.md). `BootstrapData` and
`Build` never branch on a provider name:

- The raw secret data is the user-data payload.
- If `spec.bootstrap.format` is `cloudinit`, the payload may be enriched: the XO
  user's SSH keys are merged in when the `XOCluster` sets `injectSSHKeys`, and
  kube-vip scripts are injected when the cluster sets `controlPlaneLB: kube-vip`
  and the machine is a control plane node.
- `networkConfig.GuestConfig` becomes the config-drive network payload, or `nil`
  when unset.

Anything else — Talos, or any future bootstrap provider — is delivered untouched.

## `internal/kubevip` injection

`internal/kubevip` turns a cloud-init document into a kube-vip install. `Inject`
parses the cloud-config YAML and:

- appends two `write_files` entries, `/etc/kubernetes/kube-vip-pre.sh` and
  `/etc/kubernetes/kube-vip-post.sh`, both `0755`;
- finds the `kubeadm init` or `kubeadm join` command in `runcmd` and inserts the
  pre-script before it and the post-script after it.

The pre-script pulls the pinned kube-vip image with `ctr`. The post-script waits
for the API server, builds a kubeconfig from `admin.conf`, and generates the
kube-vip static-pod manifest. Injection fails loudly when the cloud-config has no
`runcmd` section or no kubeadm command — there is nowhere to hook otherwise.

## The Xen Orchestra client

The provider talks to XO through `xenorchestra-k8s-common` and
`xenorchestra-go-sdk`. Construction is centralised:

- `ResolveXOConfig` (`internal/controller/credentials.go`) reads the `url`,
  `token` and optional `insecure` keys from an `identityRef` Secret.
- `NewXOClientFromConfig` creates the client and calls `CheckClient` to verify
  connectivity; `nil` config returns a `nil` client, which callers treat as "skip
  XO operations".
- `GetOrCreateXOClient` (`internal/controller/xomachine/client.go`) does the same
  with a cache-style fallback.

The client multiplexes two APIs: `Client.V1Client()` is the legacy JSON-RPC
interface (used for name lookups and some operations), while `Client.VM()`,
`Client.VBD()` and `Client.Task()` are the v2 REST interface. `xo_compat.go` (and
`internal/controller/xo_compat.go`) absorb the older API's habit of returning a
bare task ID instead of JSON — the `unmarshal` errors the code detects and
recovers from. The reconciler's `newClientFunc` field is overridable so tests can
substitute a mock.

## Where to go next

- Build and run it: [Development](development.md).
- Run the test suites: [Testing](testing.md).
- Configure networking details: [IPAM](../guides/ipam.md) and
  [VIP](../guides/vip.md).
