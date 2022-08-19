package mount_manager

import (
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/types"

	v1 "kubevirt.io/api/core/v1"

	containerdisk "kubevirt.io/kubevirt/pkg/container-disk"
	virtconfig "kubevirt.io/kubevirt/pkg/virt-config"
	"kubevirt.io/kubevirt/pkg/virt-handler/isolation"
	container_disk "kubevirt.io/kubevirt/pkg/virt-handler/mount-manager/container-disk"
	hotplug_volume "kubevirt.io/kubevirt/pkg/virt-handler/mount-manager/hotplug-disk"
	mountutils "kubevirt.io/kubevirt/pkg/virt-handler/mount-manager/utils"
)

//go:generate mockgen -source $GOFILE -package=$GOPACKAGE -destination=generated_mock_$GOFILE

// MountInfo wraps all the mount information
type MountInfo struct {
	containerDisksInfo map[string]*containerdisk.DiskInfo
}

func (info *MountInfo) GetContainerDisksInfo() map[string]*containerdisk.DiskInfo {
	return info.containerDisksInfo
}

// MountManager handles all the mount operations required by KubeVirt
type MountManager interface {
	Mount(vmi *v1.VirtualMachineInstance) (MountInfo, error)
	Unmount(vmi *v1.VirtualMachineInstance) error
	MountsReady(vmi *v1.VirtualMachineInstance, notInitializedSince time.Time) (bool, error)
	SyncMounts(vmi *v1.VirtualMachineInstance) error
	SyncUnmounts(vmi *v1.VirtualMachineInstance) error
	IsMounted(vmi *v1.VirtualMachineInstance, volume string, sourceUID types.UID) (bool, error)
}

type mountManager struct {
	containerDiskMounter container_disk.Mounter
	hotplugVolumeMounter hotplug_volume.VolumeMounter
}

func NewMounter(virtPrivateDir string, podIsolationDetector isolation.PodIsolationDetector, clusterConfig *virtconfig.ClusterConfig) MountManager {
	mountRecorder := mountutils.NewMountRecorder(virtPrivateDir)
	return &mountManager{
		containerDiskMounter: container_disk.NewMounter(podIsolationDetector, clusterConfig, mountRecorder),
		hotplugVolumeMounter: hotplug_volume.NewVolumeMounter(mountRecorder),
	}
}

// MountsReady returns if the mount points are ready to be used
func (m *mountManager) MountsReady(vmi *v1.VirtualMachineInstance, notInitializedSince time.Time) (bool, error) {
	// Check container diks are ready to be mounted
	return m.containerDiskMounter.ContainerDisksReady(vmi, notInitializedSince)
}

// Mount mounts the volumes managed directly by KubeVirt
func (m *mountManager) Mount(vmi *v1.VirtualMachineInstance) (MountInfo, error) {
	disksInfo, err := m.containerDiskMounter.MountAndVerify(vmi)
	if err != nil {
		return MountInfo{}, err
	}

	attachmentPodUID := types.UID("")
	if vmi.Status.MigrationState != nil {
		attachmentPodUID = vmi.Status.MigrationState.TargetAttachmentPodUID
	}
	if attachmentPodUID != types.UID("") {
		if err = m.hotplugVolumeMounter.MountFromPod(vmi, attachmentPodUID); err != nil {
			return MountInfo{}, fmt.Errorf("failed to mount hotplug volumes: %v", err)
		}

	} else {
		if err = m.hotplugVolumeMounter.Mount(vmi); err != nil {
			return MountInfo{}, fmt.Errorf("failed to mount hotplug volumes: %v", err)
		}
	}
	return MountInfo{
		containerDisksInfo: disksInfo,
	}, nil
}

// Unmount unmounts the volumes managed directly by KubeVirt
func (m *mountManager) Unmount(vmi *v1.VirtualMachineInstance) error {
	if err := m.containerDiskMounter.Unmount(vmi); err != nil {
		return err
	}
	return m.hotplugVolumeMounter.UnmountAll(vmi)
}

// SyncMounts mounts the volumes managed directly by KubeVirt on a running VMI
func (m *mountManager) SyncMounts(vmi *v1.VirtualMachineInstance) error {
	return m.hotplugVolumeMounter.Mount(vmi)
}

// SyncUnmounts unmounts the volumes managed directly by KubeVirt on a running VMI
func (m *mountManager) SyncUnmounts(vmi *v1.VirtualMachineInstance) error {
	return m.hotplugVolumeMounter.Unmount(vmi)
}

// IsMounted returns if a volume is mounted
func (m *mountManager) IsMounted(vmi *v1.VirtualMachineInstance, volume string, sourceUID types.UID) (bool, error) {
	return m.hotplugVolumeMounter.IsMounted(vmi, volume, sourceUID)
}
