// Automatically generated by MockGen. DO NOT EDIT!
// Source: mount.go

package mount_manager

import (
	time "time"

	gomock "github.com/golang/mock/gomock"
	types "k8s.io/apimachinery/pkg/types"
	v1 "kubevirt.io/api/core/v1"
)

// Mock of MountManager interface
type MockMountManager struct {
	ctrl     *gomock.Controller
	recorder *_MockMountManagerRecorder
}

// Recorder for MockMountManager (not exported)
type _MockMountManagerRecorder struct {
	mock *MockMountManager
}

func NewMockMountManager(ctrl *gomock.Controller) *MockMountManager {
	mock := &MockMountManager{ctrl: ctrl}
	mock.recorder = &_MockMountManagerRecorder{mock}
	return mock
}

func (_m *MockMountManager) EXPECT() *_MockMountManagerRecorder {
	return _m.recorder
}

func (_m *MockMountManager) Mount(vmi *v1.VirtualMachineInstance) (MountInfo, error) {
	ret := _m.ctrl.Call(_m, "Mount", vmi)
	ret0, _ := ret[0].(MountInfo)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

func (_mr *_MockMountManagerRecorder) Mount(arg0 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "Mount", arg0)
}

func (_m *MockMountManager) Unmount(vmi *v1.VirtualMachineInstance) error {
	ret := _m.ctrl.Call(_m, "Unmount", vmi)
	ret0, _ := ret[0].(error)
	return ret0
}

func (_mr *_MockMountManagerRecorder) Unmount(arg0 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "Unmount", arg0)
}

func (_m *MockMountManager) ContainerDiskMountsReady(vmi *v1.VirtualMachineInstance, notInitializedSince time.Time) (bool, error) {
	ret := _m.ctrl.Call(_m, "ContainerDiskMountsReady", vmi, notInitializedSince)
	ret0, _ := ret[0].(bool)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

func (_mr *_MockMountManagerRecorder) ContainerDiskMountsReady(arg0, arg1 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "ContainerDiskMountsReady", arg0, arg1)
}

func (_m *MockMountManager) SyncHotplugMounts(vmi *v1.VirtualMachineInstance) error {
	ret := _m.ctrl.Call(_m, "SyncHotplugMounts", vmi)
	ret0, _ := ret[0].(error)
	return ret0
}

func (_mr *_MockMountManagerRecorder) SyncHotplugMounts(arg0 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "SyncHotplugMounts", arg0)
}

func (_m *MockMountManager) SyncHotplugUnmounts(vmi *v1.VirtualMachineInstance) error {
	ret := _m.ctrl.Call(_m, "SyncHotplugUnmounts", vmi)
	ret0, _ := ret[0].(error)
	return ret0
}

func (_mr *_MockMountManagerRecorder) SyncHotplugUnmounts(arg0 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "SyncHotplugUnmounts", arg0)
}

func (_m *MockMountManager) IsHotplugVolumeMounted(vmi *v1.VirtualMachineInstance, volume string, sourceUID types.UID) (bool, error) {
	ret := _m.ctrl.Call(_m, "IsHotplugVolumeMounted", vmi, volume, sourceUID)
	ret0, _ := ret[0].(bool)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

func (_mr *_MockMountManagerRecorder) IsHotplugVolumeMounted(arg0, arg1, arg2 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "IsHotplugVolumeMounted", arg0, arg1, arg2)
}
