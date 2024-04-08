/*
 * This file is part of the KubeVirt project
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 * Copyright The KubeVirt Authors
 *
 */

package volumemigration

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/equality"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"kubevirt.io/client-go/kubecli"
	"kubevirt.io/client-go/log"

	k8sv1 "k8s.io/api/core/v1"
	virtv1 "kubevirt.io/api/core/v1"

	"kubevirt.io/kubevirt/pkg/apimachinery/patch"
	"kubevirt.io/kubevirt/pkg/controller"
	storagetypes "kubevirt.io/kubevirt/pkg/storage/types"
)

const invalidUpdateErrMsg = "The volumes can be only reverted to the previous ones during the update"

// invalidVols includes the invalid volumes for the volume migration
type invalidVols struct {
	hotplugged  []string
	fs          []string
	shareable   []string
	luns        []string
	dvs         []string
	dvTemplates []string
}

func (vols *invalidVols) errorMessage() string {
	var s strings.Builder
	if len(vols.hotplugged) < 0 && len(vols.fs) < 0 &&
		len(vols.shareable) < 0 && len(vols.luns) < 0 {
		return ""
	}
	s.WriteString("invalid volumes to update with migration:")
	if len(vols.hotplugged) > 0 {
		s.WriteString(fmt.Sprintf(" hotplugged: %v", vols.hotplugged))
	}
	if len(vols.fs) > 0 {
		s.WriteString(fmt.Sprintf(" filesystems: %v", vols.fs))
	}
	if len(vols.shareable) > 0 {
		s.WriteString(fmt.Sprintf(" shareable: %v", vols.shareable))
	}
	if len(vols.luns) > 0 {
		s.WriteString(fmt.Sprintf(" luns: %v", vols.luns))
	}
	if len(vols.dvs) > 0 {
		s.WriteString(fmt.Sprintf(" DVs: %v", vols.dvs))
	}
	if len(vols.dvTemplates) > 0 {
		s.WriteString(fmt.Sprintf(" DV templates: %v", vols.dvTemplates))
	}

	return s.String()
}

func updatedVolumesMapping(vmi *virtv1.VirtualMachineInstance, vm *virtv1.VirtualMachine) map[string]string {
	updateVols := make(map[string]string)
	vmVols := make(map[string]string)
	// New volumes
	for _, v := range vm.Spec.Template.Spec.Volumes {
		if name := storagetypes.PVCNameFromVirtVolume(&v); name != "" {
			vmVols[v.Name] = name
		}
	}
	// Old volumes
	for _, v := range vmi.Spec.Volumes {
		name := storagetypes.PVCNameFromVirtVolume(&v)
		claim, ok := vmVols[v.Name]
		if ok && name != claim {
			updateVols[v.Name] = claim
		}
	}
	return updateVols
}

func ValidateVolumes(vmi *virtv1.VirtualMachineInstance, vm *virtv1.VirtualMachine) (bool, string) {
	var invalidVols invalidVols
	if vmi == nil {
		return false, "cannot validate the migrated volumes for an empty VMI"
	}
	if vm == nil {
		return false, "cannot validate the migrated volumes for an empty VM"
	}
	updatedVols := updatedVolumesMapping(vmi, vm)
	valid := true
	disks := storagetypes.GetDisksByName(&vmi.Spec)
	filesystems := storagetypes.GetFilesystemsFromVolumes(vmi)
	dvTemplates := make(map[string]bool)
	for _, t := range vm.Spec.DataVolumeTemplates {
		dvTemplates[t.Name] = true
	}
	for _, v := range vm.Spec.Template.Spec.Volumes {
		_, ok := updatedVols[v.Name]
		if !ok {
			continue
		}

		// Datavolumes should be transformed into PVCs
		if v.VolumeSource.DataVolume != nil {
			invalidVols.dvs = append(invalidVols.dvs, v.Name)
			valid = false
			continue
		}
		// The reference to the DV template needs to be removed
		if _, ok := dvTemplates[v.Name]; ok {
			invalidVols.dvTemplates = append(invalidVols.dvTemplates, v.Name)
			valid = false
			continue
		}

		// Hotplugged volumes
		if storagetypes.IsHotplugVolume(&v) {
			invalidVols.hotplugged = append(invalidVols.hotplugged, v.Name)
			valid = false
			continue
		}
		// Filesystems
		if _, ok := filesystems[v.Name]; ok {
			invalidVols.fs = append(invalidVols.fs, v.Name)
			valid = false
			continue
		}

		d, ok := disks[v.Name]
		if !ok {
			continue
		}

		// Shareable disks
		if d.Shareable != nil && *d.Shareable {
			invalidVols.shareable = append(invalidVols.shareable, v.Name)
			valid = false
			continue
		}

		// LUN disks
		if d.DiskDevice.LUN != nil {
			invalidVols.luns = append(invalidVols.luns, v.Name)
			valid = false
			continue
		}
	}
	if !valid {
		return false, invalidVols.errorMessage()
	}

	return true, ""
}

func VolumeMigrationAbortion(clientset kubecli.KubevirtClient, vmi *virtv1.VirtualMachineInstance, vm *virtv1.VirtualMachine) (bool, error) {
	if !IsVolumeMigrating(vmi) || !changeMigratedVolumes(vmi, vm) {
		return false, nil
	}
	if revertedToOldVolumes(vmi, vm) {
		vmiCopy, err := PatchVMIVolumes(clientset, vmi, vm)
		if err != nil {
			return true, err
		}
		return true, abortVolumeMigration(clientset, vmiCopy)
	}

	return true, fmt.Errorf(invalidUpdateErrMsg)
}

func changeMigratedVolumes(vmi *virtv1.VirtualMachineInstance, vm *virtv1.VirtualMachine) bool {
	updatedVols := updatedVolumesMapping(vmi, vm)
	for _, migVol := range vmi.Status.MigratedVolumes {
		if _, ok := updatedVols[migVol.VolumeName]; ok {
			return true
		}
	}
	return false
}

// revertedToOldVolumes checks that all migrated volumes have been reverted from
// destination to the source volume
func revertedToOldVolumes(vmi *virtv1.VirtualMachineInstance, vm *virtv1.VirtualMachine) bool {
	updatedVols := updatedVolumesMapping(vmi, vm)
	for _, migVol := range vmi.Status.MigratedVolumes {
		if migVol.SourcePVCInfo == nil {
			// something wrong with the source volume
			return false
		}
		claim, ok := updatedVols[migVol.VolumeName]
		if !ok || migVol.SourcePVCInfo.ClaimName != claim {
			return false
		}
		delete(updatedVols, migVol.VolumeName)
	}
	// updatedVols should only include the source volumes and not additional
	// volumes.
	return len(updatedVols) == 0
}

func abortVolumeMigration(clientset kubecli.KubevirtClient, vmi *virtv1.VirtualMachineInstance) error {
	if vmi == nil {
		return fmt.Errorf("vmi is empty")
	}
	log.Log.V(2).Object(vmi).Infof("Abort volume migration")
	vmiConditions := controller.NewVirtualMachineInstanceConditionManager()
	vmiCopy := vmi.DeepCopy()
	vmiConditions.UpdateCondition(vmiCopy, &virtv1.VirtualMachineInstanceCondition{
		Type:               virtv1.VirtualMachineInstanceVolumesChange,
		LastTransitionTime: v1.Now(),
		Status:             k8sv1.ConditionFalse,
	})
	p, err := patch.New(
		patch.WithAdd("/status/conditions", vmiCopy.Status.Conditions),
		patch.WithAdd("/status/migratedVolumes", []virtv1.StorageMigratedVolumeInfo{}),
	).GeneratePayload()
	if err != nil {
		return err
	}
	_, err = clientset.VirtualMachineInstance(vmi.Namespace).Patch(context.Background(), vmi.Name, types.JSONPatchType, p, v1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("failed updating vmi condition: %v", err)
	}
	return nil
}

func IsVolumeMigrating(vmi *virtv1.VirtualMachineInstance) bool {
	condManager := controller.NewVirtualMachineInstanceConditionManager()
	cond := condManager.GetCondition(vmi, virtv1.VirtualMachineInstanceVolumesChange)
	if cond != nil {
		return cond.Status == k8sv1.ConditionTrue
	}
	return false
}

func PatchVMIStatusWithMigratedVolumes(clientset kubecli.KubevirtClient, pvcStore cache.Store, vmi *virtv1.VirtualMachineInstance, vm *virtv1.VirtualMachine) error {
	var migVolsInfo []virtv1.StorageMigratedVolumeInfo
	oldVols := make(map[string]string)
	for _, v := range vmi.Spec.Volumes {
		if pvcName := storagetypes.PVCNameFromVirtVolume(&v); pvcName != "" {
			oldVols[v.Name] = pvcName
		}
	}
	for _, v := range vm.Spec.Template.Spec.Volumes {
		claim := storagetypes.PVCNameFromVirtVolume(&v)
		oldClaim, ok := oldVols[v.Name]
		if !ok {
			continue
		}
		if oldClaim == claim {
			continue
		}
		oldPvc, err := storagetypes.GetPersistentVolumeClaimFromCache(vmi.Namespace, oldClaim, pvcStore)
		if err != nil {
			return err
		}
		pvc, err := storagetypes.GetPersistentVolumeClaimFromCache(vmi.Namespace, claim, pvcStore)
		if err != nil {
			return err
		}
		var oldVolMode *k8sv1.PersistentVolumeMode
		var volMode *k8sv1.PersistentVolumeMode
		if oldPvc != nil && oldPvc.Spec.VolumeMode != nil {
			oldVolMode = oldPvc.Spec.VolumeMode
		}
		if pvc != nil && pvc.Spec.VolumeMode != nil {
			volMode = pvc.Spec.VolumeMode
		}
		migVolsInfo = append(migVolsInfo, virtv1.StorageMigratedVolumeInfo{
			VolumeName: v.Name,
			DestinationPVCInfo: &virtv1.PersistentVolumeClaimInfo{
				ClaimName:  claim,
				VolumeMode: volMode,
			},
			SourcePVCInfo: &virtv1.PersistentVolumeClaimInfo{
				ClaimName:  oldClaim,
				VolumeMode: oldVolMode,
			},
		})
	}
	if equality.Semantic.DeepEqual(migVolsInfo, vmi.Status.MigratedVolumes) {
		return nil
	}
	patch, err := patch.New(patch.WithAdd("/status/migratedVolumes", migVolsInfo)).GeneratePayload()
	if err != nil {
		return err
	}
	vmi, err = clientset.VirtualMachineInstance(vmi.Namespace).Patch(context.Background(), vmi.Name, types.JSONPatchType, patch, v1.PatchOptions{})
	return err
}

func PatchVMIVolumes(clientset kubecli.KubevirtClient, vmi *virtv1.VirtualMachineInstance, vm *virtv1.VirtualMachine) (*virtv1.VirtualMachineInstance, error) {
	log.Log.V(2).Object(vmi).Infof("Patch VMI volumes")
	patch, err := patch.New(patch.WithAdd("/spec/volumes", vm.Spec.Template.Spec.Volumes)).GeneratePayload()
	if err != nil {
		return nil, err
	}
	return clientset.VirtualMachineInstance(vmi.Namespace).Patch(context.Background(), vmi.Name, types.JSONPatchType, patch, v1.PatchOptions{})
}
