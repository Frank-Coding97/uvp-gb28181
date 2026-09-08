package playauth

import (
	"context"
	"errors"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"uvplatform.cn/uvp-gb28181/app/openapi/processauthority"
)

const deviceOperationAdmissionTimeout = 5 * time.Second

var (
	ErrDeviceOperationUnavailable = errors.New("device operation barrier unavailable")
	ErrDeviceOperationInvalid     = errors.New("invalid device operation")
	ErrDeviceOperationRevoked     = errors.New("device operation revoked by device transfer")
)

// DeviceOperationLease keeps one admitted device operation visible to the
// process-local transfer barrier until the caller has completed all real media
// side effects and compensation. A transfer cancellation only cancels the
// lease context; it never removes the lease from the barrier.
type DeviceOperationLease interface {
	Context() context.Context
	OperationEpoch() int64
	Release()
}

// DeviceTransferGuard blocks new operations for one resolved device primary
// key while its caller performs the assignment transaction. Commit is called
// only after that transaction is durably committed.
type DeviceTransferGuard interface {
	Commit(newEpoch int64)
	Release()
}

// DeviceOperationBarrier is an application-instance coordination primitive.
// The database remains the authority for device identity, epochs, cleanup and
// legacy cutoff; lanes only serialize this process's admission and transfer
// notifications for one device primary key.
type DeviceOperationBarrier struct {
	store     *DeviceSecurityStore
	authority deviceIntentAuthority

	mu    sync.Mutex
	lanes map[uint]*deviceOperationLane

	rtpCleanupMu     sync.Mutex
	rtpCleanupOwners map[string]*RTPRecoveryWork
}

type deviceOperationLane struct {
	pk uint

	mu        sync.Mutex
	notify    chan struct{}
	guardHeld bool
	refs      int
	active    map[*deviceOperationLease]struct{}
}

type deviceOperationLaneRef struct {
	barrier *DeviceOperationBarrier
	lane    *deviceOperationLane
	once    sync.Once
}

type deviceOperationLease struct {
	lane        *deviceOperationLane
	ref         *deviceOperationLaneRef
	deviceCode  string
	epoch       int64
	ctx         context.Context
	cancel      context.CancelCauseFunc
	releaseOnce sync.Once
}

type deviceTransferGuard struct {
	lane      *deviceOperationLane
	ref       *deviceOperationLaneRef
	mu        sync.Mutex
	released  bool
	committed bool
}

type deviceOperationRow struct {
	ID                    *uint      `gorm:"column:id"`
	DeviceID              *string    `gorm:"column:device_id"`
	AccessEpoch           *int64     `gorm:"column:access_epoch"`
	CleanupCompletedEpoch *int64     `gorm:"column:cleanup_completed_epoch"`
	LegacyRevokedBefore   *time.Time `gorm:"column:legacy_revoked_before"`
}

// NewDeviceOperationBarrier supports observation and transfer coordination,
// but cannot admit ordinary operations. Cleanup tickets carry their own
// concrete authority and still require a fenced transaction.
func NewDeviceOperationBarrier(store *DeviceSecurityStore) *DeviceOperationBarrier {
	return newDeviceOperationBarrier(store, nil)
}

// NewAuthorizedDeviceOperationBarrier borrows the root's single authority.
// Every admission transaction rechecks it before taking the device row lock.
func NewAuthorizedDeviceOperationBarrier(store *DeviceSecurityStore, authority *processauthority.Authority) (*DeviceOperationBarrier, error) {
	if store == nil || store.db == nil || !validDeviceProcessAuthority(authority) {
		return nil, ErrDeviceOperationUnavailable
	}
	return newDeviceOperationBarrier(store, authority), nil
}

func newDeviceOperationBarrier(store *DeviceSecurityStore, authority deviceIntentAuthority) *DeviceOperationBarrier {
	return &DeviceOperationBarrier{store: store, authority: authority, lanes: make(map[uint]*deviceOperationLane)}
}

// WithDeviceOperationBarrier wires the process-local operation barrier into a
// queued authorization service. The option does not create a fallback barrier
// because a missing durable authority must remain fail-closed.
func WithDeviceOperationBarrier(barrier *DeviceOperationBarrier) AuthorizationServiceOption {
	return func(service *AuthorizationService) {
		if service != nil {
			service.operationBarrier = barrier
		}
	}
}

// BeginQueuedOperation admits the immutable queued registry snapshot. v2 uses
// only the issued-at timestamp stored in that snapshot; v4 uses only its
// stored device epoch. Caller fields are rechecked against the same registry
// record and are never upgraded or written back.
func (s *AuthorizationService) BeginQueuedOperation(ctx context.Context, queued QueuedAuthorization) (DeviceOperationLease, error) {
	if s == nil || s.operationBarrier == nil {
		return nil, ErrDeviceOperationUnavailable
	}
	if err := requireAuthorizationContext(ctx); err != nil {
		return nil, err
	}
	admissionCtx, cancel := context.WithTimeout(ctx, deviceOperationAdmissionTimeout)
	defer cancel()
	snapshot, err := s.captureQueuedAuthorization(admissionCtx, queued, 0, false)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeQueuedRecord(admissionCtx, snapshot.record); err != nil {
		return nil, err
	}
	if err := s.recheckQueuedAuthorization(admissionCtx, queued, snapshot, 0, false); err != nil {
		return nil, err
	}

	var lease DeviceOperationLease
	var beginErr error
	switch snapshot.record.binding.version {
	case tokenVersionV2:
		issuedAt := snapshot.record.issuedAt.Unix()
		if issuedAt <= 0 {
			return nil, ErrAuthorizationClaimsMismatch
		}
		lease, beginErr = s.operationBarrier.beginLegacyWithWait(ctx, admissionCtx, snapshot.record.binding.deviceID, issuedAt)
	case tokenVersionV4:
		epoch := snapshot.record.binding.deviceEpoch
		if epoch <= 0 {
			return nil, ErrAuthorizationDeviceEpoch
		}
		lease, beginErr = s.operationBarrier.beginEpochWithWait(ctx, admissionCtx, snapshot.record.binding.deviceID, epoch)
	default:
		return nil, ErrAuthorizationClaimsMismatch
	}
	if beginErr != nil {
		return nil, beginErr
	}
	if err := s.recheckQueuedAuthorization(admissionCtx, queued, snapshot, 0, false); err != nil {
		lease.Release()
		return nil, err
	}
	return lease, nil
}

// BeginEpoch admits a v4-style operation against the exact expected epoch.
// The initial lookup resolves only the unique positive primary key so the
// process-local lane cannot be selected by a mutable device-code identity.
func (b *DeviceOperationBarrier) BeginEpoch(ctx context.Context, deviceCode string, expectedEpoch int64) (DeviceOperationLease, error) {
	if err := b.validateBegin(ctx, deviceCode, expectedEpoch); err != nil {
		return nil, err
	}
	waitCtx, cancel := context.WithTimeout(ctx, deviceOperationAdmissionTimeout)
	defer cancel()
	return b.beginEpochWithWait(ctx, waitCtx, deviceCode, expectedEpoch)
}

func (b *DeviceOperationBarrier) beginEpochWithWait(ctx, waitCtx context.Context, deviceCode string, expectedEpoch int64) (DeviceOperationLease, error) {
	if err := b.validateBegin(ctx, deviceCode, expectedEpoch); err != nil {
		return nil, err
	}
	if isNilInterface(waitCtx) {
		return nil, ErrDeviceOperationUnavailable
	}
	if err := waitCtx.Err(); err != nil {
		return nil, err
	}
	devicePK, err := b.resolveDevicePK(waitCtx, deviceCode)
	if err != nil {
		return nil, normalizeDeviceOperationError(err, waitCtx)
	}
	return b.beginOnLane(ctx, waitCtx, devicePK, deviceCode, func(row deviceOperationRow, epoch int64) error {
		if epoch != expectedEpoch {
			return ErrDeviceOperationRevoked
		}
		return nil
	})
}

// AuthorizeEpoch performs a side-effect-free preflight for callers that need
// to decide whether a queued operation may proceed before acquiring an owner
// lease. It delegates the durable check to the concrete security store; it
// does not select a lane, return an epoch, or allocate operation resources.
func (b *DeviceOperationBarrier) AuthorizeEpoch(ctx context.Context, deviceCode string, expectedEpoch int64) error {
	if isNilInterface(ctx) {
		return ErrDeviceOperationUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if b == nil || b.store == nil || b.store.db == nil {
		return ErrDeviceOperationUnavailable
	}
	if !validGBID(deviceCode) || expectedEpoch <= 0 {
		return ErrDeviceOperationInvalid
	}
	return normalizeDeviceOperationError(b.store.AuthorizeEpoch(ctx, deviceCode, expectedEpoch), ctx)
}

// beginLegacy is intentionally private. Only the trusted registry snapshot
// may supply a v2 issued-at value; arbitrary callers cannot bypass the queued
// authorization path by exporting a legacy timestamp API.
func (b *DeviceOperationBarrier) beginLegacy(ctx context.Context, deviceCode string, issuedAt int64) (DeviceOperationLease, error) {
	if isNilInterface(ctx) {
		return nil, ErrDeviceOperationUnavailable
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if b == nil || b.store == nil || b.store.db == nil {
		return nil, ErrDeviceOperationUnavailable
	}
	if !validGBID(deviceCode) || issuedAt <= 0 {
		return nil, ErrDeviceOperationInvalid
	}
	waitCtx, cancel := context.WithTimeout(ctx, deviceOperationAdmissionTimeout)
	defer cancel()
	return b.beginLegacyWithWait(ctx, waitCtx, deviceCode, issuedAt)
}

func (b *DeviceOperationBarrier) beginLegacyWithWait(ctx, waitCtx context.Context, deviceCode string, issuedAt int64) (DeviceOperationLease, error) {
	if isNilInterface(ctx) || isNilInterface(waitCtx) {
		return nil, ErrDeviceOperationUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := waitCtx.Err(); err != nil {
		return nil, err
	}
	if b == nil || b.store == nil || b.store.db == nil || !validDeviceProcessAuthority(b.authority) {
		return nil, ErrDeviceOperationUnavailable
	}
	if !validGBID(deviceCode) || issuedAt <= 0 {
		return nil, ErrDeviceOperationInvalid
	}
	devicePK, err := b.resolveDevicePK(waitCtx, deviceCode)
	if err != nil {
		return nil, normalizeDeviceOperationError(err, waitCtx)
	}
	return b.beginOnLane(ctx, waitCtx, devicePK, deviceCode, func(row deviceOperationRow, _ int64) error {
		if row.LegacyRevokedBefore != nil && issuedAt < row.LegacyRevokedBefore.Unix() {
			return ErrTokenRevoked
		}
		return nil
	})
}

func (b *DeviceOperationBarrier) validateBegin(ctx context.Context, deviceCode string, expectedEpoch int64) error {
	if isNilInterface(ctx) {
		return ErrDeviceOperationUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if b == nil || b.store == nil || b.store.db == nil || !validDeviceProcessAuthority(b.authority) {
		return ErrDeviceOperationUnavailable
	}
	if !validGBID(deviceCode) || expectedEpoch <= 0 {
		return ErrDeviceOperationInvalid
	}
	return nil
}

func (b *DeviceOperationBarrier) resolveDevicePK(ctx context.Context, deviceCode string) (uint, error) {
	var rows []struct {
		ID *uint `gorm:"column:id"`
	}
	result := b.store.db.WithContext(ctx).Table("gb_device").
		Select("id").Where("device_id = ? AND deleted_at IS NULL", deviceCode).Limit(2).Find(&rows)
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected != 1 || len(rows) != 1 || rows[0].ID == nil || *rows[0].ID == 0 {
		return 0, ErrDeviceOperationUnavailable
	}
	return *rows[0].ID, nil
}

func (b *DeviceOperationBarrier) beginOnLane(ctx, waitCtx context.Context, devicePK uint, deviceCode string, validate func(deviceOperationRow, int64) error) (DeviceOperationLease, error) {
	lane, ref := b.acquireLane(devicePK)
	lane.mu.Lock()
	if err := waitLaneAvailableLocked(lane, waitCtx); err != nil {
		lane.mu.Unlock()
		ref.release()
		return nil, normalizeDeviceOperationError(err, waitCtx)
	}
	lane.guardHeld = true
	lane.mu.Unlock()

	var (
		row   deviceOperationRow
		epoch int64
	)
	err := b.store.db.WithContext(waitCtx).Transaction(func(tx *gorm.DB) error {
		if !validDeviceProcessAuthority(b.authority) || b.authority.CheckTx(tx) != nil {
			return ErrDeviceOperationUnavailable
		}
		loaded, err := loadDeviceOperationRow(tx.WithContext(waitCtx), devicePK, deviceCode)
		if err != nil {
			return err
		}
		row = loaded
		epoch, err = validateDeviceOperationRow(row, deviceCode, devicePK)
		if err != nil {
			return err
		}
		return validate(row, epoch)
	})
	if err != nil {
		b.releaseAdmissionGate(lane)
		ref.release()
		return nil, normalizeDeviceOperationError(err, waitCtx)
	}
	if err := waitCtx.Err(); err != nil {
		b.releaseAdmissionGate(lane)
		ref.release()
		return nil, err
	}
	lease := newDeviceOperationLease(ctx, lane, ref, epoch)
	lease.deviceCode = deviceCode
	lane.mu.Lock()
	lane.active[lease] = struct{}{}
	lane.guardHeld = false
	signalDeviceOperationLaneLocked(lane)
	lane.mu.Unlock()
	return lease, nil
}

func loadDeviceOperationRow(tx *gorm.DB, devicePK uint, deviceCode string) (deviceOperationRow, error) {
	var rows []deviceOperationRow
	result := lockedDeviceOperationTable(tx).
		Select("id, device_id, access_epoch, cleanup_completed_epoch, legacy_revoked_before").
		Where("id = ? AND device_id = ? AND deleted_at IS NULL", devicePK, deviceCode).
		Limit(2).Find(&rows)
	if result.Error != nil {
		return deviceOperationRow{}, result.Error
	}
	if result.RowsAffected != 1 || len(rows) != 1 {
		return deviceOperationRow{}, ErrDeviceOperationUnavailable
	}
	return rows[0], nil
}

func validateDeviceOperationRow(row deviceOperationRow, deviceCode string, devicePK uint) (int64, error) {
	if row.ID == nil || *row.ID != devicePK || row.DeviceID == nil || *row.DeviceID != deviceCode || row.AccessEpoch == nil || *row.AccessEpoch <= 0 || row.CleanupCompletedEpoch == nil || *row.CleanupCompletedEpoch <= 0 || *row.CleanupCompletedEpoch != *row.AccessEpoch {
		return 0, ErrDeviceOperationUnavailable
	}
	if row.LegacyRevokedBefore != nil {
		cutoff := row.LegacyRevokedBefore.UTC()
		if cutoff.Unix() <= 0 || cutoff.Nanosecond() != 0 {
			return 0, ErrDeviceOperationUnavailable
		}
	}
	return *row.AccessEpoch, nil
}

func lockedDeviceOperationTable(tx *gorm.DB) *gorm.DB {
	if tx != nil && tx.Dialector.Name() == "sqlserver" {
		return tx.Table("gb_device WITH (UPDLOCK, HOLDLOCK)")
	}
	return tx.Table("gb_device").Clauses(clause.Locking{Strength: "UPDATE"})
}

// LockTransfer reserves the PK lane so the assignment caller can commit its
// owner/epoch transaction without a concurrent admission. It performs no DB
// write and does not itself imply a committed transfer.
func (b *DeviceOperationBarrier) LockTransfer(ctx context.Context, devicePK uint) (DeviceTransferGuard, error) {
	if isNilInterface(ctx) {
		return nil, ErrDeviceOperationUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if b == nil || b.store == nil || b.store.db == nil {
		return nil, ErrDeviceOperationUnavailable
	}
	if devicePK == 0 {
		return nil, ErrDeviceOperationInvalid
	}
	lane, ref := b.acquireLane(devicePK)
	lane.mu.Lock()
	if err := waitLaneAvailableLocked(lane, ctx); err != nil {
		lane.mu.Unlock()
		ref.release()
		return nil, normalizeDeviceOperationError(err, ctx)
	}
	if err := ctx.Err(); err != nil {
		lane.mu.Unlock()
		ref.release()
		return nil, err
	}
	lane.guardHeld = true
	lane.mu.Unlock()
	return &deviceTransferGuard{lane: lane, ref: ref}, nil
}

// WaitBefore waits for every older active lease to call Release. A canceled
// lease remains active until that real release, so this method never treats a
// transfer cancellation, restart, or empty in-memory registry as completion.
func (b *DeviceOperationBarrier) WaitBefore(ctx context.Context, devicePK uint, targetEpoch int64) error {
	if isNilInterface(ctx) {
		return ErrDeviceOperationUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if b == nil || b.store == nil || b.store.db == nil {
		return ErrDeviceOperationUnavailable
	}
	if devicePK == 0 || targetEpoch <= 0 {
		return ErrDeviceOperationInvalid
	}
	lane, ref := b.acquireLane(devicePK)
	defer ref.release()
	for {
		lane.mu.Lock()
		var wait <-chan struct{}
		oldActive := false
		for lease := range lane.active {
			if lease.epoch < targetEpoch {
				oldActive = true
				wait = lane.notify
				break
			}
		}
		if !oldActive {
			lane.mu.Unlock()
			return nil
		}
		lane.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-wait:
		}
	}
}

func (b *DeviceOperationBarrier) acquireLane(devicePK uint) (*deviceOperationLane, *deviceOperationLaneRef) {
	b.mu.Lock()
	if b.lanes == nil {
		b.lanes = make(map[uint]*deviceOperationLane)
	}
	lane := b.lanes[devicePK]
	if lane == nil {
		lane = &deviceOperationLane{pk: devicePK, notify: make(chan struct{}), active: make(map[*deviceOperationLease]struct{})}
		b.lanes[devicePK] = lane
	}
	lane.refs++
	b.mu.Unlock()
	return lane, &deviceOperationLaneRef{barrier: b, lane: lane}
}

func (ref *deviceOperationLaneRef) release() {
	if ref == nil || ref.barrier == nil || ref.lane == nil {
		return
	}
	ref.once.Do(func() {
		b := ref.barrier
		b.mu.Lock()
		if ref.lane.refs > 0 {
			ref.lane.refs--
		}
		if ref.lane.refs == 0 {
			ref.lane.mu.Lock()
			empty := !ref.lane.guardHeld && len(ref.lane.active) == 0
			ref.lane.mu.Unlock()
			if empty && b.lanes[ref.lane.pk] == ref.lane {
				delete(b.lanes, ref.lane.pk)
			}
		}
		b.mu.Unlock()
	})
}

func waitLaneAvailableLocked(lane *deviceOperationLane, ctx context.Context) error {
	for lane.guardHeld {
		wait := lane.notify
		lane.mu.Unlock()
		select {
		case <-ctx.Done():
			lane.mu.Lock()
			return ctx.Err()
		case <-wait:
			lane.mu.Lock()
		}
	}
	return nil
}

func signalDeviceOperationLaneLocked(lane *deviceOperationLane) {
	close(lane.notify)
	lane.notify = make(chan struct{})
}

func (b *DeviceOperationBarrier) releaseAdmissionGate(lane *deviceOperationLane) {
	if lane == nil {
		return
	}
	lane.mu.Lock()
	if lane.guardHeld {
		lane.guardHeld = false
		signalDeviceOperationLaneLocked(lane)
	}
	lane.mu.Unlock()
}

func newDeviceOperationLease(parent context.Context, lane *deviceOperationLane, ref *deviceOperationLaneRef, epoch int64) *deviceOperationLease {
	operationCtx, cancel := context.WithCancelCause(parent)
	return &deviceOperationLease{lane: lane, ref: ref, epoch: epoch, ctx: operationCtx, cancel: cancel}
}

func (lease *deviceOperationLease) Context() context.Context {
	if lease == nil {
		return nil
	}
	return lease.ctx
}

func (lease *deviceOperationLease) OperationEpoch() int64 {
	if lease == nil {
		return 0
	}
	return lease.epoch
}

func (lease *deviceOperationLease) Release() {
	if lease == nil {
		return
	}
	lease.releaseOnce.Do(func() {
		lease.lane.mu.Lock()
		delete(lease.lane.active, lease)
		signalDeviceOperationLaneLocked(lease.lane)
		lease.lane.mu.Unlock()
		lease.cancel(nil)
		lease.ref.release()
	})
}

func (guard *deviceTransferGuard) Commit(newEpoch int64) {
	if guard == nil || guard.lane == nil {
		return
	}
	guard.mu.Lock()
	defer guard.mu.Unlock()
	if guard.released || guard.committed {
		return
	}
	guard.committed = true
	guard.lane.mu.Lock()
	cancel := make([]context.CancelCauseFunc, 0, len(guard.lane.active))
	for lease := range guard.lane.active {
		if lease.epoch < newEpoch {
			cancel = append(cancel, lease.cancel)
		}
	}
	guard.lane.mu.Unlock()
	for _, revoke := range cancel {
		revoke(ErrDeviceOperationRevoked)
	}
}

func (guard *deviceTransferGuard) Release() {
	if guard == nil || guard.lane == nil {
		return
	}
	guard.mu.Lock()
	if guard.released {
		guard.mu.Unlock()
		return
	}
	guard.released = true
	guard.mu.Unlock()
	guard.lane.mu.Lock()
	if guard.lane.guardHeld {
		guard.lane.guardHeld = false
		signalDeviceOperationLaneLocked(guard.lane)
	}
	guard.lane.mu.Unlock()
	guard.ref.release()
}

func normalizeDeviceOperationError(err error, ctx context.Context) error {
	if err == nil {
		return nil
	}
	if ctx != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
	}
	switch {
	case errors.Is(err, ErrDeviceOperationUnavailable), errors.Is(err, ErrDeviceOperationInvalid), errors.Is(err, ErrDeviceOperationRevoked), errors.Is(err, ErrTokenRevoked), errors.Is(err, ErrAuthorizationClaimsMismatch), errors.Is(err, ErrAuthorizationDeviceEpoch), errors.Is(err, ErrAuthorizationNotFound), errors.Is(err, ErrAuthorizationExpired), errors.Is(err, ErrAuthorizationTerminal), errors.Is(err, ErrAuthorizationLifetimeTooShort):
		return err
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	default:
		return ErrDeviceOperationUnavailable
	}
}
