package kubevip

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const testCloudConfig = `#cloud-config
runcmd:
  - hostnamectl set-hostname test
  - kubeadm init --config /etc/kubernetes/kubeadm.yaml
  - bash /etc/kubernetes/some-post.sh
write_files:
  - path: /etc/kubernetes/test.conf
    content: "hello"
`

func TestPreScript(t *testing.T) {
	script := preScript()
	if !strings.Contains(script, "command -v ctr") {
		t.Error("pre script should check for ctr")
	}
	if !strings.Contains(script, kubeVIPImage) {
		t.Error("pre script should contain kube-vip image for pull")
	}
	if strings.Contains(script, "manifest pod") {
		t.Error("pre script should NOT generate manifest (that's done in post script)")
	}
	if strings.Contains(script, "touch") {
		t.Error("pre script should NOT touch kube-vip.conf (that's done in post script)")
	}
}

func TestPostScript(t *testing.T) {
	cfg := Config{VIP: "10.30.139.10"}
	script := postScript(cfg)
	if !strings.Contains(script, "10.30.139.10") {
		t.Error("post script should contain VIP address")
	}
	if !strings.Contains(script, "admin.conf") {
		t.Error("post script should copy admin.conf")
	}
	if !strings.Contains(script, "kube-vip.conf") {
		t.Error("post script should reference kube-vip.conf")
	}
	if !strings.Contains(script, "127.0.0.1:6443") {
		t.Error("post script should sed server to 127.0.0.1")
	}
	if !strings.Contains(script, "manifest pod") {
		t.Error("post script should generate kube-vip manifest")
	}
	if !strings.Contains(script, "admin\\.conf") {
		t.Error("post script must sed admin.conf to kube-vip.conf in manifest")
	}
	if !strings.Contains(script, "/.kube/config") {
		t.Error("post script must sed mountPath to /.kube/config")
	}
	if !strings.Contains(script, "super-admin.conf") {
		t.Error("post script should reference super-admin.conf")
	}
}

func TestInject(t *testing.T) {
	cfg := Config{VIP: "10.30.139.10"}
	result, err := Inject(testCloudConfig, cfg)
	if err != nil {
		t.Fatalf("Inject failed: %v", err)
	}

	if !strings.HasPrefix(result, "#cloud-config\n") {
		t.Error("result should start with #cloud-config header")
	}

	var parsed map[string]any
	if err := yaml.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("result is not valid YAML: %v", err)
	}

	writeFiles, ok := parsed["write_files"].([]any)
	if !ok {
		t.Fatal("write_files should be a list")
	}

	paths := make(map[string]bool)
	for _, f := range writeFiles {
		fm, ok := f.(map[string]any)
		if !ok {
			continue
		}
		if p, ok := fm["path"].(string); ok {
			paths[p] = true
		}
	}

	if !paths[preScriptPath] {
		t.Error("write_files should contain kube-vip pre script")
	}
	if !paths[postScriptPath] {
		t.Error("write_files should contain kube-vip post script")
	}
	if !paths["/etc/kubernetes/test.conf"] {
		t.Error("write_files should preserve existing files")
	}

	runcmd, ok := parsed["runcmd"].([]any)
	if !ok {
		t.Fatal("runcmd should be a list")
	}

	cmds := make([]string, len(runcmd))
	for i, c := range runcmd {
		cmds[i], _ = c.(string)
	}

	preIdx, postIdx, kubeadmIdx := -1, -1, -1
	for i, c := range cmds {
		if c == "bash "+preScriptPath {
			preIdx = i
		}
		if c == "bash "+postScriptPath {
			postIdx = i
		}
		if strings.Contains(c, "kubeadm init") {
			kubeadmIdx = i
		}
	}

	if preIdx == -1 {
		t.Error("runcmd should contain pre script invocation")
	}
	if postIdx == -1 {
		t.Error("runcmd should contain post script invocation")
	}
	if kubeadmIdx == -1 {
		t.Error("runcmd should still contain kubeadm init")
	}
	if preIdx >= kubeadmIdx {
		t.Errorf("pre script (idx %d) should be before kubeadm (idx %d)", preIdx, kubeadmIdx)
	}
	if postIdx <= kubeadmIdx {
		t.Errorf("post script (idx %d) should be after kubeadm (idx %d)", postIdx, kubeadmIdx)
	}
}

func TestInjectEmptyCloudConfig(t *testing.T) {
	cfg := Config{VIP: "10.30.139.10"}
	result, err := Inject("", cfg)
	if err != nil {
		t.Fatalf("Inject with empty input should not error: %v", err)
	}
	if result != "" {
		t.Error("Inject with empty input should return empty string")
	}
}

func TestInjectNoKubeadm(t *testing.T) {
	cfg := Config{VIP: "10.30.139.10"}
	input := `#cloud-config
runcmd:
  - echo hello
`
	_, err := Inject(input, cfg)
	if err == nil {
		t.Error("Inject should fail when no kubeadm command found")
	}
}

func TestInjectJoin(t *testing.T) {
	cfg := Config{VIP: "10.30.139.10"}
	input := `#cloud-config
runcmd:
  - kubeadm join --config /etc/kubernetes/kubeadm-join.conf
`
	result, err := Inject(input, cfg)
	if err != nil {
		t.Fatalf("Inject with kubeadm join should succeed: %v", err)
	}
	if !strings.Contains(result, preScriptPath) {
		t.Error("should inject pre script for join")
	}
	if !strings.Contains(result, postScriptPath) {
		t.Error("should inject post script for join")
	}
}

func TestPreScriptContainsCtrCheck(t *testing.T) {
	script := preScript()
	if !strings.Contains(script, "command -v ctr") {
		t.Error("pre script must check for ctr binary")
	}
	if !strings.Contains(script, "exit 1") {
		t.Error("pre script must exit with error if ctr not found")
	}
}

func TestPostScriptCreatesManifest(t *testing.T) {
	cfg := Config{VIP: "10.30.139.10"}
	script := postScript(cfg)
	if !strings.Contains(script, "kube-vip.yaml") {
		t.Error("post script should create kube-vip.yaml manifest")
	}
	if !strings.Contains(script, "manifest pod") {
		t.Error("post script should use ctr manifest pod to generate manifest")
	}
}
