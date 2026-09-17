# Packer — VM template builder

Build an AlmaLinux 10 VM template with containerd, kubelet, kubeadm, and
kubectl pre-installed.

## Requirements

- [Packer](https://www.packer.io/) (tested with 1.9+)
- QEMU (`qemu-img`, `qemu-system-x86_64`)
- An AlmaLinux 10 cloud image (downloaded automatically by packer)

## Usage

```bash
export HARBOR_HOST="harbor.example.com"   # optional — registry mirror
export HARBOR_CA_PATH="/path/to/ca.crt"  # optional — self-signed Harbor cert

make k8s       # build qcow2 image
make vhd       # convert to VHD for XenServer import
make clean     # remove build artifacts
```

The Kubernetes version baked into the image defaults to `1.36.1` and can be
overridden with `K8S_VERSION`. It **must** match the `KUBERNETES_VERSION` of any
cluster using the prefilled template, which refuses to bootstrap on a mismatch:

```bash
make K8S_VERSION=1.36.1 k8s
```

Output is written to `output-almalinux10-k8s/`.

## Registry mirrors (Harbor)

When `HARBOR_HOST` is set, the script configures containerd to pull images
through a Harbor proxy cache for these upstream registries:

- `registry-1.docker.io` -> `https://<HARBOR_HOST>/v2/docker-hub`
- `quay.io`             -> `https://<HARBOR_HOST>/v2/quay`
- `ghcr.io`             -> `https://<HARBOR_HOST>/v2/ghcr`
- `registry.k8s.io`     -> `https://<HARBOR_HOST>/v2/k8s`

Set `HARBOR_CA_PATH` to a PEM file with the Harbor CA certificate if it uses
a self-signed cert. If unset, CA installation is skipped (mirrors still work
with a publicly-trusted CA).

Leave `HARBOR_HOST` unset to pull images directly from upstream registries.
