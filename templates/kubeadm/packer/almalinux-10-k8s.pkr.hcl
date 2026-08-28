packer {
  required_plugins {
    qemu = {
      version = "~> 1.1"
      source  = "github.com/hashicorp/qemu"
    }
  }
}

locals {
  iso_url          = "https://repo.almalinux.org/almalinux/10/cloud/x86_64/images/AlmaLinux-10-GenericCloud-latest.x86_64.qcow2"
  iso_checksum     = "file:https://repo.almalinux.org/almalinux/10/cloud/x86_64/images/CHECKSUM"
  shutdown_command = "echo 'packer' | sudo -S shutdown -P now"
  disk_size        = "30G"
  format           = "qcow2"
  accelerator      = "kvm"
  disk_image       = true
  ssh_username     = "almalinux"
  ssh_password     = "packer"
  ssh_timeout      = "15m"
  cd_files         = ["cloud-data/user-data", "cloud-data/meta-data"]
  cd_label         = "cidata"
  qemuargs         = [["-cpu", "host"], ["-m", "2048M"], ["-smp", "2"]]
}

source "qemu" "alma10_k8s-node" {
  output_directory = "output-almalinux10-k8s"

  iso_url          = local.iso_url
  iso_checksum     = local.iso_checksum
  shutdown_command = local.shutdown_command
  disk_size        = local.disk_size
  format           = local.format
  accelerator      = local.accelerator
  disk_image       = local.disk_image
  ssh_username     = local.ssh_username
  ssh_password     = local.ssh_password
  ssh_timeout      = local.ssh_timeout
  cd_files         = local.cd_files
  cd_label         = local.cd_label
  qemuargs         = local.qemuargs
}

build {
  sources = ["source.qemu.alma10_k8s-node"]

  provisioner "shell" {
    script = "scripts/install-k8s.sh"
  }
}
