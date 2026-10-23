# XOMachine

`XOMachine` represents **one virtual machine**, and therefore one node of a
workload cluster. Its spec describes the VM to create; its status reports what
was created and whether it is ready.

The provider creates an `XOMachine` for you whenever a `Machine`'s
`spec.infrastructureRef` points at one. You normally only touch `XOMachine`
through `XOMachineTemplate`.

## `spec`

| Field | Type | Description |
|---|---|---|
| `providerID` | `*string` | Set by the controller after creation, as `xenorchestra://<pool-uuid>/<vm-uuid>`. |
| `poolID` / `poolName` | `string` | Pool to create the VM in. Falls back to the template's pool. |
| `templateID` / `templateName` | `string` | VM template to clone. |
| `networkConfig` | `NetworkConfig` | Interfaces, IP filtering, and guest network config. |
| `resourceSet` | `ResourceSet` | vCPUs, memory, disk. |
| `failureDomain` | `*string` | Failure domain this machine belongs to. |
| `tags` | `[]string` | Extra XO tags for this VM (max 64), on top of the identifying tags the controller adds. Additive: removing a tag does not strip it from an existing VM. |
| `bootstrapProvider` | `string` | **Observability only.** Applied as a `bootstrap:<name>` tag on the VM. Does not change behavior. |
| `bootstrap` | `BootstrapBehavior` | What to *do* with the bootstrap payload. See [Bootstrap payload](../bootstrap.md). |
| `bootstrapData` | `string` | Inline bootstrap data. If empty, read from the owning `Machine`'s bootstrap secret. |
| `identityRef` | `LocalObjectReference` | Per-machine XO credentials. Falls back to the cluster's, then the controller's. |

### `networkConfig`

| Field | Type | Description |
|---|---|---|
| `networks` | `[]Network` | Interfaces to attach. Each has `networkID` (UUID) or `name`. |
| `allowedIPRange` | `string` | CIDR of IPs allowed to reach the VM, applied to all its interfaces. Unrestricted if empty. |
| `addressesFromPools` | `[]IPPoolReference` | Ask Cluster API's IPAM for a static address (`apiGroup`, `kind`, `name`). See [IPAM](../../guides/ipam.md). |
| `guestConfig` | `string` | Cloud-init netplan (v2) passed to the guest, for a static IP or custom setup before first boot. |

### `resourceSet`

| Field | Type | Description |
|---|---|---|
| `cpus` | `*int32` | Number of vCPUs (minimum 1). |
| `memory` | `string` | Memory in Kubernetes resource format, e.g. `8Gi`. |
| `diskSize` | `string` | Disk size in Kubernetes resource format, e.g. `30Gi`. |

### `bootstrap`

| Field | Type | Description |
|---|---|---|
| `format` | `string` | `cloudinit` or `passthrough` (default). See [Bootstrap payload](../bootstrap.md). |

Leaving `bootstrap` unset is the same as `format: passthrough`.

## `status`

| Field | Type | Description |
|---|---|---|
| `ready` | `bool` | The VM is up and the node has joined. |
| `vmName` | `string` | The VM's name in Xen Orchestra, recorded before the VM is created and never changed afterwards. See [VM naming](../../guides/vm-naming.md). |
| `providerID` | `*string` | The VM's Xen Orchestra provider ID. |
| `addresses` | `[]corev1.NodeAddress` | The VM's addresses, read through the guest agent. |
| `initialization.provisioned` | `bool` | Part of the CAPI contract. |
| `failureReason` / `failureMessage` | `*string` | Set on terminal failure. |
| `conditions` | `[]metav1.Condition` | Standard conditions. |

## Examples

### A full `XOMachine`

A control plane node as the provider sees it, with the settings you are most
likely to touch. `providerID` is omitted on purpose: the controller sets it
after creating the VM.

```yaml
apiVersion: vates.infrastructure.cluster.x-k8s.io/v1beta2
kind: XOMachine
metadata:
  name: my-cluster-control-plane-abcde
  namespace: default
  labels:
    cluster.x-k8s.io/cluster-name: my-cluster
spec:
  templateID: 3f2b1c0a-1d2e-4f5a-8b9c-0d1e2f3a4b5c
  poolID: 9d8c7b6a-5e4f-4a3b-9c2d-1e0f9a8b7c6d
  failureDomain: pool-a
  resourceSet:
    cpus: 4
    memory: 8Gi
    diskSize: 50Gi
  networkConfig:
    networks:
      # Either the UUID (preferred, no API lookup)...
      - networkID: 1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d
      # ...or the XO network name (resolved through the V1 client):
      # - name: "Pool-wide network associated with eth0 on VLAN60"
    allowedIPRange: 10.30.139.0/24
  bootstrapProvider: kubeadm   # observability only: a bootstrap:<name> tag
  bootstrap:
    format: cloudinit
  tags:
    - env=prod
    - team=platform
```

- `poolID` / `templateID` fall back to the template's values when empty;
  `poolName` / `templateName` are the name-based alternatives.
- `allowedIPRange` restricts which source IPs may reach the VM, applied to all
  its interfaces.
- `bootstrap.format: cloudinit` is what unlocks SSH-key and kube-vip injection
  (see [Bootstrap payload](../bootstrap.md)); `bootstrapProvider` only adds a
  tag.

### The `XOMachineTemplate` it usually comes from

You rarely write an `XOMachine` yourself. CAPI stamps one from a template, so
the same fields appear one level deeper under `spec.template.spec`:

```yaml
apiVersion: vates.infrastructure.cluster.x-k8s.io/v1beta2
kind: XOMachineTemplate
metadata:
  name: my-cluster-cp
  namespace: default
spec:
  template:
    spec:
      templateID: 3f2b1c0a-1d2e-4f5a-8b9c-0d1e2f3a4b5c
      poolID: 9d8c7b6a-5e4f-4a3b-9c2d-1e0f9a8b7c6d
      resourceSet:
        cpus: 4
        memory: 8Gi
        diskSize: 50Gi
      networkConfig:
        networks:
          - networkID: 1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d
      bootstrap:
        format: cloudinit
```

See [XOMachineTemplate](xomachinetemplate.md) for the reference fields and
which control plane / deployment objects name it.

### Static IP with `guestConfig`

When no IPAM provider is in play, hand the guest a netplan v2 document. It is
delivered through the VM's config drive and applied before the first boot:

```yaml
spec:
  templateID: 3f2b1c0a-1d2e-4f5a-8b9c-0d1e2f3a4b5c
  poolID: 9d8c7b6a-5e4f-4a3b-9c2d-1e0f9a8b7c6d
  networkConfig:
    networks:
      - networkID: 1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d
    guestConfig: |
      version: 2
      ethernets:
        eth0:
          dhcp4: false
          addresses:
            - 10.30.139.42/16
          routes:
            - to: default
              via: 10.30.0.1
          nameservers:
            addresses:
              - 10.30.0.1
```

The interface is always `eth0` in the rendered document, and a hard-coded
address is fragile for a control plane node — prefer IPAM below.

### Address from an IPAM pool

For a stable, provider-chosen address, reference one or more
`IPPoolReference` objects. The controller creates an `IPAddressClaim` and
renders the answer as netplan, replacing any `guestConfig`:

```yaml
spec:
  templateID: 3f2b1c0a-1d2e-4f5a-8b9c-0d1e2f3a4b5c
  poolID: 9d8c7b6a-5e4f-4a3b-9c2d-1e0f9a8b7c6d
  resourceSet:
    cpus: 2
    memory: 4Gi
    diskSize: 20Gi
  networkConfig:
    addressesFromPools:
      - apiGroup: ipam.cluster.x-k8s.io
        kind: InClusterIPPool
        name: my-cluster-pool
    networks:
      - networkID: 1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d
```

The pool only decides the *address*; `networks` still decides which XO network
the VM is plugged into. The full workflow — installing the provider, defining a
pool, and the order of operations — is in [IPAM](../../guides/ipam.md).

### Per-machine `identityRef`

Override the cluster's Xen Orchestra credentials for a single VM. The `Secret`
must be in the same namespace as the `XOMachine` and carry `url` and `token`
(plus optional `insecure`):

```yaml
spec:
  templateID: 3f2b1c0a-1d2e-4f5a-8b9c-0d1e2f3a4b5c
  poolID: 9d8c7b6a-5e4f-4a3b-9c2d-1e0f9a8b7c6d
  identityRef:
    name: xo-tenant-b
  networkConfig:
    networks:
      - networkID: 1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d
```

Resolution order is machine `identityRef`, then the cluster's `identityRef`,
then the controller's global credentials.

### A Talos machine: pass-through

The Talos bootstrap provider emits a machine config, not cloud-init. Declare
nothing and the payload is delivered verbatim:

```yaml
apiVersion: vates.infrastructure.cluster.x-k8s.io/v1beta2
kind: XOMachine
metadata:
  name: talos-cluster-control-plane-xyz12
  namespace: default
spec:
  templateID: 7c6b5a49-3d2e-4f10-9a8b-7c6d5e4f3a2b
  poolID: 9d8c7b6a-5e4f-4a3b-9c2d-1e0f9a8b7c6d
  resourceSet:
    cpus: 4
    memory: 8Gi
    diskSize: 40Gi
  networkConfig:
    networks:
      - networkID: 1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d
    # no `bootstrap`: full pass-through
```

## Where next

- The template that produces these: [XOMachineTemplate](xomachinetemplate.md).
- What `bootstrap` means: [Bootstrap payload](../bootstrap.md).
- IPAM in full: [IPAM](../../guides/ipam.md).
