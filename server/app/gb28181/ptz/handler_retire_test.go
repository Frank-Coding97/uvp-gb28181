package ptz

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestRetiredPTZHandlerDoesNotCreateReconcile(t *testing.T) {
	service, db, _, _ := newPTZHandlerTestService(t, nil)
	reset, preset := 30, 0
	op := createHandlerHomeControl(t, service, "retired", true, &reset, &preset)
	service.Retire()
	require.Error(t, service.OnPTZMessage(context.Background(), "D", "response", "1", deviceControlResponse(op.SN, "OK")))
	stored := storedOperation(t, db, op.OperationID)
	require.Equal(t, op.Status, stored.Status)
	require.Nil(t, stored.ReconcileOperationID)
	var count int64
	require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestPTZRetireJoinsEnteredResponse(t *testing.T) {
	service, db, _, _ := newPTZHandlerTestService(t, nil)
	reset, preset := 30, 0
	op := createHandlerHomeControl(t, service, "joining", true, &reset, &preset)
	// This test exercises lifecycle joining, not the pre-2022 reconcile gate.
	require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Where("id = ?", op.ID).Updates(map[string]interface{}{
		"profile_version": "2022", "profile_charset": "GB18030",
	}).Error)
	entered, release := make(chan struct{}), make(chan struct{})
	var once, unblock sync.Once
	defer unblock.Do(func() { close(release) })
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:block-response", func(tx *gorm.DB) {
		once.Do(func() { close(entered); <-release })
	}))
	responseDone := make(chan error, 1)
	go func() {
		responseDone <- service.OnPTZMessage(context.Background(), "D", "response", "1", deviceControlResponse(op.SN, "OK"))
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("response did not reach database")
	}
	retired := make(chan struct{})
	go func() { service.Retire(); close(retired) }()
	select {
	case <-retired:
		t.Error("Retire returned before the entered response completed")
	case <-time.After(50 * time.Millisecond):
	}
	unblock.Do(func() { close(release) })
	require.NoError(t, <-responseDone)
	select {
	case <-retired:
	case <-time.After(2 * time.Second):
		t.Fatal("Retire did not join completed response")
	}
	require.NotNil(t, storedOperation(t, db, op.OperationID).ReconcileOperationID)
}
