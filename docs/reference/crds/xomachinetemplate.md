# XOMachineTemplate

A stencil for [`XOMachine`](xomachine.md), referenced by `KubeadmControlPlane`,
`MachineDeployment`, and other machine-producing objects. Instead of writing one
`XOMachine` per node, you write one `XOMachineTemplate` and let the control
plane or deployment stamp out machines from it.

It wraps an `XOMachine` spec under `spec.template.spec`.

## `spec.template.spec`

The outer object is the template; the spec you care about is nested one level
down. It is a verbatim [`XOMachine.spec`](xomachine.md#spec):

```yaml
apiVersion: vates.infrastructure.cluster.x-k8s.io/v1beta2
kind: XOMachineTemplate
metadata:
  name: my-cluster-cp
  namespace: default
spec:
  template:
    spec:                      # <- XOMachine.spec goes here
      namePrefix: my-cluster-cp
      templateID: <your-vm-template-uuid>
      poolID: <your-xo-pool-uuid>
      resourceSet:
        cpus: 4
        memory: 8Gi
        diskSize: 30Gi
      networkConfig:
        networks:
          - networkID: <your-xo-network-uuid>
      bootstrap:
        format: cloudinit
```

| Field | Type | Description |
|---|---|---|
| `spec.template.spec` | `XOMachineSpec` | The full [`XOMachine` spec](xomachine.md#spec). |

Every field documented on [XOMachine](xomachine.md) is valid here, unchanged.
Two things are worth repeating at the template level:

!!! warning "`namePrefix` is required"

    `spec.template.spec.namePrefix` has no default and is validated as required.
    It is the prefix the provider uses to name each VM in Xen Orchestra; the
    control plane or deployment appends a unique suffix per node. Pick something
    that distinguishes the role, e.g. `my-cluster-cp` and `my-cluster-worker`.

!!! tip "Declare the bootstrap format on the template"

    The `bootstrap` field is part of `XOMachine.spec`, so it is set here and
    inherited by every stamped `XOMachine`. The kubeadm templates declare
    `format: cloudinit`; the Talos templates declare nothing (pass-through).
    See [Bootstrap payload](../bootstrap.md).

`XOMachineTemplate` has no meaningful status: it is not reconciled. The
`XOMachine` objects created from it carry `ready`, `providerID`, addresses, and
conditions.

## Referencing it from a control plane or deployment

The template is named by whichever object produces machines. The reference
field differs per kind:

| Producer | Reference field |
|---|---|
| `KubeadmControlPlane` | `spec.machineTemplate.spec.infrastructureRef` |
| `TalosControlPlane` | `spec.infrastructureTemplate` |
| `MachineDeployment` | `spec.template.spec.infrastructureRef` |
| `ClusterClass` (control plane) | `spec.controlPlane.machineInfrastructure.templateRef` |
| `ClusterClass` (workers) | `spec.workers.machineDeployments[].infrastructure.templateRef` |

A `KubeadmControlPlane` points at the control plane template by name:

```yaml
spec:
  machineTemplate:
    spec:
      infrastructureRef:
        apiGroup: vates.infrastructure.cluster.x-k8s.io
        kind: XOMachineTemplate
        name: my-cluster-cp
```

A `MachineDeployment` does the same for workers:

```yaml
spec:
  template:
    spec:
      infrastructureRef:
        apiGroup: vates.infrastructure.cluster.x-k8s.io
        kind: XOMachineTemplate
        name: my-cluster-worker
```

A `TalosControlPlane` uses its own field name, `infrastructureTemplate`:

```yaml
spec:
  infrastructureTemplate:
    kind: XOMachineTemplate
    apiVersion: vates.infrastructure.cluster.x-k8s.io/v1beta2
    name: my-cluster-cp
```

The provider does not create machines from templates itself: the control plane
provider and the `MachineDeployment`/`MachineSet` controllers do. The vates
controller only reconciles the resulting `XOMachine` objects.

## Rolling changes

Cluster API treats a template as part of the machine definition: it **hashes
the referenced `XOMachineTemplate`** and compares that hash on every Machine.
Editing the template changes the hash, which makes every affected Machine
out-of-date and triggers a **rolling replacement**:

- A `KubeadmControlPlane`/`TalosControlPlane` rolls control plane Machines one
  (or a configured batch) at a time, keeping quorum.
- A `MachineDeployment` rolls worker Machines according to its `strategy`
  (`RollingUpdate`, with `maxSurge` / `maxUnavailable`).

Common edits that trigger a roll:

- `templateID` / `templateName` — a new OS image.
- `resourceSet` — more vCPUs, memory, or disk.
- `networkConfig` — a different network or `addressesFromPools`.
- `bootstrap` — changing the payload format.

!!! note "A roll creates new VMs, it does not resize them"

    The provider creates each replacement as a fresh clone from the template,
    applies the new `resourceSet`, and deletes the old VM. Changing `resourceSet`
    is therefore a rollout, not an in-place resize: plan for the extra capacity
    while the roll is in flight.

!!! warning "Keep the Kubernetes version in step"

    With kubeadm, the requested `KUBERNETES_VERSION` must match what the VM image
    installs, or the control plane considers every Machine out-of-date and rolls
    it forever. This is an image/bootstrap concern rather than a template field,
    but a template change is a common moment to hit it — see
    `templates/kubeadm/README.md`.

## RBAC for the Talos control plane

The template is read by the machine producer, not only by the vates controller,
so that producer needs permission on `xomachinetemplates`. The two flows handle
this differently:

=== "kubeadm"

    The shipped `config/rbac/kcp_aggregate_role.yaml` is a `ClusterRole` labelled
    `kubeadm.controlplane.cluster.x-k8s.io/aggregate-to-manager: "true"`. The
    kubeadm control plane manager role aggregates it automatically, granting
    `create`, `delete`, `get`, `list`, `patch`, `update`, `watch` on both
    `xomachines` and `xomachinetemplates`. No extra step is needed.

=== "Talos"

    The Talos control plane provider (CACPPT) and bootstrap provider (CABPT) are
    **not** covered by the default install. Apply the hand-written binding:

    ```bash
    kubectl apply -k config/rbac/talos
    ```

    It creates a `ClusterRoleBinding` that binds the `vates-capi-manager-role`
    `ClusterRole` to the `default` service accounts in `cacppt-system` and
    `cabpt-system` — the namespaces clusterctl installs the Talos providers
    into. Without it, the Talos control plane cannot read the
    `XOMachineTemplate` and no control plane `Machine` is created.

## Where next

- The machine this stamps: [XOMachine](xomachine.md).
- The cluster-level stencil: [XOClusterTemplate](xoclustertemplate.md).
- IPAM addresses on a template: [IPAM](../../guides/ipam.md).
- How `bootstrap` is interpreted: [Bootstrap payload](../bootstrap.md).
