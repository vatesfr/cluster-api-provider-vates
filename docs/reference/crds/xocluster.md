# XOCluster

`XOCluster` is the *infrastructure cluster* for a workload cluster. It holds the
settings that apply to the whole cluster rather than to an individual machine,
and it publishes the control plane endpoint once it is known.

One `XOCluster` exists per workload cluster. Cluster API creates it for you when
you create a `Cluster` whose `spec.infrastructureRef` points at it.

## `spec`

| Field | Type | Default | Description |
|---|---|---|---|
| `controlPlaneEndpoint` | `APIEndpoint` | — | The reachable API server endpoint. |
| `controlPlaneLB` | `string` | — | Control plane load balancer. Only `kube-vip` is supported. |
| `injectSSHKeys` | `bool` | `false` | Merge the XO user's SSH keys into cloud-init. |
| `identityRef` | `LocalObjectReference` | — | A `Secret` with the XO credentials for this cluster. |
| `tags` | `[]string` | — | Extra XO tags applied to every VM of the cluster (max 64). Additive: removing a tag does not strip it from existing VMs. |
| `vmNaming` | `VMNamingSpec` | `<DISTRIBUTION>-<cluster>-<ROLE>-<ID>` | VM names in Xen Orchestra. See [VM naming](../../guides/vm-naming.md). |
| `addons` | `AddonsSpec` | CCM + CSI on, no CNI | What the provider installs into the workload cluster. |

### `controlPlaneEndpoint`

| Field | Type | Description |
|---|---|---|
| `host` | `string` | DNS name or IP of the API server. With kube-vip, this is the floating VIP. |
| `port` | `int32` | API server port (`6443` by convention). |
| `subnet` | `int32` | CIDR subnet mask for the load balancer. Auto-detected if unset. |

### `vmNaming`

When unset, a VM is named `<DISTRIBUTION>-<cluster>-<ROLE>-<ID>`, e.g.
`TALOS-my-cluster-CP-7n62g`: see [Default names](../../guides/vm-naming.md#default-names).

When set, the name of a VM is the prefix of its role followed by the ID of its
Cluster API `Machine`: its 5-character random suffix, e.g.
`<controlPlanePrefix>7n62g`, or its whole name when it does not end with one.
The prefixes are taken verbatim: include the separator before the ID.

| Field | Type | Description |
|---|---|---|
| `controlPlanePrefix` | `string` | **Required**, non-empty. Prefix of the control plane VM names. |
| `workerPrefix` | `string` | **Required**, non-empty. Prefix of the worker VM names. |

A VM is named once, when it is created: changing `vmNaming` renames no existing
VM and rolls out no machine. See [VM naming](../../guides/vm-naming.md).

### `addons`

Installed via `ClusterResourceSet`. Each addon is optional.

| Field | Type | Default | Description |
|---|---|---|---|
| `ccm` | `*bool` | `true` | Xen Orchestra cloud-controller-manager (node addresses, service load balancers). |
| `csi` | `*bool` | `true` | Xen Orchestra CSI driver (persistent volumes). |
| `cni` | `*string` | `none` | Container network interface. Supported: `none`, `cilium`. |
| `cilium` | `CiliumSpec` | — | Cilium settings, only when `cni: cilium`. |
| `nodeOutOfService` | `NodeOutOfServiceSpec` | enabled | CCM out-of-service controller (see below). |

`nodeOutOfService` applies the `node.kubernetes.io/out-of-service` taint to a
node whose VM is deleted, halted, paused, or suspended and whose kubelet reports
`NotReady`. That lets the Kubernetes controller manager force-detach the node's
volumes instead of waiting for its six-minute timer, so persistent volumes can
be reused promptly.

| Field | Type | Default | Description |
|---|---|---|---|
| `enabled` | `*bool` | `true` | Toggle the controller. |
| `syncPeriod` | `*Duration` | CCM default | How often nodes are reconciled. |
| `gracePeriod` | `*Duration` | CCM default | How long a powered-off VM must stay down before the taint is applied. A VM deleted from XO is tainted immediately. |

### `cilium`

Only read when `cni: cilium`. Nil means Cilium runs alongside kube-proxy.

| Field | Type | Default | Description |
|---|---|---|---|
| `kubeProxyReplacement` | `*bool` | `false` | Let Cilium fully replace kube-proxy. On Talos, also disable kube-proxy in the machine config (`cluster.proxy.disabled: true`). |
| `k8sServiceHost` | `string` | KubePrism on Talos, the control plane endpoint otherwise | API server host Cilium reaches directly when kube-proxy is replaced. |
| `k8sServicePort` | `int32` | `7445` on Talos, the endpoint port otherwise | API server port paired with `k8sServiceHost`. |

The API validates three couplings: `kubeProxyReplacement: true` requires
`cni: cilium`; `k8sServiceHost` and `k8sServicePort` must be set together; and
setting either requires `kubeProxyReplacement: true`.

The Cilium manifest is applied once (`ClusterResourceSet` strategy
`ApplyOnce`), so changing these fields does not reconfigure a cluster that is
already running.

## `status`

| Field | Type | Description |
|---|---|---|
| `ready` | `bool` | The cluster infrastructure is ready. |
| `controlPlaneEndpoint` | `APIEndpoint` | The endpoint once discovered. |
| `initialization.provisioned` | `bool` | Part of the CAPI contract; drives provisioning order. |
| `failureReason` / `failureMessage` | `*string` | Set on terminal failure. |
| `conditions` | `[]metav1.Condition` | Standard conditions. |

## Examples

### A standard cluster

The common case: a floating VIP, the default addons (CCM + CSI), Cilium as the
CNI, and SSH-key injection from the XO user profile.

```yaml
apiVersion: vates.infrastructure.cluster.x-k8s.io/v1beta2
kind: XOCluster
metadata:
  name: my-cluster
  namespace: default
spec:
  controlPlaneEndpoint:
    host: 192.168.1.240
    port: 6443
    subnet: 16
  controlPlaneLB: kube-vip
  injectSSHKeys: true
  addons:
    ccm: true
    csi: true
    cni: cilium
```

`controlPlaneEndpoint.subnet` is the prefix the load balancer uses to recognize
the floating VIP; with kube-vip it is auto-detected when unset, but setting it
avoids surprises.

### Multi-tenant cluster with `identityRef`

Point the cluster at a `Secret` holding its own Xen Orchestra credentials, so
different clusters can talk to different XO users or endpoints. The `Secret`
must live in the same namespace as the `XOCluster`.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: xo-tenant-b
  namespace: default
type: Opaque
stringData:
  url: https://xo.example.com
  token: <your-xo-api-token>
---
apiVersion: vates.infrastructure.cluster.x-k8s.io/v1beta2
kind: XOCluster
metadata:
  name: tenant-b
  namespace: default
spec:
  controlPlaneEndpoint:
    host: 10.30.139.10
    port: 6443
    subnet: 16
  controlPlaneLB: kube-vip
  identityRef:
    name: xo-tenant-b
  injectSSHKeys: false
  addons:
    ccm: true
    csi: true
    cni: none
  tags:
    - tenant-b
```

The `Secret` data keys are `url` and `token`, plus an optional `insecure`
(`"true"` to skip TLS verification). When `identityRef` is absent the provider
falls back to the controller's global credentials; per-machine
`XOMachine.spec.identityRef` can override it further, down to a single node.

### Disabling addons

Turn off what you do not want. `cni: none` leaves the CNI to you; setting
`ccm: false` or `csi: false` skips that `ClusterResourceSet` entirely.

```yaml
apiVersion: vates.infrastructure.cluster.x-k8s.io/v1beta2
kind: XOCluster
metadata:
  name: bare-cluster
  namespace: default
spec:
  controlPlaneEndpoint:
    host: 10.30.139.20
    port: 6443
    subnet: 16
  controlPlaneLB: kube-vip
  addons:
    ccm: true
    csi: false
    cni: none
```

Keep `ccm: true` in most clusters: the Xen Orchestra CCM is what sets each
node's `providerID`, which Cluster API needs to link `Machine` ↔ `Node`.

### Cilium replacing kube-proxy

On a cluster that disables kube-proxy (a Talos cluster with
`cluster.proxy.disabled: true`, for example), let Cilium take over and tell it
how to reach the API server without the `kubernetes` Service:

```yaml
apiVersion: vates.infrastructure.cluster.x-k8s.io/v1beta2
kind: XOCluster
metadata:
  name: cilium-cluster
  namespace: default
spec:
  controlPlaneEndpoint:
    host: 10.30.139.30
    port: 6443
    subnet: 16
  addons:
    cni: cilium
    cilium:
      kubeProxyReplacement: true
      k8sServiceHost: localhost   # Talos KubePrism
      k8sServicePort: 7445
```

`kubeProxyReplacement: true` without `cni: cilium`, or an endpoint without a
port (and vice versa), is rejected by the API server.

### Tuning out-of-service handling

The CCM's out-of-service controller is on by default. Tune its periods, or turn
it off, per cluster:

```yaml
apiVersion: vates.infrastructure.cluster.x-k8s.io/v1beta2
kind: XOCluster
metadata:
  name: tuned-cluster
  namespace: default
spec:
  controlPlaneEndpoint:
    host: 10.30.139.40
    port: 6443
    subnet: 16
  addons:
    nodeOutOfService:
      enabled: true
      syncPeriod: 30s
      gracePeriod: 2m
```

A VM deleted from Xen Orchestra is tainted immediately, regardless of
`gracePeriod`; the grace period only delays the taint for a VM that is merely
powered off.

## Where next

- The stencil used by managed topologies:
  [XOClusterTemplate](xoclustertemplate.md).
- The per-machine object: [XOMachine](xomachine.md).
- Networking and CNI choices: [Networking](../../guides/networking.md),
  [VIP](../../guides/vip.md).
