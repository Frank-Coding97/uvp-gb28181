package processauthority

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

var (
	ErrProcessAuthorityUnavailable    = errors.New("process authority unavailable")
	ErrProcessAuthorityDomainMismatch = errors.New("process authority domain mismatch")
)

var processID = sync.OnceValues(func() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", ErrProcessAuthorityUnavailable
	}
	id := hex.EncodeToString(raw[:])
	if !validProcessIdentity(id) {
		return "", ErrProcessAuthorityUnavailable
	}
	return id, nil
})

// ProcessID is only a durable identifier. It never grants dispatch permission.
func ProcessID() (string, error) { return processID() }

var rootRegistration registrationLatch

type registrationLatch struct {
	mu        sync.Mutex
	attempted bool
}

// Authority can only be obtained from successful, confirmed root registration.
// Copies share sealing and the same local lifetime lock. It is not a lease and
// has no renewal, reset, load or takeover constructor.
type Authority struct{ state *authorityState }

type authorityState struct {
	lock               *LocalLock
	db                 *sql.DB
	domain, generation string
	startedAt          time.Time
	sealed             atomic.Bool
}

// Register is attempted once per actual process after migrations and before
// any effect producer starts. Failure, including an unknown commit, is sticky.
// Reload must receive the root's existing Authority, never call Register again.
func Register(ctx context.Context, db *gorm.DB, lock *LocalLock) (*Authority, error) {
	return rootRegistration.register(ctx, db, lock)
}

func (r *registrationLatch) register(ctx context.Context, db *gorm.DB, lock *LocalLock) (*Authority, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.attempted {
		return nil, ErrProcessAuthorityUnavailable
	}
	r.attempted = true
	if ctx == nil || ctx.Err() != nil || db == nil || db.Error != nil || db.Statement == nil {
		return nil, ErrProcessAuthorityUnavailable
	}
	if _, transaction := db.Statement.ConnPool.(gorm.TxCommitter); transaction {
		return nil, ErrProcessAuthorityUnavailable
	}
	base, err := db.DB()
	if err != nil || base == nil {
		return nil, ErrProcessAuthorityUnavailable
	}
	domain, err := lock.DomainID()
	if err != nil {
		return nil, ErrProcessAuthorityUnavailable
	}
	id, err := ProcessID()
	if err != nil {
		return nil, ErrProcessAuthorityUnavailable
	}
	started := time.Now().UTC().Truncate(time.Microsecond)
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if lock.Check() != nil {
			return ErrProcessAuthorityUnavailable
		}
		var current []models.ProcessAuthority
		if err := tx.Omit("Generation").Limit(2).Find(&current).Error; err != nil || len(current) > 1 {
			return ErrProcessAuthorityUnavailable
		}
		if len(current) == 0 {
			var count int64
			if tx.Model(&models.ProcessGeneration{}).Count(&count).Error != nil || count != 0 {
				return ErrProcessAuthorityUnavailable
			}
		} else {
			row := current[0]
			if row.DomainID != domain {
				return errors.Join(ErrProcessAuthorityUnavailable, ErrProcessAuthorityDomainMismatch)
			}
			if row.ID != 1 || !validProcessIdentity(row.CurrentGenerationID) || row.CurrentGenerationID == id || row.RowVersion <= 0 || row.RowVersion == math.MaxInt64 {
				return ErrProcessAuthorityUnavailable
			}
			var old models.ProcessGeneration
			if tx.Where("domain_id = ? AND generation_id = ?", domain, row.CurrentGenerationID).Take(&old).Error != nil || old.StartedAt.IsZero() {
				return ErrProcessAuthorityUnavailable
			}
		}
		generation := models.ProcessGeneration{GenerationID: id, DomainID: domain, StartedAt: started}
		if err := tx.Create(&generation).Error; err != nil {
			return err
		}
		if len(current) == 0 {
			row := models.ProcessAuthority{ID: 1, DomainID: domain, CurrentGenerationID: id, RowVersion: 1}
			if err := tx.Omit("Generation").Create(&row).Error; err != nil {
				return err
			}
		} else {
			old := current[0]
			result := tx.Model(&models.ProcessAuthority{}).Where("id = 1 AND domain_id = ? AND current_generation_id = ? AND row_version = ?", domain, old.CurrentGenerationID, old.RowVersion).
				Updates(map[string]any{"current_generation_id": id, "row_version": old.RowVersion + 1})
			if result.Error != nil || result.RowsAffected != 1 {
				return ErrProcessAuthorityUnavailable
			}
		}
		return lock.Check()
	})
	if errors.Is(err, ErrProcessAuthorityDomainMismatch) {
		return nil, err
	}
	if err != nil || ctx.Err() != nil || lock.Check() != nil {
		return nil, ErrProcessAuthorityUnavailable
	}
	return &Authority{state: &authorityState{lock: lock, db: base, domain: domain, generation: id, startedAt: started}}, nil
}

func validProcessIdentity(id string) bool {
	if len(id) != 32 || id == "00000000000000000000000000000000" {
		return false
	}
	for _, c := range id {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func (a *Authority) GenerationID() string {
	if a == nil || a.state == nil {
		return ""
	}
	return a.state.generation
}

// Seal is irreversible. Root calls it after all effect owners have joined and
// before closing LocalLock. Sealing never asserts that remote cleanup finished.
func (a *Authority) Seal() {
	if a == nil || a.state == nil {
		return
	}
	a.state.sealed.Store(true)
}

// CheckTx must precede all device/business row locks in the actual effect CAS
// transaction. It returns no reusable ticket. Network I/O is permitted only
// after that whole transaction reports a confirmed commit, with its owner held.
func (a *Authority) CheckTx(tx *gorm.DB) error {
	return a.checkTx(tx, "")
}

// RequireRetiredTx only fences a fresh cleanup-owner CAS for an explicitly
// recorded earlier generation. It neither changes nor completes an old owner.
func (a *Authority) RequireRetiredTx(tx *gorm.DB, oldGeneration string) error {
	if !validProcessIdentity(oldGeneration) || oldGeneration == a.GenerationID() {
		return ErrProcessAuthorityUnavailable
	}
	return a.checkTx(tx, oldGeneration)
}

func (a *Authority) checkTx(tx *gorm.DB, retired string) error {
	if a == nil || a.state == nil || tx == nil || tx.Error != nil || tx.Statement == nil || tx.Statement.Context == nil || tx.Statement.Context.Err() != nil {
		return ErrProcessAuthorityUnavailable
	}
	pool := tx.Statement.ConnPool
	if prepared, ok := pool.(*gorm.PreparedStmtTX); ok && prepared != nil {
		pool = prepared.Tx
	}
	actual, ok := pool.(*sql.Tx)
	if !ok || actual == nil {
		return ErrProcessAuthorityUnavailable
	}
	// Resolve the concrete transaction's pool, not a wrapper's claimed pool.
	base, err := (&gorm.DB{Config: &gorm.Config{ConnPool: actual}}).DB()
	if err != nil || base == nil || base != a.state.db {
		return ErrProcessAuthorityUnavailable
	}
	s := a.state
	// Never hold an application mutex while waiting for the DB row lock:
	// the current SQL owner may need another fence before it can commit.
	if s.sealed.Load() || s.lock.Check() != nil || s.sealed.Load() {
		s.sealed.Store(true)
		return ErrProcessAuthorityUnavailable
	}
	result := tx.Model(&models.ProcessAuthority{}).
		Where("id = 1 AND domain_id = ? AND current_generation_id = ? AND row_version > 0 AND row_version < ?", s.domain, s.generation, int64(math.MaxInt64)).
		UpdateColumn("row_version", gorm.Expr("row_version + 1"))
	if result.Error != nil {
		return ErrProcessAuthorityUnavailable
	}
	if result.RowsAffected != 1 {
		s.sealed.Store(true)
		return ErrProcessAuthorityUnavailable
	}
	var row models.ProcessGeneration
	if err := tx.Where("domain_id = ? AND generation_id = ?", s.domain, s.generation).Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			s.sealed.Store(true)
		}
		return ErrProcessAuthorityUnavailable
	}
	if !row.StartedAt.Equal(s.startedAt) {
		s.sealed.Store(true)
		return ErrProcessAuthorityUnavailable
	}
	if retired != "" {
		var old models.ProcessGeneration
		if tx.Where("domain_id = ? AND generation_id = ?", s.domain, retired).Take(&old).Error != nil || old.StartedAt.IsZero() {
			return ErrProcessAuthorityUnavailable
		}
	}
	if s.lock.Check() != nil || s.sealed.Load() {
		s.sealed.Store(true)
		return ErrProcessAuthorityUnavailable
	}
	return nil
}
