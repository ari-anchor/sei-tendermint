// Code generated by mockery 2.7.5. DO NOT EDIT.

package mocks

import (
	mock "github.com/stretchr/testify/mock"

	state "github.com/ari-anchor/sei-tendermint/internal/state"

	time "time"
)

// BlockSyncReactor is an autogenerated mock type for the BlockSyncReactor type
type BlockSyncReactor struct {
	mock.Mock
}

// GetMaxPeerBlockHeight provides a mock function with given fields:
func (_m *BlockSyncReactor) GetMaxPeerBlockHeight() int64 {
	ret := _m.Called()

	var r0 int64
	if rf, ok := ret.Get(0).(func() int64); ok {
		r0 = rf()
	} else {
		r0 = ret.Get(0).(int64)
	}

	return r0
}

// GetRemainingSyncTime provides a mock function with given fields:
func (_m *BlockSyncReactor) GetRemainingSyncTime() time.Duration {
	ret := _m.Called()

	var r0 time.Duration
	if rf, ok := ret.Get(0).(func() time.Duration); ok {
		r0 = rf()
	} else {
		r0 = ret.Get(0).(time.Duration)
	}

	return r0
}

// GetTotalSyncedTime provides a mock function with given fields:
func (_m *BlockSyncReactor) GetTotalSyncedTime() time.Duration {
	ret := _m.Called()

	var r0 time.Duration
	if rf, ok := ret.Get(0).(func() time.Duration); ok {
		r0 = rf()
	} else {
		r0 = ret.Get(0).(time.Duration)
	}

	return r0
}

// SwitchToBlockSync provides a mock function with given fields: _a0
func (_m *BlockSyncReactor) SwitchToBlockSync(_a0 state.State) error {
	ret := _m.Called(_a0)

	var r0 error
	if rf, ok := ret.Get(0).(func(state.State) error); ok {
		r0 = rf(_a0)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}
