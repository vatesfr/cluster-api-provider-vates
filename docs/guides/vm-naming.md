# VM naming

By default the provider names a VM from the deprecated `XOMachine`
`spec.namePrefix`. To follow your own naming convention instead, set
`spec.vmNaming` on the [`XOCluster`](../reference/crds/xocluster.md).

## How a VM is named

With `vmNaming`, the name of a VM is the prefix of its role followed by the ID
of its Cluster API `Machine`:

```
<controlPlanePrefix><ID>    control plane VMs
<workerPrefix><ID>          worker VMs
```

- **The prefixes are taken verbatim.** The provider imposes no convention: no
  separator, case, component order or length. Put the separator before the
  ID in the prefix itself (`...-CP-`, not `...-CP`). Validate your own
  convention in your ClusterClass variables (see the example below).
- **The ID** is the 5-character random suffix the `KubeadmControlPlane`, the
  `MachineSet` and the Talos control plane provider end the `Machine` names they
  generate with. A `Machine` name that does not end with one, e.g. `vmn-0` from
  a provider that names its Machines `<cluster>-<index>`, is used whole. Either
  way the ID never changes and no other `Machine` of the namespace has it.
- **The role** comes from the `cluster.x-k8s.io/control-plane` label Cluster API
  puts on control plane machines.

For example, with `controlPlanePrefix: CAPI-CP-`:

| Machine | VM |
|---|---|
| `my-cluster-x8kz2-7n62g` | `CAPI-CP-7n62g` |
| `vmn-0` | `CAPI-CP-vmn-0` |

The random suffix is recognized only after a dash, or at the end of a name
Cluster API cut to 63 characters, and only when its characters come from the
alphabet of generated names (no vowels, no `0`, `1` or `3`). A name such as
`demo-node1` is therefore used whole rather than cut to `node1`, which another
`Machine` could end with too.

!!! note "Custom `machineNaming` templates"

    A `machineNaming.template` on a `KubeadmControlPlane` or a
    `MachineDeployment` that does not end with `-{{ .random }}` gives Machine
    names without a trailing random suffix: the VM is then named after the whole
    `Machine` name.

## The name never changes

The name is computed once, recorded in the `XOMachine` `status.vmName` **before**
the VM is created, and never looked at again once the VM exists. The VM is
tracked by its UUID, not by its name. As a consequence:

| Change | Effect |
|---|---|
| Edit `vmNaming` (or the ClusterClass variables that feed it) | The `XOCluster` is updated in place: **no machine is rolled out and no VM is renamed.** Only VMs created afterwards (scale up, remediation, upgrade) get the new names, so the cluster mixes both namings until its VMs are replaced. |
| Rename a VM in Xen Orchestra | Kept. The provider never renames a VM back. `status.vmName` keeps the name given at creation. |
| Replace a machine (upgrade, remediation, scale down then up) | The new `Machine` has a new name, so its VM has a new name. The old VM is deleted with its name. |

`vmNaming` lives on the `XOCluster` rather than on the `XOMachineTemplate` for
the first row: with a ClusterClass, changing a field of an `XOMachineTemplate`
creates a new template and rolls out every machine, while the `XOCluster` is
patched in place.

## Duplicate names

Xen Orchestra does not enforce unique VM names. If another VM, not created for
this machine, already has the name, the VM is created anyway, as before, and a
`Warning` event `DuplicateVMName` is emitted on the `XOMachine`:

```bash
kubectl get events --field-selector reason=DuplicateVMName
```

## Example: a naming convention in a ClusterClass

This example builds names of the form `<prefix>-<role>-<ID>`:

```
CAPI-CP-7n62g
CAPI-WORKER-48gx5
```

The components are ClusterClass variables, declared once and combined by a
single patch on the `XOClusterTemplate`. Only the naming parts are shown; keep
the rest of your class unchanged. To follow a convention with more components,
add one property per component and join them in the template.

```yaml
apiVersion: cluster.x-k8s.io/v1beta2
kind: ClusterClass
metadata:
  name: vates-almalinux-fromscratch
spec:
  variables:
    - name: vmNaming
      required: true
      schema:
        openAPIV3Schema:
          type: object
          required: [prefix, controlPlaneRole, workerRole]
          properties:
            # Validate your own convention here: enum, pattern, maxLength...
            prefix: { type: string, minLength: 1 }
            controlPlaneRole: { type: string, minLength: 1 }
            workerRole: { type: string, minLength: 1 }
  patches:
    - name: vmNaming
      definitions:
        - selector:
            apiVersion: vates.infrastructure.cluster.x-k8s.io/v1beta2
            kind: XOClusterTemplate
            matchResources:
              infrastructureCluster: true
          jsonPatches:
            - op: add
              path: /spec/template/spec/vmNaming
              valueFrom:
                template: |
                  {{- $n := .vmNaming -}}
                  controlPlanePrefix: {{ printf "%s-%s-" $n.prefix $n.controlPlaneRole | quote }}
                  workerPrefix: {{ printf "%s-%s-" $n.prefix $n.workerRole | quote }}
  # infrastructure, controlPlane, workers: unchanged
```

`| quote` keeps every prefix a string: Cluster API reads the rendered template as
YAML, where an unquoted value could be read as a number or a boolean.

!!! warning "ClusterClass already in use"

    This example declares the variable `required: true`, which suits a new
    ClusterClass. For a class that existing Clusters already use, see
    [Adding the naming to a ClusterClass already in use](#adding-the-naming-to-a-clusterclass-already-in-use).

The values are set once per cluster:

```yaml
apiVersion: cluster.x-k8s.io/v1beta2
kind: Cluster
metadata:
  name: my-cluster
spec:
  topology:
    classRef:
      name: vates-almalinux-fromscratch
    controlPlane:
      replicas: 3
    workers:
      machineDeployments:
        - class: worker-almalinux-fromscratch
          name: md-0
          replicas: 2
    variables:
      - name: vmNaming
        value:
          prefix: CAPI
          controlPlaneRole: CP
          workerRole: WORKER
```

Resulting VMs:

| Machine | VM |
|---|---|
| `my-cluster-x8kz2-7n62g` | `CAPI-CP-7n62g` |
| `my-cluster-x8kz2-k4xzb` | `CAPI-CP-k4xzb` |
| `my-cluster-x8kz2-q9tmw` | `CAPI-CP-q9tmw` |
| `my-cluster-md-0-hdvzv-5bxt9-48gx5` | `CAPI-WORKER-48gx5` |
| `my-cluster-md-0-hdvzv-5bxt9-h2pvr` | `CAPI-WORKER-h2pvr` |

Without a ClusterClass, set the prefixes directly on the `XOCluster`:

```yaml
apiVersion: vates.infrastructure.cluster.x-k8s.io/v1beta2
kind: XOCluster
metadata:
  name: my-cluster
spec:
  vmNaming:
    controlPlanePrefix: CAPI-CP-
    workerPrefix: CAPI-WORKER-
```

## Migrating from `namePrefix`

`namePrefix` is deprecated and will be removed in v1.2. Until then:

| `XOCluster` `vmNaming` | `XOMachine` `namePrefix` | VM name |
|---|---|---|
| unset | set | Built from `namePrefix`, as before (see below). |
| set | unset | Built from `vmNaming`. |
| set | set | Built from `vmNaming`; `namePrefix` is ignored and a `NamePrefixIgnored` warning event is emitted. |
| unset | unset | No VM is created; the `XOMachine` gets a `VMNameUnresolved` condition. |

To migrate a running cluster without a ClusterClass, set `vmNaming` on its
`XOCluster`: no machine is rolled out.

Leave `namePrefix` on the `XOMachineTemplate`s for now. Removing it changes the
templates, and with a ClusterClass that rolls out every machine. Remove it at a
moment of your choosing, for example during a Kubernetes upgrade that replaces
the VMs anyway.

### Adding the naming to a ClusterClass already in use

Do **not** add the variable of the example above, `required: true`, to a
ClusterClass that existing Clusters already use. Cluster API validates the
variables of every Cluster of the class on each reconcile: a Cluster that does
not set a required variable has its topology blocked. No VM is replaced, but the
Cluster no longer scales nor upgrades until the variable is set.

Declare the variable optional, and enable the patch only on the Clusters that
set it:

```yaml
spec:
  variables:
    - name: vmNaming
      required: false
      schema:
        openAPIV3Schema:
          type: object
          required: [prefix, controlPlaneRole, workerRole]
          properties:
            prefix: { type: string, minLength: 1 }
            controlPlaneRole: { type: string, minLength: 1 }
            workerRole: { type: string, minLength: 1 }
  patches:
    - name: vmNaming
      enabledIf: '{{ if .vmNaming }}true{{ end }}'
      definitions:
        # same selector and jsonPatches as in the example above
```

Then set the variable on each Cluster, at your own pace. Each Cluster's
`XOCluster` is updated in place and no machine is rolled out. The Clusters that
do not set it keep the `namePrefix` naming. Once every Cluster of the class sets
the variable, you can make it `required: true`.

### What `namePrefix` actually produces

`namePrefix` is not used verbatim. The provider strips a trailing `-cp` or
`-worker` (kept as the role), then inserts the **cluster name with no
separator**, then the role and the last dash-separated segment of the `Machine`
name:

| `namePrefix` | Cluster | Machine | VM |
|---|---|---|---|
| `PFT--cp` | `demo3` | `demo3-cp-7n62g` | `PFT-demo3-cp-7n62g` |
| `my-cluster-cp` | `my-cluster` | `my-cluster-x8kz2-7n62g` | `my-clustermy-cluster-cp-7n62g` |
