.PHONY: all k8s vhd console xcpng clean

HCL_FILES = almalinux-10-k8s.pkr.hcl

OUTPUT_DIR = output-almalinux10-k8s
K8S_QCOW2 = $(OUTPUT_DIR)/packer-alma10_k8s-node
K8S_VHD = $(OUTPUT_DIR)/packer-alma10_k8s-node.vhd

# Kubernetes version baked into the image. Must match the cluster
# KUBERNETES_VERSION used with the prefilled template.
K8S_VERSION ?= 1.36.1

# The node dashboard (vates-console), pinned: fetched through the Go module
# proxy and checked against the checksum database, and the binary records the
# exact module version (`go version -m console/bin/vates-console`).
VATES_KUBE_OS_MODULE = github.com/vatesfr/vates-kube-os
VATES_KUBE_OS_VERSION ?= v0.0.0-20261029122014-cf180b5256b7
CONSOLE_BIN = console/bin/vates-console
CONSOLE_LOGO = console/bin/assets/vates-name-baseline-white.png

all: k8s

# CGO off: the dashboard reaches libdrm through dlopen (purego), so the binary
# needs no particular glibc and runs on the Alma image as built here.
# Rebuilt when this Makefile (the pin) changes; after a command-line override,
# `make clean`.
$(CONSOLE_BIN): Makefile
	GOBIN=$(CURDIR)/$(@D) CGO_ENABLED=0 go install -trimpath -ldflags="-s -w" \
		$(VATES_KUBE_OS_MODULE)/cmd/vates-console@$(VATES_KUBE_OS_VERSION)

$(CONSOLE_LOGO): Makefile
	install -D -m 0644 "$$(go mod download -json $(VATES_KUBE_OS_MODULE)@$(VATES_KUBE_OS_VERSION) \
		| sed -n 's/.*"Dir": "\(.*\)",/\1/p')/image/assets/vates-name-baseline-white.png" $@

console: $(CONSOLE_BIN) $(CONSOLE_LOGO)

$(K8S_QCOW2): $(HCL_FILES) scripts/install-k8s.sh scripts/install-console.sh $(CONSOLE_BIN) $(CONSOLE_LOGO) $(wildcard console/*.service console/*.path console/*.yaml console/plymouth/*)
	packer build -force -var "k8s_version=$(K8S_VERSION)" -only="qemu.alma10_k8s-node" .

k8s: $(K8S_QCOW2)

# The same image built on an XCP-ng pool, ending as a template there. The pool
# and its credentials come from .env (see .env.example).
xcpng: $(CONSOLE_BIN) $(CONSOLE_LOGO)
	set -a && . ./.env && set +a && \
		packer build -force -var "k8s_version=$(K8S_VERSION)" -only="xenserver-iso.alma10_k8s-node" .

vhd: $(K8S_QCOW2)
	qemu-img convert -f qcow2 -O vpc "$(K8S_QCOW2)" "$(K8S_VHD)"

clean:
	rm -rf output-almalinux10-* console/bin
