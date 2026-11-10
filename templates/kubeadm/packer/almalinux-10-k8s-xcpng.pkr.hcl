// The same node image as almalinux-10-k8s.pkr.hcl, built directly on an XCP-ng
// pool, where it ends as a VM template, instead of as a qcow2 under QEMU.
//
// There is no cloud-image builder for XCP-ng: AlmaLinux is installed from the
// x86_64_v2 DVD ISO the pool already has (or one downloaded from
// repo.almalinux.org), with a kickstart (xcpng/ks.cfg), then provisioned by
// the same scripts.
// The pool credentials come from .env (PKR_VAR_*, see .env.example), loaded by
// `make xcpng`.

packer {
  required_plugins {
    xenserver = {
      version = ">= 0.11.4"
      source  = "github.com/vatesfr/xenserver"
    }
  }
}

variable "remote_host" {
  type        = string
  description = "XCP-ng pool primary (PKR_VAR_remote_host)."
  sensitive   = true
  default     = null
}

variable "remote_username" {
  type        = string
  description = "XCP-ng user (PKR_VAR_remote_username)."
  sensitive   = true
  default     = null
}

variable "remote_password" {
  type        = string
  description = "XCP-ng password (PKR_VAR_remote_password)."
  sensitive   = true
  default     = null
}

variable "sr_name" {
  type        = string
  description = "SR for the VM disk and the build's uploads (PKR_VAR_sr_name)."
  default     = null
}

variable "iso_name" {
  type        = string
  description = "DVD ISO the pool already has, found by name in any SR (\"NFS ISOs\" here); empty downloads iso_url instead (PKR_VAR_iso_name)."
  default     = "AlmaLinux-10-latest-x86_64_v2-dvd.iso"
}

variable "export_xva" {
  type        = bool
  description = "Also download the finished template, as a compressed XVA in output-almalinux10-k8s-xcpng/ (PKR_VAR_export_xva)."
  default     = false
}

variable "network_names" {
  type        = list(string)
  description = "Networks of the build VM; it needs DHCP and the internet (PKR_VAR_network_names)."
  default     = ["Pool-wide network associated with eth0"]
}

source "xenserver-iso" "alma10_k8s-node" {
  // The ISO the pool already has, found by name. With iso_name empty, the same
  // DVD is downloaded instead: once into packer_cache (10 GB), checked against
  // the published CHECKSUM, then uploaded to sr_name.
  iso_name     = var.iso_name
  iso_url      = "https://repo.almalinux.org/almalinux/10/isos/x86_64_v2/AlmaLinux-10-latest-x86_64_v2-dvd.iso"
  iso_checksum = "file:https://repo.almalinux.org/almalinux/10/isos/x86_64_v2/CHECKSUM"
  // The plugin uploads its kickstart CD (and a downloaded ISO) to sr_iso_name.
  // Not an ISO SR: a rescan there drops a VDI that is not one of the library's
  // .iso files, and the build halts when it comes to detach it.
  sr_iso_name    = var.sr_name
  sr_name        = var.sr_name
  tools_iso_name = ""

  remote_host     = var.remote_host
  remote_username = var.remote_username
  remote_password = var.remote_password

  // The plugin labels its CD "cidata": the kickstart for Anaconda, and the
  // same cloud-config as the QEMU build, which creates the almalinux user
  // Packer connects with on the installed system.
  cd_files = ["xcpng/ks.cfg", "cloud-data/user-data", "cloud-data/meta-data"]

  // The GRUB command line rather than the menu: no entry to count down to.
  boot_wait = "15s"
  boot_command = [
    "c",
    "linuxefi /images/pxeboot/vmlinuz inst.ks=hd:LABEL=cidata:/ks.cfg ",
    "<enter>",
    "initrdefi /images/pxeboot/initrd.img ",
    "<enter>",
    "boot",
    "<enter>"
  ]

  // The guest tools (installed by the kickstart) report the IP.
  ip_getter = "tools"

  clone_template  = "AlmaLinux 10"
  firmware        = "uefi"
  vm_name         = "almalinux-10-kubeadm-v${var.k8s_version}"
  vm_description  = "AlmaLinux 10, kubeadm v${var.k8s_version}, Vates console. Built by Packer."
  vm_tags         = ["packer", "kubeadm", "k8s-v${var.k8s_version}"]
  vcpus_max       = 2
  vcpus_atstartup = 2
  vm_memory       = 4096
  disk_size       = 30720
  network_names   = var.network_names

  ssh_username     = "almalinux"
  ssh_password     = "packer"
  ssh_wait_timeout = "60m"
  // No shutdown_command: install-k8s.sh ends by removing the SSH host keys and
  // locking the password, so a new SSH session cannot open. Without one, the
  // plugin asks XAPI for a clean shutdown, which the guest kernel handles.

  // A template in the pool, and a local XVA of it with export_xva; `-force`
  // replaces the template of the same name.
  format            = var.export_xva ? "xva_compressed" : "none"
  keep_vm           = "on_success"
  skip_set_template = false
  output_directory  = "output-almalinux10-k8s-xcpng"
}

build {
  sources = ["source.xenserver-iso.alma10_k8s-node"]

  # The console first: install-k8s.sh ends with the image's final cleanup.
  provisioner "file" {
    source      = "console"
    destination = "/tmp"
  }

  provisioner "shell" {
    script = "scripts/install-console.sh"
  }

  provisioner "shell" {
    environment_vars = ["K8S_VERSION=${var.k8s_version}"]
    script           = "scripts/install-k8s.sh"
  }
}
