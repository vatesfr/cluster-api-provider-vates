#!/usr/bin/env bash
#
# Bake a Kubernetes node image: kernel prerequisites, containerd, kubelet,
# kubeadm, kubectl and the Xen guest tools, then prefetch the control plane
# images. Run by Packer (see ../almalinux-10-k8s.pkr.hcl).
#
# The Kubernetes version is read from the K8S_VERSION environment variable,
# which the Makefile/HCL pass from `make K8S_VERSION=<x.y.z> k8s`. It must
# match the KUBERNETES_VERSION of any cluster using the prefilled template.
set -e

K8S_VERSION="${K8S_VERSION:-1.36.1}"   # full version, e.g. 1.36.1
K8S_MINOR="v${K8S_VERSION%.*}"         # RPM channel, e.g. v1.36

echo "=== 0. Installing missing kernel modules ==="
sudo dnf install -y kernel-modules kernel-modules-extra

echo "=== 1. Configuring kernel prerequisites for K8s ==="
cat <<EOF | sudo tee /etc/modules-load.d/k8s.conf
overlay
br_netfilter
EOF

sudo modprobe overlay
sudo modprobe br_netfilter

cat <<EOF | sudo tee /etc/sysctl.d/k8s.conf
net.bridge.bridge-nf-call-iptables  = 1
net.bridge.bridge-nf-call-ip6tables = 1
net.ipv4.ip_forward                 = 1
net.ipv4.conf.all.rp_filter         = 0
net.ipv4.conf.eth0.rp_filter        = 0
EOF

sudo sysctl --system

echo "=== 2. Installing containerd via EPEL ==="
sudo dnf install -y epel-release
sudo dnf install -y containerd

# Enable SystemdCgroup for K8s compatibility
sudo mkdir -p /etc/containerd
containerd config default | sudo tee /etc/containerd/config.toml >/dev/null
sudo sed -i 's/SystemdCgroup = false/SystemdCgroup = true/g' /etc/containerd/config.toml

sudo systemctl restart containerd
sudo systemctl enable containerd

echo "=== 3. Installing kubelet, kubeadm and kubectl (${K8S_VERSION}) ==="
cat <<EOF | sudo tee /etc/yum.repos.d/kubernetes.repo
[kubernetes]
name=Kubernetes
baseurl=https://pkgs.k8s.io/core:/stable:/${K8S_MINOR}/rpm/
enabled=1
gpgcheck=1
gpgkey=https://pkgs.k8s.io/core:/stable:/${K8S_MINOR}/rpm/repodata/repomd.xml.key
EOF

sudo dnf install -y \
  "kubelet-${K8S_VERSION}" "kubeadm-${K8S_VERSION}" "kubectl-${K8S_VERSION}" \
  xe-guest-utilities-latest --disableexcludes=kubernetes
sudo systemctl enable kubelet
sudo systemctl enable --now xe-linux-distribution

echo "=== 4. Prefetching container images ==="
sudo kubeadm config images pull --kubernetes-version "v${K8S_VERSION}"
sudo ctr image pull ghcr.io/kube-vip/kube-vip:v1.1.2

echo "=== 5. Locking almalinux password ==="
sudo passwd -l almalinux

echo "=== 6. Final cleanup ==="
sudo dnf clean all
sudo cloud-init clean --logs 2>/dev/null || true
sudo rm -f /etc/ssh/ssh_host_*
