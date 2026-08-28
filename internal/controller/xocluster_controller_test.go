/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	infrastructurev1beta2 "github.com/vatesfr/cluster-api-provider-vates/api/v1beta2"
)

var _ = Describe("XOCluster Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		vatesCluster := &infrastructurev1beta2.XOCluster{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind XOCluster")
			err := k8sClient.Get(ctx, typeNamespacedName, vatesCluster)
			if err != nil && errors.IsNotFound(err) {
				resource := &infrastructurev1beta2.XOCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: infrastructurev1beta2.XOClusterSpec{},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &infrastructurev1beta2.XOCluster{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance XOCluster")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &XOClusterReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
		})
	})
})

var _ = Describe("ensureClusterNameLabel", func() {
	var (
		scheme *runtime.Scheme
		ctx    context.Context
		r      *XOClusterReconciler
	)

	BeforeEach(func() {
		scheme = runtime.NewScheme()
		Expect(clusterv1.AddToScheme(scheme)).To(Succeed())
		ctx = context.Background()
		r = &XOClusterReconciler{}
	})

	It("adds the cluster-name label when the Cluster has no labels", func() {
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
			&clusterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "my-cluster", Namespace: "default"},
			},
		).Build()
		r.Client = fakeClient

		cluster := &clusterv1.Cluster{}
		Expect(fakeClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "my-cluster"}, cluster)).To(Succeed())
		Expect(r.ensureClusterNameLabel(ctx, cluster)).To(Succeed())

		updated := &clusterv1.Cluster{}
		Expect(fakeClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "my-cluster"}, updated)).To(Succeed())
		Expect(updated.Labels[clusterv1.ClusterNameLabel]).To(Equal("my-cluster"))
	})

	It("is a no-op when the label already matches the Cluster name", func() {
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
			&clusterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-cluster",
					Namespace: "default",
					Labels:    map[string]string{clusterv1.ClusterNameLabel: "my-cluster"},
				},
			},
		).Build()
		r.Client = fakeClient

		cluster := &clusterv1.Cluster{}
		Expect(fakeClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "my-cluster"}, cluster)).To(Succeed())
		Expect(r.ensureClusterNameLabel(ctx, cluster)).To(Succeed())

		updated := &clusterv1.Cluster{}
		Expect(fakeClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "my-cluster"}, updated)).To(Succeed())
		Expect(updated.Labels[clusterv1.ClusterNameLabel]).To(Equal("my-cluster"))
		Expect(updated.ResourceVersion).To(Equal(cluster.ResourceVersion))
	})
})
