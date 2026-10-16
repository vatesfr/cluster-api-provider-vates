# Upgrading

"Upgrading" is four different operations that are easy to conflate. Decide which
one you are doing before you touch anything, because they have different risks
and different orders.

| What you upgrade | Affects | Rolls nodes? |
|---|---|---|
| The provider itself | the management-cluster controllers | No |
| The Kubernetes version | the workload cluster's control plane and nodes | Yes |
| The VM template / machine image | the nodes built from it | Yes |
| The Talos version | the workload cluster's nodes | Yes |

The provider never invents a version and never upgrades a cluster on its own. You
change a field on a Cluster API object, and the provider — and the control plane
provider next to it — roll to match.

## Upgrading the provider

The provider implements the Cluster API contract **`v1beta2`** in every release,
from `v0.1` to `v1.1`. Because the contract does not change, an upgrade is an
in-place image and CRD update, not a migration. No VM is touched.

With `clusterctl`:

```bash
clusterctl upgrade apply --infrastructure vates:v1.1.0-rc4
```

With the single bundle:

```bash
kubectl apply -f dist/install.yaml
```

With Helm:

```bash
helm upgrade vates-capi ./dist/chart/ --namespace capi-system
```

Afterwards, confirm the Deployment runs the new image and the CRDs are current:

```bash
kubectl -n capi-system get deployment vates-capi-controller-manager \
  -o jsonpath='{.spec.template.spec.containers[0].image}{"\n"}'
kubectl get crd xomachines.vates.infrastructure.cluster.x-k8s.io \
  -o jsonpath='{.spec.versions[*].name}{"\n"}'
```

!!! tip "Read the release notes first"

    The changelog of each release is grouped by category — Features, Bug fixes,
    Breaking changes. Scan *Breaking changes* before upgrading a production
    management cluster. See [Releasing](releasing.md) for how the notes are
    produced.

## Upgrading a cluster's Kubernetes version

The provider does **not** choose the Kubernetes version. With kubeadm it comes
from the control plane provider (`KubeadmControlPlane.spec.version`) and from the
machine image; the bootstrap provider installs the matching `kubelet` and
`kubeadm`.

The kubeadm flow rolls the control plane **one node at a time**. Set the target
version on the control plane object:

```bash
kubectl -n <ns> patch kubeadmcontrolplane <cluster>-cp \
  --type=merge -p '{"spec":{"version":"v1.36.1"}}'
kubectl -n <ns> get kubeadmcontrolplane <cluster>-cp -w
```

Wait until the control plane reports the new version and every replica is ready,
then update the workers:

```bash
kubectl -n <ns> patch machinedeployment <cluster>-md-0 \
  --type=merge -p '{"spec":{"template":{"spec":{"version":"v1.36.1"}}}}'
```

!!! warning "The image and the version must agree"

    `--kubernetes-version` (and the version fields) must be the version the nodes
    will actually run. With the **from-scratch** image the node installs exactly
    that version at bootstrap; if the control plane and the node disagree, the
    machine is rolled forever. With the **pre-filled** image the version is baked
    in, so the image and the version must move **together** — see the next
    section.

!!! note "`maxUnavailable: 0` blocks rollouts"

    A `MachineDeployment` with `spec.strategy.rollout.maxUnavailable: 0` cannot
    drain a node, so a version upgrade stalls with nothing happening. Set it to
    `1` to allow the rollout. The same default makes the `Cluster` report
    `Available: False` while a replica is rolling; see
    [Troubleshooting](troubleshooting.md#the-cluster-reports-available-false-but-it-is-up).

## Upgrading the VM template or machine image

The machine image is chosen by `XOMachineTemplate.spec.template.spec.templateID`
(or `templateName`). Changing it reshapes the **next** machines created from that
template; it does not touch the machines already running. To actually move
existing nodes onto a new image, roll them.

The clean, versioned way is to create a new template and point the machine producer
at it, so Cluster API sees a template change and rolls:

1. Prepare the new image in Xen Orchestra and note its UUID.
2. Copy the machine template under a new name (for example
   `my-cluster-worker-v2`) with the new `templateID`.
3. Point the producer at the new template:
   - workers: `MachineDeployment.spec.template.spec.infrastructureRef.name`;
   - control plane: `KubeadmControlPlane.spec.machineTemplate.infrastructureRef.name`.
4. Watch the rollout; delete the old template once nothing references it.

```bash
kubectl -n <ns> get machinedeployment <cluster>-md-0 -w
kubectl -n <ns> get xomachines -w
```

With a **ClusterClass** managed topology, the templates live in the
`XOClusterTemplate` / `XOMachineTemplate` referenced by the class. Update the
template and bump the `Cluster`'s topology version; Cluster API rolls the managed
`MachineDeployment`s and the control plane for you.

!!! tip "Keep a replacement path"

    The provider never recreates a VM by itself. Replacement of a dead node is
    the job of the `MachineHealthCheck` (or the control plane provider), which
    is why the templates ship one for each role. Editing a template only changes
    future machines; recovery of an unhealthy one goes through the same
    Machine lifecycle.

## Upgrading Talos

Talos is immutable, so an upgrade replaces the image and the configuration
version together. Two fields must move in lockstep:

- the `XOMachineTemplate` used by `TalosControlPlane` and the worker
  `MachineDeployment` — its `templateID` points at the new Talos `nocloud` image;
- `TalosControlPlane.spec.controlPlaneConfig.controlplane.talosVersion` — set it
  to the version of that image.

```yaml
spec:
  controlPlaneConfig:
    controlplane:
      talosVersion: v1.13.9   # must match the image
```

!!! danger "A mismatched version leaves the node in maintenance"

    `talosVersion` **must** be the Talos version the image was built with. When
    it is unset or wrong, the bootstrap provider generates a config for its own
    default version; the node rejects the unknown documents and stays in
    **maintenance mode**, never joining. If you see a Talos node stuck in
    maintenance, check this field first — then the image's `viridian: false` and
    *never booted* properties (see the
    [Talos prerequisites](../guides/providers/talos/prerequisites.md)).

Then roll the nodes the same way as any other image change: point the control
plane and worker machine templates at the new image, update `talosVersion`, and
let the providers replace machines.

## Order of operations

When more than one layer changes at once, do them in this order:

1. **Upgrade the provider first.** New API fields and fixes should be understood
   by the running controller before you set them.
2. **Update the control plane, then the workers.** For Kubernetes and image
   changes alike, let the control plane settle before rolling the workers.
3. **Move the image and the version together** for any pre-filled or immutable
   image (kubeadm pre-filled, Talos). Splitting them is what produces loops.
4. **Keep the old template** until the rollout finishes, so a rollback is a
   reference change rather than a rebuild.

## What to watch when it stalls

A rollout in progress is a list of `Machine`s, each with an `XOMachine` and a
condition. Start with the generic view and drill down:

```bash
kubectl -n <ns> get machines -w
kubectl -n <ns> get xomachines -w
kubectl -n <ns> describe machine <name>
kubectl -n <ns> get xomachine <name> -o jsonpath='{.status.conditions}{"\n"}'
clusterctl describe cluster <cluster>
```

The condition **reason** tells you where to look:

| Reason | Likely cause |
|---|---|
| `WaitingForIPAddress` | The VM is up but Xen guest tools have not reported an address. |
| `WaitingForAddress` | IPAM is configured; the pool has not answered the claim. |
| `VmStartFailed`, `VmCreationFailed` | Xen Orchestra rejected the operation — read the controller logs. |
| No new `Machine` at all | The control plane provider or `MachineDeployment` is itself stalled (often `maxUnavailable: 0`). |
| Node joins then never becomes `Ready` | Missing CNI in the workload cluster. |

The full symptom-to-cause map is in [Troubleshooting](troubleshooting.md).

## Where next

- Publish a release: [Releasing](releasing.md).
- The kubeadm flow in detail: [kubeadm](../guides/providers/kubeadm.md).
- The Talos flow in detail: [Talos](../guides/providers/talos/index.md).
