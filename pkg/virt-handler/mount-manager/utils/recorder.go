package recorder

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/types"
	v1 "kubevirt.io/api/core/v1"

	diskutils "kubevirt.io/kubevirt/pkg/ephemeral-disk-utils"
	"kubevirt.io/kubevirt/pkg/safepath"
	"kubevirt.io/kubevirt/pkg/unsafepath"
)

//go:generate mockgen -source $GOFILE -package=$GOPACKAGE -destination=generated_mock_$GOFILE
type MountTargetEntry struct {
	TargetFile string `json:"targetFile"`
	SocketFile string `json:"socketFile,omitempty"`
}

type VMIMountTargetRecord struct {
	HotpluggedVolumes []MountTargetEntry `json:"hotpluggedDisks"`
	ContainerDisks    []MountTargetEntry `json:"containerDisks"`
	UsesSafePaths     bool               `json:"usesSafePaths"`
}

func (r *VMIMountTargetRecord) GetContainerDisks() []MountTargetEntry {
	return r.ContainerDisks
}

func (r *VMIMountTargetRecord) GetHotpluggedVolumes() []MountTargetEntry {
	return r.HotpluggedVolumes
}

func (record *VMIMountTargetRecord) IsEmpty() bool {
	return len(record.ContainerDisks) == 0 && len(record.HotpluggedVolumes) == 0
}

const (
	containerDiskMountStates     = "container-disk-mount-state"
	hotpluggedVolumesMountStates = "hotplug-volume-mount-state"
)

type vmiMountTargetRecordForCache struct {
	MountTargetEntries []MountTargetEntry `json:"mountTargetEntries"`
	UsesSafePaths      bool               `json:"usesSafePaths"`
}

func readRecordFile(recordFile string) ([]MountTargetEntry, bool, error) {
	record := vmiMountTargetRecordForCache{}
	// #nosec No risk for path injection. Using static base and cleaned filename
	bytes, err := os.ReadFile(recordFile)
	if err != nil {
		return []MountTargetEntry{}, false, err
	}
	err = json.Unmarshal(bytes, &record)
	if err != nil {
		return []MountTargetEntry{}, false, err
	}
	return record.MountTargetEntries, record.UsesSafePaths, nil
}

func (m *mounter) readRecordFiles(uid string) (*VMIMountTargetRecord, bool, error) {
	record := &VMIMountTargetRecord{}
	var useSafepathsCd, useSafepathsHp bool
	// Read the container disks entries from the filesystem
	// if not there, see if record is on disk, this can happen if virt-handler restarts
	recordFile := filepath.Join(m.mountStateDir, containerDiskMountStates, filepath.Clean(uid))
	existsCds, err := diskutils.FileExists(recordFile)
	if err != nil {
		return nil, false, err
	}
	if existsCds {
		record.ContainerDisks, useSafepathsCd, err = readRecordFile(recordFile)
		if err != nil {
			return nil, false, err
		}
	}

	// Read hotplugged volumes entries from the filesystem
	recordFile = filepath.Join(m.mountStateDir, hotpluggedVolumesMountStates, filepath.Clean(uid))
	if err != nil {
		return nil, false, err
	}
	existsHps, err := diskutils.FileExists(recordFile)
	if err != nil {
		return nil, false, err
	}
	if existsHps {
		record.HotpluggedVolumes, useSafepathsHp, err = readRecordFile(recordFile)
		if err != nil {
			return nil, false, err
		}
	}
	record.UsesSafePaths = useSafepathsHp && useSafepathsCd
	return record, existsCds || existsHps, nil
}

func writeRecordFile(recordFile string, record []MountTargetEntry) error {
	r := vmiMountTargetRecordForCache{
		MountTargetEntries: record,
		// XXX: backward compatibility for old unresolved paths, can be removed in July 2023
		// After a one-time convert and persist, old records are safe too.
		UsesSafePaths: true,
	}
	bytes, err := json.Marshal(r)
	if err != nil {
		return err
	}

	err = os.MkdirAll(filepath.Dir(recordFile), 0750)
	if err != nil {
		return err
	}

	return os.WriteFile(recordFile, bytes, 0600)
}

func (m *mounter) writeRecordFiles(uid string, record *VMIMountTargetRecord) error {
	errCd := writeRecordFile(filepath.Join(m.mountStateDir, containerDiskMountStates, uid), record.ContainerDisks)
	errHp := writeRecordFile(filepath.Join(m.mountStateDir, hotpluggedVolumesMountStates, uid), record.HotpluggedVolumes)
	if errCd != nil || errHp != nil {
		if errCd != nil {
			return errors.Wrap(errCd, errHp.Error())
		}
		return errHp
	}
	return nil
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
	SetAddMountRecordContainerDisk(vmi *v1.VirtualMachineInstance, cdRecord []MountTargetEntry, addPreviousRules bool) error
	DeleteContainerDisksMountRecord(vmi *v1.VirtualMachineInstance) error
	GetContainerDisksMountRecord(vmi *v1.VirtualMachineInstance) ([]MountTargetEntry, error)
	SetMountRecordHotpluggedVolumes(vmi *v1.VirtualMachineInstance, hpRecord []MountTargetEntry) error
	GetHotpluggedVolumesMountRecord(vmi *v1.VirtualMachineInstance) ([]MountTargetEntry, error)
	DeleteHotpluggedVolumesMountRecord(vmi *v1.VirtualMachineInstance) error
}

func NewMountRecorder(mountStateDir string) MountRecorder {
	return &mounter{
		mountStateDir: mountStateDir,
		mountRecords:  make(map[types.UID]*VMIMountTargetRecord),
	}
}

func (m *mounter) SetAddMountRecordContainerDisk(vmi *v1.VirtualMachineInstance, cdRecord []MountTargetEntry, addPreviousRules bool) error {
	record, err := m.getMountTargetRecord(vmi)
	if err != nil {
		return err
	}

	if record == nil {
		// Initialize it
		record = &VMIMountTargetRecord{}
	}
	if addPreviousRules {
		record.ContainerDisks = append(record.ContainerDisks, cdRecord...)
	} else {
		record.ContainerDisks = cdRecord
	}

	return m.setMountTargetRecord(vmi, record)
}

func (m *mounter) SetMountRecordHotpluggedVolumes(vmi *v1.VirtualMachineInstance, hpRecord []MountTargetEntry) error {
	record, err := m.getMountTargetRecord(vmi)
	if err != nil {
		return err
	}

	if record == nil {
		// Initialize it
		record = &VMIMountTargetRecord{}
	}
	record.HotpluggedVolumes = hpRecord

	return m.setMountTargetRecord(vmi, record)
}

func (m *mounter) DeleteContainerDisksMountRecord(vmi *v1.VirtualMachineInstance) error {
	return m.deleteMountTargetRecord(vmi, CONTAINERDISKS_ENTRY)
}

func (m *mounter) DeleteHotpluggedVolumesMountRecord(vmi *v1.VirtualMachineInstance) error {
	return m.deleteMountTargetRecord(vmi, HOTPLUGGEDVOLUMES_ENTRY)
}

func (m *mounter) GetContainerDisksMountRecord(vmi *v1.VirtualMachineInstance) ([]MountTargetEntry, error) {
	record, err := m.getMountTargetRecord(vmi)
	if err != nil {
		return []MountTargetEntry{}, err
	}
	if record == nil {
		return []MountTargetEntry{}, nil
	}
	return record.GetContainerDisks(), nil
}

func (m *mounter) GetHotpluggedVolumesMountRecord(vmi *v1.VirtualMachineInstance) ([]MountTargetEntry, error) {
	record, err := m.getMountTargetRecord(vmi)
	if err != nil {
		return []MountTargetEntry{}, err
	}
	if record == nil {
		return []MountTargetEntry{}, nil
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
		for _, target := range record.ContainerDisks {
			if entry != CONTAINERDISKS_ENTRY && entry != ALL_ENTRIES {
				break
			}
			os.Remove(target.TargetFile)
			os.Remove(target.SocketFile)
			if ok {
				r.ContainerDisks = []MountTargetEntry{}
			}
		}
		for _, target := range record.HotpluggedVolumes {
			if entry != HOTPLUGGEDVOLUMES_ENTRY && entry != ALL_ENTRIES {
				break
			}
			os.Remove(target.TargetFile)
			if ok {
				r.HotpluggedVolumes = []MountTargetEntry{}
			}
		}
		if r.IsEmpty() {
			os.Remove(recordFile)
			m.mountRecordsLock.Lock()
			defer m.mountRecordsLock.Unlock()
			delete(m.mountRecords, vmi.UID)
		} else {
			m.mountRecords[vmi.UID] = r
			m.setMountTargetRecord(vmi, r)
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

	record, exists, err := m.readRecordFiles(string(vmi.UID))
	if err != nil {
		return nil, err
	}
	if exists {
		// XXX: backward compatibility for old unresolved paths, can be removed in July 2023
		// After a one-time convert and persist, old records are safe too.
		if !record.UsesSafePaths {
			record.UsesSafePaths = true
			for i, entry := range record.ContainerDisks {
				safePath, err := safepath.JoinAndResolveWithRelativeRoot("/", entry.TargetFile)
				if err != nil {
					return nil, fmt.Errorf("failed converting legacy path to safepath: %v", err)
				}
				record.ContainerDisks[i].TargetFile = unsafepath.UnsafeAbsolute(safePath.Raw())
			}
			for i, entry := range record.HotpluggedVolumes {
				safePath, err := safepath.JoinAndResolveWithRelativeRoot("/", entry.TargetFile)
				if err != nil {
					return nil, fmt.Errorf("failed converting legacy path to safepath: %v", err)
				}
				record.HotpluggedVolumes[i].TargetFile = unsafepath.UnsafeAbsolute(safePath.Raw())
			}
		}

		m.mountRecords[vmi.UID] = record
		return record, nil
	}

	// not found
	return nil, nil
}

func (m *mounter) setMountTargetRecord(vmi *v1.VirtualMachineInstance, record *VMIMountTargetRecord) error {
	if string(vmi.UID) == "" {
		return fmt.Errorf("unable to find mounted directories for vmi without uid")
	}
	m.mountRecordsLock.Lock()
	defer m.mountRecordsLock.Unlock()

	if err := m.writeRecordFiles(string(vmi.UID), record); err != nil {
		return err
	}

	m.mountRecords[vmi.UID] = record
	return nil
}
