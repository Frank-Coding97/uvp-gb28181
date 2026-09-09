package workrecording

import (
	"context"
	"errors"
	"sync"

	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// MediaTarget is resolved by the server, never accepted from a browser. The
// pin must protect the live source generation, not just the stream name.
type MediaTarget struct {
	NodeID             int64
	VHost, App, Stream string
	Generation         uint64
	RecordingRoot      string
}

func (m MediaTarget) valid() bool {
	return m.NodeID > 0 && m.VHost != "" && len(m.VHost) <= 128 && m.App != "" && len(m.App) <= 64 && m.Stream != "" && len(m.Stream) <= 64 && m.Generation > 0
}
func (m MediaTarget) key() string { return MediaResource(m.NodeID, m.VHost, m.App, m.Stream) }
func targetOf(c *models.GbRecorderClaim) MediaTarget {
	return MediaTarget{NodeID: c.NodeID, VHost: c.VHost, App: c.App, Stream: c.Stream, Generation: c.Generation, RecordingRoot: c.RecordingRoot}
}

type RecorderHandle struct {
	ChannelID uint
	Owner     Owner
	Version   uint64
}
type MP4Client interface {
	IsRecording(context.Context, string, string, string) (bool, error)
	StartRecord(context.Context, string, string, string, int) error
	StopRecord(context.Context, string, string, string) error
}

type directoryMP4Client interface {
	StartMP4RecordInDirectory(context.Context, string, string, string, int, string) error
}

// Recorder serializes actual MP4 operations in one active control process.
// Every existing writer must use this same instance before work recording can
// be exposed. Persistent claims alone are not multi-process ZLM fencing.
type Recorder struct {
	claims *Claims
	pin    func(context.Context, MediaTarget) (func(), error)
	client func(MediaTarget) (MP4Client, error)
	locks  [64]sync.Mutex
}

func NewRecorder(claims *Claims, pin func(context.Context, MediaTarget) (func(), error), client func(MediaTarget) (MP4Client, error)) *Recorder {
	return &Recorder{claims: claims, pin: pin, client: client}
}
func (r *Recorder) lock(id uint) func() {
	m := &r.locks[id%uint(len(r.locks))]
	m.Lock()
	return m.Unlock
}
func (r *Recorder) Reserve(ctx context.Context, id uint, owner Owner) (RecorderHandle, error) {
	h := RecorderHandle{ChannelID: id, Owner: owner}
	if id == 0 {
		return h, ErrInvalidRequest
	}
	unlock := r.lock(id)
	defer unlock()
	err := r.claims.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		claims := NewClaims(tx)
		// Preserve the bound generation on idempotent reserve; never use a current
		// owner's handle as authorization for a different owner or a delayed stop.
		generation := uint64(0)
		existing, err := claims.Get(ctx, ChannelResource(id))
		if err == nil && existing.State != StateIdle {
			generation = existing.Generation
		} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		row, err := claims.Acquire(ctx, ChannelResource(id), owner, generation)
		if err != nil {
			return err
		}
		if err = tx.Model(row).Update("channel_id", id).Error; err != nil {
			return err
		}
		h.Version = row.Version
		return nil
	})
	return h, err
}
func (r *Recorder) owned(ctx context.Context, h RecorderHandle) (*models.GbRecorderClaim, error) {
	if h.ChannelID == 0 {
		return nil, ErrInvalidRequest
	}
	return r.claims.owned(ctx, ChannelResource(h.ChannelID), h.Owner, h.Version)
}
func (r *Recorder) bind(ctx context.Context, h RecorderHandle, target MediaTarget) (RecorderHandle, error) {
	err := r.claims.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		claims := NewClaims(tx)
		row, err := claims.owned(ctx, ChannelResource(h.ChannelID), h.Owner, h.Version)
		if err != nil {
			return err
		}
		if row.State != StateStarting {
			return ErrVersionConflict
		}
		if row.Stream != "" && targetOf(row) != target {
			return ErrVersionConflict
		}
		media, err := claims.Acquire(ctx, target.key(), h.Owner, target.Generation)
		if err != nil {
			return err
		}
		if media.State != StateStarting || (media.ChannelID != 0 && media.ChannelID != h.ChannelID) {
			return ErrOwnerConflict
		}
		values := map[string]any{"channel_id": h.ChannelID, "node_id": target.NodeID, "v_host": target.VHost, "app": target.App, "stream": target.Stream, "generation": target.Generation, "recording_root": target.RecordingRoot}
		if err = tx.Model(media).Updates(values).Error; err != nil {
			return err
		}
		values["version"] = h.Version + 1
		result := tx.Model(&models.GbRecorderClaim{}).Where("resource_key = ? AND version = ?", row.ResourceKey, h.Version).Updates(values)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrVersionConflict
		}
		return nil
	})
	if err == nil {
		h.Version++
	}
	return h, err
}

// pairState commits channel and media facts together. A crash or failed write
// never releases one resource while the other remains potentially recording.
func (r *Recorder) pairState(ctx context.Context, h RecorderHandle, state string, quarantineOwner bool) (RecorderHandle, error) {
	err := r.claims.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		claims := NewClaims(tx)
		row, err := claims.owned(ctx, ChannelResource(h.ChannelID), h.Owner, h.Version)
		if err != nil {
			return err
		}
		target := targetOf(row)
		if !target.valid() {
			return ErrInvalidRequest
		}
		media, err := claims.Get(ctx, target.key())
		if err != nil {
			return err
		}
		if media.OwnerKind != h.Owner.Kind || media.OwnerID != h.Owner.ID || media.ChannelID != h.ChannelID || targetOf(media) != target {
			return ErrOwnerConflict
		}
		for _, current := range []*models.GbRecorderClaim{row, media} {
			values := map[string]any{"state": state, "version": current.Version + 1}
			if quarantineOwner {
				values["owner_kind"] = OwnerLegacy
				values["owner_id"] = "media:" + target.key()
			}
			result := tx.Model(&models.GbRecorderClaim{}).Where("resource_key = ? AND version = ?", current.ResourceKey, current.Version).Updates(values)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrVersionConflict
			}
		}
		return nil
	})
	if err == nil {
		h.Version++
	}
	return h, err
}

// pinnedClient requires an atomic generation check plus a hold that prevents
// all local teardown/replacement until release. A snapshot bool is insufficient:
// ZLM startRecord/stopRecord have no generation parameter. This short pin does
// not replace the owner's long-lived source lease during recording/unknown.
func (r *Recorder) pinnedClient(ctx context.Context, target MediaTarget) (MP4Client, func(), error) {
	if r.pin == nil || r.client == nil {
		return nil, nil, ErrInvalidRequest
	}
	release, err := r.pin(ctx, target)
	if err != nil {
		return nil, nil, err
	}
	if release == nil {
		return nil, nil, ErrInvalidRequest
	}
	client, err := r.client(target)
	if err != nil || client == nil {
		release()
		return nil, nil, errors.Join(ErrInvalidRequest, err)
	}
	return client, release, nil
}
func (r *Recorder) Start(ctx context.Context, h RecorderHandle, target MediaTarget, maxSecond int) (RecorderHandle, error) {
	if !target.valid() || maxSecond < 0 {
		return h, ErrInvalidRequest
	}
	if h.Owner.Kind == OwnerWork && !ownsWorkDirectory(target.RecordingRoot, h.Owner.ID) {
		return h, ErrInvalidRequest
	}
	unlock := r.lock(h.ChannelID)
	defer unlock()
	var err error
	h, err = r.bind(ctx, h, target)
	if err != nil {
		return h, err
	}
	client, release, err := r.pinnedClient(ctx, target)
	if err != nil {
		return h, err
	}
	defer release()
	directoryClient, supportsDirectory := client.(directoryMP4Client)
	if h.Owner.Kind == OwnerWork && !supportsDirectory {
		return h, ErrInvalidRequest
	}
	active, err := client.IsRecording(ctx, target.VHost, target.App, target.Stream)
	if err != nil {
		return h, err
	}
	if active {
		h, err = r.pairState(ctx, h, StateUnknown, true)
		return h, errors.Join(ErrAttributionUnknown, err)
	}
	// Unknown is persisted BEFORE the external write. Repeating Start with this
	// handle is prohibited even if a response or subsequent DB write is lost.
	h, err = r.pairState(ctx, h, StateUnknown, false)
	if err != nil {
		return h, err
	}
	if h.Owner.Kind == OwnerWork {
		err = directoryClient.StartMP4RecordInDirectory(ctx, target.VHost, target.App, target.Stream, maxSecond, target.RecordingRoot)
	} else {
		err = client.StartRecord(ctx, target.VHost, target.App, target.Stream, maxSecond)
	}
	if err != nil {
		return h, err
	}
	active, err = client.IsRecording(ctx, target.VHost, target.App, target.Stream)
	if err != nil {
		return h, err
	}
	if !active {
		return h, ErrAttributionUnknown
	}
	return r.pairState(ctx, h, StateRecording, false)
}
func (r *Recorder) Stop(ctx context.Context, h RecorderHandle) (RecorderHandle, error) {
	unlock := r.lock(h.ChannelID)
	defer unlock()
	row, err := r.owned(ctx, h)
	if err != nil {
		return h, err
	}
	if row.State == StateStopped {
		return h, nil
	}
	if row.State != StateRecording && row.State != StateUnknown && row.State != StateStopping {
		return h, ErrVersionConflict
	}
	target := targetOf(row)
	if !target.valid() {
		return h, ErrInvalidRequest
	}
	client, release, err := r.pinnedClient(ctx, target)
	if err != nil {
		return h, err
	}
	defer release()
	h, err = r.pairState(ctx, h, StateStopping, false)
	if err != nil {
		return h, err
	}
	if err = client.StopRecord(ctx, target.VHost, target.App, target.Stream); err != nil {
		next, writeErr := r.pairState(ctx, h, StateUnknown, false)
		return next, errors.Join(err, writeErr)
	}
	active, err := client.IsRecording(ctx, target.VHost, target.App, target.Stream)
	if err != nil || active {
		next, writeErr := r.pairState(ctx, h, StateUnknown, false)
		return next, errors.Join(ErrAttributionUnknown, err, writeErr)
	}
	// File completion remains independent. ReleaseStopped is a separate atomic
	// operation so the caller can persist the stop fact before yielding ownership.
	return r.pairState(ctx, h, StateStopped, false)
}

// ReleaseStopped yields both recorder resources after confirmed stop. Work
// files remain attached to their original, durable unique job directory.
func (r *Recorder) ReleaseStopped(ctx context.Context, h RecorderHandle) error {
	unlock := r.lock(h.ChannelID)
	defer unlock()
	return r.claims.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		claims := NewClaims(tx)
		row, err := claims.Get(ctx, ChannelResource(h.ChannelID))
		if err != nil {
			return err
		}
		if row.State == StateIdle && row.Version == h.Version+1 {
			return nil
		}
		row, err = claims.owned(ctx, row.ResourceKey, h.Owner, h.Version)
		if err != nil {
			return err
		}
		if row.State != StateStopped {
			return ErrVersionConflict
		}
		if h.Owner.Kind == OwnerWork && !ownsWorkDirectory(row.RecordingRoot, h.Owner.ID) {
			return ErrInvalidRequest
		}
		target := targetOf(row)
		if !target.valid() {
			return ErrInvalidRequest
		}
		media, err := claims.Get(ctx, target.key())
		if err != nil {
			return err
		}
		if media.ChannelID != h.ChannelID || targetOf(media) != target {
			return ErrOwnerConflict
		}
		if err := claims.Release(ctx, media.ResourceKey, h.Owner, media.Version); err != nil {
			return err
		}
		return claims.Release(ctx, row.ResourceKey, h.Owner, h.Version)
	})
}
