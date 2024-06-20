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
 * Copyright 2017 Red Hat, Inc.
 *
 */

package containerdisk

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"kubevirt.io/kubevirt/pkg/testutils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"kubevirt.io/client-go/api"

	v1 "kubevirt.io/api/core/v1"

	virtwrapapi "kubevirt.io/kubevirt/pkg/virt-launcher/virtwrap/api"
)

var _ = Describe("ContainerDisk", func() {
	Context("GenerateContainers", func() {
		DescribeTable("by verifying that resources are set if the VMI wants the guaranteed QOS class", func(req, lim, expectedReq, expectedLimit k8sv1.ResourceList) {
			clusterConfig, _, _ := testutils.NewFakeClusterConfigUsingKVConfig(&v1.KubeVirtConfiguration{
				SupportContainerResources: []v1.SupportContainerResources{
					{
						Type: v1.ContainerDisk,
						Resources: k8sv1.ResourceRequirements{
							Requests: req,
							Limits:   lim,
						},
					},
				},
			})

			vmi := api.NewMinimalVMI("fake-vmi")
			appendContainerDisk(vmi, "r0")
			vmi.Spec.Domain.Resources = v1.ResourceRequirements{
				Requests: k8sv1.ResourceList{
					k8sv1.ResourceCPU:    resource.MustParse("1"),
					k8sv1.ResourceMemory: resource.MustParse("64M"),
				},
				Limits: k8sv1.ResourceList{
					k8sv1.ResourceCPU:    resource.MustParse("1"),
					k8sv1.ResourceMemory: resource.MustParse("64M"),
				},
			}
			containers := GenerateContainers(vmi, clusterConfig, nil, "libvirt-runtime", "/var/run/libvirt")

			Expect(containers[0].Resources.Requests).To(ContainElements(*expectedReq.Cpu(), *expectedReq.Memory(), *expectedReq.StorageEphemeral()))
			Expect(containers[0].Resources.Limits).To(BeEquivalentTo(expectedLimit))
		},
			Entry("defaults not overriden", k8sv1.ResourceList{}, k8sv1.ResourceList{}, k8sv1.ResourceList{
				k8sv1.ResourceCPU:              resource.MustParse("10m"),
				k8sv1.ResourceMemory:           resource.MustParse("40M"),
				k8sv1.ResourceEphemeralStorage: resource.MustParse("50M"),
			}, k8sv1.ResourceList{
				k8sv1.ResourceCPU:    resource.MustParse("10m"),
				k8sv1.ResourceMemory: resource.MustParse("40M"),
			}),
			Entry("defaults overriden", k8sv1.ResourceList{
				k8sv1.ResourceCPU:    resource.MustParse("1m"),
				k8sv1.ResourceMemory: resource.MustParse("25M"),
			}, k8sv1.ResourceList{
				k8sv1.ResourceCPU:    resource.MustParse("100m"),
				k8sv1.ResourceMemory: resource.MustParse("400M"),
			}, k8sv1.ResourceList{
				k8sv1.ResourceCPU:              resource.MustParse("100m"),
				k8sv1.ResourceMemory:           resource.MustParse("400M"),
				k8sv1.ResourceEphemeralStorage: resource.MustParse("50M"),
			}, k8sv1.ResourceList{
				k8sv1.ResourceCPU:    resource.MustParse("100m"),
				k8sv1.ResourceMemory: resource.MustParse("400M"),
			}),
		)

		DescribeTable("by verifying that resources are set from config", func(req, lim, expectedReq, expectedLimit k8sv1.ResourceList) {
			clusterConfig, _, _ := testutils.NewFakeClusterConfigUsingKVConfig(&v1.KubeVirtConfiguration{
				SupportContainerResources: []v1.SupportContainerResources{
					{
						Type: v1.ContainerDisk,
						Resources: k8sv1.ResourceRequirements{
							Requests: req,
							Limits:   lim,
						},
					},
				},
			})

			vmi := api.NewMinimalVMI("fake-vmi")
			appendContainerDisk(vmi, "r0")
			containers := GenerateContainers(vmi, clusterConfig, nil, "libvirt-runtime", "/var/run/libvirt")

			Expect(containers[0].Resources.Requests).To(ContainElements(*expectedReq.Cpu(), *expectedReq.Memory(), *expectedReq.StorageEphemeral()))
			Expect(containers[0].Resources.Limits).To(BeEquivalentTo(expectedLimit))
		},
			Entry("defaults not overriden", k8sv1.ResourceList{}, k8sv1.ResourceList{}, k8sv1.ResourceList{
				k8sv1.ResourceCPU:              resource.MustParse("1m"),
				k8sv1.ResourceMemory:           resource.MustParse("1M"),
				k8sv1.ResourceEphemeralStorage: resource.MustParse("50M"),
			}, k8sv1.ResourceList{
				k8sv1.ResourceCPU:    resource.MustParse("10m"),
				k8sv1.ResourceMemory: resource.MustParse("40M"),
			}),
			Entry("defaults overriden", k8sv1.ResourceList{
				k8sv1.ResourceCPU:    resource.MustParse("2m"),
				k8sv1.ResourceMemory: resource.MustParse("25M"),
			}, k8sv1.ResourceList{
				k8sv1.ResourceCPU:    resource.MustParse("110m"),
				k8sv1.ResourceMemory: resource.MustParse("400M"),
			}, k8sv1.ResourceList{
				k8sv1.ResourceCPU:              resource.MustParse("2m"),
				k8sv1.ResourceMemory:           resource.MustParse("25M"),
				k8sv1.ResourceEphemeralStorage: resource.MustParse("50M"),
			}, k8sv1.ResourceList{
				k8sv1.ResourceCPU:    resource.MustParse("110m"),
				k8sv1.ResourceMemory: resource.MustParse("400M"),
			}),
		)

		It("by verifying that ephemeral storage request is set to every container", func() {
			clusterConfig, _, _ := testutils.NewFakeClusterConfigUsingKVConfig(&v1.KubeVirtConfiguration{
				SupportContainerResources: []v1.SupportContainerResources{},
			})

			vmi := api.NewMinimalVMI("fake-vmi")
			appendContainerDisk(vmi, "r0")
			containers := GenerateContainers(vmi, clusterConfig, nil, "libvirt-runtime", "/var/run/libvirt")

			expectedEphemeralStorageRequest := resource.MustParse(ephemeralStorageOverheadSize)

			containerResourceSpecs := make([]k8sv1.ResourceList, 0)
			for _, container := range containers {
				containerResourceSpecs = append(containerResourceSpecs, container.Resources.Requests)
			}

			for _, containerResourceSpec := range containerResourceSpecs {
				Expect(containerResourceSpec).To(HaveKeyWithValue(k8sv1.ResourceEphemeralStorage, expectedEphemeralStorageRequest))
			}
		})
		It("by verifying container generation", func() {
			clusterConfig, _, _ := testutils.NewFakeClusterConfigUsingKVConfig(&v1.KubeVirtConfiguration{
				SupportContainerResources: []v1.SupportContainerResources{},
			})
			vmi := api.NewMinimalVMI("fake-vmi")
			appendContainerDisk(vmi, "r1")
			appendContainerDisk(vmi, "r0")
			containers := GenerateContainers(vmi, clusterConfig, nil, "libvirt-runtime", "bin-volume")

			Expect(containers).To(HaveLen(2))
			Expect(containers[0].ImagePullPolicy).To(Equal(k8sv1.PullAlways))
			Expect(containers[1].ImagePullPolicy).To(Equal(k8sv1.PullAlways))
		})
	})

	Context("should use the right containerID", func() {
		It("for a new migration pod with two containerDisks", func() {
			clusterConfig, _, _ := testutils.NewFakeClusterConfigUsingKVConfig(&v1.KubeVirtConfiguration{
				SupportContainerResources: []v1.SupportContainerResources{},
			})
			vmi := api.NewMinimalVMI("myvmi")
			appendContainerDisk(vmi, "disk1")
			appendNonContainerDisk(vmi, "disk3")
			appendContainerDisk(vmi, "disk2")

			pod := createMigrationSourcePod(vmi)

			imageIDs := ExtractImageIDsFromSourcePod(vmi, pod)
			Expect(imageIDs).To(HaveKeyWithValue("disk1", "someimage@sha256:0"))
			Expect(imageIDs).To(HaveKeyWithValue("disk2", "someimage@sha256:1"))
			Expect(imageIDs).To(HaveLen(2))

			newContainers := GenerateContainers(vmi, clusterConfig, imageIDs, "a-name", "something")
			Expect(newContainers[0].Image).To(Equal("someimage@sha256:0"))
			Expect(newContainers[1].Image).To(Equal("someimage@sha256:1"))
		})
		It("for a new migration pod with a containerDisk and a kernel image", func() {
			clusterConfig, _, _ := testutils.NewFakeClusterConfigUsingKVConfig(&v1.KubeVirtConfiguration{
				SupportContainerResources: []v1.SupportContainerResources{},
			})
			vmi := api.NewMinimalVMI("myvmi")
			appendContainerDisk(vmi, "disk1")
			appendNonContainerDisk(vmi, "disk3")

			vmi.Spec.Domain.Firmware = &v1.Firmware{KernelBoot: &v1.KernelBoot{Container: &v1.KernelBootContainer{Image: "someimage:v1.2.3.4"}}}

			pod := createMigrationSourcePod(vmi)

			imageIDs := ExtractImageIDsFromSourcePod(vmi, pod)
			Expect(imageIDs).To(HaveKeyWithValue("disk1", "someimage@sha256:0"))
			Expect(imageIDs).To(HaveKeyWithValue("kernel-boot-volume", "someimage@sha256:bootcontainer"))
			Expect(imageIDs).To(HaveLen(2))

			newContainers := GenerateContainers(vmi, clusterConfig, imageIDs, "a-name", "something")
			newBootContainer := GenerateKernelBootContainer(vmi, clusterConfig, imageIDs, "a-name", "something")
			newContainers = append(newContainers, *newBootContainer)
			Expect(newContainers[0].Image).To(Equal("someimage@sha256:0"))
			Expect(newContainers[1].Image).To(Equal("someimage@sha256:bootcontainer"))
		})

		It("should return the source image tag if it can't detect a reproducible imageID", func() {
			vmi := api.NewMinimalVMI("myvmi")
			appendContainerDisk(vmi, "disk1")
			pod := createMigrationSourcePod(vmi)
			pod.Status.ContainerStatuses[0].ImageID = "rubbish"
			imageIDs := ExtractImageIDsFromSourcePod(vmi, pod)
			Expect(imageIDs["disk1"]).To(Equal(vmi.Spec.Volumes[0].ContainerDisk.Image))
		})

		DescribeTable("It should detect the image ID from", func(imageID string) {
			expected := "myregistry.io/myimage@sha256:4gjffGJlg4"
			res := toPullableImageReference("myregistry.io/myimage", imageID)
			Expect(res).To(Equal(expected))
			res = toPullableImageReference("myregistry.io/myimage:1234", imageID)
			Expect(res).To(Equal(expected))
			res = toPullableImageReference("myregistry.io/myimage:latest", imageID)
			Expect(res).To(Equal(expected))
		},
			Entry("docker", "docker://sha256:4gjffGJlg4"),
			Entry("dontainerd", "sha256:4gjffGJlg4"),
			Entry("cri-o", "myregistry/myimage@sha256:4gjffGJlg4"),
		)

		DescribeTable("It should detect the base image from", func(given, expected string) {
			res := toPullableImageReference(given, "docker://sha256:4gjffGJlg4")
			Expect(strings.Split(res, "@sha256:")[0]).To(Equal(expected))
		},
			Entry("image with registry and no tags or shasum", "myregistry.io/myimage", "myregistry.io/myimage"),
			Entry("image with registry and tag", "myregistry.io/myimage:latest", "myregistry.io/myimage"),
			Entry("image with registry and shasum", "myregistry.io/myimage@sha256:123534", "myregistry.io/myimage"),
			Entry("image with registry and no tags or shasum and custom port", "myregistry.io:5000/myimage", "myregistry.io:5000/myimage"),
			Entry("image with registry and tag and custom port", "myregistry.io:5000/myimage:latest", "myregistry.io:5000/myimage"),
			Entry("image with registry and shasum and custom port", "myregistry.io:5000/myimage@sha256:123534", "myregistry.io:5000/myimage"),
			Entry("image with registry and shasum and custom port and group", "myregistry.io:5000/mygroup/myimage@sha256:123534", "myregistry.io:5000/mygroup/myimage"),
		)
	})

	Context("when generating the container", func() {
		DescribeTable("when generating the container", func(testFunc func(*k8sv1.Container)) {
			clusterConfig, _, _ := testutils.NewFakeClusterConfigUsingKVConfig(&v1.KubeVirtConfiguration{
				SupportContainerResources: []v1.SupportContainerResources{},
			})
			vmi := api.NewMinimalVMI("myvmi")
			appendContainerDisk(vmi, "disk1")

			pod := createMigrationSourcePod(vmi)
			imageIDs := ExtractImageIDsFromSourcePod(vmi, pod)

			newContainers := GenerateContainers(vmi, clusterConfig, imageIDs, "a-name", "something")
			testFunc(&newContainers[0])
		},
			Entry("AllowPrivilegeEscalation should be false", func(c *k8sv1.Container) {
				Expect(*c.SecurityContext.AllowPrivilegeEscalation).To(BeFalse())
			}),
			Entry("all capabilities should be dropped", func(c *k8sv1.Container) {
				Expect(c.SecurityContext.Capabilities.Drop).To(Equal([]k8sv1.Capability{"ALL"}))
			}),
		)
	})

	Context("CreateEphemeralImages", func() {
		const volName = "disk0"
		var (
			cdManager      ContainerDiskManager
			fakeEdc        *fakeEphemeralDiskCreator
			backendImgPath string
		)
		BeforeEach(func() {
			const pid = "1234"
			fakeEdc = &fakeEphemeralDiskCreator{}
			tmpDir := GinkgoT().TempDir()
			tmpProcDir := GinkgoT().TempDir()
			cdManager = ContainerDiskManager{
				pidFileDir: tmpDir,
				procfs:     tmpProcDir,
			}
			dir := filepath.Join(tmpDir, volName)
			Expect(os.Mkdir(dir, 0755)).NotTo(HaveOccurred())
			Expect(os.WriteFile(filepath.Join(dir, Pidfile), []byte(pid), 0755)).NotTo(HaveOccurred())
			imageFileDir := filepath.Join(tmpProcDir, pid, "root", "disk")
			Expect(os.MkdirAll(imageFileDir, 0755)).NotTo(HaveOccurred())
			backendImgPath = filepath.Join(imageFileDir, "disk.img")
			_, err := os.Create(backendImgPath)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should successfully create the emphemeral image", func() {
			vmi := api.NewMinimalVMI("myvmi")
			appendContainerDisk(vmi, volName)
			Expect(cdManager.CreateEphemeralImages(vmi, fakeEdc)).NotTo(HaveOccurred())
		})

		It("should successfully create the emphemeral image when the symlink already exists", func() {
			cdManager.mountBaseDir = GinkgoT().TempDir()
			Expect(os.Symlink(backendImgPath, cdManager.GetDiskTargetPathFromLauncherView(0))).NotTo(HaveOccurred())
			vmi := api.NewMinimalVMI("myvmi")
			appendContainerDisk(vmi, volName)
			Expect(cdManager.CreateEphemeralImages(vmi, fakeEdc)).NotTo(HaveOccurred())
		})
		It("should fail to create the emphemeral image when the symlink points to another file", func() {
			cdManager.mountBaseDir = GinkgoT().TempDir()
			Expect(os.Symlink("something-wrong", cdManager.GetDiskTargetPathFromLauncherView(0))).NotTo(HaveOccurred())
			vmi := api.NewMinimalVMI("myvmi")
			appendContainerDisk(vmi, volName)
			Expect(cdManager.CreateEphemeralImages(vmi, fakeEdc)).To(MatchError(
				fmt.Sprintf("failed checking the symlink for something-wrong, doesn't match with %s", backendImgPath),
			))
		})
	})

	Context("WaitContainerDisksToBecomeReady", func() {
		var (
			cdManager ContainerDiskManager
		)
		It("should succeed if all disks are ready", func() {
			const volName = "disk1"
			tmpDir := GinkgoT().TempDir()
			cdManager = ContainerDiskManager{pidFileDir: tmpDir}
			dir := filepath.Join(tmpDir, volName)
			writePidfile(dir)
			vmi := api.NewMinimalVMI("myvmi")
			appendContainerDisk(vmi, volName)
			Expect(cdManager.WaitContainerDisksToBecomeReady(vmi, 5)).NotTo(HaveOccurred())
		})
		It("should timeout if the pidfile doesn't exist", func() {
			vmi := api.NewMinimalVMI("myvmi")
			appendContainerDisk(vmi, "noexist")
			Expect(cdManager.WaitContainerDisksToBecomeReady(vmi, 5)).To(MatchError("timeout waiting for container disks to become ready"))
		})
		It("should succeed if VMI doesn't have any container disks", func() {
			vmi := api.NewMinimalVMI("myvmi")
			Expect(cdManager.WaitContainerDisksToBecomeReady(vmi, 5)).NotTo(HaveOccurred())
		})
		It("should succeed with a VM with kernel boot", func() {
			tmpDir := GinkgoT().TempDir()
			cdManager = ContainerDiskManager{pidFileDir: tmpDir}
			dir := filepath.Join(tmpDir, KernelBootVolumeName)
			writePidfile(dir)
			vmi := api.NewMinimalVMI("myvmi")
			vmi.Spec.Domain.Firmware = &v1.Firmware{KernelBoot: &v1.KernelBoot{Container: &v1.KernelBootContainer{Image: "someimage:v1.2.3.4"}}}
			Expect(cdManager.WaitContainerDisksToBecomeReady(vmi, 5)).NotTo(HaveOccurred())

		})
	})

	Context("StopContainerDiskContainers", func() {
		var (
			cdManager ContainerDiskManager
			fpf       *fakeProcessFinder
		)
		BeforeEach(func() {
			tmpDir := GinkgoT().TempDir()
			fpf = &fakeProcessFinder{}
			cdManager = ContainerDiskManager{
				pidFileDir:  tmpDir,
				findProcess: fpf.fakeFindProcess,
			}
		})
		DescribeTable("should successfull stop container disks", func(cds []string) {
			vmi := api.NewMinimalVMI("myvmi")
			cdManager.cdVolumes = cds
			for _, volName := range cds {
				dir := filepath.Join(cdManager.pidFileDir, volName)
				writePidfile(dir)
				appendContainerDisk(vmi, volName)
			}
			Expect(cdManager.StopContainerDiskContainers()).ToNot(HaveOccurred())
		},
			Entry("with no container disks", []string{}),
			Entry("with a single container disk", []string{"vol0"}),
			Entry("with multiple container disks", []string{"vol0", "vol1", "vol2"}),
		)

		DescribeTable("should fail if it cannot kill the container disk process", func(cds []string) {
			err := fmt.Errorf("some error")
			fpf.setError(err)
			vmi := api.NewMinimalVMI("myvmi")
			cdManager.cdVolumes = cds
			for _, volName := range cds {
				dir := filepath.Join(cdManager.pidFileDir, volName)
				writePidfile(dir)
				appendContainerDisk(vmi, volName)
			}
			errRes := fmt.Errorf("failed to stop container disks: %w", err)
			for i := 0; i < len(cds)-1; i++ {
				errRes = fmt.Errorf("%w, %w", errRes, err)
			}
			Expect(cdManager.StopContainerDiskContainers()).To(MatchError(errRes))
		},
			Entry("with a single container disk", []string{"vol0"}),
			Entry("with a single container disk", []string{"vol0", "vol1"}),
		)
	})

	Context("AccessKernelBoot", func() {
		const (
			bootDir = "/boot"
			kernel  = "vmlinuz-virt"
			initrd  = "initramfs-virt"
		)

		var (
			cdManager         ContainerDiskManager
			backendKernelPath string
			backendInitrdPath string
			vmi               *v1.VirtualMachineInstance
		)
		backendPathPerArtifact := func(artifact string) string {
			switch artifact {
			case kernel:
				return backendKernelPath
			case initrd:
				return backendInitrdPath
			default:
				Fail(fmt.Sprintf("wrong artifact %s", artifact))
			}
			return ""
		}
		BeforeEach(func() {
			const pid = "1234"
			cdManager = ContainerDiskManager{
				pidFileDir:   GinkgoT().TempDir(),
				procfs:       GinkgoT().TempDir(),
				mountBaseDir: GinkgoT().TempDir(),
			}
			dir := filepath.Join(cdManager.pidFileDir, KernelBootVolumeName)
			writePidfile(dir)
			artifactsDir := filepath.Join(cdManager.procfs, pid, "root", bootDir)
			Expect(os.MkdirAll(artifactsDir, 0755)).NotTo(HaveOccurred())
			backendKernelPath = filepath.Join(artifactsDir, kernel)
			_, err := os.Create(backendKernelPath)
			Expect(err).NotTo(HaveOccurred())
			backendInitrdPath = filepath.Join(artifactsDir, initrd)
			_, err = os.Create(backendInitrdPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(os.MkdirAll(filepath.Join(cdManager.mountBaseDir, KernelBootVolumeName), 0755)).NotTo(HaveOccurred())
			vmi = api.NewMinimalVMI("myvmi")
			vmi.Spec.Domain.Firmware = &v1.Firmware{KernelBoot: &v1.KernelBoot{
				Container: &v1.KernelBootContainer{
					Image:      "someimage:v1.2.3.4",
					KernelPath: filepath.Join(bootDir, kernel),
					InitrdPath: filepath.Join(bootDir, initrd),
				}}}
		})

		It("should successfully create the emphemeral image", func() {
			Expect(cdManager.AccessKernelBoot(vmi)).NotTo(HaveOccurred())
		})
		DescribeTable("should successfully create a bootalbe container when the symlink already exists", func(artifact string) {
			backendPath := backendPathPerArtifact(artifact)
			Expect(os.Symlink(backendPath, cdManager.GetKernelBootArtifactPathFromLauncherView(artifact))).NotTo(HaveOccurred())
			Expect(cdManager.AccessKernelBoot(vmi)).NotTo(HaveOccurred())
		},
			Entry("for the kernel", kernel),
			Entry("for the initrd", initrd),
		)
		DescribeTable("should fail to create the emphemeral image when the symlink points to another file", func(artifact string) {
			backendPath := backendPathPerArtifact(artifact)
			fmt.Printf("XXX backendPath:%s\n", backendPath)
			Expect(os.Symlink("something-wrong", cdManager.GetKernelBootArtifactPathFromLauncherView(artifact))).NotTo(HaveOccurred())
			Expect(cdManager.AccessKernelBoot(vmi)).To(MatchError(
				fmt.Sprintf("failed checking the symlink for something-wrong, doesn't match with %s", backendPath),
			))
		},
			Entry("for the kernel", kernel),
			Entry("for the initrd", initrd),
		)
	})
})

func writePidfile(dir string) {
	Expect(os.Mkdir(dir, 0755)).NotTo(HaveOccurred())
	Expect(os.WriteFile(filepath.Join(dir, Pidfile), []byte("1234"), 0755)).NotTo(HaveOccurred())
}

type fakeEphemeralDiskCreator struct{}

func (f *fakeEphemeralDiskCreator) CreateBackedImageForVolume(volume v1.Volume, backingFile string, backingFormat string) error {
	return nil
}

func (f *fakeEphemeralDiskCreator) CreateEphemeralImages(vmi *v1.VirtualMachineInstance, domain *virtwrapapi.Domain) error {
	return nil
}

func (f *fakeEphemeralDiskCreator) GetFilePath(volumeName string) string {
	return ""
}

func (f *fakeEphemeralDiskCreator) Init() error {
	return nil
}

type fakeProcessFinder struct {
	err error
}

func (f *fakeProcessFinder) setError(err error) {
	f.err = err
}

func (f *fakeProcessFinder) fakeFindProcess(pid int) (process, error) {
	return &fakeProcess{err: f.err}, nil
}

type fakeProcess struct {
	err error
}

func (f *fakeProcess) Kill() error {
	return f.err
}

func appendContainerDisk(vmi *v1.VirtualMachineInstance, diskName string) {
	vmi.Spec.Domain.Devices.Disks = append(vmi.Spec.Domain.Devices.Disks, v1.Disk{
		Name: diskName,
		DiskDevice: v1.DiskDevice{
			Disk: &v1.DiskTarget{},
		},
	})
	vmi.Spec.Volumes = append(vmi.Spec.Volumes, v1.Volume{
		Name: diskName,
		VolumeSource: v1.VolumeSource{
			ContainerDisk: &v1.ContainerDiskSource{
				Image:           "someimage:v1.2.3.4",
				ImagePullPolicy: k8sv1.PullAlways,
			},
		},
	})
}
func appendNonContainerDisk(vmi *v1.VirtualMachineInstance, diskName string) {
	vmi.Spec.Domain.Devices.Disks = append(vmi.Spec.Domain.Devices.Disks, v1.Disk{
		Name: diskName,
		DiskDevice: v1.DiskDevice{
			Disk: &v1.DiskTarget{},
		},
	})
	vmi.Spec.Volumes = append(vmi.Spec.Volumes, v1.Volume{
		Name: diskName,
		VolumeSource: v1.VolumeSource{
			DataVolume: &v1.DataVolumeSource{},
		},
	})
}

func createMigrationSourcePod(vmi *v1.VirtualMachineInstance) *k8sv1.Pod {
	clusterConfig, _, _ := testutils.NewFakeClusterConfigUsingKVConfig(&v1.KubeVirtConfiguration{
		SupportContainerResources: []v1.SupportContainerResources{},
	})
	pod := &k8sv1.Pod{Status: k8sv1.PodStatus{}}
	containers := GenerateContainers(vmi, clusterConfig, nil, "a-name", "something")

	for idx, container := range containers {
		status := k8sv1.ContainerStatus{
			Name:    container.Name,
			Image:   container.Image,
			ImageID: fmt.Sprintf("finalimg@sha256:%v", idx),
		}
		pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses, status)
	}
	bootContainer := GenerateKernelBootContainer(vmi, clusterConfig, nil, "a-name", "something")
	if bootContainer != nil {
		status := k8sv1.ContainerStatus{
			Name:    bootContainer.Name,
			Image:   bootContainer.Image,
			ImageID: fmt.Sprintf("finalimg@sha256:%v", "bootcontainer"),
		}
		pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses, status)
	}

	return pod
}
