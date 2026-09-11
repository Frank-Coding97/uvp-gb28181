package grant

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/datascope"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
	"uvplatform.cn/uvp-gb28181/internal/sqlitebootstrap"
)

func newGrantSQLiteBaselineDB(t *testing.T) (*gorm.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "grant.db")
	db, err := gormhelper.NewSQLiteClient(path)
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	_, err = sqlitebootstrap.Initialize(context.Background(), db)
	require.NoError(t, err)
	return db, path
}

func TestRepoCreateSQLiteBaselineRestoresAndSerializesNaturalKey(t *testing.T) {
	db, path := newGrantSQLiteBaselineDB(t)
	device := gbmodels.GbDevice{DeviceID: "sqlite-grant-device", Name: "grant device", OwnerDeptID: 1}
	require.NoError(t, db.Create(&device).Error)
	repo := NewRepo(db)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	created, err := repo.Create(ctx, &gbmodels.GbDeviceGrant{DeviceID: device.ID, TargetType: "dept", TargetID: 701, CreatedBy: 1})
	require.NoError(t, err)
	require.True(t, created)
	created, err = repo.Create(ctx, &gbmodels.GbDeviceGrant{DeviceID: device.ID, TargetType: "dept", TargetID: 701, CreatedBy: 2})
	require.NoError(t, err)
	require.False(t, created)

	var stored gbmodels.GbDeviceGrant
	require.NoError(t, db.Where("device_id = ? AND target_type = ? AND target_id = ?", device.ID, "dept", 701).First(&stored).Error)
	require.NoError(t, repo.Remove(ctx, device.ID, stored.ID))
	created, err = repo.Create(ctx, &gbmodels.GbDeviceGrant{DeviceID: device.ID, TargetType: "dept", TargetID: 701, CreatedBy: 9})
	require.NoError(t, err)
	require.True(t, created)
	require.NoError(t, db.Where("id = ?", stored.ID).First(&stored).Error)
	require.EqualValues(t, 9, stored.CreatedBy)

	second, err := gormhelper.NewSQLiteClient(path)
	require.NoError(t, err)
	rawSecond, err := second.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = rawSecond.Close() })
	firstRepo, otherRepo := NewRepo(db), NewRepo(second)
	start := make(chan struct{})
	results := make(chan bool, 2)
	errors := make(chan error, 2)
	var wg sync.WaitGroup
	for _, r := range []*Repo{firstRepo, otherRepo} {
		wg.Add(1)
		go func(repo *Repo) {
			defer wg.Done()
			<-start
			created, err := repo.Create(ctx, &gbmodels.GbDeviceGrant{DeviceID: device.ID, TargetType: "user", TargetID: 702, CreatedBy: 3})
			results <- created
			errors <- err
		}(r)
	}
	close(start)
	wg.Wait()
	close(results)
	close(errors)
	createdCount := 0
	for created := range results {
		if created {
			createdCount++
		}
	}
	for err := range errors {
		require.NoError(t, err)
	}
	require.Equal(t, 1, createdCount)

	var count int64
	require.NoError(t, db.Model(&gbmodels.GbDeviceGrant{}).Where("device_id = ? AND target_type = ? AND target_id = ?", device.ID, "user", 702).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestServiceApplySQLiteBaselineUsesRowsAffectedForMaskedEmpty(t *testing.T) {
	db, _ := newGrantSQLiteBaselineDB(t)
	active := int8(1)
	department := basemodels.SysDepartment{BaseModel: basemodels.BaseModel{ID: 701}, Name: "SQLite部门", Status: &active}
	require.NoError(t, db.Create(&department).Error)
	device := gbmodels.GbDevice{DeviceID: "sqlite-apply-device", Name: "apply device", OwnerDeptID: department.ID}
	require.NoError(t, db.Create(&device).Error)
	service := NewService(db, nil)
	state, err := service.Query(context.Background(), []uint{device.ID})
	require.NoError(t, err)
	require.Len(t, state.Devices, 1)

	result, err := service.Apply(context.Background(), ApplyRequest{
		Items: []ApplyDevice{{DeviceID: device.ID, ExpectedRevision: state.Devices[0].Revision}},
		Mode:  ApplyModeAdd, Targets: []ApplyTarget{{Type: gbmodels.GrantTargetTypeDept, ID: department.ID}}, CreatedBy: 11,
	}, datascopeFullAccess())
	require.NoError(t, err)
	require.EqualValues(t, 1, result.Summary.Added)
	require.Equal(t, applyStatusChanged, result.Results[0].Status)
}

func datascopeFullAccess() datascope.OwnerDeptAccess {
	return datascope.OwnerDeptAccess{FullAccess: true}
}
