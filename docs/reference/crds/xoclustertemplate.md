# XOClusterTemplate

A stencil for [`XOCluster`](xocluster.md), used by
[ClusterClass](../../guides/clusterclass.md) managed topologies. It wraps an
`XOCluster` spec under `spec.template.spec`, exactly like the built-in
infrastructure templates do for other providers.

One `XOClusterTemplate` describes the cluster-wide settings for **every**
workload cluster built from the `ClusterClass` that references it. Instead of
writing an `XOCluster` by hand, you let Cluster API stamp one out of the
template when a managed-topology `Cluster` is created.

## `spec.template.spec`

The outer object is the template; the spec you care about is nested one level
down. It is a verbatim `XOCluster.spec`:

```yaml
apiVersion: vates.infrastructure.cluster.x-k8s.io/v1beta2
kind: XOClusterTemplate
metadata:
  name: almalinux-cluster
  namespace: default
spec:
  template:
    spec:                      # <- XOCluster.spec goes here
      injectSSHKeys: true
      addons:
        cni: cilium
```

| Field | Type | Description |
|---|---|---|
| `spec.template.spec` | `XOClusterSpec` | The full [`XOCluster` spec](xocluster.md#spec). |

Every field documented on [XOCluster](xocluster.md) is valid here, unchanged.
Only the wrapping differs; the extra `template` level exists because Cluster
API's template contract is uniform across providers, so the same topology code
can read an infrastructure template without knowing which provider it is.

!!! info "The status is not templated"

    `XOClusterTemplate` has no meaningful status: it is a stencil, not a
    reconciler. The derived `XOCluster` carries the `ready`, endpoint, and
    condition fields.

## How a `ClusterClass` references it

A `ClusterClass` points at the template through
`spec.infrastructure.templateRef`. The shipped
`vates-almalinux-prefilled` class does:

```yaml
spec:
  infrastructure:
    templateRef:
      apiVersion: vates.infrastructure.cluster.x-k8s.io/v1beta2
      kind: XOClusterTemplate
      name: almalinux-cluster
```

The `ClusterClass` also declares the topology **variables**, and patches them
into the template. The shipped class adds the control plane endpoint and the
load balancer from two variables:

```yaml
patches:
  - name: controlPlaneEndpointPatch
    definitions:
      - selector:
          apiVersion: vates.infrastructure.cluster.x-k8s.io/v1beta2
          kind: XOClusterTemplate
          matchResources:
            infrastructureCluster: true
        jsonPatches:
          - op: add
            path: /spec/template/spec/controlPlaneEndpoint
            valueFrom:
              variable: controlPlaneEndpoint
  - name: kubevipPatch
    definitions:
      - selector:
          apiVersion: vates.infrastructure.cluster.x-k8s.io/v1beta2
          kind: XOClusterTemplate
          matchResources:
            infrastructureCluster: true
        jsonPatches:
          - op: add
            path: /spec/template/spec/controlPlaneLB
            valueFrom:
              variable: controlPlaneLB
```

The template ships placeholders and defaults; the variables fill in the
per-cluster values, so one template serves many clusters. In a `Cluster`'s
`spec.topology.variables`, the user sets `controlPlaneEndpoint` and
`controlPlaneLB`; the topology controller applies them through these patches.

!!! tip "The selector matches every `XOClusterTemplate`"

    `matchResources.infrastructureCluster: true` selects whatever template the
    class's `infrastructure.templateRef` names. A class can still patch a
    specific template by name, but for the single-cluster-template layout this
    broad selector is what you want.

## Editing the template and the rollout

Cluster API tracks the templates a `ClusterClass` references. When one changes,
the topology controller notices and reconciles every `Cluster` built from that
class.

For an `XOClusterTemplate` the effect is on the *infrastructure cluster*
object, not on individual machines:

1. You edit the referenced `XOClusterTemplate` (or the `ClusterClass`'s
   `infrastructure.templateRef`).
2. The topology controller updates the derived `XOCluster` for each `Cluster`
   in the class.
3. The vates controller reconciles the `XOCluster` as usual.

Because `XOCluster` fields are cluster-wide, most are applied live rather than
by replacing machines: `injectSSHKeys` and `addons` are read when the
`ClusterResourceSet`s are rendered, and `controlPlaneEndpoint` is published in
status. Machine rollout is driven by the **machine** templates, not by this
one — see [XOMachineTemplate](xomachinetemplate.md#rolling-changes).

!!! warning "Pick the template up before you create the `Cluster`"

    A `Cluster` is built from the `ClusterClass` as it exists at creation time.
    Change the class or its templates *before* creating the clusters that use
    them; retrofitting a change is possible through the topology reconcile, but
    it is far easier to get right up front.

## Where next

- The fields you can set: [XOCluster](xocluster.md).
- The class that references this template: [ClusterClass](../../guides/clusterclass.md).
- The per-machine stencils: [XOMachineTemplate](xomachinetemplate.md).
