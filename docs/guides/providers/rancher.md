# Rancher

[Rancher](https://www.rancher.com/) can provision RKE2 clusters on any Cluster
API infrastructure provider: its `provisioning.cattle.io` `Cluster` points at an
`XOCluster` and an `XOMachineTemplate`, Rancher generates the CAPI `Machine`s and
their RKE2 bootstrap data, and vates-capi creates the VMs in Xen Orchestra.

This page builds a 3-node RKE2 cluster (every node control plane, etcd and
worker) on Xen Orchestra, managed by Rancher. Like the examples of the Rancher
documentation, it uses a single machine pool with all roles for simplicity.

!!! tip "Production layout"

    Rancher's
    [recommended cluster architecture](https://ranchermanager.docs.rancher.com/how-to-guides/new-user-guides/kubernetes-clusters-in-rancher-setup/checklist-for-production-ready-clusters/recommended-cluster-architecture)
    keeps workloads off the etcd and control plane nodes: at least 3 etcd, 2
    control plane and 2 worker nodes. Here, drop `workerRole` from the `ctrl`
    pool and add a `worker` pool with `workerRole: true`, `quantity: 2` and its
    own `XOMachineTemplate` (Rancher wants one template per pool). Its VMs get
    the `workerPrefix` of `vmNaming`. Copy the pool's `userdata` with the
    `xo-lab-wk-` hostname prefix.

!!! note "No create form in the UI"

    This uses Rancher's
    [CAPI infrastructure provider](https://ranchermanager.docs.rancher.com/how-to-guides/advanced-user-guides/capi-infrastructure-providers)
    mode (Rancher 2.14+, GA in 2.15). Rancher's create and edit forms come from
    a per-provider UI extension, which only exists for AWS (CAPA) today. For
    Xen Orchestra, every
    object below is applied with `kubectl` (or **Import YAML** in the UI) on the
    Rancher management cluster. The cluster page, kubeconfig, scaling and
    Explorer work as usual.

Tested with Rancher 2.15.3 (Turtles 0.27.4), RKE2 v1.36.6+rke2r1, XO CCM chart
1.3.0.

## Prerequisites

- Rancher 2.14 or later. **Turtles** (Rancher's CAPI integration) is installed
  with it; wait for the `capiproviders.turtles-capi.cattle.io` CRD.
- The `provisioningprebootstrap` feature flag enabled (Global Settings → Feature
  Flags, Rancher restarts). It syncs the XO credentials into the new cluster
  before it boots, for the cloud controller manager.
- A cloud-init VM template in Xen Orchestra, with Xen guest tools, for example
  **Ubuntu 24.04 Cloud-Init** from the XO Hub.
- The nodes must resolve and reach the Rancher URL. If the DNS your DHCP hands
  out cannot, override it in `guestConfig` (see [step 3](#3-the-cluster)).
- A free IP for the control plane VIP (kube-vip).

## 1. Install the provider

vates-capi is not in Turtles' list of known providers: register its release in
the `ClusterctlConfig`, then install it with a `CAPIProvider`. The controller
also needs global XO credentials to start, even if every cluster uses its own
identity.

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: vates-system
---
apiVersion: v1
kind: Secret
metadata:
  name: xo-credentials
  namespace: vates-system
type: Opaque
stringData:
  url: https://xo.example.com
  token: <xo-token>
  insecure: "false"
---
apiVersion: turtles-capi.cattle.io/v1alpha1
kind: ClusterctlConfig
metadata:
  name: clusterctl-config            # fixed name
  namespace: cattle-turtles-system
spec:
  providers:
    - name: vates
      url: https://github.com/vatesfr/cluster-api-provider-vates/releases/<version>/infrastructure-components.yaml
      type: InfrastructureProvider
---
apiVersion: turtles-capi.cattle.io/v1alpha1
kind: CAPIProvider
metadata:
  name: vates
  namespace: vates-system
spec:
  name: vates
  type: infrastructure
  version: <version>
```

If `clusterctl-config` already exists, add the `vates` entry to its
`spec.providers` instead of replacing it.

```bash
kubectl get capiproviders -A     # vates: Ready
```

## 2. Credentials

Two Secrets in `fleet-default`, Rancher's namespace for downstream clusters:
the identity vates-capi uses to create the VMs, and the configuration of the
[XO cloud controller manager](https://github.com/vatesfr/xenorchestra-cloud-controller-manager)
and [CSI driver](https://github.com/vatesfr/xenorchestra-csi-driver), which
Rancher copies to `kube-system` in the new cluster.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: xo-lab-credentials
  namespace: fleet-default
type: Opaque
stringData:
  url: https://xo.example.com
  token: <xo-token>
  insecure: "false"                  # a string here
---
apiVersion: v1
kind: Secret
metadata:
  name: xenorchestra-cloud-controller-manager
  namespace: fleet-default
  annotations:
    rke.cattle.io/object-authorized-for-clusters: xo-lab
    provisioning.cattle.io/sync-bootstrap: "true"
    provisioning.cattle.io/sync-target-namespace: kube-system
type: Opaque
stringData:
  config.yaml: |
    url: https://xo.example.com
    token: <xo-token>
    insecure: false                  # a YAML boolean here: "false" crashes the CCM
```

## 3. The cluster

```yaml
apiVersion: vates.infrastructure.cluster.x-k8s.io/v1beta2
kind: XOCluster
metadata:
  name: xo-lab
  namespace: fleet-default
spec:
  identityRef:
    name: xo-lab-credentials
  controlPlaneEndpoint:              # required: the XOCluster is never ready without it
    host: 192.0.2.10                 # the kube-vip VIP
    port: 6443
  vmNaming:                          # pools with controlPlaneRole get controlPlanePrefix
    controlPlanePrefix: xo-lab-cp-
    workerPrefix: xo-lab-wk-         # required, unused until a worker-only pool exists
  addons:                            # Rancher installs these itself
    ccm: false
    csi: false
    cni: none
---
apiVersion: vates.infrastructure.cluster.x-k8s.io/v1beta2
kind: XOMachineTemplate
metadata:
  name: xo-lab-ctrl-v1               # templates are replaced, not edited
  namespace: fleet-default
spec:
  template:
    spec:
      bootstrap:
        format: cloudinit
      templateID: <xo-template-uuid>
      poolID: <xo-pool-uuid>
      resourceSet:
        cpus: 4
        memory: 8Gi
        diskSize: 40Gi
      networkConfig:
        networks:
          - networkID: <xo-network-uuid>
        # Optional: keep DHCP for the address, replace the resolver when the
        # DHCP DNS cannot resolve the Rancher URL.
        guestConfig: |
          version: 2
          ethernets:
            enX0:
              dhcp4: true
              dhcp4-overrides:
                use-dns: false
              nameservers:
                addresses: [192.0.2.53]
---
apiVersion: provisioning.cattle.io/v1
kind: Cluster
metadata:
  name: xo-lab
  namespace: fleet-default
spec:
  kubernetesVersion: v1.36.6+rke2r1
  rkeConfig:
    infrastructureRef:
      apiVersion: vates.infrastructure.cluster.x-k8s.io/v1beta2
      kind: XOCluster
      name: xo-lab
      namespace: fleet-default
    machinePools:
      - name: ctrl
        controlPlaneRole: true
        etcdRole: true
        workerRole: true
        quantity: 3
        machineConfigRef:
          apiVersion: vates.infrastructure.cluster.x-k8s.io/v1beta2
          kind: XOMachineTemplate
          name: xo-lab-ctrl-v1
          namespace: fleet-default
        userdata:
          # Every VM boots with the template's hostname and RKE2 rejects
          # duplicate node names: derive one from the VM UUID.
          # Rancher renders this as a Jinja template: avoid {{ and {%.
          inlineUserdata: |
            preserve_hostname: true
            bootcmd:
              - echo "xo-lab-cp-$(cut -c1-8 /sys/hypervisor/uuid)" > /etc/hostname
              - hostname -F /etc/hostname
    machineGlobalConfig:
      cni: calico
      disable-cloud-controller: true   # the XO CCM replaces it
      cloud-provider-name: external
      tls-san:
        - 192.0.2.10
    additionalManifest: |-
      apiVersion: helm.cattle.io/v1
      kind: HelmChart
      metadata:
        name: xenorchestra-ccm
        namespace: kube-system
      spec:
        chart: oci://ghcr.io/vatesfr/charts/xenorchestra-cloud-controller-manager
        version: 1.3.0
        targetNamespace: kube-system
        bootstrap: true
        valuesContent: |-
          existingConfigSecret: xenorchestra-cloud-controller-manager
          useHostNetwork: true
      ---
      apiVersion: helm.cattle.io/v1
      kind: HelmChart
      metadata:
        name: xenorchestra-csi
        namespace: kube-system
      spec:
        chart: oci://ghcr.io/vatesfr/charts/xenorchestra-csi-driver
        targetNamespace: kube-system
        valuesContent: |-
          existingConfigSecret: xenorchestra-cloud-controller-manager
      ---
      apiVersion: v1
      kind: ServiceAccount
      metadata: { name: kube-vip, namespace: kube-system }
      ---
      apiVersion: rbac.authorization.k8s.io/v1
      kind: ClusterRole
      metadata: { name: system:kube-vip-role }
      rules:
        - apiGroups: [""]
          resources: ["services", "services/status", "nodes"]
          verbs: ["list", "get", "watch", "update"]
        - apiGroups: ["coordination.k8s.io"]
          resources: ["leases"]
          verbs: ["list", "get", "watch", "update", "create"]
      ---
      apiVersion: rbac.authorization.k8s.io/v1
      kind: ClusterRoleBinding
      metadata: { name: system:kube-vip-binding }
      roleRef: { apiGroup: rbac.authorization.k8s.io, kind: ClusterRole, name: system:kube-vip-role }
      subjects: [{ kind: ServiceAccount, name: kube-vip, namespace: kube-system }]
      ---
      apiVersion: apps/v1
      kind: DaemonSet
      metadata: { name: kube-vip, namespace: kube-system }
      spec:
        selector: { matchLabels: { app: kube-vip } }
        template:
          metadata: { labels: { app: kube-vip } }
          spec:
            nodeSelector: { node-role.kubernetes.io/control-plane: "true" }
            tolerations: [{ operator: Exists }]
            hostNetwork: true
            serviceAccountName: kube-vip
            containers:
              - name: kube-vip
                image: ghcr.io/kube-vip/kube-vip:v1.3.1
                args: ["manager"]
                env:
                  - { name: cp_enable, value: "true" }
                  - { name: vip_arp, value: "true" }
                  - { name: vip_interface, value: "enX0" }
                  - { name: address, value: "192.0.2.10" }
                  - { name: port, value: "6443" }
                  - { name: vip_leaderelection, value: "true" }
                securityContext:
                  capabilities: { add: ["NET_ADMIN", "NET_RAW"] }
```

Apply the three objects together, then follow the cluster in Rancher (Cluster
Management → `xo-lab` → Provisioning Log) or with:

```bash
kubectl -n fleet-default get clusters.provisioning.cattle.io,machines,xomachines
```

The cluster turns **Active** after about ten minutes. Each node has a
`xenorchestra://<pool-uuid>/<vm-uuid>` provider ID, set by the XO CCM.

## Changing or deleting the cluster

- **Scale**: change `quantity`, or use the pool's −/+ buttons in Rancher. Keep
  an odd number of etcd nodes.
- **Change the VMs**: create a new `XOMachineTemplate` (`xo-lab-ctrl-v2`) and
  point `machineConfigRef.name` at it. Rancher rolls every machine of the pool.
  Delete the old template once no machine uses it. Changing the pool's
  `userdata` also recreates every machine of the pool.
- **Delete**: delete the `provisioning.cattle.io` Cluster. Rancher removes the
  VMs. Never delete the control plane `Machine`s alone: Rancher then asks for an
  etcd snapshot restore.

## Limitations

| Limitation | Workaround on this page |
|---|---|
| The guest keeps the template's hostname. | Per-pool `inlineUserdata` sets it from the VM UUID. |
| `controlPlaneLB: kube-vip` only injects kubeadm paths. | kube-vip is a DaemonSet in `additionalManifest`. |
| IPAM-addressed machines always use the gateway as DNS server. | DHCP + `guestConfig` override. |
| The provider exits at startup without the global `xo-credentials` Secret. | Created in step 1. |

!!! note "v1.1.0-rc6"

    `vmNaming` is not in v1.1.0-rc6. With that release, drop `vmNaming` from the
    `XOCluster` and set `namePrefix: xo-lab-cp-` on the `XOMachineTemplate`
    instead (it is required there).
