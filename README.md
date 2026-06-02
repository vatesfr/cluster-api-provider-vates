# vates-capi

A [Cluster API](https://cluster-api.sigs.k8s.io/) infrastructure provider for
[Xen Orchestra](https://xen-orchestra.com/). Manages the full lifecycle of VMs
on XenServer / XCP-ng pools as Kubernetes worker and control plane nodes.

## Quick start

> **Prerequisites:** A management cluster, [clusterctl](https://cluster-api.sigs.k8s.io/clusterctl/overview.html), Xen Orchestra access (VM template UUID, pool UUID, network name). Edit `config/samples/machinetemplates/` with your environment values before applying.

```bash
# Install CAPI on a management cluster
CLUSTER_TOPOLOGY=true clusterctl init --bootstrap kubeadm --control-plane kubeadm

# Build and deploy the vates provider
kubectl apply -f dist/install.yaml

# Create the XO credentials secret (required before creating clusters)
kubectl create secret generic xo-credentials \
  --namespace capi-system \
  --from-literal=url="https://<your-xoa>" \
  --from-literal=token="<your-xo-token>" \
  --from-literal=insecure="true"
```

Then you can edit config/samples/machinetemplates to stick with your own environment:

- `poolID`
- `templateID` (XCP-ng machine template that can use `cloudini`, this machine can be prefiled, see the `config/samples/packers` example)
- `network` name or ID

Afterward you can apply the clusterClasses and machines templates:

```bash
# Deploy ClusterClass + machine templates (edit templateID/poolID/network first)
kubectl apply -k config/samples/clusterclass/
kubectl apply -k config/samples/machinetemplates/

# Create a cluster (with the template from config/samples/example-cluster/)
kubectl apply -k config/samples/example-cluster/
```

## Development

```bash
# Build and test
make build
make test

# Local Kind development
make -f Makefile.dev build
make -f Makefile.dev deploy
```

## Project structure

```
├── api/                           # CRD types (+kubebuilder markers)
├── cmd/                           # Manager entry point
├── internal/
│   ├── controller/                # Reconciliation logic
│   ├── kubevip/                   # kube-vip static pod injection
│   └── xoapi/                     # Xen Orchestra API client
├── config/
│   ├── crd/                       # Generated CRDs (DO NOT EDIT)
│   ├── manager/                   # Manager Deployment
│   ├── rbac/                      # Generated RBAC (DO NOT EDIT)
│   └── samples/                   # Example manifests
│       ├── clusterclass/          # ClusterClass + templates
│       ├── machinetemplates/      # VatesMachineTemplate (edit per env)
│       ├── clusterctl/            # Template for clusterctl generate
│       ├── example-cluster/       # Complete cluster example
│       ├── standalone/            # Standalone VatesMachine tests
│       └── packers/               # Packer image definitions
├── dist/                          # Generated release artifacts
│   ├── install.yaml               # kubectl apply bundle
│   ├── infrastructure-components.yaml  # clusterctl bundle
│   ├── cluster-template.yaml      # clusterctl template
│   └── chart/                     # Helm chart

```

## Distribution

```bash
# Install bundle (single command, no clusterctl)
kubectl apply -f https://github.com/vatesfr/cluster-api-provider-vates/releases/latest/download/install.yaml

# clusterctl release artifacts
make release-manifests IMG=ghcr.io/vatesfr/cluster-api-provider-vates:latest

# Helm chart (regenerate after manifest changes)
kubebuilder edit --plugins=helm/v2-alpha --force
```

## License

Copyright 2026.

Licensed under the Apache License, Version 2.0.
