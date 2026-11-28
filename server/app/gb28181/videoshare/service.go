package videoshare

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/utils/authoritytx"
)

var (
	ErrNotFound = errors.New("video share not found")
	ErrConflict = errors.New("video share revision conflict")
	ErrExpired  = errors.New("video share expired or revoked")
)

type Service struct {
	db                *gorm.DB
	signer            *Signer
	ttl               func() time.Duration
	now               func() time.Time
	random            func([]byte) error
	authorize         Authorizer
	viewerMu          sync.Mutex
	viewers           map[[32]byte]*viewerSession
	prepare           PlaybackPreparer
	viewerEpoch       uint64
	viewerIdleTimeout time.Duration
}

type AuthorizationSnapshot struct {
	DevicePK    uint
	DeviceID    string
	DeviceEpoch uint64
}

// Authorizer checks share management, live playback, and target visibility in
// the supplied transaction. It is also used to revalidate the link creator.
type Authorizer func(context.Context, *gorm.DB, uint, uint) (AuthorizationSnapshot, error)

func NewService(db *gorm.DB, signer *Signer, ttl func() time.Duration, authorize Authorizer) (*Service, error) {
	if db == nil || signer == nil || ttl == nil || authorize == nil {
		return nil, errors.New("video share service requires database, signer, ttl, and authorizer")
	}
	return &Service{db: db, signer: signer, ttl: ttl, now: time.Now, random: func(b []byte) error { _, err := rand.Read(b); return err }, authorize: authorize, viewers: make(map[[32]byte]*viewerSession)}, nil
}

type Link struct {
	Expired    bool       `json:"expired"`
	Enabled    bool       `json:"enabled"`
	PublicID   string     `json:"publicId,omitempty"`
	Signature  string     `json:"signature,omitempty"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
	Pending    bool       `json:"pending"`
	Generation string     `json:"generation,omitempty"`
	Revision   uint64     `json:"revision"`
	ServerTime time.Time  `json:"serverTime"`
}

// Status checks current management and target visibility without returning a
// bearer signature. A missing row is the normal disabled state.
func (s *Service) Status(ctx context.Context, channelPK uint, actorID uint) (Link, error) {
	snapshot, err := s.authorize(ctx, s.db, actorID, channelPK)
	if err != nil {
		return Link{}, err
	}
	var share gbmodels.GbChannelVideoShare
	query := s.db.WithContext(ctx).Where("channel_pk = ?", channelPK).Take(&share)
	err = query.Error
	if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && query.RowsAffected == 0) {
		return Link{ServerTime: s.now().UTC()}, nil
	}
	if err != nil {
		return Link{}, err
	}
	if !matchesDevice(snapshot, share.DevicePK, share.DeviceID, share.DeviceEpoch) {
		return Link{Revision: share.Revision, ServerTime: s.now().UTC()}, nil
	}
	if share.ExpiresAt != nil && !share.ExpiresAt.After(s.now()) {
		return Link{Enabled: share.Enabled && share.RevokedAt == nil, Expired: naturallyExpired(share, s.now()), ExpiresAt: share.ExpiresAt, Revision: share.Revision, ServerTime: s.now().UTC()}, nil
	}
	return Link{
		Enabled:    share.Enabled && share.RevokedAt == nil,
		PublicID:   share.PublicID,
		ExpiresAt:  share.ExpiresAt,
		Pending:    share.Enabled && share.ActivatedAt == nil,
		Generation: share.Generation,
		Revision:   share.Revision,
		ServerTime: s.now().UTC(),
	}, nil
}

// naturallyExpired distinguishes elapsed links from manual and authority revocations.
func naturallyExpired(share gbmodels.GbChannelVideoShare, now time.Time) bool {
	return share.RevokeReason == "expired" || (share.RevokedAt == nil && share.ExpiresAt != nil && !share.ExpiresAt.After(now))
}

// EnsurePending creates an automatically enabled share without issuing a link
// or starting its expiry timer. Existing state is never reopened by catalog sync.
func (s *Service) EnsurePending(ctx context.Context, channelPK, devicePK uint, deviceID string, deviceEpoch uint64, policyActorID *uint, policyRevision uint64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return EnsurePendingInTx(ctx, tx, channelPK, devicePK, deviceID, deviceEpoch, policyActorID, policyRevision)
	})
}

// EnsurePendingInTx seeds the automatic pending state at the same transaction
// that creates a channel. A conflict preserves any existing lifecycle state.
func EnsurePendingInTx(ctx context.Context, tx *gorm.DB, channelPK, devicePK uint, deviceID string, deviceEpoch uint64, policyActorID *uint, policyRevision uint64) error {
	shareService := &Service{now: time.Now, random: func(b []byte) error { _, err := rand.Read(b); return err }}
	share, err := shareService.newShare(channelPK, devicePK, deviceID, deviceEpoch, "auto", policyActorID, policyRevision, true, nil, nil)
	if err != nil {
		return err
	}
	created := tx.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "channel_pk"}}, DoNothing: true}).Create(share)
	if created.Error != nil {
		return created.Error
	}
	if created.RowsAffected == 0 {
		return nil
	}
	operatorID := uint(0)
	if policyActorID != nil {
		operatorID = *policyActorID
	}
	return RecordAuditTx(ctx, tx, "video_share.auto_pending", operatorID, share)
}

func (s *Service) SetEnabled(ctx context.Context, channelPK uint, actorID uint, enabled bool, expectedRevision uint64) (Link, error) {
	return s.setEnabled(ctx, channelPK, actorID, enabled, expectedRevision, false)
}

func (s *Service) Regenerate(ctx context.Context, channelPK uint, actorID uint, expectedRevision uint64) (Link, error) {
	return s.setEnabled(ctx, channelPK, actorID, true, expectedRevision, true)
}

func (s *Service) setEnabled(ctx context.Context, channelPK uint, actorID uint, enabled bool, expectedRevision uint64, regenerate bool) (Link, error) {
	var result Link
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := authoritytx.Acquire(ctx, tx); err != nil {
			return err
		}
		var share gbmodels.GbChannelVideoShare
		query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("channel_pk = ?", channelPK).Take(&share)
		err := query.Error
		if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && query.RowsAffected == 0) {
			if regenerate {
				return ErrNotFound
			}
			if !enabled || expectedRevision != 0 {
				return ErrConflict
			}
			var channel gbmodels.GbChannel
			if found := tx.Where("id = ?", channelPK).Take(&channel); found.Error != nil || found.RowsAffected == 0 {
				if found.Error != nil {
					return found.Error
				}
				return ErrNotFound
			}
			var device struct {
				ID          uint
				DeviceID    string
				AccessEpoch uint64
			}
			if found := tx.Table("gb_device").Select("id, device_id, access_epoch").Where("device_id = ? AND deleted_at IS NULL", channel.DeviceID).Take(&device); found.Error != nil || found.RowsAffected == 0 {
				if found.Error != nil {
					return found.Error
				}
				return ErrNotFound
			}
			snapshot, authErr := s.authorize(ctx, tx, actorID, channelPK)
			if authErr != nil {
				return authErr
			}
			if !matchesDevice(snapshot, device.ID, device.DeviceID, device.AccessEpoch) {
				return ErrExpired
			}
			now := s.now().UTC()
			duration, err := s.validTTL()
			if err != nil {
				return err
			}
			expires := now.Add(duration)
			created, createErr := s.newShare(channel.ID, device.ID, device.DeviceID, device.AccessEpoch, "manual", nil, 0, true, &actorID, &expires)
			err = createErr
			if err != nil {
				return err
			}
			share = *created
			if createErr := tx.Create(&share).Error; createErr != nil {
				return createErr
			}
			// Sign and return the exact persisted timestamps across database precisions.
			if err := tx.Where("id = ?", share.ID).Take(&share).Error; err != nil {
				return err
			}
			if err := RecordAuditTx(ctx, tx, "video_share.enabled", actorID, &share); err != nil {
				return err
			}
			return s.link(&share, &result)
		}
		if err != nil {
			return err
		}
		snapshot, authErr := s.authorize(ctx, tx, actorID, channelPK)
		if authErr != nil {
			return authErr
		}
		deviceMatches := matchesDevice(snapshot, share.DevicePK, share.DeviceID, share.DeviceEpoch)
		if share.Revision != expectedRevision {
			return ErrConflict
		}
		if !enabled {
			if !share.Enabled || share.RevokedAt != nil {
				return s.link(&share, &result)
			}
			now := s.now().UTC()
			updates := tx.Model(&gbmodels.GbChannelVideoShare{}).Where("id = ? AND revision = ?", share.ID, expectedRevision).Updates(map[string]any{
				"enabled": false, "revoked_at": now, "revoke_reason": "disabled", "revision": expectedRevision + 1,
			})
			if updates.Error != nil {
				return updates.Error
			}
			if updates.RowsAffected != 1 {
				return ErrConflict
			}
			share.Enabled, share.RevokedAt, share.RevokeReason, share.Revision = false, &now, "disabled", expectedRevision+1
			if err := RecordAuditTx(ctx, tx, "video_share.disabled", actorID, &share); err != nil {
				return err
			}
			return s.link(&share, &result)
		}
		if !regenerate && deviceMatches && share.Enabled && share.RevokedAt == nil && (share.ExpiresAt == nil || share.ExpiresAt.After(s.now())) {
			return s.link(&share, &result)
		}
		var channel gbmodels.GbChannel
		if found := tx.Where("id = ?", channelPK).Take(&channel); found.Error != nil || found.RowsAffected == 0 {
			if found.Error != nil {
				return found.Error
			}
			return ErrNotFound
		}
		var device struct {
			ID          uint
			DeviceID    string
			AccessEpoch uint64
		}
		if found := tx.Table("gb_device").Select("id, device_id, access_epoch").Where("device_id = ? AND deleted_at IS NULL", channel.DeviceID).Take(&device); found.Error != nil || found.RowsAffected == 0 {
			if found.Error != nil {
				return found.Error
			}
			return ErrNotFound
		}
		newGeneration, err := s.randomID()
		if err != nil {
			return err
		}
		now := s.now().UTC()
		duration, err := s.validTTL()
		if err != nil {
			return err
		}
		expires := now.Add(duration)
		updates := tx.Model(&gbmodels.GbChannelVideoShare{}).Where("id = ? AND revision = ?", share.ID, expectedRevision).Updates(map[string]any{
			"generation": newGeneration, "enabled": true, "activated_at": now, "expires_at": expires, "created_by": actorID,
			"device_pk": device.ID, "device_id": device.DeviceID, "device_epoch": uint64(device.AccessEpoch), "source": "manual",
			"policy_actor_id": nil, "policy_revision": 0, "key_id": KeyID, "revoked_at": nil, "revoke_reason": "", "revision": expectedRevision + 1,
		})
		if updates.Error != nil {
			return updates.Error
		}
		if updates.RowsAffected != 1 {
			return ErrConflict
		}
		if err := tx.Where("id = ?", share.ID).Take(&share).Error; err != nil {
			return err
		}
		event := "video_share.enabled"
		if regenerate {
			event = "video_share.regenerated"
		}
		if err := RecordAuditTx(ctx, tx, event, actorID, &share); err != nil {
			return err
		}
		return s.link(&share, &result)
	})
	return result, err
}

// AcquireLink activates a pending share or renews an expired link within the
// same share authorization. Requests during its TTL reproduce the same link.
func (s *Service) AcquireLink(ctx context.Context, channelPK uint, actorID uint, expectedRevision uint64) (Link, error) {
	var result Link
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := authoritytx.Acquire(ctx, tx); err != nil {
			return err
		}
		var share gbmodels.GbChannelVideoShare
		query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("channel_pk = ?", channelPK).Take(&share)
		if err := query.Error; err != nil || query.RowsAffected == 0 {
			if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && query.RowsAffected == 0) {
				return ErrNotFound
			}
			return err
		}
		if !share.Enabled || share.RevokedAt != nil {
			return ErrExpired
		}
		if share.Revision != expectedRevision {
			return ErrConflict
		}
		snapshot, authErr := s.authorize(ctx, tx, actorID, channelPK)
		if authErr != nil {
			return authErr
		}
		if share.ActivatedAt != nil && !matchesDevice(snapshot, share.DevicePK, share.DeviceID, share.DeviceEpoch) {
			return ErrExpired
		}
		if share.ExpiresAt != nil && !share.ExpiresAt.After(s.now()) {
			if share.ActivatedAt == nil || share.CreatedBy == nil {
				return ErrExpired
			}
			// A new operator may renew the credential, but cannot replace the
			// original authority that still owns every continuing viewer.
			owner, err := s.authorize(ctx, tx, *share.CreatedBy, channelPK)
			if err != nil {
				return err
			}
			if !matchesDevice(owner, share.DevicePK, share.DeviceID, share.DeviceEpoch) {
				return ErrExpired
			}
			duration, err := s.validTTL()
			if err != nil {
				return err
			}
			publicID, err := s.randomID()
			if err != nil {
				return err
			}
			now := s.now().UTC()
			expires := now.Add(duration)
			updated := tx.Model(&gbmodels.GbChannelVideoShare{}).Where("id = ? AND revision = ?", share.ID, share.Revision).Updates(map[string]any{
				"public_id": publicID, "activated_at": now, "expires_at": expires, "key_id": KeyID,
				"revoke_reason": "", "revision": share.Revision + 1,
			})
			if updated.Error != nil {
				return updated.Error
			}
			if updated.RowsAffected != 1 {
				return ErrConflict
			}
			if err := tx.Where("id = ?", share.ID).Take(&share).Error; err != nil {
				return err
			}
			if err := RecordAuditTx(ctx, tx, "video_share.renewed", actorID, &share); err != nil {
				return err
			}
			return s.link(&share, &result)
		}
		if share.ActivatedAt == nil {
			var channel gbmodels.GbChannel
			if found := tx.Where("id = ?", channelPK).Take(&channel); found.Error != nil || found.RowsAffected == 0 {
				if found.Error != nil {
					return found.Error
				}
				return ErrNotFound
			}
			var device struct {
				ID          uint
				DeviceID    string
				AccessEpoch uint64
			}
			if found := tx.Table("gb_device").Select("id, device_id, access_epoch").Where("device_id = ? AND deleted_at IS NULL", channel.DeviceID).Take(&device); found.Error != nil || found.RowsAffected == 0 {
				if found.Error != nil {
					return found.Error
				}
				return ErrNotFound
			}
			now := s.now().UTC()
			duration, err := s.validTTL()
			if err != nil {
				return err
			}
			expires := now.Add(duration)
			updated := tx.Model(&gbmodels.GbChannelVideoShare{}).Where("id = ? AND activated_at IS NULL AND revision = ?", share.ID, share.Revision).Updates(map[string]any{
				"activated_at": now, "expires_at": expires, "created_by": actorID, "device_pk": device.ID,
				"device_id": device.DeviceID, "device_epoch": uint64(device.AccessEpoch), "key_id": KeyID, "revision": share.Revision + 1,
			})
			if updated.Error != nil {
				return updated.Error
			}
			if updated.RowsAffected != 1 {
				return ErrConflict
			}
			if err := tx.Where("id = ?", share.ID).Take(&share).Error; err != nil {
				return err
			}
			if err := RecordAuditTx(ctx, tx, "video_share.activated", actorID, &share); err != nil {
				return err
			}
		}
		return s.link(&share, &result)
	})
	return result, err
}

func (s *Service) Resolve(ctx context.Context, publicID, signature string) (*gbmodels.GbChannelVideoShare, error) {
	var share gbmodels.GbChannelVideoShare
	query := s.db.WithContext(ctx).Where("public_id = ?", publicID).Take(&share)
	if err := query.Error; err != nil || query.RowsAffected == 0 {
		if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && query.RowsAffected == 0) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if !share.Enabled || share.RevokedAt != nil || share.ActivatedAt == nil || share.ExpiresAt == nil || !share.ExpiresAt.After(s.now()) {
		return nil, ErrExpired
	}
	claims := Claims{PublicID: share.PublicID, Generation: share.Generation, ExpiresAt: share.ExpiresAt.Unix(), KeyID: share.KeyID}
	if !s.signer.Verify(claims, signature) {
		return nil, ErrInvalidCredential
	}
	if share.CreatedBy == nil {
		return nil, ErrExpired
	}
	snapshot, err := s.authorize(ctx, s.db, *share.CreatedBy, share.ChannelPK)
	if err != nil {
		return nil, err
	}
	if !matchesDevice(snapshot, share.DevicePK, share.DeviceID, share.DeviceEpoch) {
		return nil, ErrExpired
	}
	return &share, nil
}

func matchesDevice(snapshot AuthorizationSnapshot, devicePK uint, deviceID string, epoch uint64) bool {
	return snapshot.DevicePK == devicePK && snapshot.DeviceID == deviceID && snapshot.DeviceEpoch == epoch
}

func (s *Service) newShare(channelPK, devicePK uint, deviceID string, deviceEpoch uint64, source string, policyActorID *uint, policyRevision uint64, enabled bool, createdBy *uint, expiresAt *time.Time) (*gbmodels.GbChannelVideoShare, error) {
	publicID, err := s.randomID()
	if err != nil {
		return nil, err
	}
	generation, err := s.randomID()
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	var activatedAt *time.Time
	if expiresAt != nil {
		activatedAt = &now
	}
	return &gbmodels.GbChannelVideoShare{
		ChannelPK: channelPK, PublicID: publicID, Generation: generation, Enabled: enabled,
		ActivatedAt: activatedAt, ExpiresAt: expiresAt, CreatedBy: createdBy, DevicePK: devicePK,
		DeviceID: deviceID, Source: source, PolicyActorID: policyActorID, PolicyRevision: policyRevision,
		DeviceEpoch: deviceEpoch, KeyID: KeyID, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (s *Service) link(share *gbmodels.GbChannelVideoShare, out *Link) error {
	*out = Link{Enabled: share.Enabled && share.RevokedAt == nil, PublicID: share.PublicID, ExpiresAt: share.ExpiresAt, Pending: share.Enabled && share.ActivatedAt == nil, Generation: share.Generation, Revision: share.Revision, ServerTime: s.now().UTC()}
	if share.ExpiresAt == nil {
		return nil
	}
	signature, err := s.signer.Sign(Claims{PublicID: share.PublicID, Generation: share.Generation, ExpiresAt: share.ExpiresAt.Unix(), KeyID: share.KeyID})
	out.Signature = signature
	return err
}

func (s *Service) randomID() (string, error) {
	b := make([]byte, 32)
	if s.random == nil {
		return "", fmt.Errorf("video-share random source unavailable")
	}
	if err := s.random(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (s *Service) validTTL() (time.Duration, error) {
	duration := s.ttl()
	if duration <= 0 || duration%time.Minute != 0 {
		return 0, errors.New("video-share expiry must be a positive whole number of minutes")
	}
	return duration, nil
}
