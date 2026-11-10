# Packer — VM template builder

Build an AlmaLinux 10 VM template with containerd, kubelet, kubeadm and kubectl
pre-installed. The image records that version in `/etc/kubernetes-version`,
which the prefilled template checks before bootstrapping.

## Requirements

- [Packer](https://www.packer.io/) (tested with 1.9+)
- QEMU (`qemu-img`, `qemu-system-x86_64`)
- An AlmaLinux 10 cloud image (downloaded automatically by packer)

## Usage

```bash
make k8s       # build the qcow2 image
make vhd       # convert it to VHD for XenServer import
make clean     # remove build artifacts
```

The Kubernetes version baked into the image defaults to `1.36.1` and can be
overridden with `K8S_VERSION`. It **must** match the `KUBERNETES_VERSION` of any
cluster using the prefilled template, which refuses to bootstrap on a mismatch:

```bash
make K8S_VERSION=1.36.1 k8s
```

Output is written to `output-almalinux10-k8s/`.

The build runs `scripts/install-k8s.sh`, which configures the kernel modules and
sysctls, installs containerd, `kubelet`/`kubeadm`/`kubectl` (pinned to
`K8S_VERSION`) and the Xen guest tools, then prefetches the control plane
images. The script pulls everything from the upstream registries.

## Console

The image also gets a face, installed by `scripts/install-console.sh`: a Vates
Plymouth splash at boot, then the [vates-console](https://github.com/vatesfr/vates-kube-os)
dashboard on tty1 — node state, Kubernetes version, Linux and Kubernetes
distributions, pods and events. It is what the Xen Orchestra console shows.
SSH, the serial console and tty2–tty6 (Alt+F2…F6) keep their login prompts.

The dashboard is built with Go from vates-kube-os at the version pinned by
`VATES_KUBE_OS_VERSION` in the Makefile, fetched through the module proxy and
checked against the checksum database; `go version -m console/bin/vates-console`
shows what an image carries.

The image also grants every node read access to events, cluster-wide
(ClusterRole `vates:node-dashboard`, bound to the `system:nodes` group): a node
may not read them otherwise, and the dashboard's events panel would show a 403.
On control-plane nodes, `vates-node-dashboard-rbac.path` waits for kubeadm's
`/etc/kubernetes/admin.conf` and applies `console/node-dashboard-rbac.yaml`,
retrying until the API server answers. For a cluster built from an older image,
apply that manifest once by hand.

Without a DRM card (`/dev/dri/card0`) the dashboard falls back to its text face.
