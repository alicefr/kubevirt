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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// StorageMigration defines the operation of moving the storage to another
// storage backend
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

type MigratedVolume struct {
	SourcePvc      string `json:"sourcePvc,omitempty" valid:"required"`
	DestinationPvc string `json:"destinationPvc,omitempty" valid:"required"`
}

type MigrationStorageClass struct {
	SourceStorageClass      string `json:"sourceStorageClass,omitempty" valid:"required"`
	DestinationStorageClass string `json:"destinationStorageClass,omitempty" valid:"required"`
}

type ReclaimPolicySourcePvc string

const (
	DeleteReclaimPolicySourcePvc ReclaimPolicySourcePvc = "Delete"
)

// StorageMigrationSpec is the spec for a StorageMigration resource
type StorageMigrationSpec struct {
	VMIName string `json:"vmiName,omitempty" valid:"required"`
	// MigratedVolumes is a list of volumes to be migrated
	// +optional
	MigratedVolume []MigratedVolume `json:"migratedVolume,omitempty"`
	// MigrationStorageClass contains the information for relocating the
	// volumes of the source storage class to the destination storage class
	// +optional
	MigrationStorageClass *MigrationStorageClass `json:"migrationStorageClass,omitempty"`
	// ReclaimPolicySourcePvc describes how the source volumes will be
	// treated after a successful migration
	// +optional
	ReclaimPolicySourcePvc ReclaimPolicySourcePvc `json:"reclaimPolicySourcePvc,omitempty"`
}

// StorageMigrationStatus is the status for a StorageMigration resource
type StorageMigrationStatus struct {
	// MigratedVolumes is a list of volumes to be migrated
	// +optional
	MigratedVolume []MigratedVolume `json:"migratedVolume,omitempty"`
	// VirtualMachineMigrationState state of the virtual machine migration
	// triggered by the storage migration
	// +optional
	VirtualMachineMigrationName string `json:"virtualMachineMigrationName,omitempty"`
	// +optional
	Completed bool `json:"completed,omitempty"`
	// +optional
	Failed bool `json:"failed,omitempty"`
}
