# 5-6. The objects

Two files: the machine templates (what a node's VM is) and the cluster objects
(what the cluster is). Write them in this order, because the cluster objects
reference the templates by name.

## 5. The machine templates

An `XOMachineTemplate` is the stencil vates-capi clones a VM from. Two of them:
one for the control plane, one for the workers. They differ only in `namePrefix`.

```yaml title="machine-templates.yaml"
apiVersion: vates.infrastructure.cluster.x-k8s.io/v1beta2
kind: XOMachineTemplate
metadata:
  name: my-cluster-cp
  namespace: default
spec:
  template:
    spec:
      # The payload is cloud-init, so the provider may enrich it: it merges the
      # XO user's SSH keys when the cluster asks for them, and injects kube-vip
      # when the cluster sets controlPlaneLB.
      bootstrap:
        format: cloudinit
      # Observability only: sets the "bootstrap:kubeadm" tag on the VM. It does
      # not drive how the payload is handled -- spec.bootstrap does.
      bootstrapProvider: kubeadm
      namePrefix: my-cluster-cp
      templateID: <vm-template-uuid>
      poolID: <pool-uuid>
      resourceSet:
        cpus: 4
        memory: 8Gi
        diskSize: 30Gi
      networkConfig:
        networks:
          - networkID: <network-uuid>
---
apiVersion: vates.infrastructure.cluster.x-k8s.io/v1beta2
kind: XOMachineTemplate
metadata:
  name: my-cluster-worker
  namespace: default
spec:
  template:
    spec:
      bootstrap:
        format: cloudinit
      bootstrapProvider: kubeadm
      namePrefix: my-cluster-worker
      templateID: <vm-template-uuid>
      poolID: <pool-uuid>
      resourceSet:
        cpus: 4
        memory: 8Gi
        diskSize: 30Gi
      networkConfig:
        networks:
          - networkID: <network-uuid>
```

| Field | What it decides |
|---|---|
| `templateID` | which XO template is cloned (its UUID, or `templateName` instead) |
| `poolID` | where the VM is created (`poolName` also works) |
| `networkConfig.networks` | which XO networks the VM's interfaces are attached to |
| `resourceSet` | the VM's size: CPUs, memory, and the disk it is grown to |
| `bootstrap.format` | how the payload is handled: `cloudinit` here, `passthrough` for Talos |
| `networkConfig.addressesFromPools` | an IPAM pool to take the address from, instead of DHCP |

## 6. The cluster objects

Seven objects, and the references between them are the whole story:

| Object | Role |
|---|---|
| `Cluster` | the logical cluster: its networking, and the two references below |
| `XOCluster` | the infrastructure side: the control plane endpoint, the load balancer, the addons |
| `KubeadmControlPlane` | how many control plane nodes, and their kubeadm configuration |
| `KubeadmConfigTemplate` | the bootstrap configuration of the worker nodes |
| `MachineDeployment` | the worker group |
| `MachineHealthCheck` (×2) | auto-remediation, one per group |

The wiring, one line per reference:

- `Cluster.controlPlaneRef` → the `KubeadmControlPlane`
- `Cluster.infrastructureRef` → the `XOCluster`
- `KubeadmControlPlane.machineTemplate.infrastructureRef` → the control plane `XOMachineTemplate`
- `MachineDeployment.template.spec.infrastructureRef` → the worker `XOMachineTemplate`
- `MachineDeployment.template.spec.bootstrap.configRef` → the `KubeadmConfigTemplate`

From there, Cluster API creates one `Machine` per node, and vates-capi creates
one `XOMachine` per `Machine` — which becomes one VM. The objects below are the
whole cluster; nothing else is needed.

```yaml title="cluster.yaml"
apiVersion: cluster.x-k8s.io/v1beta2
kind: Cluster
metadata:
  name: my-cluster
  namespace: default
  labels:
    cluster.x-k8s.io/cluster-name: my-cluster
spec:
  clusterNetwork:
    pods:
      cidrBlocks:
        - 172.31.0.0/16
    services:
      cidrBlocks:
        - 192.168.0.0/16
    serviceDomain: cluster.local
  # The endpoint the kubelets and your kubectl talk to. With kube-vip it is a
  # floating address the control plane nodes share.
  controlPlaneEndpoint:
    host: 192.168.1.240
    port: 6443
  controlPlaneRef:
    apiGroup: controlplane.cluster.x-k8s.io
    kind: KubeadmControlPlane
    name: my-cluster-cp
  infrastructureRef:
    apiGroup: vates.infrastructure.cluster.x-k8s.io
    kind: XOCluster
    name: my-cluster
---
apiVersion: vates.infrastructure.cluster.x-k8s.io/v1beta2
kind: XOCluster
metadata:
  name: my-cluster
  namespace: default
spec:
  controlPlaneEndpoint:
    host: 192.168.1.240
    port: 6443
    subnet: 16
  # kube-vip is injected into the control plane nodes' cloud-init; the CNI is
  # installed by a ClusterResourceSet.
  controlPlaneLB: kube-vip
  addons:
    cni: cilium
  injectSSHKeys: false
---
apiVersion: controlplane.cluster.x-k8s.io/v1beta2
kind: KubeadmControlPlane
metadata:
  name: my-cluster-cp
  namespace: default
spec:
  replicas: 1
  version: v1.36.1
  machineTemplate:
    spec:
      infrastructureRef:
        apiGroup: vates.infrastructure.cluster.x-k8s.io
        kind: XOMachineTemplate
        name: my-cluster-cp
  kubeadmConfigSpec:
    clusterConfiguration:
      apiServer:
        certSANs:
          - localhost
          - 127.0.0.1
          - 192.168.1.240          # the control plane endpoint, or the cert is rejected
    initConfiguration:
      nodeRegistration:
        kubeletExtraArgs:
          - name: cgroup-driver
            value: systemd
          - name: cloud-provider
            value: external       # the Xen Orchestra CCM sets the providerID CAPI needs
    joinConfiguration:
      nodeRegistration:
        kubeletExtraArgs:
          - name: cgroup-driver
            value: systemd
          - name: cloud-provider
            value: external
    preKubeadmCommands:
      # Everything the node needs before kubeadm runs. The from-scratch image has
      # none of it, so this is where kubelet, kubeadm and containerd come from.
      - |
        IP=$(hostname -I | awk '{print $1}')
        hostnamectl set-hostname "cp-$(echo "$IP" | tr '.' '-')"
        echo "$IP $(hostname) $(hostname -s)" >> /etc/hosts
      - printf 'overlay\nbr_netfilter\nnf_conntrack\n' > /etc/modules-load.d/k8s.conf
      - modprobe overlay; modprobe br_netfilter; modprobe nf_conntrack
      - |
        printf 'net.bridge.bridge-nf-call-iptables  = 1\nnet.bridge.bridge-nf-call-ip6tables = 1\nnet.ipv4.ip_forward                 = 1\nnet.ipv4.conf.all.rp_filter         = 0\nnet.ipv4.conf.eth0.rp_filter        = 0\n' > /etc/sysctl.d/k8s.conf
      - sysctl --system
      - swapoff -a; sed -i '/ swap /d' /etc/fstab 2>/dev/null; true
      - dnf install -y epel-release
      - |
        FULL="v1.36.1"
        MINOR=$(echo "$FULL" | sed 's/\(v[0-9]*\.[0-9]*\).*/\1/')
        # Pin the exact requested version: otherwise the repository installs a
        # newer patch release, KCP sees the node as out of date, and rolls it
        # forever.
        echo "$FULL" > /etc/kubernetes-version
        cat <<EOF > /etc/yum.repos.d/kubernetes.repo
        [kubernetes]
        name=Kubernetes
        baseurl=https://pkgs.k8s.io/core:/stable:/$MINOR/rpm/
        enabled=1
        gpgcheck=1
        gpgkey=https://pkgs.k8s.io/core:/stable:/$MINOR/rpm/repodata/repomd.xml.key
        EOF
      - |
        VERSION=$(sed 's/^v//' /etc/kubernetes-version)
        dnf install -y --disableexcludes=kubernetes --setopt=install_weak_deps=False containerd runc "kubelet-$VERSION" "kubeadm-$VERSION" "kubectl-$VERSION"
      - |
        containerd config default | sed 's/SystemdCgroup = false/SystemdCgroup = true/' | tee /etc/containerd/config.toml
        sed -i "s|config_path = ''|config_path = '/etc/containerd/certs.d'|" /etc/containerd/config.toml
      - mkdir -p /etc/containerd/certs.d/registry.k8s.io && printf 'server = "https://registry.k8s.io"\n\n[host."https://mirror.gcr.io"]\n  capabilities = ["pull", "resolve"]\n' > /etc/containerd/certs.d/registry.k8s.io/hosts.toml
      - systemctl enable --now containerd
      - systemctl enable --now kubelet
---
apiVersion: bootstrap.cluster.x-k8s.io/v1beta2
kind: KubeadmConfigTemplate
metadata:
  name: my-cluster-worker
  namespace: default
spec:
  template:
    spec:
      joinConfiguration:
        nodeRegistration:
          kubeletExtraArgs:
            - name: cgroup-driver
              value: systemd
            - name: cloud-provider
              value: external
      preKubeadmCommands:
        # Same bootstrap as the control plane, plus one extra: the workers claim
        # the VIP for a moment so kube-vip can move it if the control plane is
        # replaced.
        - |
          IP=$(hostname -I | awk '{print $1}')
          hostnamectl set-hostname "worker-$(echo "$IP" | tr '.' '-')"
          echo "$IP $(hostname) $(hostname -s)" >> /etc/hosts
        - printf 'overlay\nbr_netfilter\nnf_conntrack\n' > /etc/modules-load.d/k8s.conf
        - modprobe overlay; modprobe br_netfilter; modprobe nf_conntrack
        - |
          printf 'net.bridge.bridge-nf-call-iptables  = 1\nnet.bridge.bridge-nf-call-ip6tables = 1\nnet.ipv4.ip_forward                 = 1\nnet.ipv4.conf.all.rp_filter         = 0\nnet.ipv4.conf.eth0.rp_filter        = 0\n' > /etc/sysctl.d/k8s.conf
        - sysctl --system
        - swapoff -a; sed -i '/ swap /d' /etc/fstab 2>/dev/null; true
        - dnf install -y epel-release
        - |
          FULL="v1.36.1"
          MINOR=$(echo "$FULL" | sed 's/\(v[0-9]*\.[0-9]*\).*/\1/')
          echo "$FULL" > /etc/kubernetes-version
          cat <<EOF > /etc/yum.repos.d/kubernetes.repo
          [kubernetes]
          name=Kubernetes
          baseurl=https://pkgs.k8s.io/core:/stable:/$MINOR/rpm/
          enabled=1
          gpgcheck=1
          gpgkey=https://pkgs.k8s.io/core:/stable:/$MINOR/rpm/repodata/repomd.xml.key
          EOF
        - |
          VERSION=$(sed 's/^v//' /etc/kubernetes-version)
          dnf install -y --disableexcludes=kubernetes --setopt=install_weak_deps=False containerd runc "kubelet-$VERSION" "kubeadm-$VERSION"
        - |
          containerd config default | sed 's/SystemdCgroup = false/SystemdCgroup = true/' | tee /etc/containerd/config.toml
          sed -i "s|config_path = ''|config_path = '/etc/containerd/certs.d'|" /etc/containerd/config.toml
        - mkdir -p /etc/containerd/certs.d/registry.k8s.io && printf 'server = "https://registry.k8s.io"\n\n[host."https://mirror.gcr.io"]\n  capabilities = ["pull", "resolve"]\n' > /etc/containerd/certs.d/registry.k8s.io/hosts.toml
        - systemctl enable --now containerd
        - systemctl enable --now kubelet
        - arping -I eth0 -c 3 192.168.1.240 2>/dev/null || true
---
apiVersion: cluster.x-k8s.io/v1beta2
kind: MachineDeployment
metadata:
  name: my-cluster-md-0
  namespace: default
spec:
  clusterName: my-cluster
  replicas: 1
  template:
    spec:
      clusterName: my-cluster
      version: v1.36.1
      bootstrap:
        configRef:
          apiGroup: bootstrap.cluster.x-k8s.io
          kind: KubeadmConfigTemplate
          name: my-cluster-worker
      infrastructureRef:
        apiGroup: vates.infrastructure.cluster.x-k8s.io
        kind: XOMachineTemplate
        name: my-cluster-worker
---
apiVersion: cluster.x-k8s.io/v1beta2
kind: MachineHealthCheck
metadata:
  name: my-cluster-cp
  namespace: default
  labels:
    cluster.x-k8s.io/cluster-name: my-cluster
spec:
  clusterName: my-cluster
  checks:
    nodeStartupTimeoutSeconds: 600
    unhealthyNodeConditions:
      - type: Ready
        status: "False"
        timeoutSeconds: 300
      - type: Ready
        status: Unknown
        timeoutSeconds: 300
  selector:
    matchLabels:
      cluster.x-k8s.io/control-plane: ""
  remediation:
    triggerIf:
      unhealthyLessThanOrEqualTo: 1
---
apiVersion: cluster.x-k8s.io/v1beta2
kind: MachineHealthCheck
metadata:
  name: my-cluster-worker
  namespace: default
  labels:
    cluster.x-k8s.io/cluster-name: my-cluster
spec:
  clusterName: my-cluster
  checks:
    nodeStartupTimeoutSeconds: 600
    unhealthyNodeConditions:
      - type: Ready
        status: "False"
        timeoutSeconds: 300
      - type: Ready
        status: Unknown
        timeoutSeconds: 300
  selector:
    matchLabels:
      cluster.x-k8s.io/deployment-name: my-cluster-md-0
  remediation:
    triggerIf:
      unhealthyLessThanOrEqualTo: 1
```

Two things worth noticing:

- **one Kubernetes version, everywhere**: `--kubernetes-version` (or `version:`
  in the objects) must be the version the nodes actually run. The from-scratch
  image pins exactly that version at bootstrap; if KCP and the node disagree, the
  machine is rolled forever.
- **the health checks** are what makes a cluster self-healing: a node that stops
  being `Ready` for five minutes is deleted and recreated. Without them, a dead
  node stays a dead node.

!!! note "The same objects, rendered"

    This is exactly what `clusterctl generate cluster` renders from
    `templates/kubeadm/base/clusterctl/almalinux-fromscratch.yaml`, with your
    values substituted. Compare the two once: after that, the template is the
    shortcut and the tutorial is the explanation.

Next: [apply it and watch it come up](run.md).
