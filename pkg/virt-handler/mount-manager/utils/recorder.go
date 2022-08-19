package recorder

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"

	"k8s.io/apimachinery/pkg/types"
	v1 "kubevirt.io/api/core/v1"

	diskutils "kubevirt.io/kubevirt/pkg/ephemeral-disk-utils"
	"kubevirt.io/kubevirt/pkg/safepath"
	"kubevirt.io/kubevirt/pkg/unsafepath"
)

//go:generate mockgen -source $GOFILE -package=$GOPACKAGE -destination=generated_mock_$GOFILE

type HotpluggedDisksMountTargetEntry struct {
	TargetFile string `json:"targetFile"`
}

type ContainerDisksMountTargetEntry struct {
	TargetFile string `json:"targetFile"`
	SocketFile string `json:"socketFile"`
}

type VMIMountTargetEntry struct {
	HotpluggedVolumes []HotpluggedDisksMountTargetEntry `json:"hotpluggedDisks"`
	ContainerDisks    []ContainerDisksMountTargetEntry  `json:"containerDisks"`
}

type VMIMountTargetRecord struct {
	MountTargetEntries VMIMountTargetEntry `json:"mountTargetEntries"`
	UsesSafePaths      bool                `json:"usesSafePaths"`
}

func (r *VMIMountTargetRecord) GetContainerDisks() []ContainerDisksMountTargetEntry {
	return r.MountTargetEntries.ContainerDisks
}

func (r *VMIMountTargetRecord) GetHotpluggedVolumes() []HotpluggedDisksMountTargetEntry {
	return r.MountTargetEntries.HotpluggedVolumes
}

func (record *VMIMountTargetRecord) IsEmpty() bool {
	return len(record.MountTargetEntries.ContainerDisks) == 0 && len(record.MountTargetEntries.HotpluggedVolumes) == 0
}

type RecordEntry int

const (
	CONTAINERDISKS_ENTRY RecordEntry = iota
	HOTPLUGGEDVOLUMES_ENTRY
	ALL_ENTRIES
)

type mounter struct {
	mountStateDir    string
	mountRecords     map[types.UID]*VMIMountTargetRecord
	mountRecordsLock sync.Mutex
}

type MountRecorder interface {
	SetAddMountRecordContainerDisk(vmi *v1.VirtualMachineInstance, cdRecord []ContainerDisksMountTargetEntry, addPreviousRules bool) error
	DeleteContainerDisksMountRecord(vmi *v1.VirtualMachineInstance) error
	GetContainerDisksMountRecord(vmi *v1.VirtualMachineInstance) ([]ContainerDisksMountTargetEntry, error)
	SetMountRecordHotpluggedVolumes(vmi *v1.VirtualMachineInstance, hpRecord []HotpluggedDisksMountTargetEntry) error
	GetHotpluggedVolumesMountRecord(vmi *v1.VirtualMachineInstance) ([]HotpluggedDisksMountTargetEntry, error)
	DeleteHotpluggedVolumesMountRecord(vmi *v1.VirtualMachineInstance) error
}

func NewMountRecorder(mountStateDir string) MountRecorder {
	return &mounter{
		mountStateDir: mountStateDir,
		mountRecords:  make(map[types.UID]*VMIMountTargetRecord),
	}
}

func (m *mounter) SetAddMountRecordContainerDisk(vmi *v1.VirtualMachineInstance, cdRecord []ContainerDisksMountTargetEntry, addPreviousRules bool) error {
	record, err := m.getMountTargetRecord(vmi)
	if err != nil {
		return err
	}

	if record == nil {
		// Initialize it
		record = &VMIMountTargetRecord{}
	}
	if addPreviousRules {
		record.MountTargetEntries.ContainerDisks = append(record.MountTargetEntries.ContainerDisks, cdRecord...)
	} else {
		record.MountTargetEntries.ContainerDisks = cdRecord
	}

	return m.setMountTargetRecord(vmi, record)
}

func (m *mounter) SetMountRecordHotpluggedVolumes(vmi *v1.VirtualMachineInstance, hpRecord []HotpluggedDisksMountTargetEntry) error {
	record, err := m.getMountTargetRecord(vmi)
	if err != nil {
		return err
	}

	if record == nil {
		// Initialize it
		record = &VMIMountTargetRecord{}
	}
	record.MountTargetEntries.HotpluggedVolumes = hpRecord

	return m.setMountTargetRecord(vmi, record)
}

func (m *mounter) DeleteContainerDisksMountRecord(vmi *v1.VirtualMachineInstance) error {
	return m.deleteMountTargetRecord(vmi, CONTAINERDISKS_ENTRY)
}

func (m *mounter) DeleteHotpluggedVolumesMountRecord(vmi *v1.VirtualMachineInstance) error {
	return m.deleteMountTargetRecord(vmi, HOTPLUGGEDVOLUMES_ENTRY)
}

func (m *mounter) GetContainerDisksMountRecord(vmi *v1.VirtualMachineInstance) ([]ContainerDisksMountTargetEntry, error) {
	record, err := m.getMountTargetRecord(vmi)
	if err != nil {
		return []ContainerDisksMountTargetEntry{}, err
	}
	if record == nil {
		return []ContainerDisksMountTargetEntry{}, nil
	}
	return record.GetContainerDisks(), nil
}

func (m *mounter) GetHotpluggedVolumesMountRecord(vmi *v1.VirtualMachineInstance) ([]HotpluggedDisksMountTargetEntry, error) {
	record, err := m.getMountTargetRecord(vmi)
	if err != nil {
		return []HotpluggedDisksMountTargetEntry{}, err
	}
	if record == nil {
		return []HotpluggedDisksMountTargetEntry{}, nil
	}
	return record.GetHotpluggedVolumes(), nil
}

func (m *mounter) deleteMountTargetRecord(vmi *v1.VirtualMachineInstance, entry RecordEntry) error {
	if string(vmi.UID) == "" {
		return fmt.Errorf("cannot find the mount record without the VMI uid")
	}

	recordFile := filepath.Join(m.mountStateDir, string(vmi.UID))
	exists, err := diskutils.FileExists(recordFile)
	if err != nil {
		return err
	}

	if exists {
		record, err := m.getMountTargetRecord(vmi)
		if err != nil {
			return err
		}
		r, ok := m.mountRecords[vmi.UID]
		for _, target := range record.MountTargetEntries.ContainerDisks {
			if entry != CONTAINERDISKS_ENTRY && entry != ALL_ENTRIES {
				break
			}
			os.Remove(target.TargetFile)
			os.Remove(target.SocketFile)
			if ok {
				r.MountTargetEntries.ContainerDisks = []ContainerDisksMountTargetEntry{}
			}
		}
		for _, target := range record.MountTargetEntries.HotpluggedVolumes {
			if entry != HOTPLUGGEDVOLUMES_ENTRY && entry != ALL_ENTRIES {
				break
			}
			os.Remove(target.TargetFile)
			if ok {
				r.MountTargetEntries.HotpluggedVolumes = []HotpluggedDisksMountTargetEntry{}
			}
		}

		if record.IsEmpty() {
			os.Remove(recordFile)
			m.mountRecordsLock.Lock()
			defer m.mountRecordsLock.Unlock()
			delete(m.mountRecords, vmi.UID)
		}
		return nil
	}

	// Eventually if the record file doesn't exist we delete the record also
	m.mountRecordsLock.Lock()
	defer m.mountRecordsLock.Unlock()
	delete(m.mountRecords, vmi.UID)

	return nil
}

func (m *mounter) getMountTargetRecord(vmi *v1.VirtualMachineInstance) (*VMIMountTargetRecord, error) {
	var ok bool
	var existingRecord *VMIMountTargetRecord

	if string(vmi.UID) == "" {
		return nil, fmt.Errorf("unable to find container disk mounted directories for vmi without uid")
	}

	m.mountRecordsLock.Lock()
	defer m.mountRecordsLock.Unlock()
	existingRecord, ok = m.mountRecords[vmi.UID]

	// first check memory cache
	if ok {
		return existingRecord, nil
	}

	// if not there, see if record is on disk, this can happen if virt-handler restarts
	recordFile := filepath.Join(m.mountStateDir, filepath.Clean(string(vmi.UID)))

	exists, err := diskutils.FileExists(recordFile)
	if err != nil {
		return nil, err
	}

	if exists {
		record := VMIMountTargetRecord{}
		// #nosec No risk for path injection. Using static base and cleaned filename
		bytes, err := os.ReadFile(recordFile)
		if err != nil {
			return nil, err
		}
		err = json.Unmarshal(bytes, &record)
		if err != nil {
			return nil, err
		}
		// XXX: backward compatibility for old unresolved paths, can be removed in July 2023
		// After a one-time convert and persist, old records are safe too.
		if !record.UsesSafePaths {
			record.UsesSafePaths = true
			for i, entry := range record.MountTargetEntries.ContainerDisks {
				safePath, err := safepath.JoinAndResolveWithRelativeRoot("/", entry.TargetFile)
				if err != nil {
					return nil, fmt.Errorf("failed converting legacy path to safepath: %v", err)
				}
				record.MountTargetEntries.ContainerDisks[i].TargetFile = unsafepath.UnsafeAbsolute(safePath.Raw())
			}
			for i, entry := range record.MountTargetEntries.HotpluggedVolumes {
				safePath, err := safepath.JoinAndResolveWithRelativeRoot("/", entry.TargetFile)
				if err != nil {
					return nil, fmt.Errorf("failed converting legacy path to safepath: %v", err)
				}
				record.MountTargetEntries.HotpluggedVolumes[i].TargetFile = unsafepath.UnsafeAbsolute(safePath.Raw())
			}
		}

		m.mountRecords[vmi.UID] = &record
		return &record, nil
	}

	// not found
	return nil, nil
}

func (m *mounter) setMountTargetRecord(vmi *v1.VirtualMachineInstance, record *VMIMountTargetRecord) error {
	if string(vmi.UID) == "" {
		return fmt.Errorf("unable to find container disk mounted directories for vmi without uid")
	}

	// XXX: backward compatibility for old unresolved paths, can be removed in July 2023
	// After a one-time convert and persist, old records are safe too.
	record.UsesSafePaths = true

	recordFile := filepath.Join(m.mountStateDir, string(vmi.UID))

	m.mountRecordsLock.Lock()
	defer m.mountRecordsLock.Unlock()

	bytes, err := json.Marshal(record)
	if err != nil {
		return err
	}

	err = os.MkdirAll(filepath.Dir(recordFile), 0750)
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(recordFile, bytes, 0600)
	if err != nil {
		return err
	}
	m.mountRecords[vmi.UID] = record
	return nil
}
