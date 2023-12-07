// Code generated by MockGen. DO NOT EDIT.
// Source: cluster_ready.go
//
// Generated by this command:
//
//	mockgen -destination zz_generated_mocks.go -package readiness -source=cluster_ready.go
//
// Package readiness is a generated GoMock package.
package readiness

import (
	reflect "reflect"

	gomock "go.uber.org/mock/gomock"
	reconcile "sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// MockInterface is a mock of Interface interface.
type MockInterface struct {
	ctrl     *gomock.Controller
	recorder *MockInterfaceMockRecorder
}

// MockInterfaceMockRecorder is the mock recorder for MockInterface.
type MockInterfaceMockRecorder struct {
	mock *MockInterface
}

// NewMockInterface creates a new mock instance.
func NewMockInterface(ctrl *gomock.Controller) *MockInterface {
	mock := &MockInterface{ctrl: ctrl}
	mock.recorder = &MockInterfaceMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockInterface) EXPECT() *MockInterfaceMockRecorder {
	return m.recorder
}

// IsReady mocks base method.
func (m *MockInterface) IsReady() (bool, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "IsReady")
	ret0, _ := ret[0].(bool)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// IsReady indicates an expected call of IsReady.
func (mr *MockInterfaceMockRecorder) IsReady() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "IsReady", reflect.TypeOf((*MockInterface)(nil).IsReady))
}

// Result mocks base method.
func (m *MockInterface) Result() reconcile.Result {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Result")
	ret0, _ := ret[0].(reconcile.Result)
	return ret0
}

// Result indicates an expected call of Result.
func (mr *MockInterfaceMockRecorder) Result() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Result", reflect.TypeOf((*MockInterface)(nil).Result))
}

// clusterTooOld mocks base method.
func (m *MockInterface) clusterTooOld(arg0 int) bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "clusterTooOld", arg0)
	ret0, _ := ret[0].(bool)
	return ret0
}

// clusterTooOld indicates an expected call of clusterTooOld.
func (mr *MockInterfaceMockRecorder) clusterTooOld(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "clusterTooOld", reflect.TypeOf((*MockInterface)(nil).clusterTooOld), arg0)
}

// setClusterCreationTime mocks base method.
func (m *MockInterface) setClusterCreationTime() error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "setClusterCreationTime")
	ret0, _ := ret[0].(error)
	return ret0
}

// setClusterCreationTime indicates an expected call of setClusterCreationTime.
func (mr *MockInterfaceMockRecorder) setClusterCreationTime() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "setClusterCreationTime", reflect.TypeOf((*MockInterface)(nil).setClusterCreationTime))
}

// setPromAPI mocks base method.
func (m *MockInterface) setPromAPI() error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "setPromAPI")
	ret0, _ := ret[0].(error)
	return ret0
}

// setPromAPI indicates an expected call of setPromAPI.
func (mr *MockInterfaceMockRecorder) setPromAPI() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "setPromAPI", reflect.TypeOf((*MockInterface)(nil).setPromAPI))
}
