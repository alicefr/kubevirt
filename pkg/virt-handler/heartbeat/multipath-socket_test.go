package heartbeat

import (
	"errors"
	"os"
	exec "os/exec"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
	gomock "github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	safepath "kubevirt.io/kubevirt/pkg/safepath"
)

var _ = Describe("MonitorMultipathSocket", func() {
	var (
		m         *MonitorMultipathSocket
		isMounted bool
	)

	BeforeEach(func() {
		var err error
		ctrl := gomock.NewController(GinkgoT())
		mounter := NewMockmounter(ctrl)
		mounter.
			EXPECT().
			IsMounted(gomock.Any()).
			Return(isMounted, nil).
			AnyTimes()
		mounter.
			EXPECT().
			Mount(gomock.Any(), gomock.Any(), gomock.Any()).
			Do(func(_, _ *safepath.Path, _ bool) {
				isMounted = true
			}).Return(exec.Command("")).AnyTimes()

		mounter.
			EXPECT().
			Umount(gomock.Any()).
			Do(func(_ *safepath.Path) {
				isMounted = false
			}).Return(exec.Command("")).AnyTimes()

		rootDir := GinkgoT().TempDir()
		m, err = fakeNewMonitorMultipathSocket(GinkgoT().TempDir(),
			filepath.Join(rootDir, "pr"), mounter)
		Expect(err).ToNot(HaveOccurred())
	})
	AfterEach(func() {
		Expect(m.w.Errors).To(BeEmpty())
		m.Stop()
	})

	It("It should create the host dir for the persistent reservation", func() {
		go m.Run()
		Eventually(func() bool {
			return checkFileExists(m.hostDir)
		}, 5*time.Second, time.Second).Should(BeTrue())
	})
	It("It should create the mount when the socket appears", func() {
		go m.Run()
		m.createFakeSocket()
		Eventually(func() bool {
			return isMounted
		}, 10*time.Second, time.Second).Should(BeTrue())
	})
	It("It should create mount when the socket already exists", func() {
		m.createFakeSocket()
		go m.Run()
		Eventually(func() bool {
			return isMounted
		}, 10*time.Second, time.Second).Should(BeTrue())
	})
	It("It should remove the mount when the socket is removed", func() {
		m.createFakeSocket()
		go m.Run()
		Eventually(func() bool {
			return isMounted
		}, 10*time.Second, time.Second).Should(BeTrue())
		m.removeFakeSocket()
		Eventually(func() bool {
			return isMounted
		}, 10*time.Second, time.Second).Should(BeFalse())
	})
	It("It should keep the mount when another file in run is removed", func() {
		m.createFakeSocket()
		go m.Run()
		Eventually(func() bool {
			return isMounted
		}, 10*time.Second, time.Second).Should(BeTrue())
		file := filepath.Join(m.runDir, "test")
		_, err := os.Create(file)
		Expect(err).ToNot(HaveOccurred())
		Expect(os.Remove(file)).To(Succeed())
		Eventually(func() bool {
			return isMounted
		}, 10*time.Second, time.Second).Should(BeTrue())
	})
})

func checkFileExists(filePath string) bool {
	_, error := os.Stat(filePath)
	return !errors.Is(error, os.ErrNotExist)
}

func fakeNewMonitorMultipathSocket(runDir, hostDir string, m mounter) (*MonitorMultipathSocket, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w.Add(runDir)
	return &MonitorMultipathSocket{
		w:                   w,
		runDir:              runDir,
		multipathSocketPath: filepath.Join(runDir, Socket),
		hostDir:             hostDir,
		mounter:             m,
	}, nil
}

func (m *MonitorMultipathSocket) createFakeSocket() {
	link := filepath.Join(m.runDir, Socket)
	_, err := os.Create(link)
	Expect(err).ToNot(HaveOccurred())
}

func (m *MonitorMultipathSocket) removeFakeSocket() {
	link := filepath.Join(m.runDir, Socket)
	Expect(os.Remove(link)).To(Succeed())
}
