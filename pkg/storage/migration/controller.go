package migration

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	k8score "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	v1 "kubevirt.io/api/core/v1"
	virtv1 "kubevirt.io/api/core/v1"
	virtstoragev1alpha1 "kubevirt.io/api/storage/v1alpha1"
	"kubevirt.io/client-go/kubecli"
	"kubevirt.io/client-go/log"
	cdiv1 "kubevirt.io/containerized-data-importer-api/pkg/apis/core/v1beta1"

	storagetypes "kubevirt.io/kubevirt/pkg/storage/types"

	"kubevirt.io/kubevirt/pkg/controller"
)

const labelStorageMigration = "storage.kubevirt.io/migration"

type StorageMigrationController struct {
	Queue                    workqueue.RateLimitingInterface
	clientset                kubecli.KubevirtClient
	storageMigrationInformer cache.SharedIndexInformer
	vmiInformer              cache.SharedIndexInformer
	migrationInformer        cache.SharedIndexInformer
	vmInformer               cache.SharedIndexInformer
	pvcInformer              cache.SharedIndexInformer
	cdiInformer              cache.SharedIndexInformer
	cdiConfigInformer        cache.SharedIndexInformer

	expectations *controller.UIDTrackingControllerExpectations
}

func NewStorageMigrationController(clientset kubecli.KubevirtClient,
	storageMigrationInformer cache.SharedIndexInformer,
	migrationInformer cache.SharedIndexInformer,
	vmiInformer cache.SharedIndexInformer,
	vmInformer cache.SharedIndexInformer,
	pvcInformer cache.SharedIndexInformer,
	cdiInformer cache.SharedIndexInformer,
	cdiConfigInformer cache.SharedIndexInformer,
) (*StorageMigrationController, error) {
	c := &StorageMigrationController{
		Queue:                    workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "virt-controller-storage-migration"),
		clientset:                clientset,
		storageMigrationInformer: storageMigrationInformer,
		vmiInformer:              vmiInformer,
		vmInformer:               vmInformer,
		migrationInformer:        migrationInformer,
		pvcInformer:              pvcInformer,
		cdiInformer:              cdiInformer,
		cdiConfigInformer:        cdiConfigInformer,
	}

	_, err := c.storageMigrationInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addStorageMigration,
		DeleteFunc: c.deleteStorageMigration,
		UpdateFunc: c.updateStorageMigration,
	})
	if err != nil {
		return nil, err
	}

	_, err = c.migrationInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addMigration,
		DeleteFunc: c.deleteMigration,
		UpdateFunc: c.updateMigration,
	})
	if err != nil {
		return nil, err
	}

	return c, nil
}

func (c *StorageMigrationController) Run(threadiness int, stopCh <-chan struct{}) {
	defer controller.HandlePanic()
	defer c.Queue.ShutDown()
	log.Log.Info("Starting StorageMigrationController controller.")

	// Wait for cache sync before we start the controller
	cache.WaitForCacheSync(stopCh, c.storageMigrationInformer.HasSynced)

	// Start the actual work
	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	<-stopCh
	log.Log.Info("Stopping StorageMigrationController controller.")
}

func (c *StorageMigrationController) runWorker() {
	for c.Execute() {
	}
}

func (c *StorageMigrationController) Execute() bool {
	key, quit := c.Queue.Get()
	if quit {
		return false
	}
	defer c.Queue.Done(key)
	if err := c.execute(key.(string)); err != nil {
		log.Log.Reason(err).Infof("re-enqueuing StorageMigration %v", key)
		c.Queue.AddRateLimited(key)
	} else {
		log.Log.V(4).Infof("processed StorageMigration %v", key)
		c.Queue.Forget(key)
	}
	return true
}

func (c *StorageMigrationController) triggerVirtualMachineInstanceMigration(migVols []virtstoragev1alpha1.MigratedVolume, vmiName, smName, migName, ns string) error {
	vmiObj, vmiExists, err := c.vmiInformer.GetStore().GetByKey(fmt.Sprintf("%s/%s", ns, vmiName))
	if err != nil {
		return err
	}
	if !vmiExists {
		return fmt.Errorf("VMI %s for the migration %s doesn't existed", vmiName, smName)
	}
	vmi := vmiObj.(*virtv1.VirtualMachineInstance)
	// Update the VMI status with the migrate volumes
	if err := c.updateVMIStatusWithMigratedDisksPatch(migVols, vmi); err != nil {
		return err
	}

	// Create VirtualMachineiMigration object
	vmiMig := &virtv1.VirtualMachineInstanceMigration{
		ObjectMeta: metav1.ObjectMeta{
			Name:   migName,
			Labels: map[string]string{labelStorageMigration: smName},
		},
		Spec: virtv1.VirtualMachineInstanceMigrationSpec{
			VMIName: vmiName,
		},
	}
	_, err = c.clientset.VirtualMachineInstanceMigration(ns).Create(vmiMig, &metav1.CreateOptions{})
	if err != nil {
		return err
	}
	return nil
}

func (c *StorageMigrationController) updateStatusStorageMigration(sm *virtstoragev1alpha1.StorageMigration, vmiMig *virtv1.VirtualMachineInstanceMigration, migVols []virtstoragev1alpha1.MigratedVolume) (*virtstoragev1alpha1.StorageMigration, error) {
	countStates := func(status *virtstoragev1alpha1.StorageMigrationStatus, f func(s *virtstoragev1alpha1.StorageMigrationState) bool) int {
		count := 0
		for _, s := range status.StorageMigrationStates {
			if f(&s) {
				count++
			}
		}
		return count
	}

	var err error
	found := false
	smCopy := sm.DeepCopy()
	if vmiMig.Status.MigrationState == nil {
		return nil, nil
	}
	if smCopy.Status == nil {
		smCopy.Status = &virtstoragev1alpha1.StorageMigrationStatus{}
	}

	// First, update the migration details for the VMI
	s := virtstoragev1alpha1.StorageMigrationState{
		VirtualMachineMigrationName: vmiMig.Name,
		VirtualMachineInstanceName:  vmiMig.Spec.VMIName,
		MigratedVolume:              migVols,
		Completed:                   vmiMig.Status.MigrationState.Completed,
		Failed:                      vmiMig.Status.MigrationState.Failed,
		StartTimestamp:              vmiMig.Status.MigrationState.StartTimestamp,
		EndTimestamp:                vmiMig.Status.MigrationState.EndTimestamp,
	}
	for i, state := range smCopy.Status.StorageMigrationStates {
		if state.VirtualMachineInstanceName == vmiMig.Spec.VMIName {
			found = true
			smCopy.Status.StorageMigrationStates[i] = s
		}
	}
	if !found {
		smCopy.Status.StorageMigrationStates = append(smCopy.Status.StorageMigrationStates, s)
	}
	// Second, update the migration summary
	smCopy.Status.Total = len(smCopy.Status.StorageMigrationStates)
	smCopy.Status.Failed = countStates(smCopy.Status,
		func(s *virtstoragev1alpha1.StorageMigrationState) bool {
			return s.Failed == true
		})
	smCopy.Status.Running = countStates(smCopy.Status,
		func(s *virtstoragev1alpha1.StorageMigrationState) bool {
			return s.StartTimestamp != nil && s.EndTimestamp == nil && !s.Completed
		})
	smCopy.Status.NotStarted = countStates(smCopy.Status,
		func(s *virtstoragev1alpha1.StorageMigrationState) bool {
			return s.StartTimestamp == nil
		})
	smCopy.Status.Completed = countStates(smCopy.Status,
		func(s *virtstoragev1alpha1.StorageMigrationState) bool {
			return s.Completed
		})
	if smCopy, err = c.clientset.StorageMigration(sm.ObjectMeta.Namespace).UpdateStatus(context.Background(), smCopy, metav1.UpdateOptions{}); err != nil {
		return nil, fmt.Errorf("failed updating storage migration %s: %v", sm.Name,
			err)
	}

	return smCopy, nil
}

// TODO: create common function and avoid copying this from pkg/virt-controller/watch/vmi.go
func (c *StorageMigrationController) getFilesystemOverhead(pvc *k8score.PersistentVolumeClaim) (cdiv1.Percent, error) {
	// To avoid conflicts, we only allow having one CDI instance
	if cdiInstances := len(c.cdiInformer.GetStore().List()); cdiInstances != 1 {
		if cdiInstances > 1 {
			log.Log.V(3).Object(pvc).Reason(storagetypes.ErrMultipleCdiInstances).Infof(storagetypes.FSOverheadMsg)
		} else {
			log.Log.V(3).Object(pvc).Reason(storagetypes.ErrFailedToFindCdi).Infof(storagetypes.FSOverheadMsg)
		}
		return storagetypes.DefaultFSOverhead, nil
	}

	cdiConfigInterface, cdiConfigExists, err := c.cdiConfigInformer.GetStore().GetByKey(storagetypes.ConfigName)
	if !cdiConfigExists || err != nil {
		return "0", fmt.Errorf("Failed to find CDIConfig but CDI exists: %w", err)
	}
	cdiConfig, ok := cdiConfigInterface.(*cdiv1.CDIConfig)
	if !ok {
		return "0", fmt.Errorf("Failed to convert CDIConfig object %v to type CDIConfig", cdiConfigInterface)
	}

	return storagetypes.GetFilesystemOverhead(pvc.Spec.VolumeMode, pvc.Spec.StorageClassName, cdiConfig), nil
}

func (c *StorageMigrationController) updateVMIStatusWithMigratedDisksPatch(migratedVolumes []virtstoragev1alpha1.MigratedVolume, vmi *virtv1.VirtualMachineInstance) error {
	var ops []string
	vmiCopy := vmi.DeepCopy()
	// Always reinitialized the migrated disks
	vmiCopy.Status.MigratedVolumes = []v1.StorageMigratedVolumeInfo{}
	for _, d := range migratedVolumes {
		pvcInterface, pvcExists, err := c.pvcInformer.GetStore().GetByKey(fmt.Sprintf("%s/%s", vmi.Namespace, d.DestinationPvc))
		if !pvcExists {
			return fmt.Errorf("failed getting information on the destination PVC %s: %v", d.DestinationPvc, err)
		}
		pvc := pvcInterface.(*k8score.PersistentVolumeClaim)
		filesystemOverhead, err := c.getFilesystemOverhead(pvc)
		if err != nil {
			log.Log.Reason(err).Errorf("Failed to get filesystem overhead for PVC %s/%s", vmi.Namespace, d.DestinationPvc)
			return err
		}
		vmiCopy.Status.MigratedVolumes = append(vmiCopy.Status.MigratedVolumes,
			v1.StorageMigratedVolumeInfo{
				SourcePvc:      d.SourcePvc,
				DestinationPvc: d.DestinationPvc,
				DestinationPVCInfo: &virtv1.PersistentVolumeClaimInfo{
					ClaimName:          pvc.Name,
					AccessModes:        pvc.Spec.AccessModes,
					VolumeMode:         pvc.Spec.VolumeMode,
					Capacity:           pvc.Status.Capacity,
					Requests:           pvc.Spec.Resources.Requests,
					Preallocated:       storagetypes.IsPreallocated(pvc.ObjectMeta.Annotations),
					FilesystemOverhead: &filesystemOverhead,
				},
			})

	}
	if !equality.Semantic.DeepEqual(vmi.Status, vmiCopy.Status) {
		newState, err := json.Marshal(vmiCopy.Status)
		if err != nil {
			return err
		}

		oldState, err := json.Marshal(vmi.Status)
		if err != nil {
			return err
		}
		ops = append(ops, fmt.Sprintf(`{ "op": "test", "path": "/status", "value": %s }`, string(oldState)))
		ops = append(ops, fmt.Sprintf(`{ "op": "replace", "path": "/status", "value": %s }`, string(newState)))
		_, err = c.clientset.VirtualMachineInstance(vmi.Namespace).Patch(context.Background(), vmi.Name, types.JSONPatchType, controller.GeneratePatchBytes(ops), &metav1.PatchOptions{})
		if err != nil {
			return err
		}
	}

	return nil
}

func replaceSourceVolswithDestinationVolVMI(migVols []virtstoragev1alpha1.MigratedVolume, vmi *virtv1.VirtualMachineInstance) error {
	replaceVol := make(map[string]string)
	for _, v := range migVols {
		replaceVol[v.SourcePvc] = v.DestinationPvc
	}

	for i, v := range vmi.Spec.Volumes {
		var claim string
		switch {
		case v.VolumeSource.PersistentVolumeClaim != nil:
			claim = v.VolumeSource.PersistentVolumeClaim.ClaimName
		case v.VolumeSource.DataVolume != nil:
			claim = v.VolumeSource.DataVolume.Name
		default:
			continue
		}

		if dest, ok := replaceVol[claim]; ok {
			switch {
			case v.VolumeSource.PersistentVolumeClaim != nil:
				vmi.Spec.Volumes[i].VolumeSource.PersistentVolumeClaim.ClaimName = dest
			case v.VolumeSource.DataVolume != nil:
				vmi.Spec.Volumes[i].VolumeSource.PersistentVolumeClaim = &virtv1.PersistentVolumeClaimVolumeSource{
					PersistentVolumeClaimVolumeSource: k8score.PersistentVolumeClaimVolumeSource{
						ClaimName: dest,
					},
				}
				vmi.Spec.Volumes[i].VolumeSource.DataVolume = nil
			}
			delete(replaceVol, claim)
		}
	}
	if len(replaceVol) != 0 {
		return fmt.Errorf("failed to replace the source volumes with the destination volumes in the VMI")
	}
	return nil
}

func (c *StorageMigrationController) updateVMIWithMigrationVolumes(vmi *virtv1.VirtualMachineInstance, migVols []virtstoragev1alpha1.MigratedVolume) (*virtv1.VirtualMachineInstance, error) {
	vmiCopy := vmi.DeepCopy()
	if err := replaceSourceVolswithDestinationVolVMI(migVols, vmiCopy); err != nil {
		return nil, err
	}
	if _, err := c.clientset.VirtualMachineInstance(vmi.ObjectMeta.Namespace).Update(context.Background(), vmiCopy); err != nil {
		return nil, fmt.Errorf("failed updating migrated disks: %v", err)
	}
	return vmiCopy.DeepCopy(), nil
}

// TODO: replace this function with errors.Join available from golang 1.20
func joinErrors(errors ...error) error {
	var err error
	for _, e := range errors {
		if e == nil {
			continue
		}
		if err == nil {
			err = e
		} else {
			err = fmt.Errorf("%s: %w", err.Error(), e)
		}
	}
	return err
}

func getVolName(v *virtv1.Volume) string {
	var claim string
	switch {
	case v.VolumeSource.PersistentVolumeClaim != nil:
		claim = v.VolumeSource.PersistentVolumeClaim.ClaimName
	case v.VolumeSource.DataVolume != nil:
		claim = v.VolumeSource.DataVolume.Name
	}
	return claim
}

func (c *StorageMigrationController) replaceSourceVolswithDestinationVolVM(vm *virtv1.VirtualMachine, vmi *virtv1.VirtualMachineInstance) error {
	migrateVolMap := make(map[string]string)
	volVmi := make(map[string]bool)
	if vmi == nil {
		return nil
	}
	if vmi.Status.MigrationState == nil {
		return nil
	}
	for _, v := range vmi.Status.MigratedVolumes {
		migrateVolMap[v.SourcePvc] = v.DestinationPvc
	}
	for _, v := range vmi.Spec.Volumes {
		if name := getVolName(&v); name != "" {
			volVmi[name] = true
		}
	}
	for k, v := range vm.Spec.Template.Spec.Volumes {
		if name := getVolName(&v); name != "" {
			// The volume to update in the VM needs to be one of the migrate
			// volume AND already have been changed in the VMI spec
			repName, okMig := migrateVolMap[name]
			_, okVMI := volVmi[name]
			if okMig && okVMI {
				switch {
				case v.VolumeSource.PersistentVolumeClaim != nil:
					vm.Spec.Template.Spec.Volumes[k].VolumeSource.PersistentVolumeClaim.ClaimName = repName
				case v.VolumeSource.DataVolume != nil:
					vm.Spec.Template.Spec.Volumes[k].VolumeSource.PersistentVolumeClaim = &virtv1.PersistentVolumeClaimVolumeSource{
						PersistentVolumeClaimVolumeSource: k8score.PersistentVolumeClaimVolumeSource{
							ClaimName: repName,
						},
					}
					vm.Spec.Template.Spec.Volumes[k].VolumeSource.DataVolume = nil
				}
			}
		}
	}
	return nil
}

func (c *StorageMigrationController) updateVMWithMigrationVolumes(vm *virtv1.VirtualMachine, vmi *virtv1.VirtualMachineInstance) (*virtv1.VirtualMachine, error) {
	vmCopy := vm.DeepCopy()
	if err := c.replaceSourceVolswithDestinationVolVM(vmCopy, vmi); err != nil {
		return nil, err
	}
	if _, err := c.clientset.VirtualMachine(vm.ObjectMeta.Namespace).Update(context.Background(), vmCopy); err != nil {
		return nil, fmt.Errorf("failed updating migrated disks: %v", err)
	}
	return vmCopy.DeepCopy(), nil
}

func (c *StorageMigrationController) cleanupVirtualMachineInstanceMigration(mig *virtv1.VirtualMachineInstanceMigration) error {
	var errRet error
	var podName string
	if mig.Status.MigrationState == nil {
		errRet = fmt.Errorf("migration %s has an empty state, cannot cleanup the target pod", mig.Name)
	} else {
		podName = mig.Status.MigrationState.TargetPod
	}
	if err := c.clientset.VirtualMachineInstanceMigration(mig.Namespace).Delete(mig.Name, &metav1.DeleteOptions{}); err != nil {
		errRet = joinErrors(errRet, err)
	}
	if podName == "" {
		return errRet
	}
	if err := c.clientset.CoreV1().Pods(mig.Namespace).Delete(context.TODO(), podName, metav1.DeleteOptions{}); err != nil {
		errRet = joinErrors(errRet, err)
	}
	return errRet
}

func (c *StorageMigrationController) groupVolumesPerVMI(sm *virtstoragev1alpha1.StorageMigration) (map[string][]virtstoragev1alpha1.MigratedVolume, error) {
	type destMigVol struct {
		name   string
		policy virtstoragev1alpha1.ReclaimPolicySourcePvc
	}
	migrVolPerVMI := make(map[string][]virtstoragev1alpha1.MigratedVolume)
	vmiList, err := c.clientset.VirtualMachineInstance(sm.Namespace).List(context.Background(), &metav1.ListOptions{})
	if err != nil {
		return migrVolPerVMI, fmt.Errorf("failed to get VMIs: %v", err)
	}
	vols := make(map[string]destMigVol)
	for _, volMigr := range sm.Spec.MigratedVolume {
		vols[volMigr.SourcePvc] = destMigVol{
			name:   volMigr.DestinationPvc,
			policy: volMigr.ReclaimPolicySourcePvc,
		}

	}

	for _, vmi := range vmiList.Items {
		var migrVols []virtstoragev1alpha1.MigratedVolume
		for _, v := range vmi.Spec.Volumes {
			var name string
			switch {
			case v.VolumeSource.DataVolume != nil:
				name = v.VolumeSource.DataVolume.Name
			case v.VolumeSource.PersistentVolumeClaim != nil:
				name = v.VolumeSource.PersistentVolumeClaim.PersistentVolumeClaimVolumeSource.ClaimName
			default:
				continue
			}
			if dst, ok := vols[name]; ok {
				migrVols = append(migrVols, virtstoragev1alpha1.MigratedVolume{
					SourcePvc:              name,
					DestinationPvc:         dst.name,
					ReclaimPolicySourcePvc: dst.policy,
				})
			}
		}
		if len(migrVols) > 0 {
			migrVolPerVMI[vmi.Name] = migrVols
		}
	}
	return migrVolPerVMI, nil
}

func (c *StorageMigrationController) execute(key string) error {
	var err error
	obj, exists, err := c.storageMigrationInformer.GetStore().GetByKey(key)
	if err != nil {
		return nil
	}
	if !exists {
		c.expectations.DeleteExpectations(key)
		return nil
	}
	sm := obj.(*virtstoragev1alpha1.StorageMigration)

	logger := log.Log.Object(sm)
	logger.V(1).Infof("Start processing storage class migration: %s", sm.Name)
	// this must be first step in execution. Writing the object
	// when api version changes ensures our api stored version is updated.
	if !controller.ObservedLatestApiVersionAnnotation(sm) {
		smCopy := sm.DeepCopy()
		controller.SetLatestApiVersionAnnotation(smCopy)
		_, err = c.clientset.StorageMigration(sm.Namespace).Update(context.TODO(), smCopy, metav1.UpdateOptions{})
		return err
	}
	migrVolPerVMI, err := c.groupVolumesPerVMI(sm)
	if err != nil {
		return err
	}
	for vmi, volMigr := range migrVolPerVMI {
		if err = c.executeStorageMigPerVMI(sm, volMigr, vmi); err != nil {
			logger.Object(sm).Reason(err).Errorf("Failed to migrate the storage for VMI %s", vmi)
			err = fmt.Errorf("One of the storage migration failed %s:%v", sm.Name, err)
		}
	}
	return err
}

func (c *StorageMigrationController) executeStorageMigPerVMI(sm *virtstoragev1alpha1.StorageMigration, migVols []virtstoragev1alpha1.MigratedVolume, vmiName string) error {
	var errRet error
	ns := sm.Namespace
	vmiObj, vmiExists, err := c.vmiInformer.GetStore().GetByKey(fmt.Sprintf("%s/%s", ns, vmiName))
	if err != nil {
		return err
	}
	// Update the VMI object with the migrated disks in the status
	if !vmiExists {
		return fmt.Errorf("VMI %s for the storage migration %s", vmiName, sm.Name)
	}
	vmi := vmiObj.(*virtv1.VirtualMachineInstance)

	logger := log.Log.Object(vmi)
	logger.V(1).Infof("Storage migration for volumes of VMI %s", vmi.Name)

	// Check if the migration has already been triggered
	migName := sm.GetVirtualMachiheInstanceMigrationName(vmiName)
	migObj, exists, err := c.migrationInformer.GetStore().GetByKey(fmt.Sprintf("%s/%s", ns, migName))
	if err != nil {
		return err
	}
	// Start the migration if it doesn't exist
	if !exists {
		logger.V(1).Infof("Start VM migration %s for VMI %s", migName, vmi.Name)
		return c.triggerVirtualMachineInstanceMigration(migVols, vmiName, sm.Name, migName, ns)
	}

	var err1 error
	mig := migObj.(*virtv1.VirtualMachineInstanceMigration)
	if sm, err1 = c.updateStatusStorageMigration(sm, mig, migVols); err != nil {
		return err1
	}
	if mig.Status.MigrationState != nil && mig.Status.MigrationState.Completed && !mig.Status.MigrationState.Failed {
		logger.V(1).Infof("Migration completed VMI %s update the migrate volumes", vmi.Name)
		if _, err := c.updateVMIWithMigrationVolumes(vmi, migVols); err != nil {
			return err
		}
		// If the VMI has a VM controller, then update the VM spec consequentially
		if len(vmi.ObjectMeta.OwnerReferences) == 1 {
			vmObj, vmExists, err := c.vmInformer.GetStore().GetByKey(fmt.Sprintf("%s/%s", ns, vmiName))
			if err != nil {
				return err
			}
			if !vmExists {
				return fmt.Errorf("VM %s for the storage migration doesn't exist", vmiName)
			}
			vm := vmObj.(*virtv1.VirtualMachine)
			if _, err1 = c.updateVMWithMigrationVolumes(vm, vmi); err != nil {
				return err1
			}

		}
		// Clean-up the virtual machine migration
		if err := c.cleanupVirtualMachineInstanceMigration(mig); err != nil {
			return err
		}
		// Handle source PVC
		for _, v := range migVols {
			switch v.ReclaimPolicySourcePvc {
			case virtstoragev1alpha1.DeleteReclaimPolicySourcePvc:
				err1 := c.clientset.CoreV1().PersistentVolumeClaims(ns).Delete(context.TODO(),
					v.SourcePvc, metav1.DeleteOptions{})
				errRet = joinErrors(errRet, err1)
				if err1 != nil {
					logger.V(1).Infof("Delete source volume %s", v.SourcePvc)
				}
			case virtstoragev1alpha1.RetainReclaimPolicySourcePvc:
				// Do nothing for the retain policy
				logger.V(1).Infof("Retain source volume %s", v.SourcePvc)
				continue
			default:
				errRet = joinErrors(errRet, fmt.Errorf("PVC policy '%s' not recongnized", v.ReclaimPolicySourcePvc))
			}
		}
	}

	return errRet
}

func (c *StorageMigrationController) enqueueStorageMigration(obj interface{}) {
	logger := log.Log
	sm := obj.(*virtstoragev1alpha1.StorageMigration)
	key, err := controller.KeyFunc(sm)
	if err != nil {
		logger.Object(sm).Reason(err).Error("Failed to extract key from storage migration.")
		return
	}
	c.Queue.Add(key)
}

func (c *StorageMigrationController) addStorageMigration(obj interface{}) {
	c.enqueueStorageMigration(obj)
}

func (c *StorageMigrationController) deleteStorageMigration(obj interface{}) {
	c.enqueueStorageMigration(obj)
}

func (c *StorageMigrationController) updateStorageMigration(_, curr interface{}) {
	c.enqueueStorageMigration(curr)
}

func (c *StorageMigrationController) checkAndEnqueuStorageMigration(obj interface{}) {
	mig := obj.(*virtv1.VirtualMachineInstanceMigration)
	smName, ok := mig.ObjectMeta.Labels[labelStorageMigration]
	if !ok {
		return
	}
	smObj, exists, err := c.storageMigrationInformer.GetStore().GetByKey(mig.Namespace + "/" + smName)
	if err != nil {
		return
	}
	if !exists {
		return
	}
	sm := smObj.(*virtstoragev1alpha1.StorageMigration)
	c.enqueueStorageMigration(sm)
}

func (c *StorageMigrationController) addMigration(obj interface{}) {
	c.checkAndEnqueuStorageMigration(obj)
}

func (c *StorageMigrationController) deleteMigration(obj interface{}) {
	c.checkAndEnqueuStorageMigration(obj)
}

func (c *StorageMigrationController) updateMigration(_, curr interface{}) {
	c.checkAndEnqueuStorageMigration(curr)
}
