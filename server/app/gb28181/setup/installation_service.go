package setup

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var ErrInvalidOnboardingTransition = errors.New("invalid onboarding transition")

type InstallationState struct {
	InstanceID        string
	OnboardingVersion int
	Status            OnboardingStatus
	FinishedAt        *time.Time
}

type InstallationService struct {
	db      *gorm.DB
	now     func() time.Time
	newUUID func() string
}

func NewInstallationService(db *gorm.DB) *InstallationService {
	return &InstallationService{
		db:      db,
		now:     time.Now,
		newUUID: uuid.NewString,
	}
}

func (s *InstallationService) Current(ctx context.Context) (InstallationState, error) {
	repo := NewInstallationRepository(s.db)
	row, err := repo.Get(ctx)
	if err != nil {
		return InstallationState{}, err
	}
	if row == nil {
		return InstallationState{OnboardingVersion: 1, Status: OnboardingLegacy}, nil
	}
	if row.InstanceID == "" {
		if err := repo.SetInstanceIDIfEmpty(ctx, s.newUUID()); err != nil {
			return InstallationState{}, err
		}
		row, err = repo.Get(ctx)
		if err != nil {
			return InstallationState{}, err
		}
	}
	return installationStateFromRow(row), nil
}

func (s *InstallationService) Complete(ctx context.Context) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return s.completeWithDB(ctx, tx)
	})
}

func (s *InstallationService) completeWithDB(ctx context.Context, db *gorm.DB) error {
	return s.transition(ctx, db, OnboardingCompleted)
}

func (s *InstallationService) Skip(ctx context.Context) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return s.transition(ctx, tx, OnboardingSkipped)
	})
}

func (s *InstallationService) transition(ctx context.Context, db *gorm.DB, target OnboardingStatus) error {
	repo := NewInstallationRepository(db)
	row, err := repo.Get(ctx)
	if err != nil {
		return err
	}
	if row == nil {
		if target != OnboardingCompleted {
			return ErrInvalidOnboardingTransition
		}
		now := s.now()
		return repo.Create(ctx, &SystemInstallation{
			ID:                      SingletonID,
			InstanceID:              s.newUUID(),
			OnboardingVersion:       1,
			SIPOnboardingStatus:     OnboardingCompleted,
			SIPOnboardingFinishedAt: &now,
		})
	}

	if target == OnboardingSkipped && row.SIPOnboardingStatus != OnboardingPending && row.SIPOnboardingStatus != OnboardingSkipped {
		return ErrInvalidOnboardingTransition
	}
	if target == OnboardingCompleted && row.SIPOnboardingStatus == OnboardingCompleted {
		return nil
	}

	now := s.now()
	row.SIPOnboardingStatus = target
	row.SIPOnboardingFinishedAt = &now
	if row.InstanceID == "" {
		row.InstanceID = s.newUUID()
	}
	if row.OnboardingVersion == 0 {
		row.OnboardingVersion = 1
	}
	return repo.Save(ctx, row)
}

func installationStateFromRow(row *SystemInstallation) InstallationState {
	return InstallationState{
		InstanceID:        row.InstanceID,
		OnboardingVersion: row.OnboardingVersion,
		Status:            row.SIPOnboardingStatus,
		FinishedAt:        row.SIPOnboardingFinishedAt,
	}
}
