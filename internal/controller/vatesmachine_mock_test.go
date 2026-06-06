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
)

// ---------------------------------------------------------------------------
// Reconcile integration
// ---------------------------------------------------------------------------

var _ = Describe("Reconcile", func() {
	var (
		ctrl     *gomock.Controller
		mockV1   *MockXOClient
		mockVM   *k8smocks.MockVM
		mockTask *MockTask
		mockLib  *k8smocks.MockLibrary
		r        *VatesMachineReconciler
		scheme   *runtime.Scheme
		ctx      context.Context
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		scheme = runtime.NewScheme()
		Expect(infrastructurev1beta2.AddToScheme(scheme)).To(Succeed())
		Expect(clusterv1.AddToScheme(scheme)).To(Succeed())
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		ctx = context.Background()

		mockV1 = NewMockXOClient(ctrl)
		mockVM = k8smocks.NewMockVM(ctrl)
		mockTask = NewMockTask(ctrl)
		mockLib = k8smocks.NewMockLibrary(ctrl)
		mockLib.EXPECT().V1Client().Return(mockV1).AnyTimes()
		mockLib.EXPECT().VM().Return(mockVM).AnyTimes()
		mockLib.EXPECT().Task().Return(mockTask).AnyTimes()
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	// -----------------------------------------------------------------------
	// reconcileNormal
	// -----------------------------------------------------------------------

	Describe("reconcileNormal", func() {
		It("skips when XoCreds is nil and xoClient is nil", func() {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
				&infrastructurev1beta2.VatesMachine{
					ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				},
			).Build()

			r := &VatesMachineReconciler{Client: fakeClient, Scheme: scheme, XoCreds: nil}
			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "test", Namespace: "default"},
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("requeues when bootstrap is not ready (DataSecretName nil)", func() {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
				&infrastructurev1beta2.VatesMachine{
					ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default", Finalizers: []string{vatesMachineFinalizer}},
					Spec: infrastructurev1beta2.VatesMachineSpec{
						TemplateName: "ubuntu",
						NamePrefix:   "test",
					},
				},
				&clusterv1.Machine{
					ObjectMeta: metav1.ObjectMeta{Name: "owner-machine", Namespace: "default"},
					Spec: clusterv1.MachineSpec{
						Bootstrap: clusterv1.Bootstrap{DataSecretName: nil},
						InfrastructureRef: clusterv1.ContractVersionedObjectReference{
							Kind: "VatesMachine",
							Name: "test",
						},
					},
				},
			).Build()

			r := &VatesMachineReconciler{Client: fakeClient, Scheme: scheme}
			result, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "test", Namespace: "default"},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).NotTo(BeZero())
		})

		It("skips when no Machine and no inline bootstrap data", func() {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
				&infrastructurev1beta2.VatesMachine{
					ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
					Spec: infrastructurev1beta2.VatesMachineSpec{
						TemplateName: "ubuntu",
						NamePrefix:   "test",
					},
				},
			).Build()

			r := &VatesMachineReconciler{Client: fakeClient, Scheme: scheme}
			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "test", Namespace: "default"},
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("creates the VM, starts it and reaches Ready with an IP", func() {
			vmUUID := uuid.Must(uuid.NewV4())
			poolUUID := uuid.Must(uuid.NewV4())
			templateUUID := uuid.Must(uuid.NewV4())

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&infrastructurev1beta2.VatesMachine{}).WithObjects(
				&infrastructurev1beta2.VatesMachine{
					ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
					Spec: infrastructurev1beta2.VatesMachineSpec{
						TemplateID:    templateUUID.String(),
						PoolID:        poolUUID.String(),
						NamePrefix:    "test",
						BootstrapData: "#cloud-config\n",
					},
				},
			).Build()

			r = &VatesMachineReconciler{
				Client:   fakeClient,
				Scheme:   scheme,
				xoClient: &xok8scommon.XoClient{Client: mockLib},
			}

			mockV1.EXPECT().
				GetCurrentUser().
				Return(&xoclient.User{Preferences: xoclient.Preferences{}}, nil).
				AnyTimes()

			mockVM.EXPECT().
				Create(gomock.Any(), poolUUID, gomock.Any()).
				Return(&payloads.VM{
					ID:         vmUUID,
					NameLabel:  "test",
					PowerState: payloads.PowerStateRunning,
				}, nil)

			mockVM.EXPECT().
				GetByID(gomock.Any(), vmUUID).
				Return(&payloads.VM{
					ID:            vmUUID,
					NameLabel:     "test",
					PowerState:    payloads.PowerStateRunning,
					MainIpAddress: "192.168.1.42",
				}, nil)

			result, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "test", Namespace: "default"},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())

			updated := &infrastructurev1beta2.VatesMachine{}
			err = r.Get(ctx, types.NamespacedName{Name: "test", Namespace: "default"}, updated)
			Expect(err).NotTo(HaveOccurred())
			Expect(updated.Status.Ready).To(BeTrue())
			Expect(updated.Status.ProviderID).NotTo(BeNil())
			Expect(*updated.Status.ProviderID).NotTo(BeEmpty())
			Expect(updated.Status.Addresses).To(HaveLen(1))
			Expect(updated.Status.Addresses[0].Address).To(Equal("192.168.1.42"))
		})

		It("sets the disk size when DiskSize is set", func() {
			vmUUID := uuid.Must(uuid.NewV4())
			poolUUID := uuid.Must(uuid.NewV4())
			templateUUID := uuid.Must(uuid.NewV4())

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&infrastructurev1beta2.VatesMachine{}).WithObjects(
				&infrastructurev1beta2.VatesMachine{
					ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
					Spec: infrastructurev1beta2.VatesMachineSpec{
						TemplateID:    templateUUID.String(),
						PoolID:        poolUUID.String(),
						NamePrefix:    "test",
						BootstrapData: "#cloud-config\n",
						ResourceSet: &infrastructurev1beta2.ResourceSet{
							DiskSize: "50Gi",
						},
					},
				},
			).Build()

			r = &VatesMachineReconciler{
				Client:   fakeClient,
				Scheme:   scheme,
				xoClient: &xok8scommon.XoClient{Client: mockLib},
			}

			mockV1.EXPECT().
				GetCurrentUser().
				Return(&xoclient.User{Preferences: xoclient.Preferences{}}, nil).
				AnyTimes()

			mockVM.EXPECT().
				Create(gomock.Any(), poolUUID, gomock.Any()).
				Return(&payloads.VM{
					ID:         vmUUID,
					NameLabel:  "test",
					PowerState: payloads.PowerStateRunning,
				}, nil)

			mockVM.EXPECT().
				GetByID(gomock.Any(), vmUUID).
				Return(&payloads.VM{
					ID:            vmUUID,
					NameLabel:     "test",
					PowerState:    payloads.PowerStateRunning,
					MainIpAddress: "192.168.1.42",
				}, nil)

			result, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "test", Namespace: "default"},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())

			updated := &infrastructurev1beta2.VatesMachine{}
			err = r.Get(ctx, types.NamespacedName{Name: "test", Namespace: "default"}, updated)
			Expect(err).NotTo(HaveOccurred())
			Expect(updated.Status.Ready).To(BeTrue())
			Expect(updated.Status.ProviderID).NotTo(BeNil())
			Expect(*updated.Status.ProviderID).NotTo(BeEmpty())
		})

		It("requeues when the VM started but has no IP yet", func() {
			vmUUID := uuid.Must(uuid.NewV4())
			poolUUID := uuid.Must(uuid.NewV4())
			templateUUID := uuid.Must(uuid.NewV4())

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&infrastructurev1beta2.VatesMachine{}).WithObjects(
				&infrastructurev1beta2.VatesMachine{
					ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
					Spec: infrastructurev1beta2.VatesMachineSpec{
						TemplateID:    templateUUID.String(),
						PoolID:        poolUUID.String(),
						NamePrefix:    "test",
						BootstrapData: "#cloud-config\n",
					},
				},
			).Build()

			r = &VatesMachineReconciler{
				Client:   fakeClient,
				Scheme:   scheme,
				xoClient: &xok8scommon.XoClient{Client: mockLib},
			}

			mockV1.EXPECT().
				GetCurrentUser().
				Return(&xoclient.User{Preferences: xoclient.Preferences{}}, nil).
				AnyTimes()

			mockV1.EXPECT().
				GetVm(gomock.Any()).
				Return(&xoclient.Vm{}, nil).
				AnyTimes()

			mockVM.EXPECT().
				Create(gomock.Any(), poolUUID, gomock.Any()).
				Return(&payloads.VM{ID: vmUUID, NameLabel: "test"}, nil)

			mockVM.EXPECT().
				Start(gomock.Any(), vmUUID, nil).
				Return("task-1", nil)

			mockTask.EXPECT().
				Wait(gomock.Any(), "task-1").
				Return(&payloads.Task{Status: payloads.Success}, nil)

			mockVM.EXPECT().
				GetByID(gomock.Any(), vmUUID).
				Return(&payloads.VM{
					ID:            vmUUID,
					NameLabel:     "test",
					PowerState:    payloads.PowerStateRunning,
					MainIpAddress: "",
				}, nil)

			timeoutCtx, timeoutCancel := context.WithTimeout(ctx, time.Second)
			defer timeoutCancel()
			result, err := r.Reconcile(timeoutCtx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "test", Namespace: "default"},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).NotTo(BeZero())
		})
	})

	// -----------------------------------------------------------------------
	// reconcileDelete
	// -----------------------------------------------------------------------

	Describe("reconcileDelete", func() {
		It("stops and deletes the VM, then removes the finalizer", func() {
			vmUUID := uuid.Must(uuid.NewV4())
			poolUUID := uuid.Must(uuid.NewV4())
			providerID := "xenorchestra://" + poolUUID.String() + "/" + vmUUID.String()

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&infrastructurev1beta2.VatesMachine{}).WithObjects(
				&infrastructurev1beta2.VatesMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "test",
						Namespace:         "default",
						Finalizers:        []string{vatesMachineFinalizer},
						DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
					},
					Spec: infrastructurev1beta2.VatesMachineSpec{
						TemplateID: uuid.Must(uuid.NewV4()).String(),
						NamePrefix: "test",
					},
					Status: infrastructurev1beta2.VatesMachineStatus{
						ProviderID: &providerID,
					},
				},
			).Build()

			r = &VatesMachineReconciler{
				Client:   fakeClient,
				Scheme:   scheme,
				xoClient: &xok8scommon.XoClient{Client: mockLib},
			}

			mockVM.EXPECT().
				HardShutdown(gomock.Any(), vmUUID).
				Return("task-123", nil)

			mockTask.EXPECT().
				Wait(gomock.Any(), "task-123").
				Return(&payloads.Task{Status: payloads.Success}, nil)

			mockVM.EXPECT().
				Delete(gomock.Any(), vmUUID).
				Return(nil)

			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "test", Namespace: "default"},
			})
			Expect(err).NotTo(HaveOccurred())

			updated := &infrastructurev1beta2.VatesMachine{}
			_ = r.Get(ctx, types.NamespacedName{Name: "test", Namespace: "default"}, updated)
			Expect(updated.Finalizers).To(BeEmpty())
		})
	})
})
