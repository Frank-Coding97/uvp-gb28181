package models

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSysJobResultsSummaryRoundTrip(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&SysJobResults{}))

	now := time.Now()
	result := SysJobResults{
		JobId: "cleanup", Status: "SUCCESS", Summary: "保留 7 天，删除 24 条",
		StartTime: &now, EndTime: &now, CreatedAt: &now,
	}
	require.NoError(t, db.Create(&result).Error)

	var stored SysJobResults
	require.NoError(t, db.First(&stored, result.Id).Error)
	require.Equal(t, result.Summary, stored.Summary)
}
