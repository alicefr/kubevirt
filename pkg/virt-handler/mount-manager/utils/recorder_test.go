package recorder_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/equality"
	v1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/api"

	mountutils "kubevirt.io/kubevirt/pkg/virt-handler/mount-manager/utils"
)

var _ = Describe("Recorder", func() {
	var (
		m       mountutils.MountRecorder
		vmi     *v1.VirtualMachineInstance
		tempDir string
	)

	writeRecordOnFile := func(uid string, record *mountutils.VMIMountTargetRecord) {
		r, err := json.Marshal(record)
		Expect(err).ToNot(HaveOccurred())
		err = os.WriteFile(filepath.Join(tempDir, uid), r, 0644)
		Expect(err).ToNot(HaveOccurred())
	}

	readRecordOnFile := func(uid string) *mountutils.VMIMountTargetRecord {
		var record mountutils.VMIMountTargetRecord
		data, err := os.ReadFile(filepath.Join(tempDir, uid))
		Expect(err).ToNot(HaveOccurred())
		err = json.Unmarshal(data, &record)
		Expect(err).ToNot(HaveOccurred())
		return &record
	}

	Context("From cache", func() {
		var (
			record *mountutils.VMIMountTargetRecord
		)

		BeforeEach(func() {
			tempDir = GinkgoT().TempDir()
			m = mountutils.NewMountRecorder(tempDir)
			vmi = api.NewMinimalVMI("fake-vmi")
			vmi.UID = "1234"
			record = &mountutils.VMIMountTargetRecord{
				MountTargetEntries: mountutils.VMIMountTargetEntry{
					HotpluggedVolumes: []mountutils.MountTargetEntry{
						{
							TargetFile: "/hp/target0",
						},
						{
							TargetFile: "/hp/target1",
						},
					},
					ContainerDisks: []mountutils.MountTargetEntry{
						{
							TargetFile: "/cd/target0",
							SocketFile: "/cd/sock0",
						},
						{
							TargetFile: "/cd/target1",
							SocketFile: "/cd/sock1",
						},
					},
				},
				UsesSafePaths: true,
			}
		})
		It("GetHotpluggedVolumesMountRecord from cache", func() {
			writeRecordOnFile(string(vmi.UID), record)
			res, err := m.GetHotpluggedVolumesMountRecord(vmi)
			Expect(err).ToNot(HaveOccurred())
			Expect(equality.Semantic.DeepEqual(res, record.MountTargetEntries.HotpluggedVolumes)).To(BeTrue())
		})

		It("GetContainerDisksMountRecord from cache", func() {
			writeRecordOnFile(string(vmi.UID), record)
			res, err := m.GetContainerDisksMountRecord(vmi)
			Expect(err).ToNot(HaveOccurred())
			Expect(equality.Semantic.DeepEqual(res, record.MountTargetEntries.ContainerDisks)).To(BeTrue())
		})

		DescribeTable("SetAddMountRecordContainerDisk", func(existingCachedValues, addPreviousRules bool) {
			expectedRecord := &mountutils.VMIMountTargetRecord{UsesSafePaths: true}
			if existingCachedValues {
				writeRecordOnFile(string(vmi.UID), record)
				expectedRecord = record
			}
			newRecord := []mountutils.MountTargetEntry{
				{
					TargetFile: "/cd/target2",
					SocketFile: "/cd/sock2",
				},
			}
			err := m.SetAddMountRecordContainerDisk(vmi, newRecord, addPreviousRules)
			Expect(err).ToNot(HaveOccurred())
			if addPreviousRules && existingCachedValues {
				expectedRecord.MountTargetEntries.ContainerDisks = append(record.MountTargetEntries.ContainerDisks, newRecord...)
			} else {
				expectedRecord.MountTargetEntries.ContainerDisks = newRecord
			}
			res := readRecordOnFile(string(vmi.UID))
			if !equality.Semantic.DeepEqual(expectedRecord, res) {
				ginkgo.Fail(fmt.Sprintf("expectedRecord %v not equal to %v", *expectedRecord, *res))
			}
		},
			Entry("no cached values and no adding to previous rule", false, false),
			Entry("cached values and no adding to previous rule", true, false),
			Entry("no cached values and adding to previous rule", false, true),
			Entry("cached values and adding to previous rule", true, true),
		)

		DescribeTable("SetMountRecordHotpluggedVolumes", func(existingCachedValues bool) {
			expectedRecord := &mountutils.VMIMountTargetRecord{UsesSafePaths: true}
			if existingCachedValues {
				writeRecordOnFile(string(vmi.UID), record)
				expectedRecord = record
			}
			newRecord := []mountutils.MountTargetEntry{
				{
					TargetFile: "/hp/target2",
				},
			}
			err := m.SetMountRecordHotpluggedVolumes(vmi, newRecord)
			Expect(err).ToNot(HaveOccurred())
			expectedRecord.MountTargetEntries.HotpluggedVolumes = newRecord
			res := readRecordOnFile(string(vmi.UID))
			if !equality.Semantic.DeepEqual(expectedRecord, res) {
				ginkgo.Fail(fmt.Sprintf("expectedRecord %v not equal to %v", *expectedRecord, *res))
			}
		},
			Entry("no cached values", false),
			Entry("cached values", true),
		)
	})

	Context("Clean-up", func() {
		BeforeEach(func() {
			tempDir = GinkgoT().TempDir()
			m = mountutils.NewMountRecorder(tempDir)
			vmi = api.NewMinimalVMI("fake-vmi")
			vmi.UID = "1234"
		})

		createContainerDisksTargetFiles := func(cd []mountutils.MountTargetEntry) {
			for _, entry := range cd {
				file, err := os.Create(entry.TargetFile)
				Expect(err).ToNot(HaveOccurred())
				defer file.Close()
				file, err = os.Create(entry.SocketFile)
				Expect(err).ToNot(HaveOccurred())
				defer file.Close()
			}
		}

		AreContainerDisksTargetFilesDeleted := func(cd []mountutils.MountTargetEntry) bool {
			for _, entry := range cd {
				if _, err := os.Stat(entry.TargetFile); err == nil || !errors.Is(err, os.ErrNotExist) {
					return false
				}
				if _, err := os.Stat(entry.SocketFile); err == nil || !errors.Is(err, os.ErrNotExist) {
					return false
				}
			}
			return true
		}

		fileNotExist := func(file string) bool {
			_, err := os.Stat(file)
			return errors.Is(err, os.ErrNotExist)
		}

		createHotpluggedVolumesTargetFiles := func(hp []mountutils.MountTargetEntry) {
			for _, entry := range hp {
				file, err := os.Create(entry.TargetFile)
				Expect(err).ToNot(HaveOccurred())
				defer file.Close()
			}
		}

		areHotpluggedVolumesTargetFilesDeleted := func(hp []mountutils.MountTargetEntry) bool {
			for _, entry := range hp {
				if _, err := os.Stat(entry.TargetFile); err == nil || !errors.Is(err, os.ErrNotExist) {
					return false
				}
			}
			return true
		}

		It("Should delete container disks and record entry", func() {
			cds := []mountutils.MountTargetEntry{
				{
					TargetFile: filepath.Join(tempDir, "target0"),
					SocketFile: filepath.Join(tempDir, "socket0"),
				},
				{
					TargetFile: filepath.Join(tempDir, "target1"),
					SocketFile: filepath.Join(tempDir, "socket1"),
				},
			}
			createContainerDisksTargetFiles(cds)
			m.SetAddMountRecordContainerDisk(vmi, cds, false)

			err := m.DeleteContainerDisksMountRecord(vmi)
			Expect(err).ToNot(HaveOccurred())

			// Check clean-up
			res, err := m.GetContainerDisksMountRecord(vmi)
			Expect(err).ToNot(HaveOccurred())
			Expect(len(res) == 0).To(BeTrue())
			Expect(AreContainerDisksTargetFilesDeleted(cds)).To(BeTrue())
			// No futher record the record file should be deleted
			Expect(fileNotExist(filepath.Join(tempDir, string(vmi.UID)))).To(BeTrue())
		})

		It("Should delete container disks but keep the hotplugged volumes record", func() {
			cds := []mountutils.MountTargetEntry{
				{
					TargetFile: filepath.Join(tempDir, "cd-target0"),
					SocketFile: filepath.Join(tempDir, "cd-socket0"),
				},
				{
					TargetFile: filepath.Join(tempDir, "cd-target1"),
					SocketFile: filepath.Join(tempDir, "cd-socket1"),
				},
			}
			hps := []mountutils.MountTargetEntry{
				{
					TargetFile: filepath.Join(tempDir, "hp-target0"),
				},
				{
					TargetFile: filepath.Join(tempDir, "hp-target1"),
				},
			}
			createContainerDisksTargetFiles(cds)
			err := m.SetAddMountRecordContainerDisk(vmi, cds, false)
			Expect(err).ToNot(HaveOccurred())
			err = m.SetMountRecordHotpluggedVolumes(vmi, hps)
			Expect(err).ToNot(HaveOccurred())

			err = m.DeleteContainerDisksMountRecord(vmi)
			Expect(err).ToNot(HaveOccurred())

			// Check clean-up
			res, err := m.GetContainerDisksMountRecord(vmi)
			Expect(err).ToNot(HaveOccurred())
			Expect(len(res) == 0).To(BeTrue())
			Expect(AreContainerDisksTargetFilesDeleted(cds)).To(BeTrue())
			// Check if the hotplug volumes record is still there
			resHps, err := m.GetHotpluggedVolumesMountRecord(vmi)
			Expect(err).ToNot(HaveOccurred())
			if !equality.Semantic.DeepEqual(hps, resHps) {
				ginkgo.Fail(fmt.Sprintf("expectedRecord %v not equal to %v", hps, resHps))
			}
		})

		It("Should delete hotplugged volumes and record entry", func() {
			hps := []mountutils.MountTargetEntry{
				{
					TargetFile: filepath.Join(tempDir, "hp-target0"),
				},
				{
					TargetFile: filepath.Join(tempDir, "hp-target1"),
				},
			}
			createHotpluggedVolumesTargetFiles(hps)
			err := m.SetMountRecordHotpluggedVolumes(vmi, hps)
			Expect(err).ToNot(HaveOccurred())

			err = m.DeleteHotpluggedVolumesMountRecord(vmi)
			Expect(err).ToNot(HaveOccurred())

			// Check clean-up
			res, err := m.GetHotpluggedVolumesMountRecord(vmi)
			Expect(err).ToNot(HaveOccurred())
			Expect(len(res) == 0).To(BeTrue())
			Expect(areHotpluggedVolumesTargetFilesDeleted(hps)).To(BeTrue())
			// No futher record the record file should be deleted
			Expect(fileNotExist(filepath.Join(tempDir, string(vmi.UID)))).To(BeTrue())
		})

		It("Should delete container disks but keep the hotplugged volumes record", func() {
			cds := []mountutils.MountTargetEntry{
				{
					TargetFile: filepath.Join(tempDir, "cd-target0"),
					SocketFile: filepath.Join(tempDir, "cd-socket0"),
				},
				{
					TargetFile: filepath.Join(tempDir, "cd-target1"),
					SocketFile: filepath.Join(tempDir, "cd-socket1"),
				},
			}
			hps := []mountutils.MountTargetEntry{
				{
					TargetFile: filepath.Join(tempDir, "hp-target0"),
				},
				{
					TargetFile: filepath.Join(tempDir, "hp-target1"),
				},
			}
			createHotpluggedVolumesTargetFiles(hps)
			err := m.SetAddMountRecordContainerDisk(vmi, cds, false)
			Expect(err).ToNot(HaveOccurred())
			err = m.SetMountRecordHotpluggedVolumes(vmi, hps)
			Expect(err).ToNot(HaveOccurred())

			err = m.DeleteHotpluggedVolumesMountRecord(vmi)
			Expect(err).ToNot(HaveOccurred())

			// Check clean-up
			res, err := m.GetHotpluggedVolumesMountRecord(vmi)
			Expect(err).ToNot(HaveOccurred())
			Expect(len(res) == 0).To(BeTrue())
			Expect(areHotpluggedVolumesTargetFilesDeleted(hps)).To(BeTrue())

			// Check if the container disks record is still there
			resCds, err := m.GetContainerDisksMountRecord(vmi)
			Expect(err).ToNot(HaveOccurred())
			if !equality.Semantic.DeepEqual(cds, resCds) {
				ginkgo.Fail(fmt.Sprintf("expectedRecord %v not equal to %v", cds, resCds))
			}
		})
	})

	Context("Hoplugged volume entries", func() {
		BeforeEach(func() {
			tempDir = GinkgoT().TempDir()
			m = mountutils.NewMountRecorder(tempDir)
			vmi = api.NewMinimalVMI("fake-vmi")
			vmi.UID = "1234"
		})

		It("should add, remove and re-add an hotplugged volume entry", func() {
			newRecord := []mountutils.MountTargetEntry{
				{
					TargetFile: "/hp/target1",
				},
			}
			expectedRecord := &mountutils.VMIMountTargetRecord{UsesSafePaths: true,
				MountTargetEntries: mountutils.VMIMountTargetEntry{
					HotpluggedVolumes: newRecord,
				},
			}

			// Add
			err := m.SetMountRecordHotpluggedVolumes(vmi, newRecord)
			Expect(err).ToNot(HaveOccurred())
			res, err := m.GetHotpluggedVolumesMountRecord(vmi)
			Expect(err).ToNot(HaveOccurred())
			Expect(equality.Semantic.DeepEqual(res, expectedRecord.MountTargetEntries.HotpluggedVolumes)).To(BeTrue())

			// Remove
			err = m.DeleteHotpluggedVolumesMountRecord(vmi)
			Expect(err).ToNot(HaveOccurred())

			// Re-add
			err = m.SetMountRecordHotpluggedVolumes(vmi, newRecord)
			Expect(err).ToNot(HaveOccurred())
			res, err = m.GetHotpluggedVolumesMountRecord(vmi)
			Expect(err).ToNot(HaveOccurred())
			Expect(equality.Semantic.DeepEqual(res, expectedRecord.MountTargetEntries.HotpluggedVolumes)).To(BeTrue())

		})
	})

})
