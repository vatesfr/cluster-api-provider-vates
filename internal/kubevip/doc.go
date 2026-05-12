// Package kubevip provides utilities for injecting kube-vip static pod
// configuration into cloud-init userdata for control plane nodes.
//
// It generates pre and post kubeadm scripts that pull the kube-vip image,
// create the kubeconfig from admin.conf, and deploy the kube-vip manifest
// so that the VIP is available before kube-vip starts.
package kubevip
