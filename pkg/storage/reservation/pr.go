package reservation

import (
	"fmt"
	"path/filepath"

	v1 "kubevirt.io/api/core/v1"
)

const (
	SourceDaemonsPath = "/run/kubevirt/daemons"
)

const (
	PrHelperDir    = "pr"
	PrHelperSocket = "pr-helper.sock"
	PrVolumeName   = "pr-socket-volume"
	PrResourceName = "pr-helper"
)

func GetPrResourceName() string {
	return PrResourceName
}

func GetPrHelperSocketDir() string {
	return fmt.Sprintf(filepath.Join(SourceDaemonsPath, PrHelperDir))
}

func GetPrHelperSocketPath() string {
	return fmt.Sprintf(filepath.Join(GetPrHelperSocketDir(), PrHelperSocket))
}

func GetPrHelperSocket() string {
	return PrHelperSocket
}

func HasVMIPersistentReservation(vmi *v1.VirtualMachineInstance) bool {
	for _, disk := range vmi.Spec.Domain.Devices.Disks {
		if disk.DiskDevice.LUN != nil && disk.DiskDevice.LUN.Reservation {
			return true
		}
	}
	return false
}
