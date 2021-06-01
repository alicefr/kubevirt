package storage

import (
	"context"

	"strings"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"kubevirt.io/client-go/kubecli"
	"kubevirt.io/kubevirt/pkg/virtctl/guestfs"
	"kubevirt.io/kubevirt/tests"
)

type fakeAttacher struct {
	done chan bool
}

// fakeCreateAttacher simulates the attacher to the pod console. It has to block until the test terminates.
func (f *fakeAttacher) fakeCreateAttacher(client *guestfs.K8sClient, p *corev1.Pod, command string) error {
	<-f.done
	return nil
}

func (f *fakeAttacher) closeChannel() {
	f.done <- true
}

var _ = Describe("[Serial]Guestfs", func() {
	var (
		virtClient kubecli.KubevirtClient
	)
	execCommandLibguestfsPod := func(podName string, c []string) (string, string, error) {
		pod, err := virtClient.CoreV1().Pods(tests.NamespaceTestDefault).Get(context.Background(), podName, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		return tests.ExecuteCommandOnPodV2(virtClient, pod, "libguestfs", c)
	}

	createPVCFilesystem := func(name string) *corev1.PersistentVolumeClaim {
		quantity, _ := resource.ParseQuantity("500Mi")
		return &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						"storage": quantity,
					},
				},
			},
		}

	}

	createFakeAttacher := func() *fakeAttacher {
		f := &fakeAttacher{}
		f.done = make(chan bool, 1)
		guestfs.SetAttacher(f.fakeCreateAttacher)
		return f
	}

	runGuestfsOnPVC := func(pvcClaim string) {
		podName := "libguestfs-tools-" + pvcClaim
		guestfsCmd := tests.NewVirtctlCommand("guestfs",
			"--pvc", pvcClaim,
			"--pvc-namespace", tests.NamespaceTestDefault)
		go func() {
			defer GinkgoRecover()
			Expect(guestfsCmd.Execute()).ToNot(HaveOccurred())
		}()
		// Waiting until the libguestfs pod is running
		Eventually(func() corev1.PodPhase {
			pod, _ := virtClient.CoreV1().Pods(tests.NamespaceTestDefault).Get(context.Background(), podName, metav1.GetOptions{})
			return pod.Status.Phase
		}, 90*time.Second, 2*time.Second).Should(Equal(k8sv1.PodRunning))
		// Verify that the appliance has been extracted before running any tests
		Eventually(func() bool {
			output, _, err := execCommandLibguestfsPod(podName, []string{"ls", "/usr/local/lib/guestfs/appliance"})
			Expect(err).ToNot(HaveOccurred())
			if strings.Contains(output, "kernel") {
				return true
			}
			return false
		}, 30*time.Second, 2*time.Second).Should(BeTrue())

	}

	createGuestfsWithPVC := func(pvc *corev1.PersistentVolumeClaim) {
		var err error
		_, err = virtClient.CoreV1().PersistentVolumeClaims(tests.NamespaceTestDefault).Create(context.Background(), pvc, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
		runGuestfsOnPVC(pvc.ObjectMeta.Name)

	}

	FContext("Run libguestfs on PVCs", func() {
		BeforeEach(func() {
			var err error
			virtClient, err = kubecli.GetKubevirtClient()
			Expect(err).ToNot(HaveOccurred())

		}, 120)

		// libguestfs-test-tool verifies the setup to run libguestfs-tools
		It("Should successful run libguestfs-test-tool", func() {
			f := createFakeAttacher()
			defer f.closeChannel()
			pvcClaim := "pvc-verify"
			pvc := createPVCFilesystem(pvcClaim)
			createGuestfsWithPVC(pvc)
			output, _, err := execCommandLibguestfsPod("libguestfs-tools-"+pvcClaim, []string{"libguestfs-test-tool"})
			Expect(err).ToNot(HaveOccurred())
			Expect(output).To(ContainSubstring("===== TEST FINISHED OK ====="))

		})

		It("Should successful run guestfish on a filesystem-based PVC", func() {
			f := createFakeAttacher()
			defer f.closeChannel()
			pvcClaim := "pvc-fs"
			podName := "libguestfs-tools-" + pvcClaim
			pvc := createPVCFilesystem(pvcClaim)
			createGuestfsWithPVC(pvc)
			stdout, stderr, err := execCommandLibguestfsPod(podName, []string{"qemu-img", "create", "/disks/disk.img", "500M"})
			Expect(stderr).To(Equal(""))
			Expect(stdout).To(ContainSubstring("Formatting"))
			Expect(err).ToNot(HaveOccurred())
			stdout, stderr, err = execCommandLibguestfsPod(podName, []string{"guestfish", "-a", "/disks/disk.img", "run"})
			Expect(stderr).To(Equal(""))
			Expect(stdout).To(Equal(""))
			Expect(err).ToNot(HaveOccurred())

		})

		It("Should fail run guestfs command on a PVC in use", func() {
			f := createFakeAttacher()
			defer f.closeChannel()
			pvcClaim := "pvc-fail-to-run-twice"
			pvc := createPVCFilesystem(pvcClaim)
			createGuestfsWithPVC(pvc)
			guestfsCmd := tests.NewVirtctlCommand("guestfs",
				"--pvc-namespace", tests.NamespaceTestDefault)
			Expect(guestfsCmd.Execute()).To(HaveOccurred())
		})

		It("Should successful run guestfish on a block-based PVC", func() {
			f := createFakeAttacher()
			defer f.closeChannel()
			podName := "libguestfs-tools-" + tests.BlockDiskForTest
			tests.CreateBlockVolumePvAndPvc("500Mi")
			runGuestfsOnPVC(tests.BlockDiskForTest)
			stdout, stderr, err := execCommandLibguestfsPod(podName, []string{"guestfish", "-a", "/dev/vda", "run"})
			Expect(stderr).To(Equal(""))
			Expect(stdout).To(Equal(""))
			Expect(err).ToNot(HaveOccurred())

		})

	})
})
