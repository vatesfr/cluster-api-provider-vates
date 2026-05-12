packer {
  required_plugins {
    qemu = {
      version = "~> 1.1"
      source  = "github.com/hashicorp/qemu"
    }
  }
  required_plugins {
    xenserver = {
      version = ">= v0.9.0"
      source  = "github.com/vatesfr/xenserver"
    }
  }
}

locals {
  common = {
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
}

source "qemu" "alma10-rhel-prefilled" {
  output_directory = "output-almalinux10-rhel-prefilled"

  iso_url          = local.common.iso_url
  iso_checksum     = local.common.iso_checksum
  shutdown_command = local.common.shutdown_command
  disk_size        = local.common.disk_size
  format           = local.common.format
  accelerator      = local.common.accelerator
  disk_image       = local.common.disk_image
  ssh_username     = local.common.ssh_username
  ssh_password     = local.common.ssh_password
  ssh_timeout      = local.common.ssh_timeout
  cd_files         = local.common.cd_files
  cd_label         = local.common.cd_label
  qemuargs         = local.common.qemuargs
}

build {
  sources = ["source.qemu.alma10-rhel-prefilled"]

  provisioner "shell" {
    script = "scripts/install-k8s.sh"
  }
}
