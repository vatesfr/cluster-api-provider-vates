# Development

Building the provider from source and running it against your own management
cluster.

This page is for **contributors**. If you only want to *use* a published release,
follow [Installation](../getting-started/installation.md) instead — it pulls the
official image. Here you build the controller yourself and load it onto a
cluster you control.

## Prerequisites

- **Go 1.25.8** — the toolchain pinned in `go.mod`.
- **podman or docker** — the Makefiles auto-detect one as `CONTAINER_TOOL`.
- **`kubectl`**, and a management cluster: **kind** or **k3s**.
- **`clusterctl`** — optional, only for the clusterctl flow below.
- Xen Orchestra access: a VM template UUID, a pool UUID, and a network UUID.

### The DevContainer

If you would rather not install any of this, the repository ships a
[DevContainer](https://containers.dev/) under `.devcontainer/`: `golang:1.25`
with Docker-in-Docker, plus kind, kubectl and kubebuilder. Open the repo in your
editor and choose **Reopen in Container**; everything above is already set up.

## One-time management cluster setup

Do this once per management cluster:

```bash
# 1. A management cluster (kind here; or use an existing k3s)
kind create cluster

# 2. Cluster API plus the bootstrap and control plane providers
clusterctl init --bootstrap kubeadm --control-plane kubeadm

# 3. The XO credentials secret the controller reads at startup
kubectl create namespace capi-system
kubectl create secret generic xo-credentials -n capi-system \
  --from-literal=url="https://<your-xoa>" \
  --from-literal=token="<your-xo-token>" \
  --from-literal=insecure="true"
```

For Talos, replace the two flags of step 2 with `--bootstrap talos
--control-plane talos`, and apply the extra RBAC binding
(`kubectl apply -k config/rbac/talos`) so the Talos providers can read
`XOMachineTemplate`.

## First deploy

The development image is tagged `:dev` and is **loaded onto the cluster nodes**,
never pushed to a registry. Override the tag with `IMG=` if you like.

```bash
# 1. Build the controller image
make -f Makefile.dev build

# 2. Load it onto the cluster
make -f Makefile.dev push        # kind (cluster named "kind")
# make -f Makefile.dev push-k3s  # k3s (see the note below)

# 3. Deploy CRDs, RBAC and the controller with your dev image
make deploy IMG=ghcr.io/vatesfr/cluster-api-provider-vates:dev
```

!!! warning "`make deploy` edits a tracked file"

    `make deploy` runs `kustomize edit set image`, which rewrites
    `config/manager/kustomization.yaml`. Revert it if you do not want to commit
    that change:

    ```bash
    git checkout config/manager/kustomization.yaml
    ```

Verify the controller came up:

```bash
kubectl -n capi-system get deployment vates-capi-controller-manager
kubectl -n capi-system logs -l app.kubernetes.io/name=vates-capi -f
```

## The dev loop

After each code change, one target does the whole cycle:

```bash
make -f Makefile.dev dev        # build -> push -> ensure-image
```

Or step by step:

| Target | What it does |
|---|---|
| `make -f Makefile.dev build` | Build the `:dev` image. |
| `make -f Makefile.dev push` | Load it into kind (`push-k3s` for k3s). |
| `make -f Makefile.dev ensure-image` | Point the Deployment at the image, restart, verify. |
| `make -f Makefile.dev restart` | Rollout restart only. |
| `make -f Makefile.dev deploy-image` | Print the image the Deployment is configured with. |

Why this works: the image is loaded directly onto the nodes, so a stable `:dev`
tag plus `imagePullPolicy: IfNotPresent` is enough — the rollout restart makes
kubelet schedule a new pod, which picks up the freshly loaded image.
`ensure-image` prints the image the pod actually runs, so you can tell a stale
image from a fresh one.

For k3s the loop is the same with `push-k3s`:

```bash
make -f Makefile.dev build
make -f Makefile.dev push-k3s
make -f Makefile.dev ensure-image
```

!!! note "k3s and root"

    If `k3s ctr` needs root, do **not** run `sudo make` — a rootful podman cannot
    see images built rootless. Import the saved tarball manually instead:

    ```bash
    sudo k3s ctr images import /tmp/vates-capi-dev.tar
    ```

## Fast iteration with `make run`

When you do not need the image at all, run the controller straight from your
host against the current kubeconfig:

```bash
make run
```

This runs `go run ./cmd/main.go`, so it also re-runs `manifests`, `generate`,
`fmt` and `vet` first. It is the quickest way to watch a change in the logs, but
you must keep your shell attached.

## The optional clusterctl flow

To drive clusters through `clusterctl` with your local build, refresh the local
overrides layer from `dist/` (and create
`~/.config/cluster-api/clusterctl.yaml` if it is missing):

```bash
make -f Makefile.dev dev-overrides
```

Then register the provider in `clusterctl.yaml` (see
[Installation](../getting-started/installation.md)) and use `clusterctl init` /
`clusterctl generate cluster` as usual. Note that `dist/` cannot be pointed at
directly: a `file://` URL needs a
`{basepath}/{provider-name}/{version}/` layout, which `dev-overrides` creates.

## After changing types

The CRDs and the deep-copy methods are generated, so a change to
`api/v1beta2/*_types.go` or to a kubebuilder marker is not complete until you
run:

```bash
make manifests   # regenerate CRDs and RBAC from markers
make generate    # regenerate DeepCopy methods
```

Run the quality gates before pushing:

```bash
make lint-fix    # golangci-lint, with auto-fixes
make test        # unit tests (see Testing)
```

## The Helm chart

The distributable chart lives in `dist/chart/`. It is generated by the kubebuilder
Helm plugin from the kustomize output (`dist/install.yaml`); never hand-edit the
generated CRDs or RBAC inside it.

Regenerate it with:

```bash
make helm-update-chart
```

The target rebuilds `dist/install.yaml`, regenerates the chart, and re-applies
`hack/helm-chart-manager-extras.patch` to `templates/manager/manager.yaml`. The
patch adds the vates-specific `extraArgs` / `extraEnvs` plumbing that the plugin
does not generate.

!!! danger "Never pass `--force`"

    `make helm-update-chart` intentionally regenerates **without** `--force`, so
    `values.yaml` and `.github/workflows/test-chart.yml` are preserved. Passing
    `--force` overwrites `values.yaml` and wipes the vates-specific values.
    `Chart.yaml` is never overwritten by the plugin, so bump its `version` and
    `appVersion` by hand.

The chart exposes these knobs under `manager`:

| Value | Purpose |
|---|---|
| `manager.args` | Full argument list (replaces the defaults). |
| `manager.extraArgs` | Arguments appended to the defaults. |
| `manager.env` | Environment variables. |
| `manager.envOverrides` | Extra env vars as a map. |
| `manager.extraEnvs` | Extra env vars as a `core/v1` list (with `value`/`valueFrom`). |
| `manager.extraVolumeMounts` / `manager.extraVolumes` | Extra mounts and volumes. |

To inject an external kubeconfig (the manager honors `KUBECONFIG` and
`--kubeconfig`):

```yaml
manager:
  envOverrides:
    KUBECONFIG: /etc/kubeconfig/config
  extraVolumes:
    - name: kubeconfig
      secret:
        secretName: my-kubeconfig
  extraVolumeMounts:
    - name: kubeconfig
      mountPath: /etc/kubeconfig
      readOnly: true
```

A CI job (`chart-up-to-date`) runs `make helm-update-chart` and fails if `dist/`
differs, so the chart cannot silently drift from the API types.

## Debugging

```bash
kubectl -n capi-system logs -l app.kubernetes.io/name=vates-capi -f
kubectl get machines -w
kubectl get xomachines -w
```

For what the symptoms mean once a cluster is running, see
[Troubleshooting](../operations/troubleshooting.md).

## Undeploy

```bash
make undeploy
```

That deletes the CRDs, RBAC and controller installed by `make deploy`. Workload
clusters and their VMs are not touched by `undeploy`; delete the clusters first
if you want them gone.

## Where next

- How the pieces fit: [Architecture](architecture.md).
- The test suites and how to extend them: [Testing](testing.md).
- Publishing a release: [Releasing](../operations/releasing.md).
