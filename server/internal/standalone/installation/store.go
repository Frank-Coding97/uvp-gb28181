package installation

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"
	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/setup"
	gbnode "uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	gbzlmrepo "uvplatform.cn/uvp-gb28181/app/gb28181/zlm/repo"
	"uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/passwordhelper"
)

const (
	installationTable = "standalone_installation"
	installationID    = uint(1)
	adminRoleID       = uint(1)
	adminRoleName     = "系统管理员"
)

type Phase string

const (
	PhasePendingAdmin Phase = "pending_admin"
	PhasePendingSIP   Phase = "pending_sip"
	PhaseComplete     Phase = "complete"
)

var (
	ErrAlreadyInitialized = errors.New("standalone installation is already initialized")
	ErrInvalidState       = errors.New("standalone installation state is invalid")
)

// State is the durable first-run state. Tokens and credentials are deliberately
// not part of this value or its backing table.
type State struct {
	Phase           Phase
	AdministratorID *uint
	CompletedAt     *time.Time
}

type Store struct {
	db *gorm.DB
}

type installationRow struct {
	ID              uint       `gorm:"column:id;primaryKey"`
	Phase           Phase      `gorm:"column:phase"`
	AdministratorID *uint      `gorm:"column:admin_user_id"`
	CompletedAt     *time.Time `gorm:"column:completed_at"`
	CreatedAt       time.Time  `gorm:"column:created_at"`
	UpdatedAt       time.Time  `gorm:"column:updated_at"`
}

func (installationRow) TableName() string { return installationTable }

type casbinRelation struct {
	PType string `gorm:"column:ptype"`
	V0    string `gorm:"column:v0"`
	V1    string `gorm:"column:v1"`
	V2    string `gorm:"column:v2"`
	V3    string `gorm:"column:v3"`
	V4    string `gorm:"column:v4"`
	V5    string `gorm:"column:v5"`
}

func NewStore(db *gorm.DB) *Store {
	return &Store{db: db}
}

func (s *Store) State(ctx context.Context) (State, error) {
	db, err := s.database()
	if err != nil {
		return State{}, err
	}
	row, err := readState(ctx, db)
	if err != nil {
		return State{}, err
	}
	if row.Phase == PhasePendingAdmin {
		count, err := countUsers(ctx, db)
		if err != nil {
			return State{}, stateDatabaseError("count users while validating pending admin state", err)
		}
		if count != 0 {
			return State{}, invalidState("pending_admin cannot coexist with existing users")
		}
	}
	return stateFromRow(row), nil
}

func (s *Store) CreateAdmin(ctx context.Context, username, password string) (State, error) {
	db, err := s.database()
	if err != nil {
		return State{}, err
	}
	if err := validateAdminCredentials(username, password); err != nil {
		return State{}, err
	}
	hashedPassword, err := passwordhelper.HashPassword(password)
	if err != nil {
		return State{}, fmt.Errorf("hash administrator password: %w", err)
	}

	var result State
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row, err := readState(ctx, tx)
		if err != nil {
			return err
		}
		if row.Phase != PhasePendingAdmin {
			if row.Phase == PhasePendingSIP || row.Phase == PhaseComplete {
				return ErrAlreadyInitialized
			}
			return invalidState("cannot create an administrator from phase %q", row.Phase)
		}
		if row.AdministratorID != nil || row.CompletedAt != nil {
			return invalidState("pending_admin row contains completion metadata")
		}
		count, err := countUsers(ctx, tx)
		if err != nil {
			return stateDatabaseError("count users before creating administrator", err)
		}
		if count != 0 {
			return invalidState("refusing administrator credentials because users already exist")
		}

		var role models.SysRole
		roleResult := tx.WithContext(ctx).Where("id = ? AND deleted_at IS NULL", adminRoleID).Take(&role)
		if roleResult.Error != nil {
			return stateDatabaseError("read system administrator role", roleResult.Error)
		}
		if roleResult.RowsAffected != 1 || role.Name != adminRoleName || role.Status != 1 || role.DataScope != 1 {
			return invalidState("system administrator role %d is missing or not the full-access role", adminRoleID)
		}

		user := models.User{
			Username:    username,
			Password:    string(hashedPassword),
			Status:      1,
			DeptID:      1,
			Sex:         "1",
			NickName:    username,
			Description: "首装管理员",
		}
		if err := tx.WithContext(ctx).Create(&user).Error; err != nil {
			return fmt.Errorf("create administrator: %w", err)
		}
		if err := tx.WithContext(ctx).Create(&models.SysUserRole{UserID: user.ID, RoleID: adminRoleID}).Error; err != nil {
			return fmt.Errorf("link administrator role: %w", err)
		}
		relation := casbinRelation{
			PType: "g",
			V0:    fmt.Sprintf("user_%d", user.ID),
			V1:    fmt.Sprintf("role_%d", adminRoleID),
			V2:    "*",
		}
		if err := tx.WithContext(ctx).Table("sys_casbin_rule").Create(&relation).Error; err != nil {
			return fmt.Errorf("link administrator permission role: %w", err)
		}

		updatedAt := time.Now().UTC()
		update := tx.WithContext(ctx).Model(&installationRow{}).
			Where("id = ? AND phase = ?", installationID, PhasePendingAdmin).
			Updates(map[string]interface{}{
				"phase":         PhasePendingSIP,
				"admin_user_id": user.ID,
				"updated_at":    updatedAt,
			})
		if update.Error != nil {
			return fmt.Errorf("advance standalone installation state: %w", update.Error)
		}
		if update.RowsAffected != 1 {
			return invalidState("standalone installation state changed while creating administrator")
		}
		row.Phase = PhasePendingSIP
		row.AdministratorID = &user.ID
		row.UpdatedAt = updatedAt
		result = stateFromRow(row)
		return nil
	})
	if err != nil {
		return State{}, err
	}
	return result, nil
}

func (s *Store) CompleteSIP(ctx context.Context, req setup.SaveSIPConfigRequest) (setup.SIPConfigView, error) {
	return s.completeSIP(ctx, req, completeSIPOptions{})
}

// CompleteSIPWithMedia completes standalone first installation and persists
// the local media endpoint in the same transaction as the SIP configuration
// and installation phase transition. Existing meta nodes are user-owned: only
// an exact endpoint plus API secret match may be reused, and its identity and
// media addresses are never silently changed.
func (s *Store) CompleteSIPWithMedia(ctx context.Context, req setup.SaveSIPConfigRequest, localZLM gbconfig.ZLMConfig) (setup.SIPConfigView, error) {
	return s.completeSIP(ctx, req, completeSIPOptions{
		beforeSave: func(_ context.Context, _ *gorm.DB, req setup.SaveSIPConfigRequest) error {
			return validateStandaloneMediaHosts(req)
		},
		afterSave: func(ctx context.Context, tx *gorm.DB, req setup.SaveSIPConfigRequest) error {
			return ensureStandaloneMediaNode(ctx, tx, req, localZLM)
		},
	})
}

type completeSIPOptions struct {
	beforeSave func(context.Context, *gorm.DB, setup.SaveSIPConfigRequest) error
	afterSave  func(context.Context, *gorm.DB, setup.SaveSIPConfigRequest) error
}

func (s *Store) completeSIP(ctx context.Context, req setup.SaveSIPConfigRequest, options completeSIPOptions) (setup.SIPConfigView, error) {
	db, err := s.database()
	if err != nil {
		return setup.SIPConfigView{}, err
	}
	var result setup.SIPConfigView
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row, err := readState(ctx, tx)
		if err != nil {
			return err
		}
		if err := validateSIPCompletionState(row); err != nil {
			return err
		}
		if options.beforeSave != nil {
			if err := options.beforeSave(ctx, tx, req); err != nil {
				return err
			}
		}

		// SIPConfigService.Save uses a nested savepoint when handed this
		// transaction, so its write and the installation CAS commit together.
		result, err = setup.NewSIPConfigService(tx).Save(ctx, req)
		if err != nil {
			return fmt.Errorf("save SIP configuration: %w", err)
		}
		if options.afterSave != nil {
			if err := options.afterSave(ctx, tx, req); err != nil {
				return err
			}
		}

		completedAt := time.Now().UTC()
		update := tx.WithContext(ctx).Model(&installationRow{}).
			Where("id = ? AND phase = ?", installationID, PhasePendingSIP).
			Updates(map[string]interface{}{
				"phase":        PhaseComplete,
				"completed_at": completedAt,
				"updated_at":   completedAt,
			})
		if update.Error != nil {
			return fmt.Errorf("complete standalone installation state: %w", update.Error)
		}
		if update.RowsAffected != 1 {
			if options.afterSave != nil {
				return invalidState("standalone installation state changed while saving SIP and media configuration")
			}
			return invalidState("standalone installation state changed while saving SIP configuration")
		}
		return nil
	})
	if err != nil {
		return setup.SIPConfigView{}, err
	}
	return result, nil
}

func validateSIPCompletionState(row installationRow) error {
	switch row.Phase {
	case PhaseComplete:
		return ErrAlreadyInitialized
	case PhasePendingAdmin:
		return invalidState("SIP cannot be completed before the administrator is created")
	case PhasePendingSIP:
		if row.AdministratorID == nil || *row.AdministratorID == 0 {
			return invalidState("pending_sip row has no administrator")
		}
	default:
		return invalidState("cannot complete SIP from phase %q", row.Phase)
	}
	return nil
}

func validateStandaloneMediaHosts(req setup.SaveSIPConfigRequest) error {
	fields := make(map[string]string)
	if !isConcreteIPv4(req.MediaReceiveHost) {
		fields["mediaReceiveHost"] = "must be a concrete IPv4 address"
	}
	if !isConcreteIPv4(req.MediaPlaybackHost) {
		fields["mediaPlaybackHost"] = "must be a concrete IPv4 address"
	}
	if len(fields) > 0 {
		return &setup.ValidationError{Fields: fields}
	}
	return nil
}

func isConcreteIPv4(value string) bool {
	ip := net.ParseIP(value)
	if ip == nil || ip.To4() == nil || value != ip.To4().String() {
		return false
	}
	return !ip.IsUnspecified() && !ip.IsMulticast() && !ip.Equal(net.IPv4bcast)
}

func ensureStandaloneMediaNode(ctx context.Context, tx *gorm.DB, req setup.SaveSIPConfigRequest, localZLM gbconfig.ZLMConfig) error {
	var rows []gbzlmrepo.MetaNode
	if err := tx.WithContext(ctx).Order("id").Find(&rows).Error; err != nil {
		return fmt.Errorf("read standalone media nodes: %w", err)
	}

	wantHost, validHost := normalizeLiteralHost(localZLM.Host)
	if !validHost || localZLM.HTTPPort < 1 || localZLM.HTTPPort > 65535 {
		return &setup.ValidationError{Fields: map[string]string{
			"mediaNode": "local ZLM endpoint must be a literal host and valid port",
		}}
	}
	var endpointMatches int
	var identityMatches []*gbzlmrepo.MetaNode
	for i := range rows {
		host, ok := normalizeLiteralHost(rows[i].Host)
		if !ok || host != wantHost || rows[i].APIPort != localZLM.HTTPPort {
			continue
		}
		endpointMatches++
		if rows[i].APISecret == localZLM.Secret {
			identityMatches = append(identityMatches, &rows[i])
		}
	}
	if len(identityMatches) > 1 {
		return &setup.ValidationError{Fields: map[string]string{
			"mediaNode": "multiple nodes match the local ZLM endpoint and API secret",
		}}
	}
	if len(identityMatches) == 1 {
		match := identityMatches[0]
		fields := make(map[string]string)
		if match.ReceiveHost != req.MediaReceiveHost {
			fields["mediaReceiveHost"] = "does not match the existing local media node"
		}
		if match.PlaybackHost != req.MediaPlaybackHost {
			fields["mediaPlaybackHost"] = "does not match the existing local media node"
		}
		if len(fields) > 0 {
			return &setup.ValidationError{Fields: fields}
		}
		return nil
	}
	if endpointMatches != 0 {
		return &setup.ValidationError{Fields: map[string]string{
			"mediaNode": "the local ZLM API secret does not match the registered endpoint",
		}}
	}
	if len(rows) != 0 {
		return &setup.ValidationError{Fields: map[string]string{
			"mediaNode": "the local ZLM endpoint and API secret are not registered",
		}}
	}

	now := time.Now().UTC()
	row := gbzlmrepo.MetaNode{
		Revision:        1,
		Name:            "zlm-default",
		Host:            localZLM.Host,
		ReceiveHost:     req.MediaReceiveHost,
		PlaybackHost:    req.MediaPlaybackHost,
		APIPort:         localZLM.HTTPPort,
		APISecret:       localZLM.Secret,
		MediaServerUUID: uuid.NewString(),
		Weight:          50,
		State:           string(gbnode.StateActive),
		RTPPortStart:    30000,
		RTPPortEnd:      35000,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := tx.WithContext(ctx).Create(&row).Error; err != nil {
		return fmt.Errorf("seed standalone media node: %w", err)
	}
	return nil
}

func normalizeLiteralHost(raw string) (string, bool) {
	host := strings.TrimSpace(raw)
	if strings.EqualFold(host, "localhost") {
		return "127.0.0.1", true
	}
	host = strings.Trim(host, "[]")
	if strings.Contains(host, "%") {
		return "", false
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return "", false
	}
	return addr.Unmap().String(), true
}

func (s *Store) database() (*gorm.DB, error) {
	if s == nil || s.db == nil {
		return nil, invalidState("installation store has no database")
	}
	return s.db, nil
}

func readState(ctx context.Context, db *gorm.DB) (installationRow, error) {
	var row installationRow
	result := db.WithContext(ctx).Where("id = ?", installationID).Take(&row)
	if result.Error != nil {
		return installationRow{}, stateDatabaseError("read standalone installation state", result.Error)
	}
	if result.RowsAffected != 1 {
		return installationRow{}, invalidState("standalone installation state row is missing")
	}
	if err := validateStateRow(row); err != nil {
		return installationRow{}, err
	}
	return row, nil
}

func validateStateRow(row installationRow) error {
	switch row.Phase {
	case PhasePendingAdmin:
		if row.AdministratorID != nil || row.CompletedAt != nil {
			return invalidState("pending_admin row contains administrator or completion metadata")
		}
	case PhasePendingSIP:
		if row.AdministratorID == nil || *row.AdministratorID == 0 || row.CompletedAt != nil {
			return invalidState("pending_sip row has invalid administrator or completion metadata")
		}
	case PhaseComplete:
		if row.CompletedAt == nil {
			return invalidState("complete row has no completion timestamp")
		}
		if row.AdministratorID != nil && *row.AdministratorID == 0 {
			return invalidState("complete row has an invalid administrator")
		}
	default:
		return invalidState("unknown standalone installation phase %q", row.Phase)
	}
	return nil
}

func countUsers(ctx context.Context, db *gorm.DB) (int64, error) {
	var count int64
	result := db.WithContext(ctx).Unscoped().Table("sys_users").Count(&count)
	return count, result.Error
}

func stateFromRow(row installationRow) State {
	return State{Phase: row.Phase, AdministratorID: row.AdministratorID, CompletedAt: row.CompletedAt}
}

func validateAdminCredentials(username, password string) error {
	if username == "" {
		return errors.New("administrator username is required")
	}
	if !utf8.ValidString(username) {
		return errors.New("administrator username must be valid UTF-8")
	}
	if utf8.RuneCountInString(username) > 50 {
		return errors.New("administrator username must be at most 50 characters")
	}
	if err := passwordhelper.ValidatePassword(password); err != nil {
		return err
	}
	if len([]byte(password)) > 72 {
		return errors.New("administrator password must be at most 72 bytes")
	}
	return nil
}

func invalidState(format string, args ...interface{}) error {
	return fmt.Errorf("%w: %s", ErrInvalidState, fmt.Sprintf(format, args...))
}

func stateDatabaseError(operation string, err error) error {
	return errors.Join(ErrInvalidState, fmt.Errorf("%s: %w", operation, err))
}
