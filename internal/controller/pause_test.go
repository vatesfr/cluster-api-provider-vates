package controller

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	infra "github.com/vatesfr/cluster-api-provider-vates/api/v1beta2"
	xok8scommon "github.com/vatesfr/xenorchestra-k8s-common"
)

const (
	pauseTestClusterKind = "XOCluster"
	pauseTestMachineKind = "Machine"
)

var _ = Describe("Reconciliation pause", func() {
	var scheme *runtime.Scheme
	BeforeEach(func() {
		scheme = runtime.NewScheme()
		Expect(infra.AddToScheme(scheme)).To(Succeed())
		Expect(clusterv1.AddToScheme(scheme)).To(Succeed())
	})

	for _, kind := range []string{infra.KindXOMachine, pauseTestClusterKind} {
		for _, source := range []string{"infrastructure", "cluster", "owner", "label"} {
			if kind == pauseTestClusterKind && source == "owner" {
				continue
			}
			for _, deleting := range []bool{false, true} {
				It(fmt.Sprintf("pauses %s via %s and resumes (deleting=%t)", kind, source, deleting), func() {
					ctx := context.Background()
					cluster := &clusterv1.Cluster{
						ObjectMeta: metav1.ObjectMeta{Name: "cluster", Namespace: "default", UID: "cluster-uid"},
						Spec:       clusterv1.ClusterSpec{Paused: ptr.To(source == "cluster" || source == "label")},
					}
					machine := &clusterv1.Machine{
						ObjectMeta: metav1.ObjectMeta{Name: "machine", Namespace: "default", UID: "machine-uid"},
						Spec:       clusterv1.MachineSpec{ClusterName: cluster.Name},
					}
					if source == "owner" {
						machine.Annotations = map[string]string{clusterv1.PausedAnnotation: "false"}
					}
					objectMeta := metav1.ObjectMeta{Name: "infra", Namespace: "default", Generation: 3}
					if source == "infrastructure" {
						objectMeta.Annotations = map[string]string{clusterv1.PausedAnnotation: ""}
					}
					if source == "label" {
						objectMeta.Labels = map[string]string{clusterv1.ClusterNameLabel: cluster.Name}
					} else {
						ownerKind, ownerName, ownerUID := "Cluster", cluster.Name, cluster.UID
						if kind == infra.KindXOMachine {
							ownerKind, ownerName, ownerUID = pauseTestMachineKind, machine.Name, machine.UID
						}
						objectMeta.OwnerReferences = []metav1.OwnerReference{{
							APIVersion: clusterv1.GroupVersion.String(), Kind: ownerKind, Name: ownerName,
							UID: ownerUID, Controller: ptr.To(true),
						}}
					}
					finalizer := xoClusterFinalizer
					if kind == infra.KindXOMachine {
						finalizer = xoMachineFinalizer
					}
					if deleting {
						now := metav1.Now()
						objectMeta.DeletionTimestamp = &now
						objectMeta.Finalizers = []string{finalizer}
					}
					ready := metav1.Condition{
						Type: "Ready", Status: metav1.ConditionTrue, Reason: "Ready",
						LastTransitionTime: metav1.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
					}
					var obj client.Object
					var conditions *[]metav1.Condition
					if kind == infra.KindXOMachine {
						xo := &infra.XOMachine{ObjectMeta: objectMeta, Status: infra.XOMachineStatus{Conditions: []metav1.Condition{ready}}}
						obj, conditions = xo, &xo.Status.Conditions
					} else {
						xo := &infra.XOCluster{ObjectMeta: objectMeta, Status: infra.XOClusterStatus{Conditions: []metav1.Condition{ready}}}
						obj, conditions = xo, &xo.Status.Conditions
					}
					c := fake.NewClientBuilder().WithScheme(scheme).
						WithStatusSubresource(&infra.XOMachine{}, &infra.XOCluster{}).
						WithObjects(cluster, machine, obj).Build()
					var reconciler reconcile.Reconciler
					if kind == infra.KindXOMachine {
						reconciler = &XOMachineReconciler{Client: c, Scheme: scheme,
							newClientFunc: func(context.Context, *xok8scommon.XoConfig) (*xok8scommon.XoClient, error) {
								Fail("Paused reconciliation must not connect to XO")
								return nil, nil
							}}
					} else {
						reconciler = &XOClusterReconciler{Client: c, Scheme: scheme}
					}
					key := client.ObjectKeyFromObject(obj)
					for range 2 {
						result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))
						Expect(c.Get(ctx, key, obj)).To(Succeed())
						condition := meta.FindStatusCondition(*conditions, clusterv1.PausedCondition)
						Expect(condition).NotTo(BeNil())
						Expect(condition.Status).To(Equal(metav1.ConditionTrue))
						Expect(condition.ObservedGeneration).To(Equal(int64(3)))
						savedReady := meta.FindStatusCondition(*conditions, "Ready")
						Expect(savedReady).NotTo(BeNil())
						Expect(savedReady.Status).To(Equal(ready.Status))
						Expect(savedReady.Reason).To(Equal(ready.Reason))
						Expect(savedReady.LastTransitionTime.Equal(&ready.LastTransitionTime)).To(BeTrue())
						Expect(obj.GetFinalizers()).To(Equal(objectMeta.Finalizers))
					}
					// Lift the pause at its source; the next reconcile must resume work.
					switch source {
					case "infrastructure":
						obj.SetAnnotations(nil)
						Expect(c.Update(ctx, obj)).To(Succeed())
					case "owner":
						machine.Annotations = nil
						Expect(c.Update(ctx, machine)).To(Succeed())
					default:
						cluster.Spec.Paused = ptr.To(false)
						Expect(c.Update(ctx, cluster)).To(Succeed())
					}
					_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
					Expect(err).NotTo(HaveOccurred())
					if deleting {
						Expect(c.Get(ctx, key, obj)).To(MatchError(ContainSubstring("not found")))
					} else {
						Expect(c.Get(ctx, key, obj)).To(Succeed())
						Expect(meta.IsStatusConditionFalse(*conditions, clusterv1.PausedCondition)).To(BeTrue())
						Expect(obj.GetFinalizers()).To(ContainElement(finalizer))
					}
				})
			}
		}
	}

	It("maps Cluster transitions to its machines only, deduplicating labelled infrastructure", func() {
		cluster := &clusterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "target", Namespace: "default"}}
		makeMachine := func(name, namespace, clusterName string) *clusterv1.Machine {
			return &clusterv1.Machine{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
				Spec: clusterv1.MachineSpec{ClusterName: clusterName,
					InfrastructureRef: clusterv1.ContractVersionedObjectReference{Kind: infra.KindXOMachine, Name: name}}}
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
			makeMachine("owned", "default", "target"), makeMachine("other", "default", "other"),
			makeMachine("other-ns", "other", "target"),
			&infra.XOMachine{ObjectMeta: metav1.ObjectMeta{Name: "owned", Namespace: "default",
				Labels: map[string]string{clusterv1.ClusterNameLabel: "target"}}},
			&infra.XOMachine{ObjectMeta: metav1.ObjectMeta{Name: "orphan", Namespace: "default",
				Labels: map[string]string{clusterv1.ClusterNameLabel: "target"}}},
		).Build()
		r := &XOMachineReconciler{Client: c}
		Expect(r.clusterToXOMachines(context.Background(), cluster)).To(ConsistOf(
			reconcile.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "owned"}},
			reconcile.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "orphan"}},
		))
	})

	It("resumes XOMachine automatically when the Cluster is unpaused", func() {
		ctx := context.Background()
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "pause-watch-"}}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, ns)).To(Succeed()) })
		cluster := &clusterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster", Namespace: ns.Name},
			Spec: clusterv1.ClusterSpec{Paused: ptr.To(true)}}
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		xo := &infra.XOMachine{ObjectMeta: metav1.ObjectMeta{Name: "machine", Namespace: ns.Name,
			Labels: map[string]string{clusterv1.ClusterNameLabel: cluster.Name}},
			Spec: infra.XOMachineSpec{TemplateName: "ubuntu"}}
		Expect(k8sClient.Create(ctx, xo)).To(Succeed())
		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme: k8sClient.Scheme(), Metrics: metricsserver.Options{BindAddress: "0"},
			Cache: cache.Options{DefaultNamespaces: map[string]cache.Config{ns.Name: {}}},
		})
		Expect(err).NotTo(HaveOccurred())
		r := &XOMachineReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme()}
		Expect(r.SetupWithManager(mgr)).To(Succeed())
		managerCtx, stop := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() { done <- mgr.Start(managerCtx) }()
		DeferCleanup(func() {
			stop()
			Eventually(done, 10*time.Second).Should(Receive(Succeed()))
		})
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(xo), xo)).To(Succeed())
			g.Expect(meta.IsStatusConditionTrue(xo.Status.Conditions, clusterv1.PausedCondition)).To(BeTrue())
			g.Expect(xo.Finalizers).To(BeEmpty())
		}, 10*time.Second).Should(Succeed())
		cluster.Spec.Paused = ptr.To(false)
		Expect(k8sClient.Update(ctx, cluster)).To(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(xo), xo)).To(Succeed())
			g.Expect(meta.IsStatusConditionFalse(xo.Status.Conditions, clusterv1.PausedCondition)).To(BeTrue())
			g.Expect(xo.Finalizers).To(ContainElement(xoMachineFinalizer))
		}, 10*time.Second).Should(Succeed())
	})
})
