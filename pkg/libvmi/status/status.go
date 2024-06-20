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

// Package status provides utility options to manage the vmi status.
// This package should be used only in unit test files in which there is the need
// to set the status of a vmi to simulate different behaviors.
// BE AWARE: any usage of this package outside the unit test files or controllers
// is to be considered wrong since the status should only be manipulated by the controllers.
package status

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	v1 "kubevirt.io/api/core/v1"

	"kubevirt.io/kubevirt/pkg/libvmi"
)

type Option func(vmiStatus *v1.VirtualMachineInstanceStatus)

// WithStatus sets the status with specified value
func WithStatus(status v1.VirtualMachineInstanceStatus) libvmi.Option {
	return func(vmi *v1.VirtualMachineInstance) {
		vmi.Status = status
	}
}

// New instantiates a new VMI status configuration,
// building its properties based on the specified With* options.
func New(opts ...Option) v1.VirtualMachineInstanceStatus {
	vmiStatus := &v1.VirtualMachineInstanceStatus{}
	for _, f := range opts {
		f(vmiStatus)
	}

	return *vmiStatus
}

// WithPhase sets the vmi phase
func WithPhase(phase v1.VirtualMachineInstancePhase) Option {
	return func(vmiStatus *v1.VirtualMachineInstanceStatus) {
		vmiStatus.Phase = phase
		vmiStatus.PhaseTransitionTimestamps = append(vmiStatus.PhaseTransitionTimestamps, v1.VirtualMachineInstancePhaseTransitionTimestamp{
			Phase:                    phase,
			PhaseTransitionTimestamp: metav1.Now(),
		})
	}
}

// WithPhaseTransitionTimestamps adds the vmi phase transition timestamp
func WithPhaseTransitionTimestamps(ts v1.VirtualMachineInstancePhaseTransitionTimestamp) Option {
	return func(vmiStatus *v1.VirtualMachineInstanceStatus) {
		vmiStatus.PhaseTransitionTimestamps = append(vmiStatus.PhaseTransitionTimestamps, ts)
	}
}

// WithCondition adds the condition to the status conditions list
func WithCondition(condition v1.VirtualMachineInstanceCondition) Option {
	return func(vmiStatus *v1.VirtualMachineInstanceStatus) {
		vmiStatus.Conditions = append(vmiStatus.Conditions, condition)
	}
}

// WithLauncherContainerImageVersion sets the status.launcherContainerImageVersion
func WithLauncherContainerImageVersion(image string) Option {
	return func(vmiStatus *v1.VirtualMachineInstanceStatus) {
		vmiStatus.LauncherContainerImageVersion = image
	}
}

// WithActivePod adds an active pod with the given uid and nodename
func WithActivePod(uid types.UID, nodeName string) Option {
	return func(vmiStatus *v1.VirtualMachineInstanceStatus) {
		if vmiStatus.ActivePods == nil {
			vmiStatus.ActivePods = map[types.UID]string{}
		}
		vmiStatus.ActivePods[uid] = nodeName
	}
}

// WithMigratedVolume adds a migrated volume
func WithMigratedVolume(volumeInfo v1.StorageMigratedVolumeInfo) Option {
	return func(vmiStatus *v1.VirtualMachineInstanceStatus) {
		vmiStatus.MigratedVolumes = append(vmiStatus.MigratedVolumes, volumeInfo)
	}
}

// WithVolumeStatus adds a volume status
func WithVolumeStatus(volumeStatus v1.VolumeStatus) Option {
	return func(vmiStatus *v1.VirtualMachineInstanceStatus) {
		vmiStatus.VolumeStatus = append(vmiStatus.VolumeStatus, volumeStatus)
	}
}
