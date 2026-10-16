# Installation

!!! warning "Experimental"

    The Vates providers — this infrastructure provider and the Vates
    bootstrap / control plane providers — are **experimental**: no support, not
    for production.

You install two separate things: **Cluster API with its bootstrap and control
plane providers**, and **vates-capi**. The first is generic; the second is the
infrastructure provider this documentation is about.

There are three ways to install vates-capi. Pick one — they all end up with the
same controllers running.

## Before anything: the credentials secret

As noted in [Prerequisites](prerequisites.md), the provider exits at startup if
the Xen Orchestra credentials secret is missing. Create it first:

```bash
kubectl create namespace capi-system
kubectl create secret generic xo-credentials -n capi-system \
  --from-literal=url="https://<your-xoa>" \
  --from-literal=token="<your-xo-token>" \
  --from-literal=insecure="true"
```

## Option A — `clusterctl` (recommended)

`clusterctl` is the Cluster API package manager. It installs providers, checks
their contract version, and knows how to generate cluster manifests later.

### 1. Install Cluster API and the kubeadm providers

```bash
clusterctl init --bootstrap kubeadm --control-plane kubeadm
```

This installs CAPI core plus the reference kubeadm bootstrap and control plane
providers. (For Talos, replace the two flags with `--bootstrap talos
--control-plane talos` — see the [Talos guide](../guides/providers/talos/index.md).)

### 2. Register vates-capi

`clusterctl` does not know this provider yet, so tell it where to find the
provider's `infrastructure-components.yaml`. Add this to
`~/.config/cluster-api/clusterctl.yaml` (note the path — *cluster-api*, not just
*clusterctl*):

```yaml
providers:
  - name: vates
    url: https://github.com/vatesfr/cluster-api-provider-vates/releases/latest/infrastructure-components.yaml
    type: InfrastructureProvider
```

### 3. Install it

```bash
clusterctl init --infrastructure vates:v1.1.0-rc4
```

Use the version you actually want; `clusterctl` reports the available ones if
the tag is unknown.

## Option B — a single `kubectl apply`

Every release ships a self-contained bundle containing the CRDs, RBAC, and the
controller Deployment:

```bash
kubectl apply -f https://raw.githubusercontent.com/vatesfr/cluster-api-provider-vates/refs/heads/main/dist/install.yaml
```

!!! warning "This does not install Cluster API itself"

    The bundle deploys **only** the vates infrastructure provider. You still
    need CAPI core and a bootstrap / control plane provider:

    ```bash
    clusterctl init --bootstrap kubeadm --control-plane kubeadm
    ```

    Without them there is nothing to create `Machine`s or bootstrap payloads.

## Option C — Helm

A Helm chart is generated for every release and lives in `dist/chart/` in the
repository:

```bash
helm install vates-capi ./dist/chart/ \
  --namespace capi-system --create-namespace
```

The chart exposes a few knobs under `manager` (extra arguments, environment
variables, extra volumes) and supports injecting an external kubeconfig through
`KUBECONFIG`. See [Development → Helm chart](../contributing/development.md) if
you are generating the chart yourself.

## Checking the installation

Whatever route you took, confirm the controller is running:

```bash
kubectl -n capi-system get deployment vates-capi-controller-manager
kubectl -n capi-system logs -l app.kubernetes.io/name=vates-capi -f
```

You should see the controller start and connect to Xen Orchestra. The custom
resources are now available:

```bash
kubectl get crds | grep vates.infrastructure.cluster.x-k8s.io
```

That lists `xoclusters`, `xoclustertemplates`, `xomachines`, and
`xomachinetemplates`.

## Installing from source (development)

If you are hacking on the provider rather than using a release, the developer
workflow is different: you build the image locally and load it onto the cluster
instead of pulling from a registry. It is documented in
[Contributing → Development](../contributing/development.md).

Next: [create your first cluster](quickstart.md).
