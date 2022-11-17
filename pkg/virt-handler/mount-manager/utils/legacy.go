package recorder

import (
	"os"
	"path/filepath"

	"github.com/pkg/errors"

	"kubevirt.io/kubevirt/pkg/safepath"
)

// Convert legacy recording into new version
// Old way uses a directory per volumes/disk type:
// /var/run/kubevirt-private/
//   |____ container-disk-mount-state/
//   |          |________________ 6856c1b7-fb20-4b05-aeab-b23f211cfa76
//   |          |________________ df2ca0e1-f48e-48d5-9ba6-50634742c9e3
//   |
//   |____ hotplug-volume-mount-state
//              |________________ 6856c1b7-fb20-4b05-aeab-b23f211cfa76
//
// New way uses a single file per VM:
// /var/run/kubevirt-private/
//   |______ 6856c1b7-fb20-4b05-aeab-b23f211cfa76
//   |______ df2ca0e1-f48e-48d5-9ba6-50634742c9e3

const (
	ContainerDiskMountStates     = "container-disk-mount-state"
	HotpluggedVolumesMountStates = "hotplug-volume-mount-state"
)

type LegacyVmiContainerDisksMountTargetEntry struct {
	TargetFile string `json:"targetFile"`
	SocketFile string `json:"socketFile"`
}

type LegacyVmiContainerDisksMountTargetRecord struct {
	MountTargetEntries []LegacyVmiContainerDisksMountTargetEntry `json:"mountTargetEntries"`
	UsesSafePaths      bool                                      `json:"usesSafePaths"`
}

type LegacyVmiHotpluggedVolumesMountTargetEntry struct {
	TargetFile string `json:"targetFile"`
}

type LegacyVmiVmiHotpluggedVolumesMountTargetRecord struct {
	MountTargetEntries []LegacyVmiHotpluggedVolumesMountTargetEntry `json:"mountTargetEntries"`
	UsesSafePaths      bool                                         `json:"usesSafePaths"`
}

type LegacyRecordEntriesConverter struct {
	mountStateDir string
}

func NewLegacyRecordEntriesConverthotpluggedVolumesMountStateser(path string) *LegacyRecordEntriesConverter {
	return &LegacyRecordEntriesConverter{
		mountStateDir: path,
	}
}

func wrapErrors(e1, e2 error) error {
	if e1 == nil {
		return e2
	}
	return errors.Wrap(e1, e2.Error())
}

func convertContainerDisksMountEntry(path *safepath.Path) ([]MountTargetEntry, error) {
	var entry []MountTargetEntry
	f, err := safepath.OpenAtNoFollow(path)
	if err != nil {
		return []MountTargetEntry{}, err
	}
	defer f.Close()
	file := f.SafePath()
	// Read the legacy json
	record := LegacyVmiContainerDisksMountTargetRecord{}
	if err := convert(file, &record); err != nil {
		return []MountTargetEntry{}, err
	}
	for _, l := range record.MountTargetEntries {
		entry = append(entry, MountTargetEntry{
			TargetFile: l.TargetFile,
			SocketFile: l.SocketFile,
		})
	}
	return entry, nil
}

func convertHotpluggedVolumesMountEntry(path *safepath.Path) ([]MountTargetEntry, error) {
	var entry []MountTargetEntry
	f, err := safepath.OpenAtNoFollow(path)
	if err != nil {
		return []MountTargetEntry{}, err
	}
	defer f.Close()
	file := f.SafePath()
	// Read the legacy json
	record := LegacyVmiVmiHotpluggedVolumesMountTargetRecord{}
	if err := convert(file, &record); err != nil {
		return []MountTargetEntry{}, err
	}
	for _, l := range record.MountTargetEntries {
		entry = append(entry, MountTargetEntry{
			TargetFile: l.TargetFile,
			SocketFile: "",
		})
	}
	return entry, nil
}

func (l *LegacyRecordEntriesConverter) ConvertLegacyRecordEntriesFile() error {
	vmiEntries := make(map[string]*VMIMountTargetRecord)
	var parsingError error
	// Check container disks
	cdPath, errCd := safepath.JoinAndResolveWithRelativeRoot("/", l.mountStateDir, ContainerDiskMountStates)
	if errCd == nil {
		entries, err := safepath.ReadDirNoFollow(cdPath)
		if err != nil {
			wrapErrors(parsingError, err)
		}
		for _, entry := range entries {
			vmiUID := entry.Name()
			pathEntry, err := cdPath.AppendAndResolveWithRelativeRoot(vmiUID)
			if err != nil {
				wrapErrors(parsingError, err)
				continue
			}
			v, ok := vmiEntries[vmiUID]
			if !ok {
				v = &VMIMountTargetRecord{}
			}
			entry, err := convertContainerDisksMountEntry(pathEntry)
			if err != nil {
				wrapErrors(parsingError, err)
				continue
			}
			v.MountTargetEntries.ContainerDisks = entry
			vmiEntries[vmiUID] = v
		}
	}
	//	// Check hotplugged volumes
	hpPath, errHp := safepath.JoinAndResolveWithRelativeRoot("/", l.mountStateDir, HotpluggedVolumesMountStates)
	if errHp != nil {
		wrapErrors(parsingError, errHp)
	}

	// Write each VMI entry in the new file format
	for vmiUID, entry := range vmiEntries {
		if err := writeVMIMountTargetRecordToFile(filepath.Join(l.mountStateDir, vmiUID), entry); err != nil {
			wrapErrors(parsingError, err)
			continue
		}
		// Remove the legacy file
		os.Remove(filepath.Join(l.mountStateDir, ContainerDiskMountStates, vmiUID))
		os.Remove(filepath.Join(l.mountStateDir, HotpluggedVolumesMountStates, vmiUID))

	}

	// Delete the the directory for the legacy file format if empty
	entries, err := safepath.ReadDirNoFollow(cdPath)
	if err != nil {
		wrapErrors(parsingError, err)
	}
	if len(entries) == 0 {
		os.Remove(filepath.Join(l.mountStateDir, ContainerDiskMountStates))
	}
	entries, err = safepath.ReadDirNoFollow(hpPath)
	if err != nil {
		wrapErrors(parsingError, err)
	}
	if len(entries) == 0 {
		os.Remove(filepath.Join(l.mountStateDir, ContainerDiskMountStates))
	}
	return parsingError
}
