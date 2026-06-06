package kubevip

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	preScriptPath  = "/etc/kubernetes/kube-vip-pre.sh"
	postScriptPath = "/etc/kubernetes/kube-vip-post.sh"

	kubeVIPImage     = "ghcr.io/kube-vip/kube-vip:v1.1.2"
	kubeVIPInterface = "eth0"
)

type Config struct {
	VIP    string
	Subnet *int32
}

func preScript() string {
	return fmt.Sprintf(`#!/bin/bash
set -e
command -v ctr >/dev/null 2>&1 || { echo "ERROR: ctr not found, kube-vip requires containerd"; exit 1; }
ctr image pull %s
`, kubeVIPImage)
}

func postScript(cfg Config) string {
	vipSubnetFlag := ""
	if cfg.Subnet != nil {
		vipSubnetFlag = fmt.Sprintf(" --vipSubnet %d", *cfg.Subnet)
	}
	return fmt.Sprintf(`#!/bin/bash
set -e
until curl -sk https://127.0.0.1:6443/healthz > /dev/null 2>&1; do sleep 2; done
if [ -f /etc/kubernetes/super-admin.conf ]; then
  cp /etc/kubernetes/super-admin.conf /etc/kubernetes/kube-vip.conf
else
  cp /etc/kubernetes/admin.conf /etc/kubernetes/kube-vip.conf
fi
sed -i 's|https://[^:]*:6443|https://127.0.0.1:6443|g' /etc/kubernetes/kube-vip.conf
ctr run --rm --net-host %s gen /kube-vip manifest pod \
  --interface %s --address %s --controlplane --services --arp --leaderElection%s \
  > /etc/kubernetes/manifests/kube-vip.yaml
sed -i 's|admin\.conf|kube-vip.conf|g' /etc/kubernetes/manifests/kube-vip.yaml
sed -i 's|mountPath: /etc/kubernetes/kube-vip.conf|mountPath: /.kube/config|g' /etc/kubernetes/manifests/kube-vip.yaml
`, kubeVIPImage, kubeVIPInterface, cfg.VIP, vipSubnetFlag)
}

func Inject(cloudConfig string, cfg Config) (string, error) {
	if cloudConfig == "" {
		return "", nil
	}

	var config map[string]any
	if err := yaml.Unmarshal([]byte(cloudConfig), &config); err != nil {
		return "", fmt.Errorf("failed to parse cloud-config: %w", err)
	}

	writeFiles, ok := config["write_files"].([]any)
	if !ok {
		writeFiles = nil
	}

	writeFiles = append(writeFiles, map[string]any{
		"path":        preScriptPath,
		"permissions": "0755",
		"content":     preScript(),
	})
	writeFiles = append(writeFiles, map[string]any{
		"path":        postScriptPath,
		"permissions": "0755",
		"content":     postScript(cfg),
	})
	config["write_files"] = writeFiles

	runcmd, ok := config["runcmd"].([]any)
	if !ok {
		return "", fmt.Errorf("cloud-config has no runcmd section, cannot inject kube-vip commands")
	}

	newRuncmd, err := injectAroundKubeadm(runcmd)
	if err != nil {
		return "", err
	}
	config["runcmd"] = newRuncmd

	out, err := yaml.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("failed to marshal cloud-config: %w", err)
	}

	return "#cloud-config\n" + string(out), nil
}

func injectAroundKubeadm(runcmd []any) ([]any, error) {
	kubeadmIdx := -1
	for i, cmd := range runcmd {
		s, ok := cmd.(string)
		if ok && (strings.Contains(s, "kubeadm init") || strings.Contains(s, "kubeadm join")) {
			kubeadmIdx = i
			break
		}
	}

	if kubeadmIdx == -1 {
		return nil, fmt.Errorf("could not find kubeadm init/join command in runcmd")
	}

	result := make([]any, 0, kubeadmIdx+3+len(runcmd)-kubeadmIdx)
	result = append(result, runcmd[:kubeadmIdx]...)
	result = append(result, "bash "+preScriptPath)
	result = append(result, runcmd[kubeadmIdx])
	result = append(result, "bash "+postScriptPath)
	result = append(result, runcmd[kubeadmIdx+1:]...)
	return result, nil
}
