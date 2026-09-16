package controller

import (
	"context"
	"errors"
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
		mockVBD  *MockVBD
		mockLib  *k8smocks.MockLibrary
		r        *XOMachineReconciler
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
		mockVBD = NewMockVBD(ctrl)
		mockLib = k8smocks.NewMockLibrary(ctrl)
		mockLib.EXPECT().V1Client().Return(mockV1).AnyTimes()
		mockLib.EXPECT().VM().Return(mockVM).AnyTimes()
		mockLib.EXPECT().Task().Return(mockTask).AnyTimes()
		mockLib.EXPECT().VBD().Return(mockVBD).AnyTimes()
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
				&infrastructurev1beta2.XOMachine{
					ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				},
			).Build()

			r := &XOMachineReconciler{Client: fakeClient, Scheme: scheme, XoCreds: nil}
			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "test", Namespace: "default"},
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("requeues when bootstrap is not ready (DataSecretName nil)", func() {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
				&infrastructurev1beta2.XOMachine{
					ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default", Finalizers: []string{xoMachineFinalizer}},
					Spec: infrastructurev1beta2.XOMachineSpec{
						TemplateName: "ubuntu",
						NamePrefix:   "test",
					},
				},
				&clusterv1.Machine{
					ObjectMeta: metav1.ObjectMeta{Name: "owner-machine", Namespace: "default"},
					Spec: clusterv1.MachineSpec{
						Bootstrap: clusterv1.Bootstrap{DataSecretName: nil},
						InfrastructureRef: clusterv1.ContractVersionedObjectReference{
							Kind: "XOMachine",
							Name: "test",
						},
					},
				},
			).Build()

			r := &XOMachineReconciler{Client: fakeClient, Scheme: scheme}
			result, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "test", Namespace: "default"},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).NotTo(BeZero())
		})

		It("skips when no Machine and no inline bootstrap data", func() {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
				&infrastructurev1beta2.XOMachine{
					ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
					Spec: infrastructurev1beta2.XOMachineSpec{
						TemplateName: "ubuntu",
						NamePrefix:   "test",
					},
				},
			).Build()

			r := &XOMachineReconciler{Client: fakeClient, Scheme: scheme}
			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "test", Namespace: "default"},
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("creates the VM, starts it and reaches Ready with an IP", func() {
			vmUUID := uuid.Must(uuid.NewV4())
			poolUUID := uuid.Must(uuid.NewV4())
			templateUUID := uuid.Must(uuid.NewV4())

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&infrastructurev1beta2.XOMachine{}).WithObjects(
				&infrastructurev1beta2.XOMachine{
					ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
					Spec: infrastructurev1beta2.XOMachineSpec{
						TemplateID:    templateUUID.String(),
						PoolID:        poolUUID.String(),
						NamePrefix:    "test",
						BootstrapData: "#cloud-config\n",
					},
				},
			).Build()

			r = &XOMachineReconciler{
				Client: fakeClient,
				Scheme: scheme,
				newClientFunc: func(_ context.Context, _ *xok8scommon.XoConfig) (*xok8scommon.XoClient, error) {
					return &xok8scommon.XoClient{Client: mockLib}, nil
				},
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

			updated := &infrastructurev1beta2.XOMachine{}
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

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&infrastructurev1beta2.XOMachine{}).WithObjects(
				&infrastructurev1beta2.XOMachine{
					ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
					Spec: infrastructurev1beta2.XOMachineSpec{
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

			r = &XOMachineReconciler{
				Client: fakeClient,
				Scheme: scheme,
				newClientFunc: func(_ context.Context, _ *xok8scommon.XoConfig) (*xok8scommon.XoClient, error) {
					return &xok8scommon.XoClient{Client: mockLib}, nil
				},
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

			updated := &infrastructurev1beta2.XOMachine{}
			err = r.Get(ctx, types.NamespacedName{Name: "test", Namespace: "default"}, updated)
			Expect(err).NotTo(HaveOccurred())
			Expect(updated.Status.Ready).To(BeTrue())
			Expect(updated.Status.ProviderID).NotTo(BeNil())
			Expect(*updated.Status.ProviderID).NotTo(BeEmpty())
		})

		It("creates the VM with Talos bootstrap data passthrough", func() {
			vmUUID := uuid.Must(uuid.NewV4())
			poolUUID := uuid.Must(uuid.NewV4())
			templateUUID := uuid.Must(uuid.NewV4())
			talosData := "version: v1alpha1\nmachine:\n  type: controlplane\n"

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&infrastructurev1beta2.XOMachine{}).WithObjects(
				&infrastructurev1beta2.XOMachine{
					ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
					Spec: infrastructurev1beta2.XOMachineSpec{
						TemplateID:        templateUUID.String(),
						PoolID:            poolUUID.String(),
						NamePrefix:        "test",
						BootstrapData:     talosData,
						BootstrapProvider: "talos",
					},
				},
			).Build()

			r = &XOMachineReconciler{
				Client: fakeClient,
				Scheme: scheme,
				newClientFunc: func(_ context.Context, _ *xok8scommon.XoConfig) (*xok8scommon.XoClient, error) {
					return &xok8scommon.XoClient{Client: mockLib}, nil
				},
			}

			// GetCurrentUser should NOT be called for Talos (no SSH key injection, no cloud-config build)
			mockVM.EXPECT().
				Create(gomock.Any(), poolUUID, gomock.Any()).
				DoAndReturn(func(_ context.Context, _ uuid.UUID, params *payloads.CreateVMParams) (*payloads.VM, error) {
					Expect(params.CloudConfig).NotTo(BeNil())
					Expect(*params.CloudConfig).To(Equal(talosData))
					return &payloads.VM{
						ID:         vmUUID,
						NameLabel:  "test",
						PowerState: payloads.PowerStateRunning,
					}, nil
				})

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

			updated := &infrastructurev1beta2.XOMachine{}
			err = r.Get(ctx, types.NamespacedName{Name: "test", Namespace: "default"}, updated)
			Expect(err).NotTo(HaveOccurred())
			Expect(updated.Status.Ready).To(BeTrue())
		})

		It("requeues when the VM started but has no IP yet", func() {
			vmUUID := uuid.Must(uuid.NewV4())
			poolUUID := uuid.Must(uuid.NewV4())
			templateUUID := uuid.Must(uuid.NewV4())

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&infrastructurev1beta2.XOMachine{}).WithObjects(
				&infrastructurev1beta2.XOMachine{
					ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
					Spec: infrastructurev1beta2.XOMachineSpec{
						TemplateID:    templateUUID.String(),
						PoolID:        poolUUID.String(),
						NamePrefix:    "test",
						BootstrapData: "#cloud-config\n",
					},
				},
			).Build()

			r = &XOMachineReconciler{
				Client: fakeClient,
				Scheme: scheme,
				newClientFunc: func(_ context.Context, _ *xok8scommon.XoConfig) (*xok8scommon.XoClient, error) {
					return &xok8scommon.XoClient{Client: mockLib}, nil
				},
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

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&infrastructurev1beta2.XOMachine{}).WithObjects(
				&infrastructurev1beta2.XOMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "test",
						Namespace:         "default",
						Finalizers:        []string{xoMachineFinalizer},
						DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
					},
					Spec: infrastructurev1beta2.XOMachineSpec{
						TemplateID: uuid.Must(uuid.NewV4()).String(),
						NamePrefix: "test",
					},
					Status: infrastructurev1beta2.XOMachineStatus{
						ProviderID: &providerID,
					},
				},
			).Build()

			r = &XOMachineReconciler{
				Client: fakeClient,
				Scheme: scheme,
				newClientFunc: func(_ context.Context, _ *xok8scommon.XoConfig) (*xok8scommon.XoClient, error) {
					return &xok8scommon.XoClient{Client: mockLib}, nil
				},
			}

			pvVDI := uuid.Must(uuid.NewV4())
			pvVBD := uuid.Must(uuid.NewV4())

			mockVM.EXPECT().
				GetVDIs(gomock.Any(), vmUUID, 0, "tags:/^k8s:volumeId:/").
				Return([]*payloads.VDI{{ID: pvVDI}}, nil)
			mockVBD.EXPECT().
				GetAll(gomock.Any(), 0, "VM:"+vmUUID.String()).
				Return([]*payloads.VBD{{ID: pvVBD, VDI: &pvVDI, VM: vmUUID, Attached: true}}, nil)
			mockVBD.EXPECT().
				Disconnect(gomock.Any(), pvVBD).
				Return("", nil)
			mockVBD.EXPECT().
				Delete(gomock.Any(), pvVBD).
				Return(nil)

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

			updated := &infrastructurev1beta2.XOMachine{}
			_ = r.Get(ctx, types.NamespacedName{Name: "test", Namespace: "default"}, updated)
			Expect(updated.Finalizers).To(BeEmpty())
		})

		It("requeues and keeps the finalizer when the XO client cannot be created", func() {
			vmUUID := uuid.Must(uuid.NewV4())
			poolUUID := uuid.Must(uuid.NewV4())
			providerID := "xenorchestra://" + poolUUID.String() + "/" + vmUUID.String()

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&infrastructurev1beta2.XOMachine{}).WithObjects(
				&infrastructurev1beta2.XOMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "test",
						Namespace:         "default",
						Finalizers:        []string{xoMachineFinalizer},
						DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
					},
					Spec: infrastructurev1beta2.XOMachineSpec{
						TemplateID: uuid.Must(uuid.NewV4()).String(),
						NamePrefix: "test",
					},
					Status: infrastructurev1beta2.XOMachineStatus{
						ProviderID: &providerID,
					},
				},
			).Build()

			r = &XOMachineReconciler{
				Client: fakeClient,
				Scheme: scheme,
				newClientFunc: func(_ context.Context, _ *xok8scommon.XoConfig) (*xok8scommon.XoClient, error) {
					return nil, errors.New("connect to XO: connection refused")
				},
			}

			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "test", Namespace: "default"},
			})
			Expect(err).To(HaveOccurred())

			updated := &infrastructurev1beta2.XOMachine{}
			_ = r.Get(ctx, types.NamespacedName{Name: "test", Namespace: "default"}, updated)
			Expect(updated.Finalizers).To(ContainElement(xoMachineFinalizer))
		})

		It("requeues and keeps the finalizer when listing the VM's persistent volume VDIs fails with a transient error", func() {
			vmUUID := uuid.Must(uuid.NewV4())
			poolUUID := uuid.Must(uuid.NewV4())
			providerID := "xenorchestra://" + poolUUID.String() + "/" + vmUUID.String()

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&infrastructurev1beta2.XOMachine{}).WithObjects(
				&infrastructurev1beta2.XOMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "test",
						Namespace:         "default",
						Finalizers:        []string{xoMachineFinalizer},
						DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
					},
					Spec: infrastructurev1beta2.XOMachineSpec{
						TemplateID: uuid.Must(uuid.NewV4()).String(),
						NamePrefix: "test",
					},
					Status: infrastructurev1beta2.XOMachineStatus{
						ProviderID: &providerID,
					},
				},
			).Build()

			r = &XOMachineReconciler{
				Client: fakeClient,
				Scheme: scheme,
				newClientFunc: func(_ context.Context, _ *xok8scommon.XoConfig) (*xok8scommon.XoClient, error) {
					return &xok8scommon.XoClient{Client: mockLib}, nil
				},
			}

			mockVM.EXPECT().
				GetVDIs(gomock.Any(), vmUUID, 0, "tags:/^k8s:volumeId:/").
				Return(nil, errors.New("API error: 500 Internal Server Error"))

			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "test", Namespace: "default"},
			})
			Expect(err).To(HaveOccurred())

			updated := &infrastructurev1beta2.XOMachine{}
			_ = r.Get(ctx, types.NamespacedName{Name: "test", Namespace: "default"}, updated)
			Expect(updated.Finalizers).To(ContainElement(xoMachineFinalizer))
		})

		It("removes the finalizer without deleting the VM when the VM is not found (404)", func() {
			vmUUID := uuid.Must(uuid.NewV4())
			poolUUID := uuid.Must(uuid.NewV4())
			providerID := "xenorchestra://" + poolUUID.String() + "/" + vmUUID.String()

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&infrastructurev1beta2.XOMachine{}).WithObjects(
				&infrastructurev1beta2.XOMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "test",
						Namespace:         "default",
						Finalizers:        []string{xoMachineFinalizer},
						DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
					},
					Spec: infrastructurev1beta2.XOMachineSpec{
						TemplateID: uuid.Must(uuid.NewV4()).String(),
						NamePrefix: "test",
					},
					Status: infrastructurev1beta2.XOMachineStatus{
						ProviderID: &providerID,
					},
				},
			).Build()

			r = &XOMachineReconciler{
				Client: fakeClient,
				Scheme: scheme,
				newClientFunc: func(_ context.Context, _ *xok8scommon.XoConfig) (*xok8scommon.XoClient, error) {
					return &xok8scommon.XoClient{Client: mockLib}, nil
				},
			}

			mockVM.EXPECT().
				GetVDIs(gomock.Any(), vmUUID, 0, "tags:/^k8s:volumeId:/").
				Return(nil, errors.New("API error: 404 Not Found - no such VM"))

			mockVM.EXPECT().
				HardShutdown(gomock.Any(), vmUUID).
				Return("", errors.New("API error: 404 Not Found - no such VM"))

			mockVM.EXPECT().
				Delete(gomock.Any(), vmUUID).
				Return(errors.New("API error: 404 Not Found - no such VM"))

			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "test", Namespace: "default"},
			})
			Expect(err).NotTo(HaveOccurred())

			updated := &infrastructurev1beta2.XOMachine{}
			_ = r.Get(ctx, types.NamespacedName{Name: "test", Namespace: "default"}, updated)
			Expect(updated.Finalizers).To(BeEmpty())
		})

		It("tolerates the XOMachine being already deleted while removing the finalizer", func() {
			// The fake client holds no object: the in-flight reconcile only has a
			// stale cached copy, so the finalizer update fails with NotFound. That
			// race must not be reported as an error.
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
			r = &XOMachineReconciler{Client: fakeClient, Scheme: scheme}

			xoMachine := &infrastructurev1beta2.XOMachine{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "already-gone",
					Namespace:  "default",
					Finalizers: []string{xoMachineFinalizer},
				},
			}

			_, err := r.reconcileDelete(ctx, xoMachine)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
