# Developing vates-capi

This guide is for **contributors** who build the provider from source and run
the controller against their own management cluster. End users who install a
published release should follow [README.md](README.md) instead (it uses the
official image via `dist/install.yaml`).

## Prerequisites

- **Go 1.25.8** (see `go.mod`)
- **podman or docker** — auto-detected by the Makefiles (`CONTAINER_TOOL`)
- **kubectl**
- A management cluster: **kind** or **k3s**
- **clusterctl** — only for the clusterctl flow (optional)
- Xen Orchestra access: VM template UUID, pool UUID, network UUID

Alternatively, use the **DevContainer** (`.devcontainer/`): `golang:1.25` with
docker-in-docker, plus kind, kubectl and kubebuilder preinstalled. Open the
repo with "Reopen in Container" and everything above is already set up.

## One-time management cluster setup

```bash
# 1. Management cluster (kind; or use an existing k3s)
kind create cluster

# 2. CAPI (kubeadm flow; use --bootstrap talos --control-plane talos for Talos)
clusterctl init --bootstrap kubeadm --control-plane kubeadm

# 3. XO credentials (the controller reads this secret: --xoa-secret-name=xo-credentials)
kubectl create secret generic xo-credentials -n capi-system \
  --from-literal=url="https://<your-xoa>" \
  --from-literal=token="<your-xo-token>" \
  --from-literal=insecure="true"
```

## First deploy (once per cluster)

The dev image is tagged `:dev` by default (override with `IMG=`). It is built
locally and **loaded onto the cluster nodes** — never pushed to a registry.

```bash
# 1. Build the controller image
make -f Makefile.dev build

# 2. Load it into the cluster
make -f Makefile.dev push        # kind (cluster named "kind")
# make -f Makefile.dev push-k3s  # k3s (see the note below if k3s ctr needs root)

# 3. Deploy CRDs + RBAC + controller with your dev image
make deploy IMG=ghcr.io/vatesfr/cluster-api-provider-vates:dev
```

> `make deploy` runs `kustomize edit set image`, which rewrites
> `config/manager/kustomization.yaml`. Revert it if you don't want to commit
> the change: `git checkout config/manager/kustomization.yaml`.

Verify:

```bash
kubectl -n capi-system get deployment vates-capi-controller-manager
kubectl -n capi-system logs -l app.kubernetes.io/name=vates-capi -f
```

## Dev loop (after each code change)

One-shot:

```bash
make -f Makefile.dev dev        # build -> push -> ensure-image
```

Or step by step:

| Target | What it does |
|---|---|
| `make -f Makefile.dev build` | Build the `:dev` image |
| `make -f Makefile.dev push` | Load it into kind (`push-k3s` for k3s) |
| `make -f Makefile.dev ensure-image` | Point the deployment at the image, rollout restart, verify the running image |
| `make -f Makefile.dev restart` | Rollout restart only |
| `make -f Makefile.dev deploy-image` | Print the image currently configured in the deployment |

Why this works: the image is loaded directly onto the nodes, so the stable
`:dev` tag plus `imagePullPolicy: IfNotPresent` is enough — the rollout
restart makes kubelet schedule a new pod, which picks up the freshly loaded
image. `ensure-image` prints the image the pod actually runs so you can tell
a stale image from a fresh one.

For k3s, the loop is:

```bash
make -f Makefile.dev build
make -f Makefile.dev push-k3s
make -f Makefile.dev ensure-image
```

> If `k3s ctr` requires root, don't `sudo make` (a rootful podman can't see
> images built rootless). Import the saved tarball manually instead:
> `sudo k3s ctr images import /tmp/vates-capi-dev.tar`.

**Fast iteration without an image:** `make run` runs the controller from your
host against the current kubeconfig (no build/push needed).

## clusterctl flow (optional)

To drive clusters through `clusterctl` with your local build, refresh the
local overrides layer from `dist/` (and create `~/.config/cluster-api/clusterctl.yaml`
if missing):

```bash
make -f Makefile.dev dev-overrides
```

Then register the provider in `clusterctl.yaml` (see the README section
*Installing the provider with clusterctl*) and use:

```bash
clusterctl init --infrastructure vates:v0.1.0
clusterctl generate cluster my-cluster --infrastructure vates:v0.1.0 \
  --control-plane-machine-count 3 --worker-machine-count 2
```

## Code changes

After editing `*_types.go` or kubebuilder markers:

```bash
make manifests   # Regenerate CRDs / RBAC
make generate    # Regenerate DeepCopy methods
```

Quality gates:

```bash
make lint-fix    # golangci-lint (auto-fix)
make test        # Unit tests (envtest: real K8s API + etcd)
```

E2E tests run in an **isolated** kind cluster (`vates-capi-test-e2e`,
created and deleted automatically):

```bash
make test-e2e
```

> The e2e suite hardcodes `docker` as the container tool — podman-only
> machines need docker for this target.

## Debugging

```bash
kubectl -n capi-system logs -l app.kubernetes.io/name=vates-capi -f
kubectl get machines -w
kubectl get xomachines -w
```

## Undeploy

```bash
make undeploy
```

## See also

- [README.md](README.md) — end-user installation and cluster creation
- [RELEASING.md](RELEASING.md) — publishing a release
- [templates/README.md](templates/README.md) — cluster templates (kubeadm / Talos)
