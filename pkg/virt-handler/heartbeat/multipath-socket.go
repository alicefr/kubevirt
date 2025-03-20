package heartbeat

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
	"kubevirt.io/client-go/log"

	"kubevirt.io/kubevirt/pkg/safepath"
	"kubevirt.io/kubevirt/pkg/storage/reservation"
	"kubevirt.io/kubevirt/pkg/virt-handler/isolation"
	virt_chroot "kubevirt.io/kubevirt/pkg/virt-handler/virt-chroot"
)

const (
	Socket   = "multipathd.socket"
	runDir   = "/run"
	procRoot = "/proc/1/root"
)

type MonitorMultipathSocket struct {
	w                   *fsnotify.Watcher
	runDir              string
	multipathSocketPath string
	hostDir             string
	mounter             mounter
}

type mountManager struct{}

func (m *mountManager) Mount(sourcePath, targetPath *safepath.Path, ro bool) *exec.Cmd {
	return virt_chroot.MountChroot(sourcePath, targetPath, ro)
}

func (m *mountManager) Umount(path *safepath.Path) *exec.Cmd {
	return virt_chroot.UmountChroot(path)
}

func (m *mountManager) IsMounted(mountPoint *safepath.Path) (isMounted bool, err error) {
	return isolation.IsMounted(mountPoint)
}

//go:generate mockgen -source $GOFILE -package=$GOPACKAGE -destination=generated_mock_$GOFILE
type mounter interface {
	Mount(sourcePath, targetPath *safepath.Path, ro bool) *exec.Cmd
	Umount(path *safepath.Path) *exec.Cmd
	IsMounted(mountPoint *safepath.Path) (isMounted bool, err error)
}

func NewMonitorMultipathSocket() *MonitorMultipathSocket {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		log.Log.Reason(err).Errorf("failed creating watcher for multipath socket")
	}
	w.Add(runDir)
	return &MonitorMultipathSocket{
		w:                   w,
		runDir:              runDir,
		multipathSocketPath: filepath.Join(runDir, Socket),
		hostDir:             reservation.GetPrHelperHostSocketDir(),
		mounter:             &mountManager{},
	}
}

func (m *MonitorMultipathSocket) Run() {
	if _, err := os.Stat(m.hostDir); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(m.hostDir, 0700); err != nil {
			log.Log.Reason(err).Errorf("failed creating persistent reservation directory %s", m.hostDir)
			return
		}
		log.Log.Infof("Created persistent reservation directory %s", m.hostDir)
	}

	path := filepath.Join(m.hostDir, Socket)
	os.Create(path)
	sPath, err := safepath.JoinAndResolveWithRelativeRoot("/", path)
	if err != nil {
		log.Log.Reason(err).Errorf("failed to create the safepath for the multipath socket mount %v", sPath)
		return
	}
	// Remove old bind mount if there was one
	if isMounted, err := m.mounter.IsMounted(sPath); err == nil && isMounted {
		m.mounter.Umount(sPath).CombinedOutput()
	}

	sMultipathSocket, err := safepath.JoinAndResolveWithRelativeRoot(procRoot, m.multipathSocketPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Log.Reason(err).Errorf("failed to create the safepath for the multipath socket")
		return
	}
	if err == nil {
		out, err := m.mounter.Mount(sMultipathSocket, sPath, false).CombinedOutput()
		if err != nil {
			log.Log.Reason(err).Errorf("failed to create the multipath socket bind mount %s: %s", path, string(out))
		}
		log.Log.Infof("Created bind mount for the multipath socket")
	}
	for {
		select {
		case event := <-m.w.Events:
			// Filter the event by path
			if event.Name != m.multipathSocketPath {
				continue
			}
			if event.Op&fsnotify.Remove == fsnotify.Remove {
				out, err := m.mounter.Umount(sPath).CombinedOutput()
				if err != nil && !errors.Is(err, os.ErrNotExist) {
					log.Log.Reason(err).Errorf("failed to remove the multipath socket bind mount %s: %s", path, string(out))
					continue
				}
				log.Log.Infof("Removed bind mount for the multipath socket")
				continue
			}
			sMultipathSocket, err := safepath.JoinAndResolveWithRelativeRoot(procRoot, m.multipathSocketPath)
			if err != nil {
				log.Log.Reason(err).Errorf("failed to create the safepath for the multipath socket")
				continue
			}
			out, err := m.mounter.Mount(sMultipathSocket, sPath, false).CombinedOutput()
			if err != nil {
				log.Log.Reason(err).Errorf("failed to create the multipath socket bind mount %s: %s", path, string(out))
				continue
			}
			log.Log.Infof("Created bind mount for the multipath socket")
		case err := <-m.w.Errors:
			log.Log.Reason(err).Errorf("Failed monitoring multipath socket")
		}
	}
}

func (m *MonitorMultipathSocket) Stop() {
	m.w.Close()
}
