package setup

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newConfigTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&SIPConfig{}))
	return db
}

func validSaveRequest(password *string) SaveSIPConfigRequest {
	return SaveSIPConfigRequest{
		DeploymentMode: DeploymentLAN,
		ListenIP:       "0.0.0.0",
		AdvertiseIP:    "192.168.1.10",
		Port:           5061,
		Domain:         "3402000000",
		ServerID:       "34020000002000000001",
		Password:       password,
	}
}

func TestSIPConfigService_SaveNewConfig(t *testing.T) {
	db := newConfigTestDB(t)
	svc := NewSIPConfigService(db)
	password := "Secret123"

	view, err := svc.Save(context.Background(), validSaveRequest(&password))
	require.NoError(t, err)
	require.True(t, view.HasPassword)
	require.Equal(t, "192.168.1.10", view.AdvertiseIP)

	row, err := NewSIPConfigRepository(db).Get(context.Background())
	require.NoError(t, err)
	require.NotNil(t, row)
	require.Equal(t, "Secret123", row.Password)
}

func TestSIPConfigService_EditPreservesPassword(t *testing.T) {
	db := newConfigTestDB(t)
	svc := NewSIPConfigService(db)
	password := "Secret123"
	_, err := svc.Save(context.Background(), validSaveRequest(&password))
	require.NoError(t, err)

	req := validSaveRequest(nil)
	req.Port = 5062
	view, err := svc.Save(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, 5062, view.Port)

	row, err := NewSIPConfigRepository(db).Get(context.Background())
	require.NoError(t, err)
	require.Equal(t, "Secret123", row.Password)
}

func TestSIPConfigService_NewConfigRequiresPassword(t *testing.T) {
	db := newConfigTestDB(t)
	_, err := NewSIPConfigService(db).Save(context.Background(), validSaveRequest(nil))
	require.ErrorIs(t, err, ErrSIPPasswordRequired)

	var count int64
	require.NoError(t, db.Model(&SIPConfig{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestSIPConfigService_ViewDoesNotExposePassword(t *testing.T) {
	db := newConfigTestDB(t)
	password := "Secret123"
	svc := NewSIPConfigService(db)
	_, err := svc.Save(context.Background(), validSaveRequest(&password))
	require.NoError(t, err)

	view, err := svc.Get(context.Background())
	require.NoError(t, err)
	require.NotNil(t, view)
	require.True(t, view.HasPassword)
}
