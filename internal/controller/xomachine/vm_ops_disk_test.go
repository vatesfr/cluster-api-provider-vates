package xomachine

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	xoclient "github.com/vatesfr/xenorchestra-go-sdk/client"
)

func disk(id, name string, size int, bootable bool, position, device string) xoclient.Disk {
	return xoclient.Disk{
		VBD: xoclient.VBD{
			Id:       "vbd-" + id,
			Bootable: bootable,
			Position: position,
			Device:   device,
		},
		VDI: xoclient.VDI{
			VDIId:     "vdi-" + id,
			NameLabel: name,
			Size:      size,
		},
	}
}

var _ = Describe("findMainDisk", func() {
	It("returns nil when there are no disks", func() {
		Expect(findMainDisk(nil)).To(BeNil())
	})

	It("returns the only disk when a single disk is present", func() {
		only := disk("1", "main", 1024, false, "0", "xvda")
		Expect(findMainDisk([]xoclient.Disk{only})).To(Equal(&only))
	})

	It("returns the bootable disk even when it is not first in the slice", func() {
		cloudInit := disk("ci", "XO CloudConfigDrive", 100, false, "1", "xvdb")
		main := disk("main", "rhel-template", 2048, true, "0", "xvda")
		result := findMainDisk([]xoclient.Disk{cloudInit, main})
		Expect(result).NotTo(BeNil())
		Expect(result.VDIId).To(Equal("vdi-main"))
		Expect(result.Bootable).To(BeTrue())
	})

	It("ignores the cloud-init config drive even when it is bootable", func() {
		cloudInitBootable := disk("ci", "XO CloudConfigDrive", 12582912, true, "1", "xvdb")
		main := disk("main", "PFT-Talos-Nocloud", 4454350848, false, "0", "xvda")
		result := findMainDisk([]xoclient.Disk{cloudInitBootable, main})
		Expect(result).NotTo(BeNil())
		Expect(result.VDIId).To(Equal("vdi-main"))
		Expect(result.Bootable).To(BeFalse())
	})

	It("skips CD drives while looking for the bootable disk", func() {
		cd := xoclient.Disk{VBD: xoclient.VBD{Id: "vbd-cd", IsCdDrive: true, Bootable: true}}
		main := disk("main", "rhel-template", 2048, true, "0", "xvda")
		result := findMainDisk([]xoclient.Disk{cd, main})
		Expect(result).NotTo(BeNil())
		Expect(result.VDIId).To(Equal("vdi-main"))
	})

	It("falls back to the lowest position, ignoring the cloud-init drive, when no disk is bootable", func() {
		cloudInit := disk("ci", "XO CloudConfigDrive", 100, false, "1", "xvdb")
		secondary := disk("data", "data-disk", 500, false, "2", "xvdc")
		main := disk("main", "rhel-template", 1024, false, "0", "xvda")
		result := findMainDisk([]xoclient.Disk{cloudInit, secondary, main})
		Expect(result).NotTo(BeNil())
		Expect(result.VDIId).To(Equal("vdi-main"))
	})

	It("uses the device name as a tie-breaker when positions are missing", func() {
		cloudInit := disk("ci", "XO CloudConfigDrive", 100, false, "", "xvdb")
		main := disk("main", "rhel-template", 1024, false, "", "xvda")
		result := findMainDisk([]xoclient.Disk{cloudInit, main})
		Expect(result).NotTo(BeNil())
		Expect(result.VDIId).To(Equal("vdi-main"))
	})

	It("returns nil when all disks are cloud-init drives", func() {
		cloudInitA := disk("a", "XO CloudConfigDrive", 100, false, "0", "xvda")
		cloudInitB := disk("b", "XO CloudConfigDrive", 100, false, "1", "xvdb")
		result := findMainDisk([]xoclient.Disk{cloudInitA, cloudInitB})
		Expect(result).To(BeNil())
	})
})
