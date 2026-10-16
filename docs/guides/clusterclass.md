# ClusterClass

Instead of a flat set of manifests, you can describe a cluster as a **managed
topology**: a `Cluster` with a `topology` block, built from a `ClusterClass`.
Cluster API then derives every machine and template for you, and can roll them
out in a controlled way when you change the class.

With vates-capi, this relies on the `XOClusterTemplate` and `XOMachineTemplate`
templates (and their bootstrap counterparts `KubeadmConfigTemplate`).

## ClusterClass versus a flat template

Both produce the same cluster; they differ in who owns the objects afterwards.

| | Flat template | ClusterClass (managed topology) |
|---|---|---|
| You write | Every resource: `Cluster`, `XOCluster`, machine templates, `KubeadmControlPlane`, `MachineDeployment`. | One `Cluster` with a `topology` block. |
| Expanded by | You, or `clusterctl generate cluster`. | Cluster API, from the class. |
| To change the shape | Edit and re-apply each object. | Edit the `Cluster`'s variables, or the class; the topology controller propagates the change. |
| Upgrades | You roll the control plane and workers yourself. | Change `topology.version`; the topology controller rolls in order. |
| Reusable | Copy the file. | One class, many clusters. |

Reach for a **flat template** when you want to see and tweak every object — the
[clusterctl flow](providers/kubeadm.md). Reach for a **ClusterClass** when you
run several clusters that should stay alike and be upgradeable in one edit.

## The shipped classes

The kubeadm flow ships two classes, mirroring the two AlmaLinux image variants
([from-scratch or pre-filled](../getting-started/tutorial.md)):

| ClusterClass | Worker class | Image | Bootstrap |
|---|---|---|---|
| `vates-almalinux-fromscratch` | `worker-almalinux-fromscratch` | Minimal | Full: installs `kubelet`/`kubeadm` at first boot. |
| `vates-almalinux-prefilled` | `worker-almalinux-prefilled` | Pre-baked | Minimal: assumes the packages are installed. |

Each class composes a set of templates, all under
`templates/kubeadm/base/clusterclass/`:

| Object | Name | Role |
|---|---|---|
| `XOClusterTemplate` | `almalinux-cluster` | The infrastructure cluster. Shared by both classes; enables SSH key injection and the Cilium CNI. |
| `KubeadmControlPlaneTemplate` | `almalinux-…-control-plane` | The control plane. |
| `KubeadmConfigTemplate` | `almalinux-…-worker` | Worker bootstrap configuration. |
| `XOMachineTemplate` | `almalinux-cp-…` / `almalinux-worker-…` | The control plane and worker VMs. |

The class exposes three variables you set on the `Cluster`:

| Variable | Required | Purpose |
|---|---|---|
| `controlPlaneEndpoint` | yes | `host` (the VIP), `port` (default `6443`), `subnet`. |
| `vmNamePrefix` | yes | Prefix for the VM names in Xen Orchestra. |
| `controlPlaneLB` | no | `kube-vip` to enable the floating endpoint (see [VIP](vip.md)). |

The class patches these into the templates: it sets the endpoint and the load
balancer on the `XOClusterTemplate`, adds the endpoint host to the API server's
`certSANs`, and derives the `-cp` / `-worker` VM name prefixes.

## The `base/` + `overlays/` layout

Like everything under `templates/`, the class follows a two-layer convention:

```
templates/kubeadm/
├── base/                      # community templates — placeholders, no IDs
│   ├── clusterclass/          # the ClusterClass + control plane/bootstrap templates
│   ├── machinetemplates/      # the XOMachineTemplates the class references
│   └── example-cluster/       # a Cluster scaffold
└── overlays/<your-env>/       # your environment's UUIDs — git-ignored
```

- **`base/`** uses placeholders (`<your-xo-template-uuid>`, …) and contains no
  environment-specific values. **Never edit it.**
- **`overlays/`** holds per-environment values as kustomize patches: your pool,
  template, and network UUIDs. Create one overlay per environment; it is meant
  for local use and is not distributed.

!!! warning "The class and its machine templates are separate"

    A `ClusterClass` references its `XOMachineTemplate`s **by name**, and those
    live in `machinetemplates/`, a different kustomization from
    `clusterclass/`. Apply both, or the class has dangling references.
    Applying the whole `base/` includes them (and the example cluster too).

## Prerequisite: the `ClusterTopology` feature gate

Managed topologies are behind the `ClusterTopology` feature gate, and
`clusterctl init` **disables it by default**. Export it *before* initializing —
otherwise the management cluster rejects every `ClusterClass` and `…Template`:

```
spec: Forbidden: can be set only if the ClusterTopology feature flag is enabled
```

```bash
export CLUSTER_TOPOLOGY=true
clusterctl init --bootstrap kubeadm --control-plane kubeadm \
  --infrastructure vates:v1.1.0-rc4
```

The gate must be on for the CAPI core, control plane, and bootstrap controllers:
each checks it for its own `…Template` kinds. The flat `clusterctl generate` flow
does **not** need it.

## Create a cluster from a class

Register the class once — it is cluster-scoped — then apply your overlay:

```bash
# 1. The class and the templates it references (once per management cluster)
kubectl apply -k templates/kubeadm/base/clusterclass/
kubectl apply -k templates/kubeadm/base/machinetemplates/

# 2. Your overlay: the Cluster plus patches with your XO values
kubectl apply -k templates/kubeadm/overlays/my-env/
```

The `Cluster` itself is small — the whole point of a class:

```yaml
apiVersion: cluster.x-k8s.io/v1beta2
kind: Cluster
metadata:
  name: my-cluster
spec:
  topology:
    classRef:
      name: vates-almalinux-prefilled
    version: v1.36.1
    variables:
      - name: controlPlaneEndpoint
        value:
          host: 192.168.1.240
          port: 6443
          subnet: 16
      - name: vmNamePrefix
        value: my-cluster
      - name: controlPlaneLB
        value: kube-vip
    controlPlane:
      replicas: 3
    workers:
      machineDeployments:
        - class: worker-almalinux-prefilled
          name: worker-md-0
          replicas: 2
```

The overlay is where your UUIDs go, because `base/` is not editable. See
[Cluster templates](../reference/templates.md) and
`templates/kubeadm/README.md` for the complete overlay files.

## Edit a managed topology and watch the rollout

Once the cluster is up, the `Cluster` is the API you drive; the derived objects
are managed for you. What changes depends on what you edit:

| Edit | What happens |
|---|---|
| `spec.topology.controlPlane.replicas` / a `machineDeployments[].replicas` | Scale up or down; new machines are stamped from the same template. |
| `spec.topology.version` | A rolling upgrade: the control plane rolls first, then each worker `MachineDeployment`. |
| A `topology.variables` value that patches a template | Cluster API rotates the affected template to a new revision and rolls the machines that use it. |
| The `ClusterClass` or one of its templates | Every topology built from that class picks up the change on its next reconcile and rolls. |

Watch it happen at both levels — the abstract topology and the concrete objects:

```bash
kubectl get cluster my-cluster -w
kubectl get machinedeployment -w      # one per worker group
kubectl get machines -w               # the individual nodes being replaced
```

Because machine templates are immutable, Cluster API does not edit one in place
when it changes: it creates a new revision (the derived objects carry a
content-hash suffix in their names) and reconciles toward it. The effect is the
same as any other roll — one new machine per step, old ones drained — and it
follows `maxUnavailable` exactly as described in
[Troubleshooting](../operations/troubleshooting.md#the-cluster-reports-available-false-but-it-is-up).

!!! note "Changing the class is not an in-place operation"

    `classRef.name` identifies the cluster's shape at creation. To move an
    existing cluster to a different class, create a new cluster and migrate
    workloads; do not expect an edit of `classRef` to reshape it.

!!! note "Talos has no ClusterClass here"

    The Talos flow ships flat templates only: the Talos control plane provider
    does not support managed topologies. See the
    [Talos guide](providers/talos/index.md).

## Where next

- The bootstrap/control plane flow behind these classes:
  [kubeadm](providers/kubeadm.md).
- The template layer in full: [Cluster templates](../reference/templates.md).
- The class's infrastructure pieces:
  [XOClusterTemplate](../reference/crds/xoclustertemplate.md) and
  [XOMachineTemplate](../reference/crds/xomachinetemplate.md).
- The endpoint a class variable feeds: [VIP](vip.md).
