// Automatically generated by MockGen. DO NOT EDIT!
// Source: mount.go

package container_disk

import (
	gomock "github.com/golang/mock/gomock"
	v1 "kubevirt.io/api/core/v1"
)

// Mock of Mounter interface
type MockMounter struct {
	ctrl     *gomock.Controller
	recorder *_MockMounterRecorder
}

// Recorder for MockMounter (not exported)
type _MockMounterRecorder struct {
	mock *MockMounter
}

func NewMockMounter(ctrl *gomock.Controller) *MockMounter {
	mock := &MockMounter{ctrl: ctrl}
	mock.recorder = &_MockMounterRecorder{mock}
	return mock
}

func (_m *MockMounter) EXPECT() *_MockMounterRecorder {
	return _m.recorder
}

func (_m *MockMounter) Unmount(vmi *v1.VirtualMachineInstance) error {
	ret := _m.ctrl.Call(_m, "Unmount", vmi)
	ret0, _ := ret[0].(error)
	return ret0
}

func (_mr *_MockMounterRecorder) Unmount(arg0 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "Unmount", arg0)
}
