package migration

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/golang/mock/gomock"
	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	v1 "kubevirt.io/api/core/v1"
	virtv1 "kubevirt.io/api/core/v1"
	virtstoragev1alpha1 "kubevirt.io/api/storage/v1alpha1"
	"kubevirt.io/client-go/api"
	kubevirtfake "kubevirt.io/client-go/generated/kubevirt/clientset/versioned/fake"
	"kubevirt.io/client-go/kubecli"

	"kubevirt.io/kubevirt/pkg/testutils"
)

var _ = Describe("Storage Migration", func() {
	var (
		stop                     chan struct{}
		ctrl                     *gomock.Controller
		controller               *StorageMigrationController
		virtClient               *kubecli.MockKubevirtClient
		storageMigrationInformer cache.SharedIndexInformer
		migrationInformer        cache.SharedIndexInformer
		vmiInformer              cache.SharedIndexInformer
		vmInformer               cache.SharedIndexInformer
	)

	syncCaches := func(stop chan struct{}) {
		go storageMigrationInformer.Run(stop)
		go migrationInformer.Run(stop)
		go vmiInformer.Run(stop)
		go vmInformer.Run(stop)

		Expect(cache.WaitForCacheSync(stop,
			storageMigrationInformer.HasSynced,
			vmiInformer.HasSynced,
			vmInformer.HasSynced,
			migrationInformer.HasSynced)).To(BeTrue())

	}

	createVMIWithPVCs := func(name string, pvcs ...string) *virtv1.VirtualMachineInstance {
		vmi := api.NewMinimalVMI(name)
		for _, p := range pvcs {
			vmi.Spec.Domain.Devices.Disks = append(vmi.Spec.Domain.Devices.Disks, v1.Disk{
				Name: p,
				DiskDevice: v1.DiskDevice{
					Disk: &v1.DiskTarget{
						Bus: v1.DiskBusVirtio,
					},
				},
			})
			vmi.Spec.Volumes = append(vmi.Spec.Volumes, v1.Volume{
				Name: p,
				VolumeSource: v1.VolumeSource{
					PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
						PersistentVolumeClaimVolumeSource: k8sv1.PersistentVolumeClaimVolumeSource{
							ClaimName: p,
						}},
				},
			})
		}
		return vmi
	}
	BeforeEach(func() {
		var err error
		stop = make(chan struct{})
		ctrl = gomock.NewController(GinkgoT())
		virtClient = kubecli.NewMockKubevirtClient(ctrl)
		storageMigrationInformer, _ = testutils.NewFakeInformerFor(&virtstoragev1alpha1.StorageMigration{})
		vmiInformer, _ = testutils.NewFakeInformerFor(&virtv1.VirtualMachineInstance{})
		vmInformer, _ = testutils.NewFakeInformerFor(&virtv1.VirtualMachine{})
		migrationInformer, _ = testutils.NewFakeInformerFor(&virtv1.VirtualMachineInstanceMigration{})

		controller, err = NewStorageMigrationController(virtClient, storageMigrationInformer, migrationInformer, vmiInformer, vmInformer)
		Expect(err).ShouldNot(HaveOccurred())

		syncCaches(stop)
	})
	AfterEach(func() {
		close(stop)
	})
	Context("Update volumes after successful migration", func() {
		var (
			vmiInterface *kubecli.MockVirtualMachineInstanceInterface
		)
		BeforeEach(func() {
			vmiInterface = kubecli.NewMockVirtualMachineInstanceInterface(ctrl)
			virtClient.EXPECT().VirtualMachineInstance(metav1.NamespaceDefault).Return(vmiInterface).AnyTimes()
			vmiInterface.EXPECT().Update(context.Background(), gomock.Any()).AnyTimes()
		})
		DescribeTable("updateVMIWithMigrationVolumes",
			func(pvcs []string, migVols []virtstoragev1alpha1.MigratedVolume, expectedErr error) {
				vmi := createVMIWithPVCs("testvmi", pvcs...)
				vmi, err := controller.updateVMIWithMigrationVolumes(vmi, migVols)
				if expectedErr != nil {
					Expect(err).Should(MatchError(expectedErr))
					return
				}
				Expect(err).ShouldNot(HaveOccurred())
				mapVol := make(map[string]bool)
				for _, v := range migVols {
					mapVol[v.DestinationPvc] = true
				}
				for _, v := range vmi.Spec.Volumes {
					name := getVolName(&v)
					if name == "" {
						continue
					}
					if _, ok := mapVol[name]; ok {
						delete(mapVol, name)
					}
				}
				Expect(mapVol).Should(BeEmpty())
			},
			Entry("successful update simple VMI", []string{"src1"}, []virtstoragev1alpha1.MigratedVolume{{SourcePvc: "src1", DestinationPvc: "dest1"}}, nil),
			Entry("successful update VMI with multiple PVCs", []string{"src1", "src2", "src3"}, []virtstoragev1alpha1.MigratedVolume{{SourcePvc: "src1", DestinationPvc: "dest1"}}, nil),
			Entry("successful update VMI with multiple PVCs and migrated volumes", []string{"src1", "src2", "src3"}, []virtstoragev1alpha1.MigratedVolume{
				{SourcePvc: "src1", DestinationPvc: "dest1"},
				{SourcePvc: "src2", DestinationPvc: "dest2"},
				{SourcePvc: "src3", DestinationPvc: "dest3"},
			}, nil),
			Entry("failed to update missing migrated volume", []string{"src1", "src2", "src3"},
				[]virtstoragev1alpha1.MigratedVolume{{SourcePvc: "src4", DestinationPvc: "dest4"}},
				fmt.Errorf("failed to replace the source volumes with the destination volumes in the VMI")),
		)
	})
	Context("Update Storage Migration status", func() {
		var (
			storageMigClient *kubevirtfake.Clientset
			startTime        metav1.Time
			endTime          metav1.Time
		)
		const (
			vmMigName = "mig-vm"
			vmName    = "vm"
			testNs    = "testNs"
		)

		migratedVols := []virtstoragev1alpha1.MigratedVolume{
			{
				SourcePvc:              "src-0",
				DestinationPvc:         "dst-0",
				ReclaimPolicySourcePvc: virtstoragev1alpha1.DeleteReclaimPolicySourcePvc,
			},
		}
		smEmptyStatus := virtstoragev1alpha1.StorageMigration{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: testNs},
			Spec: virtstoragev1alpha1.StorageMigrationSpec{
				MigratedVolume: migratedVols,
			},
		}
		multipleVols := []virtstoragev1alpha1.MigratedVolume{
			{
				SourcePvc:              "src-1",
				DestinationPvc:         "dst-1",
				ReclaimPolicySourcePvc: virtstoragev1alpha1.DeleteReclaimPolicySourcePvc,
			},
			{
				SourcePvc:              "src-2",
				DestinationPvc:         "dst-2",
				ReclaimPolicySourcePvc: virtstoragev1alpha1.DeleteReclaimPolicySourcePvc,
			},
			{
				SourcePvc:              "src-3",
				DestinationPvc:         "dst-3",
				ReclaimPolicySourcePvc: virtstoragev1alpha1.DeleteReclaimPolicySourcePvc,
			},
			{
				SourcePvc:              "src-4",
				DestinationPvc:         "dst-4",
				ReclaimPolicySourcePvc: virtstoragev1alpha1.DeleteReclaimPolicySourcePvc,
			},
		}
		multipleMigStates := []virtstoragev1alpha1.StorageMigrationState{
			{
				// Running
				MigratedVolume: []virtstoragev1alpha1.MigratedVolume{
					{
						SourcePvc:              "src-1",
						DestinationPvc:         "dst-1",
						ReclaimPolicySourcePvc: virtstoragev1alpha1.DeleteReclaimPolicySourcePvc,
					}},
				VirtualMachineInstanceName:  "vm1",
				VirtualMachineMigrationName: "mig-vm1",
				StartTimestamp:              &startTime,
			},
			{
				// Completed
				MigratedVolume: []virtstoragev1alpha1.MigratedVolume{
					{
						SourcePvc:              "src-2",
						DestinationPvc:         "dst-2",
						ReclaimPolicySourcePvc: virtstoragev1alpha1.DeleteReclaimPolicySourcePvc,
					}},
				VirtualMachineInstanceName:  "vm2",
				VirtualMachineMigrationName: "mig-vm2",
				StartTimestamp:              &startTime,
				EndTimestamp:                &endTime,
				Completed:                   true,
			},
			{
				// Failed
				MigratedVolume: []virtstoragev1alpha1.MigratedVolume{
					{
						SourcePvc:              "src-3",
						DestinationPvc:         "dst-3",
						ReclaimPolicySourcePvc: virtstoragev1alpha1.DeleteReclaimPolicySourcePvc,
					}},
				VirtualMachineInstanceName:  "vm3",
				VirtualMachineMigrationName: "mig-vm3",
				StartTimestamp:              &startTime,
				EndTimestamp:                &endTime,
				Failed:                      true,
			},
			{
				// Not Started
				MigratedVolume: []virtstoragev1alpha1.MigratedVolume{
					{
						SourcePvc:              "src-4",
						DestinationPvc:         "dst-4",
						ReclaimPolicySourcePvc: virtstoragev1alpha1.DeleteReclaimPolicySourcePvc,
					}},
				VirtualMachineInstanceName:  "vm4",
				VirtualMachineMigrationName: "mig-vm4",
			},
		}
		smMultipleMigStatus := virtstoragev1alpha1.StorageMigration{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: testNs},
			Spec: virtstoragev1alpha1.StorageMigrationSpec{
				MigratedVolume: multipleVols,
			},
			Status: &virtstoragev1alpha1.StorageMigrationStatus{
				StorageMigrationStates: multipleMigStates,
				Running:                1,
				Completed:              1,
				Failed:                 1,
				Total:                  3,
			},
		}
		addDefaultReactors := func() {
			storageMigClient.Fake.PrependReactor("update", "storagemigrations", func(action testing.Action) (handled bool, obj runtime.Object, err error) {
				update, ok := action.(testing.UpdateAction)
				Expect(ok).To(BeTrue())
				sm, ok := update.GetObject().(*virtstoragev1alpha1.StorageMigration)
				Expect(ok).To(BeTrue())

				return true, sm, nil
			})

		}

		BeforeEach(func() {
			startTime = metav1.Date(2000, 2, 1, 12, 00, 0, 0, time.UTC)
			endTime = metav1.Date(2000, 2, 1, 12, 30, 0, 0, time.UTC)
			storageMigClient = kubevirtfake.NewSimpleClientset()
			virtClient.EXPECT().StorageMigration(testNs).
				Return(storageMigClient.StorageV1alpha1().StorageMigrations(testNs)).AnyTimes()
			storageMigClient.StorageV1alpha1().StorageMigrations(testNs)
			addDefaultReactors()
		})

		DescribeTable("updateStatusStorageMigration", func(sm *virtstoragev1alpha1.StorageMigration, mig *virtv1.VirtualMachineInstanceMigration,
			migVols []virtstoragev1alpha1.MigratedVolume, status *virtstoragev1alpha1.StorageMigrationStatus) {
			res, err := controller.updateStatusStorageMigration(sm, mig, migVols)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(equality.Semantic.DeepEqual(res.Status, status)).To(BeTrue(),
				"Expect status:%v to be equal to %v", res.Status, status)
		},
			Entry("add new completed VM migration to empty state", &smEmptyStatus,
				&virtv1.VirtualMachineInstanceMigration{
					ObjectMeta: metav1.ObjectMeta{Name: vmMigName, Namespace: testNs},
					Spec:       virtv1.VirtualMachineInstanceMigrationSpec{VMIName: vmName},
					Status: virtv1.VirtualMachineInstanceMigrationStatus{
						MigrationState: &virtv1.VirtualMachineInstanceMigrationState{
							Completed:      true,
							StartTimestamp: &startTime,
							EndTimestamp:   &endTime,
						},
					},
				}, migratedVols,
				&virtstoragev1alpha1.StorageMigrationStatus{
					StorageMigrationStates: []virtstoragev1alpha1.StorageMigrationState{
						{
							MigratedVolume:              migratedVols,
							Completed:                   true,
							VirtualMachineMigrationName: vmMigName,
							VirtualMachineInstanceName:  vmName,
							StartTimestamp:              &startTime,
							EndTimestamp:                &endTime,
						},
					},
					Completed: 1,
					Total:     1,
				}),
			Entry("add new running VM migration to empty state", &smEmptyStatus,
				&virtv1.VirtualMachineInstanceMigration{
					ObjectMeta: metav1.ObjectMeta{Name: vmMigName, Namespace: testNs},
					Spec:       virtv1.VirtualMachineInstanceMigrationSpec{VMIName: vmName},
					Status: virtv1.VirtualMachineInstanceMigrationStatus{
						MigrationState: &virtv1.VirtualMachineInstanceMigrationState{
							StartTimestamp: &startTime,
						},
					},
				}, migratedVols,
				&virtstoragev1alpha1.StorageMigrationStatus{
					StorageMigrationStates: []virtstoragev1alpha1.StorageMigrationState{
						{
							MigratedVolume:              migratedVols,
							VirtualMachineMigrationName: vmMigName,
							VirtualMachineInstanceName:  vmName,
							StartTimestamp:              &startTime,
						},
					},
					Running: 1,
					Total:   1,
				}),
			Entry("add new not started VM migration to empty state", &smEmptyStatus,
				&virtv1.VirtualMachineInstanceMigration{
					ObjectMeta: metav1.ObjectMeta{Name: vmMigName, Namespace: testNs},
					Spec:       virtv1.VirtualMachineInstanceMigrationSpec{VMIName: vmName},
					Status: virtv1.VirtualMachineInstanceMigrationStatus{
						MigrationState: &virtv1.VirtualMachineInstanceMigrationState{},
					},
				}, migratedVols,
				&virtstoragev1alpha1.StorageMigrationStatus{
					StorageMigrationStates: []virtstoragev1alpha1.StorageMigrationState{
						{
							MigratedVolume:              migratedVols,
							VirtualMachineMigrationName: vmMigName,
							VirtualMachineInstanceName:  vmName,
						},
					},
					NotStarted: 1,
					Total:      1,
				}),
			Entry("add new running VM migration to state", &virtstoragev1alpha1.StorageMigration{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: testNs},
				Spec: virtstoragev1alpha1.StorageMigrationSpec{
					MigratedVolume: append(multipleVols, migratedVols...),
				},
				Status: &virtstoragev1alpha1.StorageMigrationStatus{
					StorageMigrationStates: multipleMigStates,
					Running:                1,
					Completed:              1,
					Failed:                 1,
					Total:                  3,
				},
			},
				&virtv1.VirtualMachineInstanceMigration{
					ObjectMeta: metav1.ObjectMeta{Name: vmMigName, Namespace: testNs},
					Spec:       virtv1.VirtualMachineInstanceMigrationSpec{VMIName: vmName},
					Status: virtv1.VirtualMachineInstanceMigrationStatus{
						MigrationState: &virtv1.VirtualMachineInstanceMigrationState{
							StartTimestamp: &startTime,
							EndTimestamp:   nil,
						},
					},
				}, migratedVols,
				&virtstoragev1alpha1.StorageMigrationStatus{
					StorageMigrationStates: append(multipleMigStates, virtstoragev1alpha1.StorageMigrationState{
						MigratedVolume:              migratedVols,
						VirtualMachineMigrationName: vmMigName,
						VirtualMachineInstanceName:  vmName,
						StartTimestamp:              &startTime,
					}),
					Total:      5,
					Failed:     1,
					Completed:  1,
					NotStarted: 1,
					Running:    2,
				}),
			Entry("update running VM migration to state", &smMultipleMigStatus,
				&virtv1.VirtualMachineInstanceMigration{
					ObjectMeta: metav1.ObjectMeta{Name: "mig-vm4", Namespace: testNs},
					Spec:       virtv1.VirtualMachineInstanceMigrationSpec{VMIName: "vm4"},
					Status: virtv1.VirtualMachineInstanceMigrationStatus{
						MigrationState: &virtv1.VirtualMachineInstanceMigrationState{
							StartTimestamp: &startTime,
							EndTimestamp:   nil,
						},
					},
				}, []virtstoragev1alpha1.MigratedVolume{
					{
						SourcePvc:              "src-4",
						DestinationPvc:         "dst-4",
						ReclaimPolicySourcePvc: virtstoragev1alpha1.DeleteReclaimPolicySourcePvc,
					}},
				&virtstoragev1alpha1.StorageMigrationStatus{
					StorageMigrationStates: []virtstoragev1alpha1.StorageMigrationState{
						{
							MigratedVolume: []virtstoragev1alpha1.MigratedVolume{
								{
									SourcePvc:              "src-1",
									DestinationPvc:         "dst-1",
									ReclaimPolicySourcePvc: virtstoragev1alpha1.DeleteReclaimPolicySourcePvc,
								}},
							VirtualMachineInstanceName:  "vm1",
							VirtualMachineMigrationName: "mig-vm1",
							StartTimestamp:              &startTime,
						},
						{
							MigratedVolume: []virtstoragev1alpha1.MigratedVolume{
								{
									SourcePvc:              "src-2",
									DestinationPvc:         "dst-2",
									ReclaimPolicySourcePvc: virtstoragev1alpha1.DeleteReclaimPolicySourcePvc,
								}},
							VirtualMachineInstanceName:  "vm2",
							VirtualMachineMigrationName: "mig-vm2",
							StartTimestamp:              &startTime,
							EndTimestamp:                &endTime,
							Completed:                   true,
						},
						{
							MigratedVolume: []virtstoragev1alpha1.MigratedVolume{
								{
									SourcePvc:              "src-3",
									DestinationPvc:         "dst-3",
									ReclaimPolicySourcePvc: virtstoragev1alpha1.DeleteReclaimPolicySourcePvc,
								}},
							VirtualMachineInstanceName:  "vm3",
							VirtualMachineMigrationName: "mig-vm3",
							StartTimestamp:              &startTime,
							EndTimestamp:                &endTime,
							Failed:                      true,
						},
						{
							// Not Started
							MigratedVolume: []virtstoragev1alpha1.MigratedVolume{
								{
									SourcePvc:              "src-4",
									DestinationPvc:         "dst-4",
									ReclaimPolicySourcePvc: virtstoragev1alpha1.DeleteReclaimPolicySourcePvc,
								}},
							VirtualMachineInstanceName:  "vm4",
							VirtualMachineMigrationName: "mig-vm4",
							StartTimestamp:              &startTime,
						},
					},
					Total:      4,
					Failed:     1,
					Completed:  1,
					NotStarted: 0,
					Running:    2,
				}),
		)
	})
})
