# Where Vates CAPI fits

vates-capi is the **infrastructure provider** for Xen Orchestra and XCP-ng. That
is its entire job. It answers the *where* of every `Machine`: create the VM, put
it on the right network, start it, deliver its bootstrap data, and report back
when it is up.

Everything else — which OS image, which bootstrap mechanism, how the control
plane is managed — belongs to the bootstrap and control plane providers you
install next to it. This provider never inspects which one you chose.

## The custom resources it brings

Installing the provider adds four custom resources to your management cluster,
all in the API group `vates.infrastructure.cluster.x-k8s.io/v1beta2`:

| Resource | Kind | Role |
|---|---|---|
| `xoclusters` | `XOCluster` | One per workload cluster. Holds cluster-wide infrastructure settings. |
| `xoclustertemplates` | `XOClusterTemplate` | Stencil for `XOCluster`, used by ClusterClass topologies. |
| `xomachines` | `XOMachine` | One per node. Describes the VM to create. |
| `xomachinetemplates` | `XOMachineTemplate` | Stencil that control planes and machine deployments stamp machines from. |

You will mostly let Cluster API create and manage these objects for you by
referencing templates. In day-to-day use you edit the *templates*, not the
individual machines.

## What it does with a XOMachine

When a `XOMachine` appears, the controller:

1. **Resolves the target.** Turns `poolID`/`poolName`, `templateID`/`templateName`,
   and the network references into Xen Orchestra IDs, preferring the ID fields
   when set so it can skip the lookup.
2. **Clones the template** into a new VM, sized by `resourceSet` (vCPUs, memory,
   disk) and named with `namePrefix`.
3. **Attaches the networks** listed in `networkConfig.networks`, applies the
   `allowedIPRange` if any, and passes the guest-side netplan config through.
4. **Delivers the bootstrap payload** from the owning `Machine`'s bootstrap
   secret, according to the [`bootstrap` declaration](#the-bootstrap-declaration).
5. **Starts the VM** and waits for it to become reachable.
6. **Publishes the result** on `status`: a `providerID` of the form
   `xenorchestra://<pool-uuid>/<vm-uuid>`, the node addresses, and a `Ready`
   condition.

Deletion reverses this: the controller stops the VM, optionally cleans up
attached disks, and removes it from Xen Orchestra.

## The bootstrap declaration

The provider is deliberately blind to *which* bootstrap provider you use. It
does not branch on "kubeadm" versus "Talos". Instead, each `XOMachine` /
`XOMachineTemplate` declares the **format** of the payload via `spec.bootstrap`:

| `spec.bootstrap.format` | Effect |
|---|---|
| *(unset)* / `passthrough` | Pass the payload through untouched, and label the config drive so the guest platform finds it. |
| `cloudinit` | Treat it as cloud-init, which may be enriched: the XO user's SSH keys are merged in when `XOCluster.spec.injectSSHKeys` is on, and kube-vip is injected when the cluster sets `controlPlaneLB: kube-vip`. |

Because the declaration describes *what the payload is*, not *who produced it*,
supporting a brand-new bootstrap provider requires **no change to this
controller**. You just declare it on the template. The Talos flow, for instance,
declares `format: passthrough`: its configuration is passed through as-is, and
the config drive is labelled `cidata` so Talos' `nocloud` datasource reads it.

## Cluster-level settings: `XOCluster`

A few things are cluster-wide, so they live on `XOCluster` rather than on each
machine:

- `controlPlaneEndpoint` — the API server endpoint (host, port, optional subnet).
- `controlPlaneLB: kube-vip` — enable kube-vip injection for the control plane.
- `injectSSHKeys` — merge the XO user's SSH keys into cloud-init.
- `identityRef` — a `Secret` with the Xen Orchestra credentials for this cluster.
- `addons` — what the provider installs into the workload cluster via
  `ClusterResourceSet`: the Xen Orchestra **CCM**, the Xen Orchestra **CSI**
  driver, an optional **CNI** (Cilium), and the CCM out-of-service controller.

The addons are covered in detail in [Reference → Custom resources](../reference/crds/index.md).

## What it does *not* do

To keep expectations honest:

- It does not install Cluster API core or the bootstrap / control plane
  providers. You install those with `clusterctl` (or however you prefer).
- It does not run a CNI by default. Cilium is available as an addon, but it is
  opt-in.
- It does not decide the Kubernetes version. With kubeadm that comes from the
  bootstrap provider and the machine image; with Talos, from the Talos control
  plane provider.

Ready to use it? Head to [Getting started](../getting-started/prerequisites.md).
