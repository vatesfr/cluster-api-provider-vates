package vatesmachine

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	infrastructurev1beta2 "github.com/vatesfr/cluster-api-provider-vates/api/v1beta2"
)

var _ = Describe("SetCondition", func() {
	It("adds a new condition to the status", func() {
		xm := &infrastructurev1beta2.VatesMachine{}
		SetCondition(xm, "Ready", metav1.ConditionTrue, "TestReason", "test message")
		Expect(xm.Status.Conditions).To(HaveLen(1))
		c := xm.Status.Conditions[0]
		Expect(c.Type).To(Equal("Ready"))
		Expect(c.Status).To(Equal(metav1.ConditionTrue))
		Expect(c.Reason).To(Equal("TestReason"))
		Expect(c.Message).To(Equal("test message"))
	})

	It("updates an existing condition", func() {
		xm := &infrastructurev1beta2.VatesMachine{}
		SetCondition(xm, "Ready", metav1.ConditionFalse, "OldReason", "old")
		SetCondition(xm, "Ready", metav1.ConditionTrue, "NewReason", "new")
		Expect(xm.Status.Conditions).To(HaveLen(1))
		c := xm.Status.Conditions[0]
		Expect(c.Status).To(Equal(metav1.ConditionTrue))
		Expect(c.Reason).To(Equal("NewReason"))
	})

	It("handles multiple condition types simultaneously", func() {
		xm := &infrastructurev1beta2.VatesMachine{}
		SetCondition(xm, "Ready", metav1.ConditionTrue, "R", "ready")
		SetCondition(xm, "Initialized", metav1.ConditionTrue, "I", "init")
		Expect(xm.Status.Conditions).To(HaveLen(2))
	})
})
