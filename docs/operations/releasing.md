# Releasing

How to publish a release that users can install with `clusterctl`, without
cloning the repository.

## What a `clusterctl` release must contain

`clusterctl` fetches everything it needs from a **provider repository** — for
this project, a GitHub release. A valid release carries three assets:

| Asset | Required | Used for |
|---|---|---|
| `infrastructure-components.yaml` | yes | `clusterctl init` — CRDs, RBAC and the controller. |
| `metadata.yaml` | yes | Contract validation and version resolution. |
| `cluster-template.yaml` | recommended | Default template for `clusterctl generate cluster`. |

Users register the provider once in `~/.config/cluster-api/clusterctl.yaml`:

```yaml
providers:
  - name: vates
    url: https://github.com/vatesfr/cluster-api-provider-vates/releases/latest/infrastructure-components.yaml
    type: InfrastructureProvider
```

From then on they install and generate clusters without cloning anything:

```bash
clusterctl init --infrastructure vates
clusterctl generate cluster my-cluster --infrastructure vates \
  --control-plane-machine-count 3 --worker-machine-count 2
```

## The contract version and `metadata.yaml`

Cluster API providers declare which **contract version** they implement, and
`clusterctl` refuses to install a provider whose contract it does not understand.
This provider implements `v1beta2`, and `metadata.yaml` records it per release
series:

```yaml
apiVersion: clusterctl.cluster.x-k8s.io/v1alpha3
kind: Metadata
releaseSeries:
  - major: 0
    minor: 1
    contract: v1beta2
  - major: 1
    minor: 0
    contract: v1beta2
  - major: 1
    minor: 1
    contract: v1beta2
```

Two rules follow from that file:

- **Add a `releaseSeries` entry** when you open a new minor (here, when `1.2`
  starts), with the contract it implements.
- **A contract bump is a breaking change** for users, not a routine release. It
  changes the shared object shapes, so it belongs in a major/minor release and
  in the *Breaking changes* section of the notes.

## Steps

### 1. Pick the version

Use a valid semantic version as the git tag; this page writes it `<version>`
(for example `v1.2.0`). **Alpha and beta** tags are published as GitHub
**prereleases**; anything else — release candidates included — becomes a normal
release and therefore the repository's *latest*, which is what `clusterctl`
resolves when no version is given.

The committed Helm chart (`dist/chart/Chart.yaml`) carries the same version, in
`version` and `appVersion`; set both to `<version>` before tagging — the chart is
committed and does not regenerate on its own. Regenerating it with the
kubebuilder Helm plugin does the same thing.

### 2. Build and push the controller image

The image referenced by `infrastructure-components.yaml` must be pullable by the
management cluster:

```bash
export IMG=ghcr.io/vatesfr/cluster-api-provider-vates:<version>
make docker-build docker-push IMG=$IMG
```

### 3. Generate the release artifacts

```bash
make release-manifests IMG=$IMG
```

This produces the three assets in `dist/`:

- `dist/infrastructure-components.yaml` — a `capi-system` namespace followed by
  the full kustomize build of `config/default` (CRDs, RBAC, manager Deployment);
- `dist/metadata.yaml` — copied from the repository root;
- `dist/cluster-template.yaml` — copied from
  `templates/kubeadm/base/clusterctl/almalinux-fromscratch.yaml`.

### 4. Create the GitHub release

Tag the release, attach the three files from `dist/`, and let GitHub generate the
notes:

```bash
gh release create <version> --verify-tag --generate-notes \
  dist/infrastructure-components.yaml \
  dist/metadata.yaml \
  dist/cluster-template.yaml
```

Most of this is automated in `.github/workflows/release.yml`, which triggers on
`v*` tags: it runs `make release-manifests` with the tag as the image tag, then
creates the release with `--generate-notes`, adding `--prerelease` only for
alpha and beta tags. The controller **image** is built and pushed by the
separate *Build and Push Image* workflow, which also runs on tag pushes.

### 5. How the changelog is generated

The notes group merged pull requests by **label**, from `.github/release.yml`:

| Category | Labels |
|---|---|
| ⚠️ Breaking changes | `breaking` |
| 🚀 Features | `feat` |
| 🐛 Bug fixes | `fix` |
| 📚 Documentation | `docs` |
| 🧹 Maintenance | `chore`, `refactor`, `build`, `ci`, `test` |

The labels themselves are applied automatically by the **PR labels** workflow
(`.github/workflows/pr-labels.yml`) from the conventional-commit prefix of the
pull request title. A title like `feat(xomachine): …` gets the `feat` label;
`fix: …` gets `fix`; a `!` before the colon (`feat!: …`) or a `BREAKING CHANGE`
note in the body adds `breaking`. `dependabot` and `renovate` authors are
excluded, and adding the `skip-changelog` label keeps a pull request out
entirely.

!!! tip "Write the PR title for the changelog"

    The release notes are only as good as the pull request titles. Since the
    label (and therefore the category) is derived from the title, use a
    conventional-commit title on every pull request you want in the notes.

## Local development (no release)

While developing, `clusterctl` can read from a local overrides layer instead of a
published release:

```bash
make -f Makefile.dev dev-overrides
```

That regenerates `dist/` and copies the three assets into
`~/.config/cluster-api/overrides/infrastructure-vates/<version>/`. `dist/`
itself cannot be pointed at directly: a `file://` URL needs a
`{basepath}/{provider-name}/{version}/` layout for version resolution. See
[Development](../contributing/development.md) for the rest of the local loop.

## Caveats

!!! warning "Version detection uses the Go proxy"

    `clusterctl` detects available provider versions through `proxy.golang.org`.
    While the module is not published there, detection may fail. Work around it
    by setting `GOPROXY=off` or `GOPROXY=direct`, or by pinning the version
    explicitly (`vates:<version>`) on the command line.

!!! note "Not in the built-in provider list yet"

    Until vates is added to `clusterctl`'s built-in provider list (a pull request
    to `kubernetes-sigs/cluster-api`), users must register the provider in
    `clusterctl.yaml` as shown above. This is the same step as for any other
    out-of-tree infrastructure provider.

## Where next

- Install a release: [Installation](../getting-started/installation.md).
- Upgrade an existing installation: [Upgrading](upgrading.md).
- Build the artifacts by hand while developing:
  [Development](../contributing/development.md).
