package vatesmachine

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	infrastructurev1beta2 "github.com/vatesfr/cluster-api-provider-vates/api/v1beta2"
)

// UpdateCondition sets the "Ready" condition on the VatesMachine and persists
// it via the status sub-resource. A condition update error is returned, if any.
func UpdateCondition(ctx context.Context, c client.Client, vatesMachine *infrastructurev1beta2.VatesMachine, status metav1.ConditionStatus, reason, msg string) error {
	SetCondition(vatesMachine, "Ready", status, reason, msg)
	return c.Status().Update(ctx, vatesMachine)
}

// SetCondition updates or adds a condition in the object's Conditions slice.
// LastTransitionTime is only updated when the condition status changes.
func SetCondition(vatesMachine *infrastructurev1beta2.VatesMachine, conditionType string, status metav1.ConditionStatus, reason, message string) {
	now := metav1.Now()
	for i, c := range vatesMachine.Status.Conditions {
		if c.Type == conditionType {
			// Only update LastTransitionTime when the status actually changes
			if c.Status != status {
				vatesMachine.Status.Conditions[i].LastTransitionTime = now
			}
			vatesMachine.Status.Conditions[i].Status = status
			vatesMachine.Status.Conditions[i].Reason = reason
			vatesMachine.Status.Conditions[i].Message = message
			return
		}
	}
	vatesMachine.Status.Conditions = append(vatesMachine.Status.Conditions, metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: now,
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
func WithConditionUpdate(ctx context.Context, c client.Client, vatesMachine *infrastructurev1beta2.VatesMachine, original error, status metav1.ConditionStatus, reason string) error {
	logger := log.FromContext(ctx)
	logger.Error(original, reason)
	if condErr := UpdateCondition(ctx, c, vatesMachine, status, reason, original.Error()); condErr != nil {
		return &ConditionError{Original: original, CondErr: condErr}
	}
	return original
}
