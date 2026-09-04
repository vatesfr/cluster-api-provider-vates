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
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	infrastructurev1beta2 "github.com/vatesfr/cluster-api-provider-vates/api/v1beta2"
	"github.com/vatesfr/cluster-api-provider-vates/internal/bootstrap"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

var _ = Describe("XOMachine Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		vatesMachine := &infrastructurev1beta2.XOMachine{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind XOMachine")
			err := k8sClient.Get(ctx, typeNamespacedName, vatesMachine)
			if err != nil && errors.IsNotFound(err) {
				resource := &infrastructurev1beta2.XOMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: infrastructurev1beta2.XOMachineSpec{
						TemplateName: "ubuntu-22.04",
						NamePrefix:   resourceName,
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &infrastructurev1beta2.XOMachine{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance XOMachine")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &XOMachineReconciler{
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

var _ = Describe("buildVMName", func() {
	r := &XOMachineReconciler{}

	machine := func(name string) *clusterv1.Machine {
		return &clusterv1.Machine{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "default",
				Labels:    map[string]string{"cluster.x-k8s.io/cluster-name": "demo3"},
			},
		}
	}

	It("appends a unique machine suffix to control plane VMs", func() {
		vm := &infrastructurev1beta2.XOMachine{Spec: infrastructurev1beta2.XOMachineSpec{NamePrefix: "PFT--cp"}}
		name := r.buildVMName(vm, bootstrap.ResolveBootstrapDataResult{Machine: machine("demo3-cp-7n62g")})
		Expect(name).To(Equal("PFT-demo3-cp-7n62g"))
	})

	It("appends a unique machine suffix to worker VMs", func() {
		vm := &infrastructurev1beta2.XOMachine{Spec: infrastructurev1beta2.XOMachineSpec{NamePrefix: "PFT--worker"}}
		name := r.buildVMName(vm, bootstrap.ResolveBootstrapDataResult{Machine: machine("demo3-md-0-hdvzv-48gx5")})
		Expect(name).To(Equal("PFT-demo3-worker-48gx5"))
	})

	It("falls back to the name prefix without a Machine", func() {
		vm := &infrastructurev1beta2.XOMachine{Spec: infrastructurev1beta2.XOMachineSpec{NamePrefix: "PFT--cp"}}
		name := r.buildVMName(vm, bootstrap.ResolveBootstrapDataResult{})
		Expect(name).To(Equal("PFT--cp"))
	})
})
