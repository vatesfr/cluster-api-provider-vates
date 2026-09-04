package xomachine

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1beta2 "github.com/vatesfr/cluster-api-provider-vates/api/v1beta2"
)

// GetXOCluster retrieves a XOCluster by namespace and cluster name.
func GetXOCluster(ctx context.Context, c client.Client, namespace, clusterName string) (*infrastructurev1beta2.XOCluster, error) {
	cluster := &clusterv1.Cluster{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: clusterName}, cluster); err != nil {
		return nil, err
	}

	if cluster.Spec.InfrastructureRef.Kind != "XOCluster" || cluster.Spec.InfrastructureRef.Name == "" {
		return nil, nil
	}

	xoCluster := &infrastructurev1beta2.XOCluster{}
	if err := c.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      cluster.Spec.InfrastructureRef.Name,
	}, xoCluster); err != nil {
		return nil, err
	}

	return xoCluster, nil
}

// GetOwnerMachine returns the CAPI Machine that references this XOMachine
// via OwnerReferences, avoiding a namespace-wide list.
func GetOwnerMachine(ctx context.Context, c client.Client, xoMachine *infrastructurev1beta2.XOMachine) (*clusterv1.Machine, error) {
	ownerRef := metav1.GetControllerOf(xoMachine)
	if ownerRef == nil {
		return nil, nil
	}

	machine := &clusterv1.Machine{}
	if err := c.Get(ctx, types.NamespacedName{
		Namespace: xoMachine.Namespace,
		Name:      ownerRef.Name,
	}, machine); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return nil, err
		}
		return nil, nil
	}

	if machine.UID != ownerRef.UID {
		return nil, nil
	}
	return machine, nil
}
