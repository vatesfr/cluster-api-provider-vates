.PHONY: all rhel-prefilled vhd vhd-rhel-prefilled clean

RHEL_PREFILLED_QCOW2 = output-almalinux10-rhel-prefilled/packer-alma10-rhel-prefilled
RHEL_PREFILLED_VHD = output-almalinux10-rhel-prefilled/packer-alma10-rhel-prefilled.vhd

HCL_FILES = almalinux-10-rhel-prefilled.pkr.hcl

all:
	packer build .

rhel-prefilled: $(RHEL_PREFILLED_QCOW2)

$(RHEL_PREFILLED_QCOW2): $(HCL_FILES) scripts/install-k8s.sh
	packer build -only="qemu.alma10-rhel-prefilled" .

vhd-rhel-prefilled: $(RHEL_PREFILLED_QCOW2)
	qemu-img convert -f qcow2 -O vpc "$(RHEL_PREFILLED_QCOW2)" "$(RHEL_PREFILLED_VHD)"

vhd: vhd-rhel-prefilled

clean:
	rm -rf output-almalinux10-*
