package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// pauseMessage checks the infrastructure object's annotation and the cluster-wide pause.
// An annotation pauses an object regardless of its value.
func pauseMessage(obj client.Object, cluster *clusterv1.Cluster) string {
	if annotations.HasPaused(obj) {
		return "Object has the cluster.x-k8s.io/paused annotation"
	}
	if cluster != nil && cluster.Spec.Paused != nil && *cluster.Spec.Paused {
		return "Cluster spec.paused is set to true"
	}
	return ""
}

// patchPausedCondition only writes status when the pause state changes. Leave the
// condition absent on objects that have never been paused to avoid unnecessary writes.
func patchPausedCondition(ctx context.Context, c client.Client, obj client.Object, conditions *[]metav1.Condition, message string) error {
	previous := meta.FindStatusCondition(*conditions, clusterv1.PausedCondition)
	if message == "" && previous == nil {
		return nil
	}
	condition := metav1.Condition{
		Type: clusterv1.PausedCondition, Status: metav1.ConditionFalse,
		Reason: clusterv1.NotPausedReason, ObservedGeneration: obj.GetGeneration(),
	}
	if message != "" {
		condition.Status = metav1.ConditionTrue
		condition.Reason = clusterv1.PausedReason
		condition.Message = message
	}
	base := obj.DeepCopyObject().(client.Object)
	if !meta.SetStatusCondition(conditions, condition) {
		return nil
	}
	return c.Status().Patch(ctx, obj, client.MergeFrom(base))
}

func clusterForPause(ctx context.Context, c client.Client, namespace, name string) (*clusterv1.Cluster, error) {
	if name == "" {
		return nil, nil
	}
	cluster := &clusterv1.Cluster{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, cluster); err != nil {
		return nil, client.IgnoreNotFound(err)
	}
	return cluster, nil
}
