# Releasing vates-capi

This guide describes how to publish a release of the vates infrastructure
provider so that users can install and use it with `clusterctl` **without
cloning the repository**.

## How `clusterctl` consumes a provider

`clusterctl` fetches everything it needs from a **provider repository** — for
us, a GitHub release. A provider release must contain:

| Asset | Required | Used for |
|---|---|---|
| `infrastructure-components.yaml` | yes | `clusterctl init` (CRDs, RBAC, controller) |
| `metadata.yaml` | yes | contract validation (`v1beta2`) and version resolution |
| `cluster-template.yaml` | recommended | default template for `clusterctl generate cluster` |

Users register the provider once in `~/.config/cluster-api/clusterctl.yaml`:

```yaml
providers:
  - name: vates
    url: https://github.com/vatesfr/cluster-api-provider-vates/releases/latest/infrastructure-components.yaml
    type: InfrastructureProvider
```

Then, without pulling this repository:

```bash
clusterctl init --infrastructure vates:v0.1.0
clusterctl generate cluster my-cluster --infrastructure vates:v0.1.0 \
  --control-plane-machine-count 3 --worker-machine-count 2
```

## Steps

### 1. Build and push the controller image

The controller image referenced by `infrastructure-components.yaml` must be
pullable by the management cluster:

```bash
export IMG=ghcr.io/vatesfr/cluster-api-provider-vates:v0.1.0
make docker-build docker-push IMG=$IMG
```

### 2. Generate the release artifacts

```bash
make release-manifests IMG=$IMG
```

This produces `dist/infrastructure-components.yaml`, `dist/metadata.yaml` and
`dist/cluster-template.yaml` (all three required by `clusterctl`).

### 3. Create the GitHub release

- Tag the release with a **valid semantic version** (e.g. `v0.1.0`).
- Attach the three files from `dist/` as release assets:
  `infrastructure-components.yaml`, `metadata.yaml`, `cluster-template.yaml`.

## Local development (no release)

While developing, `clusterctl` reads from the local overrides layer instead of
a published release:

```bash
make -f Makefile.dev dev-overrides     # refresh overrides from dist/
```

This regenerates `dist/` and copies the three assets into
`~/.config/cluster-api/overrides/infrastructure-vates/v0.1.0/`.

## Caveats

- **Version detection uses the Go proxy.** `clusterctl` detects available
  provider versions via `proxy.golang.org`. While the module is not published
  there, version detection may fail; workarounds: set `GOPROXY=off` /
  `GOPROXY=direct`, or pin the version explicitly (`vates:v0.1.0`).
- **Not in the pre-defined provider list.** Until vates is added to
  `clusterctl`'s built-in provider list (PR to `kubernetes-sigs/cluster-api`),
  users must register the provider in `clusterctl.yaml` as shown above.