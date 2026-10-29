.PHONY: all k8s vhd console clean

HCL_FILES = almalinux-10-k8s.pkr.hcl

OUTPUT_DIR = output-almalinux10-k8s
K8S_QCOW2 = $(OUTPUT_DIR)/packer-alma10_k8s-node
K8S_VHD = $(OUTPUT_DIR)/packer-alma10_k8s-node.vhd

# Kubernetes version baked into the image. Must match the cluster
# KUBERNETES_VERSION used with the prefilled template.
K8S_VERSION ?= 1.36.1

# vates-kube-os checkout the node dashboard (vates-console) is built from.
VATES_KUBE_OS ?= $(abspath ../../../../vates-kube-os)
CONSOLE_BIN = console/bin/vates-console
CONSOLE_LOGO = console/bin/assets/vates-name-baseline-white.png

all: k8s

# CGO off: the dashboard reaches libdrm through dlopen (purego), so the binary
# needs no particular glibc and runs on the Alma image as built here.
$(CONSOLE_BIN): $(shell find $(VATES_KUBE_OS)/cmd $(VATES_KUBE_OS)/internal -name '*.go' 2>/dev/null)
	cd $(VATES_KUBE_OS) && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(CURDIR)/$@ ./cmd/vates-console

$(CONSOLE_LOGO): $(VATES_KUBE_OS)/image/assets/vates-name-baseline-white.png
	install -D -m 0644 $< $@

console: $(CONSOLE_BIN) $(CONSOLE_LOGO)

$(K8S_QCOW2): $(HCL_FILES) scripts/install-k8s.sh scripts/install-console.sh $(CONSOLE_BIN) $(CONSOLE_LOGO) $(wildcard console/*.service console/plymouth/*)
	packer build -force -var "k8s_version=$(K8S_VERSION)" -only="qemu.alma10_k8s-node" .

k8s: $(K8S_QCOW2)

vhd: $(K8S_QCOW2)
	qemu-img convert -f qcow2 -O vpc "$(K8S_QCOW2)" "$(K8S_VHD)"

clean:
	rm -rf output-almalinux10-* console/bin
