package gb28181

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbdashboard "uvplatform.cn/uvp-gb28181/app/gb28181/dashboard"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestRetentionLifecycleStartsImmediatelyOnlyWhenAllFactTablesExist(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.Nil(t, startDashboardRetentionRuntime(db, time.Hour, nil))
	require.NoError(t, db.AutoMigrate(&gbmodels.GbSipMetricMinute{}, &gbmodels.GbSipMetricFlush{}, &gbmodels.GbSipMetricGap{}, &gbmodels.GbPlayAttempt{}))

	reports := make(chan gbdashboard.RetentionResult, 1)
	cancel := startDashboardRetentionRuntime(db, time.Hour, func(result gbdashboard.RetentionResult, err error) {
		require.NoError(t, err)
		reports <- result
	})
	require.NotNil(t, cancel)
	defer cancel()
	select {
	case <-reports:
	case <-time.After(time.Second):
		t.Fatal("dashboard retention did not run on startup")
	}
}
