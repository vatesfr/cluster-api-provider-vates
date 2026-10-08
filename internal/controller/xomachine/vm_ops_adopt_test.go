package xomachine

import (
	"github.com/gofrs/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/vatesfr/xenorchestra-go-sdk/pkg/payloads"

	infrastructurev1beta2 "github.com/vatesfr/cluster-api-provider-vates/api/v1beta2"
)

var _ = Describe("adoptableVM", func() {
	const marker = "vates-capi-machine:8b3d1f2e-0000-0000-0000-000000000001"

	It("adopts the VM carrying this machine's marker", func() {
		vms := []*payloads.VM{{ID: uuid.Must(uuid.NewV4()), NameLabel: "demo-cp-1", NameDescription: marker}}
		Expect(adoptableVM(vms, "demo-cp-1", marker)).NotTo(BeNil())
	})

	// The user's objection, pinned as a test: name_label is not unique in Xen
	// Orchestra, so a same-named VM without the marker must NOT be adopted.
	It("refuses a VM with the same name but no marker", func() {
		vms := []*payloads.VM{{NameLabel: "demo-cp-1"}}
		Expect(adoptableVM(vms, "demo-cp-1", marker)).To(BeNil())
	})

	It("refuses a VM with the same name but another machine's marker", func() {
		vms := []*payloads.VM{{NameLabel: "demo-cp-1", NameDescription: "vates-capi-machine:other"}}
		Expect(adoptableVM(vms, "demo-cp-1", marker)).To(BeNil())
	})

	It("refuses when the marker is empty (no UID), even with the right name", func() {
		vms := []*payloads.VM{{NameLabel: "demo-cp-1"}}
		Expect(adoptableVM(vms, "demo-cp-1", "")).To(BeNil())
	})

	It("ignores a VM with the marker but a different name", func() {
		vms := []*payloads.VM{{NameLabel: "something-else", NameDescription: marker}}
		Expect(adoptableVM(vms, "demo-cp-1", marker)).To(BeNil())
	})

	It("skips a nil entry and keeps looking", func() {
		vms := []*payloads.VM{nil, {NameLabel: "demo-cp-1", NameDescription: marker}}
		Expect(adoptableVM(vms, "demo-cp-1", marker)).NotTo(BeNil())
	})
})

var _ = Describe("vmMarker", func() {
	It("is empty without a UID, which disables adoption", func() {
		Expect(vmMarker(&infrastructurev1beta2.XOMachine{})).To(BeEmpty())
	})

	It("derives a unique marker from the XOMachine UID", func() {
		m := &infrastructurev1beta2.XOMachine{
			ObjectMeta: metav1.ObjectMeta{UID: types.UID("abc-123")},
		}
		Expect(vmMarker(m)).To(Equal("vates-capi-machine:abc-123"))
	})
})
