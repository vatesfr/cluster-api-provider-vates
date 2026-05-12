# AlmaLinux 10 — RHEL Prefilled Node

This directory builds an **AlmaLinux 10** VM image via [Packer](https://www.packer.io/) with containerd, kubelet, and kubeadm pre-installed. The resulting image is used as a VM template for the `rhel-prefilled` ClusterClass.

## Building

```bash
make rhel-prefilled
```

The QCOW2 image is written to `output-almalinux10-rhel-prefilled/`.

## Converting to VHD (optional)

```bash
make vhd-rhel-prefilled
```

## All targets

| Target | Description |
|---|---|
| `rhel-prefilled` | Build the QCOW2 image |
| `vhd-rhel-prefilled` | Convert QCOW2 to VHD (Fixed VPC) |
| `vhd` | Build + convert all |
| `clean` | Remove all output directories |

## Deploying to Xen Orchestra

> **Note:** Packer currently builds with the **QEMU** builder (AlmaLinux 10). It does **not** push directly to XenServer / XCP-ng.  
> After building, you must manually:

1. Upload the QCOW2 or VHD image to your Xen Orchestra storage.
2. Create a VM from the image (import the disk).
3. Convert the VM to a template.
4. Reference the template UUID (`template:` field) in the `prefilled/` VatesMachineTemplate manifests.
