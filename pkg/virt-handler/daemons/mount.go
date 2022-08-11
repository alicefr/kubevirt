package daemons

import (
	"fmt"
	"os"
	"path/filepath"

	v1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/log"

	"kubevirt.io/kubevirt/pkg/daemons"
	diskutils "kubevirt.io/kubevirt/pkg/ephemeral-disk-utils"
	"kubevirt.io/kubevirt/pkg/safepath"
	"kubevirt.io/kubevirt/pkg/util"
	"kubevirt.io/kubevirt/pkg/virt-handler/isolation"
	"kubevirt.io/kubevirt/pkg/virt-handler/selinux"
)

type Mounter interface {
	MountDaemonsSockets(vmi *v1.VirtualMachineInstance) error
	UmountDaemonsSockets(vmi *v1.VirtualMachineInstance) error
}

func MountDaemonsSockets(vmi *v1.VirtualMachineInstance) error {
	var lastError error

	// Check if VMI requires persistent reservation
	if !daemons.IsPRHelperNeeded(vmi) {
		return nil
	}

	// Get source path for the pr daemon directory
	safeSourceDaemonsPath, err := safepath.JoinAndResolveWithRelativeRoot("/", daemons.GetPrHelperSocketDir())
	if err != nil {
		return fmt.Errorf("failed parsing source path %s:%v", daemons.GetPrHelperSocketDir(), err)
	}
	log.Log.V(1).Infof("XXX Pr helper is needed")
	if len(vmi.Status.ActivePods) < 1 {
		return fmt.Errorf("failed bindmount daemons socket dir: no active pods for the vmi %s", vmi.Name)
	}
	for uid, _ := range vmi.Status.ActivePods {
		log.Log.V(1).Infof("XXX Active pod")
		path := filepath.Join("/var/lib/kubelet/pods", string(uid), daemons.SuffixDaemonPath)
		targetDir, err := safepath.JoinAndResolveWithRelativeRoot("/", path)
		if err != nil {
			lastError = wrapError(lastError, fmt.Errorf("failed creating the safe path %s for pod uid%s:%v", path, string(uid), err))
			continue
		}
		// Create the safe path for the pr helper daemon
		if err := safepath.MkdirAtNoFollow(targetDir, daemons.PrHelperDir, 0755); err != nil {
			if !os.IsExist(err) {
				lastError = wrapError(lastError, err)
				continue
			}
		}
		log.Log.V(1).Infof("XXX mkdir target dir: %v", targetDir)
		socketDir, err := safepath.JoinNoFollow(targetDir, daemons.PrHelperDir)
		if err != nil {
			lastError = wrapError(lastError, fmt.Errorf("failed creating the path for the socket dir for pod uid%s:%v", string(uid), err))
			continue
		}
		log.Log.V(1).Infof("XXX path socket: %v", socketDir.String())

		mounted, err := isolation.IsMounted(socketDir)
		if err != nil {
			lastError = wrapError(lastError, fmt.Errorf("failed checking if daemon socket dir %s is mounted: %v", socketDir.String(), err))
			continue
		}
		if mounted {
			log.Log.V(1).Infof("socket directory already mounted: %v", socketDir.String())
			continue
		}

		err = safepath.MountNoFollow(safeSourceDaemonsPath, socketDir, true)
		if err != nil {
			lastError = wrapError(lastError, fmt.Errorf("failed bindmount daemons socket dir %s to %s: %v", safeSourceDaemonsPath.String(), socketDir.String(), err))
		}
		log.Log.V(1).Infof("mounted: %s to %s", safeSourceDaemonsPath.String(), socketDir.String())
		log.Log.V(1).Infof("XXX relabelled: %s", socketDir.String())
		// Change ownership to the directory and relabel
		err = changeOwnershipAndRelabel(socketDir)
		if err != nil {
			lastError = wrapError(lastError, fmt.Errorf("failed relabeling pr socket dir: %s: %v", socketDir.String(), err))
			continue
		}
		// Change ownership to the socket and relabel
		socket, err := safepath.JoinNoFollow(socketDir, daemons.PrHelperSocket)
		if err != nil {
			lastError = wrapError(lastError, fmt.Errorf("failed creating socket path: %v", err))
			continue
		}

		err = changeOwnershipAndRelabel(socket)
		if err != nil {
			lastError = wrapError(lastError, fmt.Errorf("failed relabeling socket: %s: %v", socket.String(), err))
			lastError = wrapError(lastError, err)
			continue
		}
		log.Log.V(1).Infof("mounted daemon socket: %s", socket.String())
	}
	return lastError
}

func UmountDaemonsSocket(vmi *v1.VirtualMachineInstance) error {
	var lastError error
	// Check if VMI requires persistent reservation
	if !daemons.IsPRHelperNeeded(vmi) {
		return nil
	}
	for uid, _ := range vmi.Status.ActivePods {
		socketDir, err := safepath.NewPathNoFollow(filepath.Join(util.KubeletPodsDir, string(uid), daemons.SuffixDaemonPath, daemons.PrHelperDir))
		if err != nil {
			lastError = wrapError(lastError, fmt.Errorf("failed creating the path for the socket dir for pod uid%s:%v", string(uid), err))
			continue
		}
		// If the path doesn't exist it has already been deleted
		if _, err := safepath.StatAtNoFollow(socketDir); !os.IsExist(err) {
			log.Log.V(1).Infof("%s doesn't exist anymore", socketDir.String())
			continue
		}
		socket, err := safepath.JoinNoFollow(socketDir, daemons.PrHelperSocket)
		if err != nil {
			if !os.IsExist(err) {
				continue
			}
			lastError = wrapError(lastError, fmt.Errorf("failed creating socket path: %v", err))
			continue
		}
		if err := safepath.UnlinkAtNoFollow(socket); err != nil {
			lastError = wrapError(lastError, err)
		}
		err = safepath.UnmountNoFollow(socketDir)
		if err != nil {
			lastError = wrapError(lastError, fmt.Errorf("failed unmount daemons socket dir: %v", err))
		}
		if err := safepath.UnlinkAtNoFollow(socketDir); err != nil {
			lastError = wrapError(lastError, err)
		}
	}
	return lastError
}

func wrapError(lastError, err error) error {
	if lastError == nil {
		return err
	}
	return fmt.Errorf("%w, %s", lastError, err.Error())
}

func changeOwnershipAndRelabel(path *safepath.Path) error {
	err := diskutils.DefaultOwnershipManager.SetFileOwnership(path)
	if err != nil {
		return err
	}

	seLinux, selinuxEnabled, err := selinux.NewSELinux()
	if err == nil && selinuxEnabled {
		unprivilegedContainerSELinuxLabel := "system_u:object_r:container_file_t:s0"
		err = selinux.RelabelFiles(unprivilegedContainerSELinuxLabel, seLinux.IsPermissive(), path)
		if err != nil {
			return (fmt.Errorf("error relabeling %s: %v", path, err))
		}

	}
	return err
}
