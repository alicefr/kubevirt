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
 * Copyright 2023 Red Hat, Inc.
 *
 */

package v1alpha1

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// StorageMigration defines the operation of moving the storage to another
// storage backend.
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type StorageMigration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec StorageMigrationSpec `json:"spec"`

	// +optional
	Status *StorageMigrationStatus `json:"status,omitempty"`
}

// StorageMigrationList is a list of StorageMigration resources
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type StorageMigrationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	// +listType=atomic
	Items []StorageMigration `json:"items"`
}

// ReclaimPolicySourcePvc describes how the source PVC will be treated after the storage migration completes.
// The policies follows the same behavior as the RetainPolicy for PVs: https://kubernetes.io/docs/concepts/storage/persistent-volumes/#reclaiming
type ReclaimPolicySourcePvc string

const (
	DeleteReclaimPolicySourcePvc ReclaimPolicySourcePvc = "Delete"
	RetainReclaimPolicySourcePvc ReclaimPolicySourcePvc = "Retain"
)

type MigratedVolume struct {
	//	VMIName        string `json:"vmiName,omitempty" valid:"required"`
	SourcePvc      string `json:"sourcePvc,omitempty" valid:"required"`
	DestinationPvc string `json:"destinationPvc,omitempty" valid:"required"`
	// ReclaimPolicySourcePvc describes how the source volumes will be
	// treated after a successful migration
	// +optional
	ReclaimPolicySourcePvc ReclaimPolicySourcePvc `json:"reclaimPolicySourcePvc,omitempty"`
}

// StorageMigrationSpec is the spec for a StorageMigration resource
type StorageMigrationSpec struct {
	// MigratedVolumes is a list of volumes to be migrated
	// +optional
	MigratedVolume []MigratedVolume `json:"migratedVolume,omitempty"`
}

// StorageMigrationState is the status for a StorageMigration resource
type StorageMigrationState struct {
	// MigratedVolumes is a list of volumes to be migrated
	// +optional
	MigratedVolume []MigratedVolume `json:"migratedVolume,omitempty"`
	// VirtualMachineMigrationName state of the virtual machine migration
	// triggered by the storage migration
	// +optional
	VirtualMachineMigrationName string `json:"virtualMachineMigrationName,omitempty"`
	// VirtualMachineMigrationState state of the virtual machine migration
	// triggered by the storage migration
	// +optional
	VirtualMachineInstanceName string `json:"virtualMachineInstanceName,omitempty"`
	// +optional
	Completed bool `json:"completed,omitempty"`
	// +optional
	Failed bool `json:"failed,omitempty"`
	// The time the migration action began
	// +nullable
	StartTimestamp *metav1.Time `json:"startTimestamp,omitempty"`
	// The time the migration action ended
	// +nullable
	EndTimestamp *metav1.Time `json:"endTimestamp,omitempty"`
}

// +kubebuilder:printcolumn:JSONPath=".status.total",name="Total",type="integer"
// +kubebuilder:printcolumn:JSONPath=".status.failed",name="Failed",type="integer"
// +kubebuilder:printcolumn:JSONPath=".status.completed",name="Completed",type="integer"
// +kubebuilder:printcolumn:JSONPath=".status.notStarted",name="NotStarted",type="integer"
// +kubebuilder:printcolumn:JSONPath=".status.running",name="Running",type="integer"
type StorageMigrationStatus struct {
	StorageMigrationStates []StorageMigrationState `json:"storageMigrationStates,omitempty"`
	Total                  int                     `json:"total,omitempty"`
	Failed                 int                     `json:"failed,omitempty"`
	Completed              int                     `json:"completed,omitempty"`
	NotStarted             int                     `json:"notStarted,omitempty"`
	Running                int                     `json:"running,omitempty"`
}

func (sm *StorageMigration) GetStorageMigrationStateForVMI(vmiName string) *StorageMigrationState {
	if sm.Status == nil {
		return nil
	}
	for _, s := range sm.Status.StorageMigrationStates {
		if s.VirtualMachineInstanceName == vmiName {
			return &s
		}
	}
	return nil
}

func (sm *StorageMigration) GetVirtualMachiheInstanceMigrationName(vmiName string) string {
	return fmt.Sprintf("%s-%s", sm.Name, vmiName)
}
