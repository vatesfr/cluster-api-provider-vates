# Talos installation

The Talos providers add `TalosControlPlane` and `TalosConfig` resources, and
they need RBAC access to vates-capi's `XOMachineTemplate` resources, which the
default bindings do not grant.

Installing the Talos flow is therefore two things: the providers themselves, and
one extra RBAC binding. The vates infrastructure provider is installed exactly
as for kubeadm.

## Install the Talos providers

`TalosControlPlane` and `TalosConfig` come from the Talos control plane (CACPPT)
and bootstrap (CABPT) providers. `clusterctl` knows them by default:

```bash
clusterctl init --bootstrap talos --control-plane talos
```

This installs Cluster API core plus CACPPT and CABPT. It does **not** install
vates-capi.

If you have not installed vates-capi yet, do it in the same command:

```bash
clusterctl init --bootstrap talos --control-plane talos --infrastructure vates
```

`clusterctl` needs to know where the vates provider lives; see
[Installation](../../../getting-started/installation.md) and the
[Talos templates README](https://github.com/vatesfr/cluster-api-provider-vates/blob/main/templates/talos/README.md)
for registering it through a local override or a published release.

!!! warning "`clusterctl` and the credentials secret"

    vates-capi reads its Xen Orchestra credentials from a `Secret` in its own
    namespace and exits at startup if it is missing. Create it **before**
    installing the provider:

    ```bash
    kubectl create namespace capi-system
    kubectl create secret generic xo-credentials -n capi-system \
      --from-literal=url="https://<your-xoa>" \
      --from-literal=token="<your-xo-token>" \
      --from-literal=insecure="true"
    ```

    Details: [Prerequisites](../../../getting-started/prerequisites.md).

## Why the RBAC binding is separate

The control plane provider has to read the `XOMachineTemplate` referenced by the
`TalosControlPlane`, then create the `Machine`s that stamp machines from it. In
Cluster API, permission to touch another provider's resources is always granted
explicitly. The default vates install grants it to the **kubeadm** control plane
only.

That grant lives in `config/rbac/kcp_aggregate_role.yaml`: a `ClusterRole`
labeled

```
kubeadm.controlplane.cluster.x-k8s.io/aggregate-to-manager: "true"
```

The kubeadm control-plane manager **aggregates** every `ClusterRole` carrying
that label, so the permissions flow to it with no explicit binding. Talos has no
such aggregation, which is why CACPPT/CABPT get nothing — and why the Talos
control plane cannot read `XOMachineTemplate` and never creates a control plane
`Machine` on its own.

The Talos binding could not simply be added to the default RBAC, either: a
cross-namespace `ClusterRoleBinding` written by hand is silently rewritten by
`clusterctl` (it changes the subject namespaces to the provider namespace),
which breaks it. So it ships as a **separate kustomization** you apply yourself.

For the reasoning about aggregation and cross-namespace bindings, see
[How they fit together](../../../concept/how-they-link.md); the binding lives
in `config/rbac/talos/`.

## Apply the Talos RBAC binding

From a clone of the repository, apply the binding:

```bash
kubectl apply -k config/rbac/talos
```

This creates one `ClusterRoleBinding`, named
`vates-capi-talos-bootstrap-control-plane` (the `vates-capi-` prefix comes from
the kustomization's `namePrefix`). It:

- references the **`vates-capi-manager-role` `ClusterRole`** — the same role the
  vates controller uses, which grants `create`, `delete`, `get`, `list`,
  `patch`, `update`, `watch` on `xomachines` and `xomachinetemplates` (and the
  other vates resources);
- binds it to the `default` `ServiceAccount` in **`cacppt-system`** and in
  **`cabpt-system`**, the namespaces the Talos providers run in.

That is what lets CACPPT read the `XOMachineTemplate` and create the control
plane `Machine`s, mirroring what the kubeadm aggregate role does for KCP.

!!! note "The binding is not part of the default install"

    `config/rbac/talos/` is deliberately **excluded** from the default
    kustomize build. `kubectl apply -k` on it is a required step of the Talos
    flow, not something the provider release does for you.

## Verify

Check that all the providers are running:

```bash
kubectl get deployment -n capi-system -l cluster.x-k8s.io/provider
```

Check the vates controller and its logs:

```bash
kubectl get deployment -n capi-system vates-capi-controller-manager
kubectl logs -n capi-system deployment/vates-capi-controller-manager -c manager -f
```

The vates controller handles both the kubeadm and the Talos flows without any
change, so there is nothing else to configure on it.

## Where next

- Build the cluster: [Building a Talos cluster](cluster.md).
- General provider installation options:
  [Installation](../../../getting-started/installation.md).
- The templates and their variables:
  [Cluster templates](../../../reference/templates.md).
