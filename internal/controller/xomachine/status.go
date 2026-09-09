package xomachine

import (
	"context"
	"fmt"
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	infrastructurev1beta2 "github.com/vatesfr/cluster-api-provider-vates/api/v1beta2"
)

// UpdateCondition sets the "Ready" condition on the XOMachine and persists
// it via the status sub-resource. A condition update error is returned, if any.
func UpdateCondition(ctx context.Context, c client.Client, xoMachine *infrastructurev1beta2.XOMachine, status metav1.ConditionStatus, reason, msg string) error {
	SetCondition(xoMachine, "Ready", status, reason, msg)
	return c.Status().Update(ctx, xoMachine)
}

// SetCondition updates or adds a condition in the object's Conditions slice.
// LastTransitionTime is only updated when the condition status changes.
func SetCondition(xoMachine *infrastructurev1beta2.XOMachine, conditionType string, status metav1.ConditionStatus, reason, message string) {
	i := slices.IndexFunc(xoMachine.Status.Conditions, func(c metav1.Condition) bool {
		return c.Type == conditionType
	})

	if i >= 0 {
		c := &xoMachine.Status.Conditions[i]
		if c.Status != status {
			c.LastTransitionTime = metav1.Now()
		}
		c.Status = status
		c.Reason = reason
		c.Message = message
		return
	}

	xoMachine.Status.Conditions = append(xoMachine.Status.Conditions, metav1.Condition{
		Type: conditionType, Status: status, Reason: reason, Message: message,
		LastTransitionTime: metav1.Now(),
	})
}

// ConditionError is a wrapper that combines an original error with a condition
// update attempt. It ensures the caller sees both failures if the condition
// update itself fails.
type ConditionError struct {
	Original error
	CondErr  error
}

func (e *ConditionError) Error() string {
	return fmt.Sprintf("%s (condition update failed: %s)", e.Original.Error(), e.CondErr.Error())
}

func (e *ConditionError) Unwrap() error {
	return e.Original
}

// WithConditionUpdate calls updateCondition and returns a ConditionError if
// either the original error or the condition update fails.
func WithConditionUpdate(ctx context.Context, c client.Client, xoMachine *infrastructurev1beta2.XOMachine, original error, status metav1.ConditionStatus, reason string) error {
	logger := log.FromContext(ctx)
	logger.Error(original, reason)
	if condErr := UpdateCondition(ctx, c, xoMachine, status, reason, original.Error()); condErr != nil {
		return &ConditionError{Original: original, CondErr: condErr}
	}
	return original
}
