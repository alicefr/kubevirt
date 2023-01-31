package storage

import (
	"context"
	"fmt"
	"strings"
	"time"

	expect "github.com/google/goexpect"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/kubecli"

	"kubevirt.io/kubevirt/tests"
	"kubevirt.io/kubevirt/tests/console"
	"kubevirt.io/kubevirt/tests/exec"
	"kubevirt.io/kubevirt/tests/libstorage"
	"kubevirt.io/kubevirt/tests/libvmi"
	"kubevirt.io/kubevirt/tests/libwait"
	"kubevirt.io/kubevirt/tests/testsuite"
	"kubevirt.io/kubevirt/tests/util"
)

var _ = SIGDescribe("SCSI persistent reservation", func() {
	const (
		naa          = "50014051998a423d"
		backendDisk  = "disk0"
		disks        = "disks"
		targetCliPod = "targetcli"
	)
	var (
		virtClient kubecli.KubevirtClient
		node       string
		device     string
	)

	// executeTargetCli executes command targetcli
	executeTargetCli := func(args []string) {
		cmd := append([]string{"/usr/bin/targetcli"}, args...)
		pod, err := virtClient.CoreV1().Pods(testsuite.NamespacePrivileged).Get(context.Background(), targetCliPod, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())

		// Create SCSI disk using targetcli
		stdout, stderr, err := exec.ExecuteCommandOnPodWithResults(virtClient, pod, "targetcli", cmd)
		By(fmt.Sprintf("targetcli: stdout: %v stderr: %v", stdout, stderr))
		Expect(err).ToNot(HaveOccurred())

	}

	// createSCSIDisk creates a SCSI using targetcli utility and LinuxIO (see
	// http://linux-iscsi.org/wiki/LIO).
	// For avoiding any confusion, this function doesn't rely on the scsi_debug module
	// as creates a SCSI disk that supports the SCSI protocol. Hence, it can be used to test  // SCSI commands such as the persistent reservation
	createSCSIDisk := func() {
		diskSize := "1G"
		// Create PVC where we store the backend storage for the SCSI disks
		libstorage.CreateFSPVC(disks, testsuite.NamespacePrivileged, diskSize)
		// Create targetcli conainer
		By("Create targetcli pod")
		pod := tests.RunPodInNamespace(tests.RenderTargetcliPod(targetCliPod, disks), testsuite.NamespacePrivileged)
		//pod := tests.CreatePodAndWaitUntil(tests.RenderTargetcliPod(targetCliPod, disks), corev1.PodRunning)
		node = pod.Spec.NodeName
		// Create backend file
		executeTargetCli([]string{
			"backstores/fileio",
			"create", backendDisk, "/disks/disk.img", "1G"})
		executeTargetCli([]string{
			"loopback/", "create", naa})
		// Create LUN
		executeTargetCli([]string{
			fmt.Sprintf("loopback/naa.%s/luns", naa),
			"create",
			fmt.Sprintf("/backstores/fileio/%s", backendDisk)})
	}

	// findSCSIdisk returns the first scsi disk that correspond to the model. With targetcli the model name correspond to the name of the storage backend.
	// Example:
	// $ lsblk --scsi -o NAME,MODEL -p -n
	// /dev/sda disk1
	findSCSIdisk := func(model string) string {
		var device string
		pod, err := virtClient.CoreV1().Pods(testsuite.NamespacePrivileged).Get(context.Background(), targetCliPod, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())

		stdout, stderr, err := exec.ExecuteCommandOnPodWithResults(virtClient, pod, "targetcli",
			[]string{"/bin/lsblk", "--scsi", "-o", "NAME,MODEL", "-p", "-n"})
		By(fmt.Sprintf("targetcli: stdout: %v stderr: %v", stdout, stderr))
		Expect(err).ToNot(HaveOccurred())
		lines := strings.Split(stdout, "\n")
		for _, line := range lines {
			if strings.Contains(line, model) {
				line = strings.TrimSpace(line)
				disk := strings.Split(line, " ")
				if len(disk) < 1 {
					continue
				}
				device = disk[0]
				break
			}
		}
		return device

	}

	checkPersistentReservation := func(vmi *v1.VirtualMachineInstance) bool {
		res, err := console.SafeExpectBatchWithResponse(vmi, []expect.Batcher{
			&expect.BSnd{S: "sg_persist -i -k /dev/sda\n"},
			&expect.BExp{R: console.PromptExpression},
		}, 20)
		Expect(err).ToNot(HaveOccurred())
		if strings.Contains(res[0].Output,
			"1 registered reservation key follows:\r\n    0x12345678\r\n") {
			return true
		}
		return false
	}

	BeforeEach(func() {
		var err error
		virtClient, err = kubecli.GetKubevirtClient()
		Expect(err).ToNot(HaveOccurred())
		// Create the scsi disk
		createSCSIDisk()
		device = findSCSIdisk(backendDisk)
		Expect(device).ToNot(BeEmpty())
	})

	AfterEach(func() {
		// Delete the scsi disk
		executeTargetCli([]string{
			"loopback/", "delete", naa})
		executeTargetCli([]string{
			"backstores/fileio", "delete", backendDisk})
		// Delete targetcli pod
		err := virtClient.CoreV1().Pods(testsuite.NamespacePrivileged).Delete(context.Background(), targetCliPod, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())
		// Delete pvc for storing the backend
		err = virtClient.CoreV1().PersistentVolumeClaims(testsuite.NamespacePrivileged).Delete(context.Background(), disks, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())

	})

	Context("Use LUN disk with presistent reservation", func() {
		It("Should successfully start a VM with persistent reservation", func() {
			scsiPVC := "scsipvc"
			By(fmt.Sprintf("Create PVC with SCSI disk %s", device))
			_, pvc, err := tests.CreatePVandPVCwithSCSIDisk(node, device, util.NamespaceTestDefault, "scsi-disks", "scsipv", scsiPVC)
			Expect(err).ToNot(HaveOccurred())
			By("Create VMI with the SCSI disk")
			vmi := libvmi.NewFedora(
				libvmi.WithPersistentVolumeClaimLun("lun0", pvc.Name, true),
			)
			vmi = tests.CreateVmiOnNode(vmi, node)
			libwait.WaitForSuccessfulVMIStartWithTimeoutIgnoreWarnings(vmi, 180)
			By("Reading from disk")
			Expect(console.LoginToFedora(vmi)).To(Succeed(), "Should be able to login to the Fedora VM")
			Expect(console.SafeExpectBatch(vmi, []expect.Batcher{
				&expect.BSnd{S: "sg_persist -i -k /dev/sda\n"},
				&expect.BExp{R: "  LIO-ORG   disk0             4.0 \r\n  Peripheral device type: disk\r\n  PR generation=0x0, there are NO registered reservation keys\r\n"},
				&expect.BSnd{S: "sg_persist -o -G  --param-sark=12345678 /dev/sda\n"},
				&expect.BExp{R: "  LIO-ORG   disk0             4.0 \r\n  Peripheral device type: disk\r\n"},
			}, 30)).To(Succeed())

			Eventually(checkPersistentReservation(vmi), 10*time.Second, 60*time.Second).Should(BeTrue())

		})
	})
})
