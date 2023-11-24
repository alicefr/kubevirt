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

package storage

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	. "github.com/onsi/gomega"
	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "kubevirt.io/api/core/v1"
	virtv1 "kubevirt.io/api/core/v1"
	virtstoragev1alpha1 "kubevirt.io/api/storage/v1alpha1"
	"kubevirt.io/client-go/kubecli"

	"kubevirt.io/kubevirt/tests"
	"kubevirt.io/kubevirt/tests/console"
	cd "kubevirt.io/kubevirt/tests/containerdisk"
	"kubevirt.io/kubevirt/tests/framework/kubevirt"
	"kubevirt.io/kubevirt/tests/libstorage"
	"kubevirt.io/kubevirt/tests/testsuite"
)

var _ = SIGDescribe("Storage migration", func() {
	var virtClient kubecli.KubevirtClient
	BeforeEach(func() {
		virtClient = kubevirt.Client()
	})
	waitForStorageMigrationForVMIToComplete := func(smName, vmiName, ns string, seconds int) {
		gomega.EventuallyWithOffset(1, func() bool {
			sm, err := virtClient.StorageMigration(ns).Get(context.Background(), smName, metav1.GetOptions{})
			Expect(err).ShouldNot(HaveOccurred())
			s := sm.GetStorageMigrationStateForVMI(vmiName)
			if s == nil {
				return false
			}
			if s.Completed {
				return true
			}
			return false
		}, seconds, 1*time.Second).Should(gomega.BeTrue(), fmt.Sprintf("The storage migration for %s should be completed", vmiName))

	}

	waitForPVCToBeDeleted := func(claimName, ns string, seconds int) {
		gomega.EventuallyWithOffset(1, func() bool {
			_, err := virtClient.CoreV1().PersistentVolumeClaims(ns).Get(context.Background(), claimName, metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, seconds, 1*time.Second).Should(gomega.BeTrue(), fmt.Sprintf("The PVC %s should be deleted", claimName))

	}

	checkStorageMigrationFailed := func(smName, vmiName, ns string) bool {
		sm, err := virtClient.StorageMigration(ns).Get(context.Background(), smName, metav1.GetOptions{})
		Expect(err).ShouldNot(HaveOccurred())
		s := sm.GetStorageMigrationStateForVMI(vmiName)
		Expect(s).ShouldNot(BeNil())
		if s.Failed {
			return true
		}
		return false

	}

	checkPVCVMI := func(vmi *virtv1.VirtualMachineInstance, claimName string, seconds int) {
		gomega.EventuallyWithOffset(1, func() bool {
			for _, v := range vmi.Spec.Volumes {
				if v.VolumeSource.PersistentVolumeClaim == nil {
					continue
				}
				if v.VolumeSource.PersistentVolumeClaim.ClaimName == claimName {
					return true
				}
			}
			return false
		}, seconds, 1*time.Second).Should(gomega.BeTrue(), fmt.Sprintf("The VMI %s should have the destination PVC %s", vmi.Name, claimName))
	}
	getVMIMigrationConditions := func(migName, ns string) string {
		var str strings.Builder
		mig, err := virtClient.VirtualMachineInstanceMigration(ns).Get(migName, &metav1.GetOptions{})
		Expect(err).ShouldNot(HaveOccurred())
		str.WriteString(fmt.Sprintf("VMI migrations %s conditions:\n", mig.Name))
		for _, c := range mig.Status.Conditions {
			str.WriteString(fmt.Sprintf("%s: %s: %s\n", c.Status, c.Reason, c.Message))
		}
		return str.String()

	}
	Describe("Starting storage migration", func() {
		FIt("storage migration with a single VM and filesystem DV", func() {
			const (
				destPVC = "dest-pvc"
				smName  = "sm"
			)
			ns := testsuite.GetTestNamespace(nil)
			sc, exists := libstorage.GetRWOFileSystemStorageClass()
			if !exists {
				Skip("Skip test when Filesystem storage is not present")
			}
			// Create VM with a filesystem DV
			vmi, dataVolume := tests.NewRandomVirtualMachineInstanceWithDisk(cd.DataVolumeImportUrlForContainerDisk(cd.ContainerDiskCirros), ns, sc, k8sv1.ReadWriteOnce, k8sv1.PersistentVolumeFilesystem)
			tests.AddUserData(vmi, "cloud-init", "#!/bin/bash\necho 'hello'\n")
			vmi = tests.RunVMIAndExpectLaunch(vmi, 500)

			By("Expecting the VirtualMachineInstance console")
			Expect(console.LoginToCirros(vmi)).To(Succeed())

			size := dataVolume.Spec.PVC.Resources.Requests.Storage()
			Expect(size).ShouldNot(BeNil())

			// Create dest PVC
			libstorage.CreateFSPVC(destPVC, ns, size.String(), nil)
			// Create Storage Migration
			sm := virtstoragev1alpha1.StorageMigration{
				ObjectMeta: metav1.ObjectMeta{Name: smName},
				Spec: virtstoragev1alpha1.StorageMigrationSpec{
					MigratedVolume: []virtstoragev1alpha1.MigratedVolume{
						{
							SourcePvc:              dataVolume.Name,
							DestinationPvc:         destPVC,
							ReclaimPolicySourcePvc: virtstoragev1alpha1.DeleteReclaimPolicySourcePvc,
						},
					},
				},
			}
			By("Creating the storage migration")
			_, err := virtClient.StorageMigration(ns).Create(context.Background(), &sm, metav1.CreateOptions{})
			Expect(err).ShouldNot(HaveOccurred())
			// Wait Storage Migration to complete
			waitForStorageMigrationForVMIToComplete(smName, vmi.Name, ns, 180)
			migName := sm.GetVirtualMachiheInstanceMigrationName(vmi.Name)
			Expect(checkStorageMigrationFailed(smName, vmi.Name, ns)).To(BeFalse(), getVMIMigrationConditions(migName, ns))

			// Check status for the migration for the VMI
			vmi, err = virtClient.VirtualMachineInstance(ns).Get(context.Background(), vmi.Name, &metav1.GetOptions{})
			Expect(err).ShouldNot(HaveOccurred())
			// Check the VMI is running after the migration
			Expect(vmi.Status.Phase).To(Equal(v1.Running))
			// Check if the source volume have been replace with the destination PVC
			checkPVCVMI(vmi, destPVC, 90)

			// Check src PVC has been deleted
			waitForPVCToBeDeleted(dataVolume.Name, ns, 90)

		})
	})

	// Same test with VM with multiple disks
	// Same test with 2 VMs
	// Same test with dst block PVC

})
