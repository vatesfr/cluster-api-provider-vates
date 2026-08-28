.PHONY: all k8s vhd clean

HCL_FILES = almalinux-10-k8s.pkr.hcl

OUTPUT_DIR = output-almalinux10-k8s
K8S_QCOW2 = $(OUTPUT_DIR)/packer-alma10_k8s-node
K8S_VHD = $(OUTPUT_DIR)/packer-alma10_k8s-node.vhd

all: k8s

$(K8S_QCOW2): $(HCL_FILES) scripts/install-k8s.sh
	packer build -force -only="qemu.alma10_k8s-node" .

k8s: $(K8S_QCOW2)

vhd: $(K8S_QCOW2)
	qemu-img convert -f qcow2 -O vpc "$(K8S_QCOW2)" "$(K8S_VHD)"

clean:
	rm -rf output-almalinux10-*
