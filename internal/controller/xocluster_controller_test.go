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
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	yamlv3 "gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"
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
		scheme  *runtime.Scheme
		ctx     context.Context
		r       *XOClusterReconciler
		cluster *clusterv1.Cluster
	)

	BeforeEach(func() {
		scheme = runtime.NewScheme()
		Expect(clusterv1.AddToScheme(scheme)).To(Succeed())
		Expect(addonsv1.AddToScheme(scheme)).To(Succeed())
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		ctx = context.Background()
		r = &XOClusterReconciler{}
		cluster = &clusterv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "my-cluster", Namespace: "default"},
			Spec: clusterv1.ClusterSpec{
				ControlPlaneRef: clusterv1.ContractVersionedObjectReference{Kind: "KubeadmControlPlane", Name: "my-cluster-cp"},
			},
		}
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
		// A non-default namespace, so a regression to a hardcoded "default"
		// namespace fails here.
		const ns = "vates-prod"

		It("creates the ConfigMap and ClusterResourceSet in the cluster namespace", func() {
			r.Client = fake.NewClientBuilder().WithScheme(scheme).Build()

			Expect(r.ensureAddon(ctx, ns, "my-cluster", "cni", "cni-deployment-my-cluster", "cni-manifests-my-cluster", "ApplyOnce", "---\nkind: ConfigMap\n")).To(Succeed())

			cm := &corev1.ConfigMap{}
			Expect(r.Get(ctx, types.NamespacedName{Namespace: ns, Name: "cni-manifests-my-cluster"}, cm)).To(Succeed())
			Expect(cm.Data["cni.yaml"]).To(ContainSubstring("kind: ConfigMap"))

			crs := &addonsv1.ClusterResourceSet{}
			Expect(r.Get(ctx, types.NamespacedName{Namespace: ns, Name: "cni-deployment-my-cluster"}, crs)).To(Succeed())
			Expect(crs.Spec.Strategy).To(Equal("ApplyOnce"))
			Expect(crs.Spec.Resources).To(Equal([]addonsv1.ResourceRef{{Kind: "ConfigMap", Name: "cni-manifests-my-cluster"}}))
			Expect(crs.Spec.ClusterSelector.MatchLabels).To(HaveKeyWithValue("cluster.x-k8s.io/cluster-name", "my-cluster"))
		})

		It("removes the ConfigMap and ClusterResourceSet of a disabled addon", func() {
			cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "csi-manifests-my-cluster", Namespace: ns}, Data: map[string]string{"csi.yaml": "x"}}
			crs := &addonsv1.ClusterResourceSet{ObjectMeta: metav1.ObjectMeta{Name: "csi-deployment-my-cluster", Namespace: ns}}
			r.Client = fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm, crs).Build()

			Expect(r.removeAddon(ctx, ns, "my-cluster", "csi", "csi-deployment-my-cluster", "csi-manifests-my-cluster")).To(Succeed())

			Expect(r.Get(ctx, types.NamespacedName{Namespace: ns, Name: "csi-manifests-my-cluster"}, &corev1.ConfigMap{})).NotTo(Succeed())
			Expect(r.Get(ctx, types.NamespacedName{Namespace: ns, Name: "csi-deployment-my-cluster"}, &addonsv1.ClusterResourceSet{})).NotTo(Succeed())
		})

		It("is a no-op when removing an addon that was never created", func() {
			r.Client = fake.NewClientBuilder().WithScheme(scheme).Build()
			Expect(r.removeAddon(ctx, ns, "my-cluster", "cni", "cni-deployment-my-cluster", "cni-manifests-my-cluster")).To(Succeed())
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

			Expect(r.reconcileAddons(ctx, xoCluster, cluster, &xok8scommon.XoConfig{URL: "https://xo.test", Token: "tok", Insecure: true})).To(Succeed())

			for _, name := range []string{"ccm-deployment-my-cluster", "csi-deployment-my-cluster", "cni-deployment-my-cluster"} {
				crs := &addonsv1.ClusterResourceSet{}
				Expect(r.Get(ctx, types.NamespacedName{Namespace: "default", Name: name}, crs)).To(Succeed(), "expected %s", name)
			}
		})

		It("does not create the cni ClusterResourceSet when cni is none", func() {
			r.Client = fake.NewClientBuilder().WithScheme(scheme).Build()

			Expect(r.reconcileAddons(ctx, xoCluster, cluster, &xok8scommon.XoConfig{URL: "https://xo.test", Token: "tok", Insecure: true})).To(Succeed())

			Expect(r.Get(ctx, types.NamespacedName{Namespace: "default", Name: "cni-deployment-my-cluster"}, &addonsv1.ClusterResourceSet{})).NotTo(Succeed())
			Expect(r.Get(ctx, types.NamespacedName{Namespace: "default", Name: "ccm-deployment-my-cluster"}, &addonsv1.ClusterResourceSet{})).To(Succeed())
		})

		It("does not create the ccm ClusterResourceSet when ccm is disabled", func() {
			r.Client = fake.NewClientBuilder().WithScheme(scheme).Build()
			disabled := false
			xoCluster.Spec.Addons = &infrastructurev1beta2.AddonsSpec{CCM: &disabled}

			Expect(r.reconcileAddons(ctx, xoCluster, cluster, &xok8scommon.XoConfig{URL: "https://xo.test", Token: "tok", Insecure: true})).To(Succeed())

			Expect(r.Get(ctx, types.NamespacedName{Namespace: "default", Name: "ccm-deployment-my-cluster"}, &addonsv1.ClusterResourceSet{})).NotTo(Succeed())
			Expect(r.Get(ctx, types.NamespacedName{Namespace: "default", Name: "csi-deployment-my-cluster"}, &addonsv1.ClusterResourceSet{})).To(Succeed())
		})
	})

	Describe("CCM manifest", func() {
		var xoCluster *infrastructurev1beta2.XOCluster

		ccmYAML := func() string {
			cm := &corev1.ConfigMap{}
			Expect(r.Get(ctx, types.NamespacedName{Namespace: "default", Name: "ccm-manifests-my-cluster"}, cm)).To(Succeed())
			return cm.Data["ccm.yaml"]
		}

		BeforeEach(func() {
			r.Client = fake.NewClientBuilder().WithScheme(scheme).Build()
			xoCluster = &infrastructurev1beta2.XOCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "my-cluster", Namespace: "default"},
			}
		})

		It("uses the v1.2.0 image and runs the out-of-service controller by default", func() {
			Expect(r.reconcileAddons(ctx, xoCluster, cluster, &xok8scommon.XoConfig{URL: "https://xo.test", Token: "tok"})).To(Succeed())

			yaml := ccmYAML()
			Expect(yaml).To(ContainSubstring("ghcr.io/vatesfr/xenorchestra-cloud-controller-manager:v1.2.0"))
			Expect(yaml).To(ContainSubstring("--controllers=cloud-node,cloud-node-lifecycle,cloud-node-label-sync,cloud-node-out-of-service"))
		})

		It("does not render the out-of-service flags when no override is set", func() {
			Expect(r.reconcileAddons(ctx, xoCluster, cluster, &xok8scommon.XoConfig{URL: "https://xo.test", Token: "tok"})).To(Succeed())

			Expect(ccmYAML()).NotTo(ContainSubstring("--node-out-of-service-"))
		})

		It("omits the out-of-service controller and its flags when disabled", func() {
			disabled := false
			xoCluster.Spec.Addons = &infrastructurev1beta2.AddonsSpec{
				NodeOutOfService: &infrastructurev1beta2.NodeOutOfServiceSpec{
					Enabled:     &disabled,
					GracePeriod: &metav1.Duration{Duration: 2 * time.Minute},
				},
			}
			Expect(r.reconcileAddons(ctx, xoCluster, cluster, &xok8scommon.XoConfig{URL: "https://xo.test", Token: "tok"})).To(Succeed())

			yaml := ccmYAML()
			Expect(yaml).To(ContainSubstring("--controllers=cloud-node,cloud-node-lifecycle,cloud-node-label-sync\n"))
			Expect(yaml).NotTo(ContainSubstring("cloud-node-out-of-service"))
			Expect(yaml).NotTo(ContainSubstring("--node-out-of-service-"))
		})

		It("renders only the flags explicitly overridden", func() {
			xoCluster.Spec.Addons = &infrastructurev1beta2.AddonsSpec{
				NodeOutOfService: &infrastructurev1beta2.NodeOutOfServiceSpec{
					GracePeriod: &metav1.Duration{Duration: 2 * time.Minute},
				},
			}
			Expect(r.reconcileAddons(ctx, xoCluster, cluster, &xok8scommon.XoConfig{URL: "https://xo.test", Token: "tok"})).To(Succeed())

			yaml := ccmYAML()
			Expect(yaml).To(ContainSubstring("--node-out-of-service-grace-period=2m0s"))
			Expect(yaml).NotTo(ContainSubstring("--node-out-of-service-sync-period"))
		})

		It("renders both flags when both are overridden", func() {
			xoCluster.Spec.Addons = &infrastructurev1beta2.AddonsSpec{
				NodeOutOfService: &infrastructurev1beta2.NodeOutOfServiceSpec{
					SyncPeriod:  &metav1.Duration{Duration: 5 * time.Second},
					GracePeriod: &metav1.Duration{Duration: 2 * time.Minute},
				},
			}
			Expect(r.reconcileAddons(ctx, xoCluster, cluster, &xok8scommon.XoConfig{URL: "https://xo.test", Token: "tok"})).To(Succeed())

			yaml := ccmYAML()
			Expect(yaml).To(ContainSubstring("--node-out-of-service-sync-period=5s"))
			Expect(yaml).To(ContainSubstring("--node-out-of-service-grace-period=2m0s"))
		})
	})
})

var _ = Describe("Cilium CNI addon", func() {
	var (
		ctx       context.Context
		r         *XOClusterReconciler
		cluster   *clusterv1.Cluster
		xoCluster *infrastructurev1beta2.XOCluster
	)

	creds := &xok8scommon.XoConfig{URL: "https://xo.test", Token: "tok"}

	talosCluster := func() {
		cluster.Spec.ControlPlaneRef = clusterv1.ContractVersionedObjectReference{
			APIGroup: "controlplane.cluster.x-k8s.io", Kind: "TalosControlPlane", Name: "my-cluster-cp",
		}
	}

	withCilium := func(c *infrastructurev1beta2.CiliumSpec) {
		cni := "cilium"
		xoCluster.Spec.Addons = &infrastructurev1beta2.AddonsSpec{CNI: &cni, Cilium: c}
	}

	kpr := func() *infrastructurev1beta2.CiliumSpec {
		return &infrastructurev1beta2.CiliumSpec{KubeProxyReplacement: ptr.To(true)}
	}

	// ciliumYAML reconciles the addons and returns the Cilium manifest stored
	// in the ClusterResourceSet ConfigMap.
	ciliumYAML := func() string {
		Expect(r.reconcileAddons(ctx, xoCluster, cluster, creds)).To(Succeed())
		cm := &corev1.ConfigMap{}
		Expect(r.Get(ctx, types.NamespacedName{Namespace: "default", Name: "cni-manifests-my-cluster"}, cm)).To(Succeed())
		return cm.Data["cni.yaml"]
	}

	// apiEnv returns the KUBERNETES_SERVICE_HOST/PORT env entries rendered for
	// one container.
	apiEnv := func(host, port string) string {
		return "        - name: KUBERNETES_SERVICE_HOST\n          value: \"" + host + "\"\n" +
			"        - name: KUBERNETES_SERVICE_PORT\n          value: \"" + port + "\"\n"
	}

	BeforeEach(func() {
		scheme := runtime.NewScheme()
		Expect(clusterv1.AddToScheme(scheme)).To(Succeed())
		Expect(addonsv1.AddToScheme(scheme)).To(Succeed())
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		ctx = context.Background()
		r = &XOClusterReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).Build()}
		cluster = &clusterv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "my-cluster", Namespace: "default"},
			Spec: clusterv1.ClusterSpec{
				ControlPlaneRef: clusterv1.ContractVersionedObjectReference{
					APIGroup: "controlplane.cluster.x-k8s.io", Kind: "KubeadmControlPlane", Name: "my-cluster-cp",
				},
			},
		}
		xoCluster = &infrastructurev1beta2.XOCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "my-cluster", Namespace: "default"},
			Spec: infrastructurev1beta2.XOClusterSpec{
				ControlPlaneEndpoint: &infrastructurev1beta2.APIEndpoint{Host: "10.0.0.10", Port: 6443},
			},
		}
	})

	It("renders a valid YAML document stream in every mode", func() {
		for _, talos := range []bool{false, true} {
			for _, replace := range []bool{false, true} {
				data, err := ciliumData(xoCluster, &infrastructurev1beta2.CiliumSpec{KubeProxyReplacement: ptr.To(replace)}, talos)
				Expect(err).NotTo(HaveOccurred())
				manifest, err := renderCiliumManifest(data)
				Expect(err).NotTo(HaveOccurred())
				Expect(manifest).NotTo(ContainSubstring("{{"))

				docs := 0
				for doc := range strings.SplitSeq(manifest, "\n---\n") {
					obj := map[string]any{}
					Expect(yamlv3.Unmarshal([]byte(doc), &obj)).To(Succeed(), "talos=%v kpr=%v", talos, replace)
					if len(obj) > 0 {
						Expect(obj).To(HaveKey("kind"))
						docs++
					}
				}
				Expect(docs).To(Equal(strings.Count(manifest, "# Source: cilium/")))
			}
		}
	})

	Context("standard mode (kube-proxy enabled)", func() {
		It("keeps the previous manifest when no cilium settings are given", func() {
			withCilium(nil)
			yaml := ciliumYAML()

			Expect(yaml).To(ContainSubstring(`kube-proxy-replacement: "false"`))
			Expect(yaml).To(ContainSubstring(`enable-node-port: "false"`))
			Expect(yaml).NotTo(ContainSubstring("KUBERNETES_SERVICE_HOST"))
			Expect(yaml).To(ContainSubstring(`ipam: "cluster-pool"`))
			Expect(yaml).To(ContainSubstring(`cgroup-root: "/run/cilium/cgroupv2"`))
			Expect(yaml).To(ContainSubstring("- name: mount-cgroup"))
			Expect(strings.Count(yaml, "- SYS_MODULE")).To(Equal(2))

			crs := &addonsv1.ClusterResourceSet{}
			Expect(r.Get(ctx, types.NamespacedName{Namespace: "default", Name: "cni-deployment-my-cluster"}, crs)).To(Succeed())
			Expect(crs.Spec.Strategy).To(Equal("ApplyOnce"))
		})

		It("is the same manifest when kubeProxyReplacement is explicitly false", func() {
			withCilium(nil)
			implicit := ciliumYAML()
			withCilium(&infrastructurev1beta2.CiliumSpec{KubeProxyReplacement: ptr.To(false)})
			Expect(ciliumYAML()).To(Equal(implicit))
		})

		It("renders the Talos values on a Talos cluster", func() {
			talosCluster()
			withCilium(nil)
			yaml := ciliumYAML()

			Expect(yaml).To(ContainSubstring(`kube-proxy-replacement: "false"`))
			Expect(yaml).NotTo(ContainSubstring("KUBERNETES_SERVICE_HOST"))
			Expect(yaml).NotTo(ContainSubstring("SYS_MODULE"))
			Expect(yaml).NotTo(ContainSubstring("- name: mount-cgroup"))
			Expect(yaml).NotTo(ContainSubstring("/run/cilium/cgroupv2"))
			Expect(yaml).To(ContainSubstring(`cgroup-root: "/sys/fs/cgroup"`))
			Expect(yaml).To(ContainSubstring(`ipam: "kubernetes"`))
			Expect(yaml).NotTo(ContainSubstring("cluster-pool-ipv4-cidr"))
		})
	})

	Context("kube-proxy replacement mode", func() {
		It("uses Talos KubePrism as the API server endpoint on Talos", func() {
			talosCluster()
			withCilium(kpr())
			yaml := ciliumYAML()

			Expect(yaml).To(ContainSubstring(`kube-proxy-replacement: "true"`))
			Expect(yaml).NotTo(ContainSubstring(`enable-node-port: "false"`))
			// agent, config and clean-cilium-state containers, envoy and operator
			Expect(strings.Count(yaml, apiEnv("localhost", "7445"))).To(Equal(5))
			Expect(yaml).NotTo(ContainSubstring("SYS_MODULE"))
			Expect(yaml).To(ContainSubstring(`ipam: "kubernetes"`))
		})

		It("uses the explicit endpoint when set", func() {
			talosCluster()
			withCilium(&infrastructurev1beta2.CiliumSpec{
				KubeProxyReplacement: ptr.To(true),
				K8sServiceHost:       "api.example.test",
				K8sServicePort:       6443,
			})

			Expect(strings.Count(ciliumYAML(), apiEnv("api.example.test", "6443"))).To(Equal(5))
		})

		It("uses the control plane endpoint on non-Talos clusters", func() {
			withCilium(kpr())
			yaml := ciliumYAML()

			Expect(yaml).To(ContainSubstring(`kube-proxy-replacement: "true"`))
			Expect(strings.Count(yaml, apiEnv("10.0.0.10", "6443"))).To(Equal(5))
			Expect(strings.Count(yaml, "- SYS_MODULE")).To(Equal(2))
		})

		It("falls back to the discovered control plane endpoint", func() {
			xoCluster.Spec.ControlPlaneEndpoint = nil
			xoCluster.Status.ControlPlaneEndpoint = &infrastructurev1beta2.APIEndpoint{Host: "10.0.0.20", Port: 6443}
			withCilium(kpr())

			Expect(strings.Count(ciliumYAML(), apiEnv("10.0.0.20", "6443"))).To(Equal(5))
		})

		It("fails without any API server endpoint on non-Talos clusters", func() {
			xoCluster.Spec.ControlPlaneEndpoint = nil
			withCilium(kpr())

			Expect(r.reconcileAddons(ctx, xoCluster, cluster, creds)).To(MatchError(ContainSubstring("requires an API server endpoint")))
			Expect(r.Get(ctx, types.NamespacedName{Namespace: "default", Name: "cni-deployment-my-cluster"}, &addonsv1.ClusterResourceSet{})).NotTo(Succeed())
		})
	})

	Describe("API validation", func() {
		var name string

		create := func(addons *infrastructurev1beta2.AddonsSpec) error {
			name = "cilium-validation-" + strings.ToLower(rand.String(6))
			return k8sClient.Create(ctx, &infrastructurev1beta2.XOCluster{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
				Spec:       infrastructurev1beta2.XOClusterSpec{Addons: addons},
			})
		}

		AfterEach(func() {
			_ = k8sClient.Delete(ctx, &infrastructurev1beta2.XOCluster{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"}})
		})

		It("accepts kube-proxy replacement with cni cilium and defaults it to false", func() {
			Expect(create(&infrastructurev1beta2.AddonsSpec{CNI: ptr.To("cilium"), Cilium: kpr()})).To(Succeed())
			Expect(create(&infrastructurev1beta2.AddonsSpec{CNI: ptr.To("cilium"), Cilium: &infrastructurev1beta2.CiliumSpec{}})).To(Succeed())

			got := &infrastructurev1beta2.XOCluster{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, got)).To(Succeed())
			Expect(got.Spec.Addons.Cilium.KubeProxyReplacement).To(Equal(ptr.To(false)))
		})

		It("accepts an explicit API server endpoint", func() {
			Expect(create(&infrastructurev1beta2.AddonsSpec{CNI: ptr.To("cilium"), Cilium: &infrastructurev1beta2.CiliumSpec{
				KubeProxyReplacement: ptr.To(true), K8sServiceHost: "localhost", K8sServicePort: 7445,
			}})).To(Succeed())
		})

		It("rejects kube-proxy replacement without the cilium CNI", func() {
			Expect(create(&infrastructurev1beta2.AddonsSpec{Cilium: kpr()})).To(MatchError(ContainSubstring("requires cni to be cilium")))
			Expect(create(&infrastructurev1beta2.AddonsSpec{CNI: ptr.To("none"), Cilium: kpr()})).To(MatchError(ContainSubstring("requires cni to be cilium")))
		})

		It("rejects an incomplete API server endpoint", func() {
			Expect(create(&infrastructurev1beta2.AddonsSpec{CNI: ptr.To("cilium"), Cilium: &infrastructurev1beta2.CiliumSpec{
				KubeProxyReplacement: ptr.To(true), K8sServiceHost: "localhost",
			}})).To(MatchError(ContainSubstring("must be set together")))
		})

		It("rejects an API server endpoint without kube-proxy replacement", func() {
			Expect(create(&infrastructurev1beta2.AddonsSpec{CNI: ptr.To("cilium"), Cilium: &infrastructurev1beta2.CiliumSpec{
				K8sServiceHost: "localhost", K8sServicePort: 7445,
			}})).To(MatchError(ContainSubstring("require kubeProxyReplacement")))
		})
	})
})
