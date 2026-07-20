package setup

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newInstallationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&SystemInstallation{}))
	return db
}

func TestInstallationService_MissingRowIsLegacyWithoutInsert(t *testing.T) {
	db := newInstallationTestDB(t)
	svc := NewInstallationService(db)

	state, err := svc.Current(context.Background())
	require.NoError(t, err)
	require.Equal(t, OnboardingLegacy, state.Status)

	var count int64
	require.NoError(t, db.Model(&SystemInstallation{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestInstallationService_FillsInstanceIDOnce(t *testing.T) {
	db := newInstallationTestDB(t)
	require.NoError(t, db.Create(&SystemInstallation{ID: SingletonID, OnboardingVersion: 1, SIPOnboardingStatus: OnboardingPending}).Error)
	svc := NewInstallationService(db)

	first, err := svc.Current(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, first.InstanceID)
	second, err := svc.Current(context.Background())
	require.NoError(t, err)
	require.Equal(t, first.InstanceID, second.InstanceID)
}

func TestInstallationService_Transitions(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)

	t.Run("pending to skipped", func(t *testing.T) {
		db := newInstallationTestDB(t)
		require.NoError(t, db.Create(&SystemInstallation{ID: SingletonID, OnboardingVersion: 1, SIPOnboardingStatus: OnboardingPending}).Error)
		svc := NewInstallationService(db)
		svc.now = func() time.Time { return now }

		require.NoError(t, svc.Skip(context.Background()))
		state, err := svc.Current(context.Background())
		require.NoError(t, err)
		require.Equal(t, OnboardingSkipped, state.Status)
		require.Equal(t, &now, state.FinishedAt)
	})

	t.Run("skipped and legacy can complete", func(t *testing.T) {
		for _, initial := range []OnboardingStatus{OnboardingSkipped, OnboardingLegacy} {
			db := newInstallationTestDB(t)
			require.NoError(t, db.Create(&SystemInstallation{ID: SingletonID, OnboardingVersion: 1, SIPOnboardingStatus: initial}).Error)
			svc := NewInstallationService(db)
			require.NoError(t, svc.Complete(context.Background()))
			state, err := svc.Current(context.Background())
			require.NoError(t, err)
			require.Equal(t, OnboardingCompleted, state.Status)
		}
	})

	t.Run("completed cannot skip", func(t *testing.T) {
		db := newInstallationTestDB(t)
		require.NoError(t, db.Create(&SystemInstallation{ID: SingletonID, OnboardingVersion: 1, SIPOnboardingStatus: OnboardingCompleted}).Error)
		svc := NewInstallationService(db)
		err := svc.Skip(context.Background())
		require.ErrorIs(t, err, ErrInvalidOnboardingTransition)
		state, currentErr := svc.Current(context.Background())
		require.NoError(t, currentErr)
		require.Equal(t, OnboardingCompleted, state.Status)
	})
}
