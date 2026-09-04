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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	addonsv1 "sigs.k8s.io/cluster-api/api/addons/v1beta2"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	infrastructurev1beta2 "github.com/vatesfr/cluster-api-provider-vates/api/v1beta2"
	xok8scommon "github.com/vatesfr/xenorchestra-k8s-common"
)

var _ = Describe("XOCluster Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		xoCluster := &infrastructurev1beta2.XOCluster{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind XOCluster")
			err := k8sClient.Get(ctx, typeNamespacedName, xoCluster)
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

var _ = Describe("addons", func() {
	var (
		scheme *runtime.Scheme
		ctx    context.Context
		r      *XOClusterReconciler
	)

	BeforeEach(func() {
		scheme = runtime.NewScheme()
		Expect(clusterv1.AddToScheme(scheme)).To(Succeed())
		Expect(addonsv1.AddToScheme(scheme)).To(Succeed())
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		ctx = context.Background()
		r = &XOClusterReconciler{}
	})

	Describe("defaultAddons", func() {
		It("defaults to CCM+CSI enabled and no CNI when nil", func() {
			a := defaultAddons(nil)
			Expect(*a.CCM).To(BeTrue())
			Expect(*a.CSI).To(BeTrue())
			Expect(*a.CNI).To(Equal("none"))
		})

		It("fills only the unset fields", func() {
			cni := "cilium"
			a := defaultAddons(&infrastructurev1beta2.AddonsSpec{CNI: &cni})
			Expect(*a.CCM).To(BeTrue())
			Expect(*a.CSI).To(BeTrue())
			Expect(*a.CNI).To(Equal("cilium"))
		})
	})

	Describe("ensureAddon / removeAddon", func() {
		It("creates a ConfigMap and a ClusterResourceSet for an addon", func() {
			r.Client = fake.NewClientBuilder().WithScheme(scheme).Build()

			Expect(r.ensureAddon(ctx, "my-cluster", "cni", "cni-deployment-my-cluster", "cni-manifests-my-cluster", "ApplyOnce", "---\nkind: ConfigMap\n")).To(Succeed())

			cm := &corev1.ConfigMap{}
			Expect(r.Get(ctx, types.NamespacedName{Namespace: "default", Name: "cni-manifests-my-cluster"}, cm)).To(Succeed())
			Expect(cm.Data["cni.yaml"]).To(ContainSubstring("kind: ConfigMap"))

			crs := &addonsv1.ClusterResourceSet{}
			Expect(r.Get(ctx, types.NamespacedName{Namespace: "default", Name: "cni-deployment-my-cluster"}, crs)).To(Succeed())
			Expect(crs.Spec.Strategy).To(Equal("ApplyOnce"))
			Expect(crs.Spec.Resources).To(Equal([]addonsv1.ResourceRef{{Kind: "ConfigMap", Name: "cni-manifests-my-cluster"}}))
			Expect(crs.Spec.ClusterSelector.MatchLabels).To(HaveKeyWithValue("cluster.x-k8s.io/cluster-name", "my-cluster"))
		})

		It("removes the ConfigMap and ClusterResourceSet of a disabled addon", func() {
			cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "csi-manifests-my-cluster", Namespace: "default"}, Data: map[string]string{"csi.yaml": "x"}}
			crs := &addonsv1.ClusterResourceSet{ObjectMeta: metav1.ObjectMeta{Name: "csi-deployment-my-cluster", Namespace: "default"}}
			r.Client = fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm, crs).Build()

			Expect(r.removeAddon(ctx, "my-cluster", "csi", "csi-deployment-my-cluster", "csi-manifests-my-cluster")).To(Succeed())

			Expect(r.Get(ctx, types.NamespacedName{Namespace: "default", Name: "csi-manifests-my-cluster"}, &corev1.ConfigMap{})).NotTo(Succeed())
			Expect(r.Get(ctx, types.NamespacedName{Namespace: "default", Name: "csi-deployment-my-cluster"}, &addonsv1.ClusterResourceSet{})).NotTo(Succeed())
		})

		It("is a no-op when removing an addon that was never created", func() {
			r.Client = fake.NewClientBuilder().WithScheme(scheme).Build()
			Expect(r.removeAddon(ctx, "my-cluster", "cni", "cni-deployment-my-cluster", "cni-manifests-my-cluster")).To(Succeed())
		})
	})

	Describe("reconcileAddons", func() {
		var xoCluster *infrastructurev1beta2.XOCluster

		BeforeEach(func() {
			xoCluster = &infrastructurev1beta2.XOCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "my-cluster", Namespace: "default"},
			}
		})

		It("creates ccm, csi and cni ClusterResourceSets when all addons enabled", func() {
			r.Client = fake.NewClientBuilder().WithScheme(scheme).Build()
			cni := "cilium"
			xoCluster.Spec.Addons = &infrastructurev1beta2.AddonsSpec{CNI: &cni}

			Expect(r.reconcileAddons(ctx, xoCluster, "my-cluster", &xok8scommon.XoConfig{URL: "https://xo.test", Token: "tok", Insecure: true})).To(Succeed())

			for _, name := range []string{"ccm-deployment-my-cluster", "csi-deployment-my-cluster", "cni-deployment-my-cluster"} {
				crs := &addonsv1.ClusterResourceSet{}
				Expect(r.Get(ctx, types.NamespacedName{Namespace: "default", Name: name}, crs)).To(Succeed(), "expected %s", name)
			}
		})

		It("does not create the cni ClusterResourceSet when cni is none", func() {
			r.Client = fake.NewClientBuilder().WithScheme(scheme).Build()

			Expect(r.reconcileAddons(ctx, xoCluster, "my-cluster", &xok8scommon.XoConfig{URL: "https://xo.test", Token: "tok", Insecure: true})).To(Succeed())

			Expect(r.Get(ctx, types.NamespacedName{Namespace: "default", Name: "cni-deployment-my-cluster"}, &addonsv1.ClusterResourceSet{})).NotTo(Succeed())
			Expect(r.Get(ctx, types.NamespacedName{Namespace: "default", Name: "ccm-deployment-my-cluster"}, &addonsv1.ClusterResourceSet{})).To(Succeed())
		})

		It("does not create the ccm ClusterResourceSet when ccm is disabled", func() {
			r.Client = fake.NewClientBuilder().WithScheme(scheme).Build()
			disabled := false
			xoCluster.Spec.Addons = &infrastructurev1beta2.AddonsSpec{CCM: &disabled}

			Expect(r.reconcileAddons(ctx, xoCluster, "my-cluster", &xok8scommon.XoConfig{URL: "https://xo.test", Token: "tok", Insecure: true})).To(Succeed())

			Expect(r.Get(ctx, types.NamespacedName{Namespace: "default", Name: "ccm-deployment-my-cluster"}, &addonsv1.ClusterResourceSet{})).NotTo(Succeed())
			Expect(r.Get(ctx, types.NamespacedName{Namespace: "default", Name: "csi-deployment-my-cluster"}, &addonsv1.ClusterResourceSet{})).To(Succeed())
		})
	})
})
