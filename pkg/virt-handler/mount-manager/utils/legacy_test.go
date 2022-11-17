package recorder_test

import (
	"encoding/json"
	"io/ioutil"
	"path/filepath"

	mountutils "kubevirt.io/kubevirt/pkg/virt-handler/mount-manager/utils"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type mountTargetEntryTests struct {
	legacyHp []mountutils.LegacyVmiVmiHotpluggedVolumesMountTargetRecord
	legacyCd []mountutils.LegacyVmiContainerDisksMountTargetEntry
	expect   mountutils.VMIMountTargetEntry
}

var _ = Describe("Legacy", func() {
	var (
		tempDir   string
		converter mountutils.LegacyRecordEntriesConverter
	)

	const vmiUID = "abcdefg"
	BeforeEach(func() {
		tempDir = GinkgoT().TempDir()
		converter = mountutils.NewLegacyRecordEntriesConverthotpluggedVolumesMountStateser(tempDir)
	})

	createLegacyFileEntriesWithHotpluggedVolumes := func(dir, uid string, hp *mountutils.LegacyVmiVmiHotpluggedVolumesMountTargetRecord) {
		b, err := json.Marshal(hp)
		Expect(err).ToNot(HaveOccurred())
		ioutil.WriteFile(filepath.Join(tempDir, mountutils.HotpluggedVolumesMountStates, uid), b, 0644)
	}

	createLegacyFileEntriesWithContainerDisks := func(dir, uid string, cd *mountutils.LegacyVmiContainerDisksMountTargetEntry) {
		b, err := json.Marshal(cd)
		Expect(err).ToNot(HaveOccurred())
		ioutil.WriteFile(filepath.Join(tempDir, mountutils.ContainerDiskMountStates, uid), b, 0644)
	}

	verifyConvertedFile := func(dir, uid string, testMountEntries *mountTargetEntryTests) {
		var record mountutils.VMIMountTargetEntry
		bytes, err := os.ReadFile(filepath.Join(tempDir, uid))
		Expect(err).ToNot(HaveOccurred())
		err = json.Unmarshal(bytes, &record)
		Expect(err).ToNot(HaveOccurred())
		Expect(cmp.Equal(testMountEntries.expect, record)).To(BeTrue())

		// Check if legacy files don't exist anymore
		_, err = os.Stats(filepath.Join(tempDir, mountutils.HotpluggedVolumesMountStates, vmiUID))
		Expect(err).Should(Equal(errors.ErrNotExist))
		_, err = os.Stats(filepath.Join(tempDir, mountutils.ContainerDiskMountStates, vmiUID))
		Expect(err).Should(Equal(errors.ErrNotExist))
	}
	FContext("Converting record recording file", func() {
		It("Should succeed with an emptry dir", func() {
			err := converter.ConvertLegacyRecordEntriesFile()
			Expect(err).ToNot(HaveOccurred())
		})
		It("Should succesfully convert the legacy record file with only container disks", func() {
			testMountEntries := mountTargetEntryTests{
				legacyCd: []mountutils.LegacyVmiContainerDisksMountTargetEntry{
					{
						{TargetFile: "test", SocketFile: "socket"},
					},
					UsesSafePaths: true,
				},
				expect: mountutils.VMIMountTargetEntry{
					MountTargetEntries: VMIMountTargetEntry{
						ContainerDisks: []MountTargetEntry{
							{TargetFile: "test", SocketFile: "socket"},
						},
						UsesSafePaths: true,
					},
				},
			}
			createLegacyFileEntriesWithContainerDisks(tempDir, vmiUID, testMountEntries.legacyCd)
			err := converter.ConvertLegacyRecordEntriesFile()
			Expect(err).ToNot(HaveOccurred())
			verifyConvertedFile(tempDir, vmiUID, testMountEntries.expect)
		})
		It("Should succesfully convert the legacy record file with only hotplugged disks", func() {
			testMountEntries := mountTargetEntryTests{
				legacyCd: []mountutils.LegacyVmiContainerDisksMountTargetEntry{
					{
						{TargetFile: "test", SocketFile: "socket"},
					},
					UsesSafePaths: true,
				},
				legacyHp: []mountutils.LegacyVmiVmiHotpluggedVolumesMountTargetRecord{
					{
						{TargetFile: "test"},
					},
					UsesSafePaths: true,
				},
				expect: mountutils.VMIMountTargetEntry{
					MountTargetEntries: VMIMountTargetEntry{
						HotpluggedVolumes: []MountTargetEntry{
							{TargetFile: "test", SocketFile: "socket"},
						},
						UsesSafePaths: true,
					},
				},
			}
			createLegacyFileEntriesWithHotpluggedVolumes(tempDir, vmiUID, testMountEntries.legacyHp)
			err := converter.ConvertLegacyRecordEntriesFile()
			Expect(err).ToNot(HaveOccurred())
			verifyConvertedFile(tempDir, vmiUID, testMountEntries.expect)
		})
		It("Should succesfully convert the legacy record file with container and hotplugged disks", func() {
			testMountEntries := mountTargetEntryTests{
				legacyHp: []mountutils.LegacyVmiVmiHotpluggedVolumesMountTargetRecord{
					{
						{TargetFile: "test"},
					},
					UsesSafePaths: true,
				},
				expect: mountutils.VMIMountTargetEntry{
					MountTargetEntries: VMIMountTargetEntry{
						ContainerDisks: []MountTargetEntry{
							{TargetFile: "test", SocketFile: "socket"},
						},
						HotpluggedVolumes: []MountTargetEntry{
							{TargetFile: "test", SocketFile: "socket"},
						},
						UsesSafePaths: true,
					},
				},
			}
			createLegacyFileEntriesWithHotpluggedVolumes(tempDir, vmiUID, testMountEntries.legacyHp)
			err := converter.ConvertLegacyRecordEntriesFile()
			Expect(err).ToNot(HaveOccurred())
			verifyConvertedFile(tempDir, vmiUID, testMountEntries.expect)
		})
	})
})
