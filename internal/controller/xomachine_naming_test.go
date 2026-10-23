package controller

import (
	"context"
	"time"

	"github.com/gofrs/uuid"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	xoclient "github.com/vatesfr/xenorchestra-go-sdk/client"
	"github.com/vatesfr/xenorchestra-go-sdk/pkg/payloads"
	xok8scommon "github.com/vatesfr/xenorchestra-k8s-common"
	k8smocks "github.com/vatesfr/xenorchestra-k8s-common/mocks"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	infrastructurev1beta2 "github.com/vatesfr/cluster-api-provider-vates/api/v1beta2"
	"github.com/vatesfr/cluster-api-provider-vates/internal/bootstrap"
)

var _ = Describe("VM naming", func() {
	const (
		ns            = "default"
		clusterName   = "my-cluster"
		xoClusterName = "my-cluster-x8kz2"
		cpPrefix      = "CAPI-CP-"
		workerPrefix  = "CAPI-WORKER-"
	)

	var (
		ctx      context.Context
		scheme   *runtime.Scheme
		recorder *events.FakeRecorder
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		Expect(infrastructurev1beta2.AddToScheme(scheme)).To(Succeed())
		Expect(clusterv1.AddToScheme(scheme)).To(Succeed())
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		recorder = events.NewFakeRecorder(10)
	})

	cluster := func() *clusterv1.Cluster {
		return &clusterv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: ns},
			Spec: clusterv1.ClusterSpec{
				InfrastructureRef: clusterv1.ContractVersionedObjectReference{
					APIGroup: infrastructurev1beta2.GroupVersion.Group,
					Kind:     "XOCluster",
					Name:     xoClusterName,
				},
			},
		}
	}

	xoCluster := func(naming *infrastructurev1beta2.VMNamingSpec) *infrastructurev1beta2.XOCluster {
		return &infrastructurev1beta2.XOCluster{
			ObjectMeta: metav1.ObjectMeta{Name: xoClusterName, Namespace: ns},
			Spec:       infrastructurev1beta2.XOClusterSpec{VMNaming: naming},
		}
	}

	naming := func() *infrastructurev1beta2.VMNamingSpec {
		return &infrastructurev1beta2.VMNamingSpec{ControlPlanePrefix: cpPrefix, WorkerPrefix: workerPrefix}
	}

	machine := func(name string, controlPlane bool) *clusterv1.Machine {
		labels := map[string]string{clusterv1.ClusterNameLabel: clusterName}
		if controlPlane {
			labels[clusterv1.MachineControlPlaneLabel] = ""
		}
		return &clusterv1.Machine{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: labels}}
	}

	xoMachine := func(name string) *infrastructurev1beta2.XOMachine {
		return &infrastructurev1beta2.XOMachine{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: ns,
				Labels:    map[string]string{clusterv1.ClusterNameLabel: clusterName},
			},
		}
	}

	reconciler := func(objs ...client.Object) *XOMachineReconciler {
		c := fake.NewClientBuilder().WithScheme(scheme).
			WithStatusSubresource(&infrastructurev1beta2.XOMachine{}).
			WithObjects(objs...).Build()
		return &XOMachineReconciler{Client: c, Scheme: scheme, Recorder: recorder}
	}

	fetch := func(r *XOMachineReconciler, name string) *infrastructurev1beta2.XOMachine {
		m := &infrastructurev1beta2.XOMachine{}
		Expect(r.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, m)).To(Succeed())
		return m
	}

	Describe("resolveVMName", func() {
		It("names three control planes and two workers from the XOCluster vmNaming", func() {
			cases := []struct {
				machine      string
				controlPlane bool
				want         string
			}{
				{"my-cluster-x8kz2-7n62g", true, cpPrefix + "7n62g"},
				{"my-cluster-x8kz2-k4xzb", true, cpPrefix + "k4xzb"},
				{"my-cluster-x8kz2-q9tmw", true, cpPrefix + "q9tmw"},
				{"my-cluster-md-0-hdvzv-5bxt9-48gx5", false, workerPrefix + "48gx5"},
				{"my-cluster-md-0-hdvzv-5bxt9-h2pvr", false, workerPrefix + "h2pvr"},
			}
			for _, tc := range cases {
				xm := xoMachine(tc.machine)
				r := reconciler(cluster(), xoCluster(naming()), xm)

				name, err := r.resolveVMName(ctx, xm,
					bootstrap.ResolveBootstrapDataResult{Machine: machine(tc.machine, tc.controlPlane)})
				Expect(err).NotTo(HaveOccurred())
				Expect(name).To(Equal(tc.want))
				Expect(fetch(r, tc.machine).Status.VMName).To(Equal(tc.want), "the name is recorded before the VM is created")
			}
		})

		It("keeps the recorded name when the XOCluster vmNaming changes", func() {
			xm := xoMachine("my-cluster-x8kz2-7n62g")
			xc := xoCluster(naming())
			r := reconciler(cluster(), xc, xm)
			bs := bootstrap.ResolveBootstrapDataResult{Machine: machine(xm.Name, true)}

			first, err := r.resolveVMName(ctx, xm, bs)
			Expect(err).NotTo(HaveOccurred())

			xc.Spec.VMNaming.ControlPlanePrefix = "CHANGED-"
			Expect(r.Update(ctx, xc)).To(Succeed())

			again, err := r.resolveVMName(ctx, fetch(r, xm.Name), bs)
			Expect(err).NotTo(HaveOccurred())
			Expect(again).To(Equal(first))
		})

		It("names VMs <DISTRIBUTION>-<cluster>-<ROLE>-<ID> when the XOCluster sets no vmNaming", func() {
			cases := []struct {
				bootstrapKind string
				controlPlane  bool
				want          string
			}{
				{"TalosConfig", true, "TALOS-my-cluster-CP-7n62g"},
				{"TalosConfig", false, "TALOS-my-cluster-WORKER-7n62g"},
				{"KubeadmConfig", true, "KUBEADM-my-cluster-CP-7n62g"},
				{"KubeadmConfig", false, "KUBEADM-my-cluster-WORKER-7n62g"},
				{"VatesConfig", true, "VATES-my-cluster-CP-7n62g"},
				{"VatesConfig", false, "VATES-my-cluster-WORKER-7n62g"},
				{"RKE2Config", true, "RKE2-my-cluster-CP-7n62g"},
				{"", false, "CAPI-my-cluster-WORKER-7n62g"},
			}
			for _, tc := range cases {
				xm := xoMachine("my-cluster-x8kz2-7n62g")
				r := reconciler(cluster(), xoCluster(nil), xm)
				m := machine(xm.Name, tc.controlPlane)
				m.Spec.Bootstrap.ConfigRef.Kind = tc.bootstrapKind

				name, err := r.resolveVMName(ctx, xm, bootstrap.ResolveBootstrapDataResult{Machine: m})
				Expect(err).NotTo(HaveOccurred())
				Expect(name).To(Equal(tc.want), "bootstrap kind %q", tc.bootstrapKind)
				Expect(fetch(r, xm.Name).Status.VMName).To(Equal(tc.want))
			}
		})

		It("names the VM of an XOMachine without an owner Machine after the XOMachine", func() {
			xm := xoMachine("standalone")
			r := reconciler(cluster(), xoCluster(nil), xm)

			name, err := r.resolveVMName(ctx, xm, bootstrap.ResolveBootstrapDataResult{})
			Expect(err).NotTo(HaveOccurred())
			Expect(name).To(Equal("standalone"))
		})

		It("waits instead of naming the VM when the XOCluster cannot be read", func() {
			xm := xoMachine("my-cluster-x8kz2-7n62g")
			r := reconciler(cluster(), xm) // the XOCluster does not exist

			_, err := r.resolveVMName(ctx, xm,
				bootstrap.ResolveBootstrapDataResult{Machine: machine(xm.Name, true)})
			Expect(err).To(HaveOccurred())
			updated := fetch(r, xm.Name)
			Expect(updated.Status.VMName).To(BeEmpty())
			Expect(updated.Status.Conditions).To(ContainElement(HaveField("Reason", "VMNameUnresolved")))
		})

		It("rejects vmNaming without an owner Machine", func() {
			xm := xoMachine("my-cluster-x8kz2-7n62g")
			r := reconciler(cluster(), xoCluster(naming()), xm)

			_, err := r.resolveVMName(ctx, xm, bootstrap.ResolveBootstrapDataResult{})
			Expect(err).To(HaveOccurred())
			Expect(fetch(r, xm.Name).Status.VMName).To(BeEmpty())
		})

		It("names a Talos control plane VM after its Machine, not its XOMachine", func() {
			// TalosControlPlane (CACPPT v0.5.x) names the Machine
			// names.SimpleNameGenerator.GenerateName(tcp.Name + "-") and clones
			// the XOMachine from the template under another random name, with
			// the cluster-name and control-plane labels.
			xm := xoMachine("talos-xomachinetemplate-cp-xk2p9")
			xm.Labels[clusterv1.MachineControlPlaneLabel] = ""
			r := reconciler(cluster(), xoCluster(naming()), xm)

			name, err := r.resolveVMName(ctx, xm,
				bootstrap.ResolveBootstrapDataResult{Machine: machine("my-cluster-cp-4hgx7", true)})
			Expect(err).NotTo(HaveOccurred())
			Expect(name).To(Equal(cpPrefix + "4hgx7"))
		})

		It("names a VM after a Machine name cut to 63 characters", func() {
			// Over 58 characters, the generators keep the first 58 and append
			// the 5 random characters without a dash.
			tcpName := "my-cluster-a-very-long-talos-control-plane-name-for-unit-tests"
			machineName := (tcpName + "-")[:58] + "k2pq7"
			xm := xoMachine("talos-xomachinetemplate-cp-xk2p9")
			r := reconciler(cluster(), xoCluster(naming()), xm)

			name, err := r.resolveVMName(ctx, xm,
				bootstrap.ResolveBootstrapDataResult{Machine: machine(machineName, true)})
			Expect(err).NotTo(HaveOccurred())
			Expect(name).To(Equal(cpPrefix + "k2pq7"))
		})

		It("names a VM after the whole Machine name when it has no random suffix, and keeps it", func() {
			// A control plane provider may name its Machines <cluster>-<index>.
			xm := xoMachine("vmn-0")
			r := reconciler(cluster(), xoCluster(naming()), xm)
			bs := bootstrap.ResolveBootstrapDataResult{Machine: machine("vmn-0", true)}

			name, err := r.resolveVMName(ctx, xm, bs)
			Expect(err).NotTo(HaveOccurred())
			Expect(name).To(Equal(cpPrefix + "vmn-0"))
			Expect(fetch(r, xm.Name).Status.VMName).To(Equal(cpPrefix + "vmn-0"))

			again, err := r.resolveVMName(ctx, fetch(r, xm.Name), bs)
			Expect(err).NotTo(HaveOccurred())
			Expect(again).To(Equal(name))
		})
	})

	Describe("Reconcile", func() {
		var (
			mockCtrl *gomock.Controller
			mockV1   *MockXOClient
			mockVM   *k8smocks.MockVM
			mockTask *MockTask
			mockLib  *k8smocks.MockLibrary
		)

		BeforeEach(func() {
			mockCtrl = gomock.NewController(GinkgoT())
			mockV1 = NewMockXOClient(mockCtrl)
			mockVM = k8smocks.NewMockVM(mockCtrl)
			mockTask = NewMockTask(mockCtrl)
			mockLib = k8smocks.NewMockLibrary(mockCtrl)
			mockLib.EXPECT().V1Client().Return(mockV1).AnyTimes()
			mockLib.EXPECT().VM().Return(mockVM).AnyTimes()
			mockLib.EXPECT().Task().Return(mockTask).AnyTimes()
		})

		AfterEach(func() {
			mockCtrl.Finish()
		})

		withXO := func(r *XOMachineReconciler) *XOMachineReconciler {
			r.newClientFunc = func(_ context.Context, _ *xok8scommon.XoConfig) (*xok8scommon.XoClient, error) {
				return &xok8scommon.XoClient{Client: mockLib}, nil
			}
			return r
		}

		It("creates the VM anyway and warns when another VM already has its name", func() {
			vmUUID := uuid.Must(uuid.NewV4())
			poolUUID := uuid.Must(uuid.NewV4())
			xm := &infrastructurev1beta2.XOMachine{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: ns, UID: "machine-uid-dup"},
				Spec: infrastructurev1beta2.XOMachineSpec{
					TemplateID:    uuid.Must(uuid.NewV4()).String(),
					PoolID:        poolUUID.String(),
					BootstrapData: "#cloud-config\n",
				},
			}
			r := withXO(reconciler(xm))

			mockV1.EXPECT().GetCurrentUser().Return(&xoclient.User{}, nil).AnyTimes()
			mockV1.EXPECT().GetVm(gomock.Any()).Return(&xoclient.Vm{}, nil).AnyTimes()

			// A VM of someone else's already carries the name: no marker.
			mockVM.EXPECT().
				GetAll(gomock.Any(), 0, `name_label:"test"`).
				Return([]*payloads.VM{{ID: uuid.Must(uuid.NewV4()), NameLabel: "test", NameDescription: "made by hand"}}, nil)
			mockVM.EXPECT().
				Create(gomock.Any(), poolUUID, gomock.Any()).
				Return(&payloads.VM{ID: vmUUID, NameLabel: "test"}, nil)
			mockVM.EXPECT().Start(gomock.Any(), vmUUID, nil).Return("task-1", nil)
			mockTask.EXPECT().Wait(gomock.Any(), "task-1").Return(&payloads.Task{Status: payloads.Success}, nil)
			mockVM.EXPECT().GetByID(gomock.Any(), vmUUID).
				Return(&payloads.VM{ID: vmUUID, NameLabel: "test", PowerState: payloads.PowerStateRunning}, nil)

			timeoutCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_, err := r.Reconcile(timeoutCtx, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: "test"}})
			Expect(err).NotTo(HaveOccurred())

			updated := fetch(r, "test")
			Expect(updated.Status.VMName).To(Equal("test"))
			Expect(updated.Status.ProviderID).NotTo(BeNil())
			Expect(recorder.Events).To(Receive(ContainSubstring("Warning DuplicateVMName")))
		})

		It("adopts the VM an interrupted create left behind by its recorded name, after the naming changed", func() {
			vmUUID := uuid.Must(uuid.NewV4())
			poolUUID := uuid.Must(uuid.NewV4())
			uid := types.UID("machine-uid-resume")
			recorded := cpPrefix + "7n62g"

			// The controller recorded the name, created the VM in Xen Orchestra
			// and stopped before saving the providerID. Meanwhile the naming
			// configuration changed.
			xm := xoMachine("my-cluster-x8kz2-7n62g")
			xm.UID = uid
			xm.Spec.TemplateID = uuid.Must(uuid.NewV4()).String()
			xm.Spec.PoolID = poolUUID.String()
			xm.Spec.BootstrapData = "#cloud-config\n"
			xm.Status.VMName = recorded
			changed := &infrastructurev1beta2.VMNamingSpec{ControlPlanePrefix: "NEW-CP-", WorkerPrefix: "NEW-WORKER-"}
			r := withXO(reconciler(cluster(), xoCluster(changed), xm))

			// The lookup searches the recorded name with the exact filter and
			// finds the VM by its marker. Create has NO expectation: gomock
			// fails if a second VM is created.
			mockVM.EXPECT().
				GetAll(gomock.Any(), 0, `name_label:"`+recorded+`"`).
				Return([]*payloads.VM{{
					ID:              vmUUID,
					NameLabel:       recorded,
					NameDescription: "vates-capi-machine:" + string(uid),
					PowerState:      payloads.PowerStateRunning,
					MainIpAddress:   "192.168.1.42",
				}}, nil)
			mockVM.EXPECT().GetByID(gomock.Any(), vmUUID).
				Return(&payloads.VM{ID: vmUUID, NameLabel: recorded, PowerState: payloads.PowerStateRunning, MainIpAddress: "192.168.1.42"}, nil)

			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: xm.Name}})
			Expect(err).NotTo(HaveOccurred())

			updated := fetch(r, xm.Name)
			Expect(updated.Status.VMName).To(Equal(recorded))
			Expect(updated.Status.ProviderID).NotTo(BeNil())
			Expect(*updated.Status.ProviderID).To(HaveSuffix(vmUUID.String()))
			Expect(recorder.Events).To(BeEmpty())
		})

		It("never renames a ready VM, even after it was renamed in Xen Orchestra", func() {
			vmUUID := uuid.Must(uuid.NewV4())
			poolUUID := uuid.Must(uuid.NewV4())
			providerID := "xenorchestra://" + poolUUID.String() + "/" + vmUUID.String()
			xm := &infrastructurev1beta2.XOMachine{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: ns, Finalizers: []string{xoMachineFinalizer}},
				Spec:       infrastructurev1beta2.XOMachineSpec{ProviderID: &providerID},
				Status: infrastructurev1beta2.XOMachineStatus{
					Ready:      true,
					ProviderID: &providerID,
					VMName:     "test",
				},
			}
			r := withXO(reconciler(xm))

			// The VM was renamed by hand. Only a read is expected: gomock fails
			// on any call that would rename it back.
			mockVM.EXPECT().GetByID(gomock.Any(), vmUUID).
				Return(&payloads.VM{ID: vmUUID, NameLabel: "renamed-by-hand", PowerState: payloads.PowerStateRunning, MainIpAddress: "192.168.1.42"}, nil)

			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: "test"}})
			Expect(err).NotTo(HaveOccurred())
			Expect(fetch(r, "test").Status.VMName).To(Equal("test"))
		})
	})

	Describe("CRD validation", func() {
		It("accepts any prefix, whatever the convention", func() {
			xc := &infrastructurev1beta2.XOCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "naming-free", Namespace: ns},
				Spec: infrastructurev1beta2.XOClusterSpec{VMNaming: &infrastructurev1beta2.VMNamingSpec{
					ControlPlanePrefix: cpPrefix,
					WorkerPrefix:       "w x.y_",
				}},
			}
			Expect(k8sClient.Create(ctx, xc)).To(Succeed())
			Expect(k8sClient.Delete(ctx, xc)).To(Succeed())
		})

		It("rejects an empty prefix", func() {
			xc := &infrastructurev1beta2.XOCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "naming-empty", Namespace: ns},
				Spec: infrastructurev1beta2.XOClusterSpec{VMNaming: &infrastructurev1beta2.VMNamingSpec{
					ControlPlanePrefix: cpPrefix,
				}},
			}
			Expect(k8sClient.Create(ctx, xc)).NotTo(Succeed())
		})
	})
})
