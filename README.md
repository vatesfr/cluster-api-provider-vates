# vates-capi

A [Cluster API](https://cluster-api.sigs.k8s.io/) infrastructure provider for
[Xen Orchestra](https://xen-orchestra.com/). Manages the full lifecycle of VMs
on XenServer / XCP-ng pools as Kubernetes worker and control plane nodes.

## Quick start

> **Prerequisites:** A management cluster (Kind, k3s, etc.), [clusterctl](https://cluster-api.sigs.k8s.io/clusterctl/overview.html), Xen Orchestra access (VM template UUID, pool UUID, network name).

```bash
# 1. Install CAPI with ClusterClass support
CLUSTER_TOPOLOGY=true clusterctl init --bootstrap kubeadm --control-plane kubeadm

# 2. Deploy the vates provider
kubectl apply -f https://github.com/vatesfr/cluster-api-provider-vates/releases/latest/download/install.yaml

# 3. Create the XO credentials secret
kubectl create secret generic xo-credentials -n capi-system \
  --from-literal=url="https://<your-xoa>" \
  --from-literal=token="<your-xo-token>" \
  --from-literal=insecure="true"
```

### Template requirements

All templates must have **cloud-init** support enabled and **Xen guest tools** installed and running.

Two ClusterClass variants are provided:

- **`prefilled`** — for templates that already have kubelet, kubeadm, containerd, kube-vip, and Cilium images pre-installed. Edit `examples/machinetemplates/prefilled/`.
- **`from-scratch`** — for minimal templates (just containerd + Xen guest tools). The ClusterClass handles installing everything via `preKubeadmCommands`. Edit `examples/machinetemplates/from-scratch/`.

Edit the matching `VatesMachineTemplate` files to set your `templateID`, `poolID`, and network name, then apply:

```bash
# 4. Deploy ClusterClass + machine templates
kubectl apply -k examples/clusterclass/
kubectl apply -k examples/machinetemplates/prefilled/

# 5. Create a cluster
kubectl apply -f examples/example-cluster/capi-cluster.yaml
```

## Development

```bash
make -f Makefile.dev build    # Build controller image
make -f Makefile.dev push     # Load into Kind
make -f Makefile.dev restart  # Restart the controller pod
```

## Project structure

```
├── api/                           # CRD types (+kubebuilder markers)
├── cmd/                           # Manager entry point
├── internal/
│   ├── controller/                # Reconciliation logic
│   └── kubevip/                   # kube-vip static pod injection
├── config/
│   ├── crd/                       # Generated CRDs (DO NOT EDIT)
│   ├── manager/                   # Manager Deployment
│   ├── rbac/                      # Generated RBAC (DO NOT EDIT)
├── examples/                      # Example manifests
│   ├── ccm/                       # CCM deployment + ClusterResourceSet
│   ├── clusterclass/              # ClusterClass + templates
│   ├── clusterctl/                # Template for clusterctl generate
│   ├── machinetemplates/          # VatesMachineTemplate (edit per env)
│   ├── example-cluster/           # Complete cluster example
│   └── standalone/                # Standalone VatesMachine tests
├── dist/                          # Generated release artifacts
│   ├── install.yaml               # kubectl apply bundle
│   ├── infrastructure-components.yaml  # clusterctl bundle
│   ├── cluster-template.yaml      # clusterctl template
│   └── chart/                     # Helm chart

```

## Release artifacts

```bash
# Generate dist/ from the current source
make release-manifests IMG=ghcr.io/vatesfr/cluster-api-provider-vates:latest
```

## License

Copyright 2026.

Licensed under the Apache License, Version 2.0.
