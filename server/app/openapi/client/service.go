package client

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	appmodels "uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

var (
	ErrDependencyUnavailable    = errors.New("OpenAPI client dependency unavailable")
	ErrInvalidArgument          = errors.New("invalid OpenAPI client argument")
	ErrNotFound                 = errors.New("OpenAPI client not found")
	ErrConflict                 = errors.New("OpenAPI client update conflict")
	ErrRevoked                  = errors.New("OpenAPI client is revoked")
	ErrClientDisabled           = errors.New("OpenAPI client is disabled")
	ErrAuthenticationFailed     = errors.New("OpenAPI client authentication failed")
	ErrRevocationUnavailable    = errors.New("OpenAPI revocation dependency unavailable")
	ErrAuthorizationUnavailable = errors.New("OpenAPI management authorization unavailable")
	ErrUnknownScope             = errors.New("unknown OpenAPI scope")
	ErrOwnerDeptImmutable       = errors.New("OpenAPI client owner department is immutable")
)

var supportedScopes = map[string]struct{}{
	"device:list":     {},
	"device:detail":   {},
	"device:status":   {},
	"channel:list":    {},
	"channel:detail":  {},
	"channel:status":  {},
	"play:live:apply": {},
}

// ClientView is the management-safe representation of a client. It contains
// no encrypted verification material and never contains a plaintext SK.
type ClientView struct {
	ID                int64     `json:"id"`
	AK                string    `json:"ak"`
	Name              string    `json:"name"`
	OwnerDeptID       uint      `json:"ownerDeptId"`
	ResponsibleUserID uint      `json:"responsibleUserId"`
	Status            string    `json:"status"`
	SecretVersion     int64     `json:"secretVersion"`
	AuthEpoch         int64     `json:"authEpoch"`
	RateLimit         int       `json:"rateLimit"`
	Burst             int       `json:"burst"`
	ViewerQuota       int       `json:"viewerQuota"`
	RowVersion        int64     `json:"rowVersion"`
	CreatedBy         uint      `json:"createdBy"`
	UpdatedBy         uint      `json:"updatedBy"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

type ScopeView struct {
	ClientID   int64     `json:"clientId"`
	Scope      string    `json:"scope"`
	Enabled    bool      `json:"enabled"`
	ScopeEpoch int64     `json:"scopeEpoch"`
	UpdatedBy  uint      `json:"updatedBy"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type CreateRequest struct {
	Name              string
	OwnerDeptID       uint
	ResponsibleUserID uint
	CreatedBy         uint
}

type VerificationMaterial struct {
	ClientID      int64  `json:"clientId"`
	AccessKey     string `json:"accessKey"`
	SecretVersion int64  `json:"secretVersion"`
	AuthEpoch     int64  `json:"authEpoch"`
	SecretKey     string `json:"-"`
}

// RevocationIntent is the durable handoff from client lifecycle state to the
// T12 media revocation implementation. T03 never pretends to kick media.
type RevocationIntent struct {
	ClientID    int64
	ClientEpoch int64
	Scope       string
	ScopeEpoch  int64
	Reason      string
	CreatedAt   time.Time
}

// RevocationIntentStore must write its marker using the transaction supplied
// by Service. A nil store is deliberately fail-closed for revoking changes.
type RevocationIntentStore interface {
	RecordRevocationIntent(ctx context.Context, tx *gorm.DB, intent RevocationIntent) error
}

// ManagementBoundary is supplied by T04. It must resolve a current trusted
// operator and department scope; Service intentionally defaults to deny.
type ManagementBoundary interface {
	AuthorizeCreate(ctx context.Context, actorID uint, ownerDeptID uint) error
	AuthorizeClient(ctx context.Context, actorID uint, action string, ownerDeptID uint) error
}

type ServiceOption func(*Service)

type Service struct {
	db                 *gorm.DB
	secretManager      *SecretManager
	now                func() time.Time
	revocationStore    RevocationIntentStore
	managementBoundary ManagementBoundary
}

func WithClock(now func() time.Time) ServiceOption {
	return func(service *Service) {
		if now != nil {
			service.now = now
		}
	}
}

func WithRevocationIntentStore(store RevocationIntentStore) ServiceOption {
	return func(service *Service) { service.revocationStore = store }
}

func WithManagementBoundary(boundary ManagementBoundary) ServiceOption {
	return func(service *Service) { service.managementBoundary = boundary }
}

func NewService(db *gorm.DB, secretManager *SecretManager, options ...ServiceOption) (*Service, error) {
	if db == nil || secretManager == nil {
		return nil, ErrDependencyUnavailable
	}
	service := &Service{
		db:            db,
		secretManager: secretManager,
		now:           func() time.Time { return time.Now().UTC() },
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service, nil
}

func (s *Service) Create(ctx context.Context, request CreateRequest) (ClientView, string, error) {
	if strings.TrimSpace(request.Name) == "" || request.OwnerDeptID == 0 {
		return ClientView{}, "", ErrInvalidArgument
	}
	if request.CreatedBy == 0 {
		return ClientView{}, "", ErrAuthorizationUnavailable
	}
	if s.managementBoundary == nil {
		return ClientView{}, "", ErrAuthorizationUnavailable
	}
	if err := s.managementBoundary.AuthorizeCreate(normalizeContext(ctx), request.CreatedBy, request.OwnerDeptID); err != nil {
		return ClientView{}, "", err
	}
	ak, err := s.secretManager.GenerateAccessKey()
	if err != nil {
		return ClientView{}, "", ErrDependencyUnavailable
	}
	secret, err := s.secretManager.GenerateSecretKey()
	if err != nil {
		return ClientView{}, "", ErrDependencyUnavailable
	}
	initialCiphertext, initialIV, err := s.secretManager.Encrypt(0, ak, secretVersion, secret)
	if err != nil {
		return ClientView{}, "", ErrDependencyUnavailable
	}
	now := s.currentTime()
	var view ClientView
	err = s.db.WithContext(normalizeContext(ctx)).Transaction(func(tx *gorm.DB) error {
		row := &models.Client{
			AK:                ak,
			Name:              request.Name,
			OwnerDeptID:       request.OwnerDeptID,
			ResponsibleUserID: request.ResponsibleUserID,
			Status:            models.StatusActive,
			SecretCiphertext:  initialCiphertext,
			SecretIV:          initialIV,
			SecretKeyID:       s.secretManager.KeyID(),
			SecretVersion:     secretVersion,
			AuthEpoch:         secretVersion,
			RateLimit:         10,
			Burst:             20,
			ViewerQuota:       10,
			RowVersion:        secretVersion,
			CreatedBy:         request.CreatedBy,
			UpdatedBy:         request.CreatedBy,
			CreatedAt:         now,
			UpdatedAt:         now,
		}
		if err := tx.Create(row).Error; err != nil {
			return err
		}
		ciphertext, iv, err := s.secretManager.Encrypt(row.ID, row.AK, row.SecretVersion, secret)
		if err != nil {
			return ErrDependencyUnavailable
		}
		result := tx.Model(&models.Client{}).Where("id = ?", row.ID).Updates(map[string]any{
			"secret_ciphertext": ciphertext,
			"secret_iv":         iv,
			"updated_at":        now,
		})
		if result.Error != nil || result.RowsAffected != 1 {
			if result.Error != nil {
				return result.Error
			}
			return ErrConflict
		}
		row.SecretCiphertext = ciphertext
		row.SecretIV = iv
		if err := s.recordAudit(tx, row, "client.create", request.CreatedBy, now); err != nil {
			return err
		}
		view = toClientView(*row)
		return nil
	})
	if err != nil {
		return ClientView{}, "", err
	}
	return view, secret, nil
}

func (s *Service) Get(ctx context.Context, id int64) (ClientView, error) {
	if id <= 0 {
		return ClientView{}, ErrInvalidArgument
	}
	row, err := loadClient(s.db, normalizeContext(ctx), id)
	if err != nil {
		return ClientView{}, err
	}
	return toClientView(row), nil
}

func (s *Service) List(ctx context.Context) ([]ClientView, error) {
	var rows []models.Client
	if err := s.db.WithContext(normalizeContext(ctx)).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	views := make([]ClientView, len(rows))
	for i, row := range rows {
		views[i] = toClientView(row)
	}
	return views, nil
}

func (s *Service) ListScopes(ctx context.Context, clientID int64) ([]ScopeView, error) {
	if clientID <= 0 {
		return nil, ErrInvalidArgument
	}
	if _, err := loadClient(s.db, normalizeContext(ctx), clientID); err != nil {
		return nil, err
	}
	var rows []models.ClientScope
	if err := s.db.WithContext(normalizeContext(ctx)).Where("client_id = ?", clientID).Order("scope ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	views := make([]ScopeView, len(rows))
	for i, row := range rows {
		views[i] = toScopeView(row)
	}
	return views, nil
}

func (s *Service) GetScope(ctx context.Context, clientID int64, scope string) (ScopeView, error) {
	if clientID <= 0 || !isSupportedScope(scope) {
		return ScopeView{}, ErrInvalidArgument
	}
	var row models.ClientScope
	result := s.db.WithContext(normalizeContext(ctx)).Where("client_id = ? AND scope = ?", clientID, scope).First(&row)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return ScopeView{}, ErrNotFound
		}
		return ScopeView{}, result.Error
	}
	if result.RowsAffected == 0 || row.ClientID == 0 {
		return ScopeView{}, ErrNotFound
	}
	return toScopeView(row), nil
}

// LoadVerificationMaterial is an internal handoff for the HMAC admission
// layer. It only returns a secret after the current client is active.
func (s *Service) LoadVerificationMaterial(ctx context.Context, accessKey string) (VerificationMaterial, error) {
	var row models.Client
	if accessKey == "" {
		return VerificationMaterial{}, ErrAuthenticationFailed
	}
	result := s.db.WithContext(normalizeContext(ctx)).Where("ak = ?", accessKey).First(&row)
	if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return VerificationMaterial{}, ErrDependencyUnavailable
	}
	if result.RowsAffected == 0 || row.ID == 0 {
		return VerificationMaterial{}, ErrAuthenticationFailed
	}
	if row.Status != models.StatusActive {
		return VerificationMaterial{}, ErrAuthenticationFailed
	}
	secret, err := s.secretManager.Decrypt(row.ID, row.AK, row.SecretKeyID, row.SecretVersion, row.SecretCiphertext, row.SecretIV)
	if err != nil {
		return VerificationMaterial{}, ErrDependencyUnavailable
	}
	return VerificationMaterial{
		ClientID:      row.ID,
		AccessKey:     row.AK,
		SecretVersion: row.SecretVersion,
		AuthEpoch:     row.AuthEpoch,
		SecretKey:     secret,
	}, nil
}

func (s *Service) ValidateSecret(ctx context.Context, accessKey, presentedSecret string) error {
	material, err := s.LoadVerificationMaterial(ctx, accessKey)
	if err != nil || !validSecretText(presentedSecret) || subtle.ConstantTimeCompare([]byte(material.SecretKey), []byte(presentedSecret)) != 1 {
		return ErrAuthenticationFailed
	}
	return nil
}

func (s *Service) RotateSecret(ctx context.Context, id int64, expectedRowVersion int64, actorID uint) (ClientView, string, error) {
	if actorID == 0 {
		return ClientView{}, "", ErrAuthorizationUnavailable
	}
	if id <= 0 || expectedRowVersion <= 0 {
		return ClientView{}, "", ErrInvalidArgument
	}
	secret, err := s.secretManager.GenerateSecretKey()
	if err != nil {
		return ClientView{}, "", ErrDependencyUnavailable
	}
	now := s.currentTime()
	var view ClientView
	err = s.db.WithContext(normalizeContext(ctx)).Transaction(func(tx *gorm.DB) error {
		row, err := loadClient(tx, normalizeContext(ctx), id)
		if err != nil {
			return err
		}
		if row.Status == models.StatusRevoked {
			return ErrRevoked
		}
		if row.RowVersion != expectedRowVersion {
			return ErrConflict
		}
		if err := s.authorizeClient(ctx, actorID, "client.rotate", row.OwnerDeptID); err != nil {
			return err
		}
		newVersion := row.SecretVersion + 1
		ciphertext, iv, err := s.secretManager.Encrypt(row.ID, row.AK, newVersion, secret)
		if err != nil {
			return ErrDependencyUnavailable
		}
		newRowVersion := row.RowVersion + 1
		result := tx.Model(&models.Client{}).Where("id = ? AND row_version = ?", row.ID, expectedRowVersion).Updates(map[string]any{
			"secret_ciphertext": ciphertext,
			"secret_iv":         iv,
			"secret_key_id":     s.secretManager.KeyID(),
			"secret_version":    newVersion,
			"row_version":       newRowVersion,
			"updated_by":        actorID,
			"updated_at":        now,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrConflict
		}
		row.SecretCiphertext = ciphertext
		row.SecretIV = iv
		row.SecretKeyID = s.secretManager.KeyID()
		row.SecretVersion = newVersion
		row.RowVersion = newRowVersion
		row.UpdatedBy = actorID
		row.UpdatedAt = now
		if err := s.recordAudit(tx, &row, "client.rotate", actorID, now); err != nil {
			return err
		}
		view = toClientView(row)
		return nil
	})
	if err != nil {
		return ClientView{}, "", err
	}
	return view, secret, nil
}

func (s *Service) SetStatus(ctx context.Context, id int64, status string, expectedRowVersion int64, actorID uint) (ClientView, error) {
	if actorID == 0 {
		return ClientView{}, ErrAuthorizationUnavailable
	}
	if id <= 0 || expectedRowVersion <= 0 || !validStatus(status) {
		return ClientView{}, ErrInvalidArgument
	}
	now := s.currentTime()
	var view ClientView
	err := s.db.WithContext(normalizeContext(ctx)).Transaction(func(tx *gorm.DB) error {
		row, err := loadClient(tx, normalizeContext(ctx), id)
		if err != nil {
			return err
		}
		if row.Status == models.StatusRevoked {
			return ErrRevoked
		}
		if row.RowVersion != expectedRowVersion {
			return ErrConflict
		}
		if err := s.authorizeClient(ctx, actorID, "client."+status, row.OwnerDeptID); err != nil {
			return err
		}
		if row.Status == status {
			view = toClientView(row)
			return nil
		}
		requiresIntent := status == models.StatusDisabled || status == models.StatusRevoked
		if requiresIntent && s.revocationStore == nil {
			return ErrRevocationUnavailable
		}
		newEpoch := row.AuthEpoch + 1
		newRowVersion := row.RowVersion + 1
		result := tx.Model(&models.Client{}).Where("id = ? AND row_version = ?", row.ID, expectedRowVersion).Updates(map[string]any{
			"status":      status,
			"auth_epoch":  newEpoch,
			"row_version": newRowVersion,
			"updated_by":  actorID,
			"updated_at":  now,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrConflict
		}
		row.Status = status
		row.AuthEpoch = newEpoch
		row.RowVersion = newRowVersion
		row.UpdatedBy = actorID
		row.UpdatedAt = now
		if requiresIntent {
			if err := s.revocationStore.RecordRevocationIntent(normalizeContext(ctx), tx, RevocationIntent{
				ClientID:    row.ID,
				ClientEpoch: row.AuthEpoch,
				ScopeEpoch:  0,
				Reason:      "client." + status,
				CreatedAt:   now,
			}); err != nil {
				return err
			}
		}
		if err := s.recordAudit(tx, &row, "client."+status, actorID, now); err != nil {
			return err
		}
		view = toClientView(row)
		return nil
	})
	if err != nil {
		return ClientView{}, err
	}
	return view, nil
}

func (s *Service) SetScope(ctx context.Context, id int64, scope string, enabled bool, expectedRowVersion int64, actorID uint) (ClientView, error) {
	if actorID == 0 {
		return ClientView{}, ErrAuthorizationUnavailable
	}
	if id <= 0 || expectedRowVersion <= 0 || !isSupportedScope(scope) {
		if !isSupportedScope(scope) {
			return ClientView{}, ErrUnknownScope
		}
		return ClientView{}, ErrInvalidArgument
	}
	now := s.currentTime()
	var view ClientView
	err := s.db.WithContext(normalizeContext(ctx)).Transaction(func(tx *gorm.DB) error {
		row, err := loadClient(tx, normalizeContext(ctx), id)
		if err != nil {
			return err
		}
		if row.Status == models.StatusRevoked {
			return ErrRevoked
		}
		if row.Status == models.StatusDisabled {
			return ErrClientDisabled
		}
		if row.RowVersion != expectedRowVersion {
			return ErrConflict
		}
		if err := s.authorizeClient(ctx, actorID, "scope.set", row.OwnerDeptID); err != nil {
			return err
		}
		var scopeRow models.ClientScope
		scopeResult := tx.Where("client_id = ? AND scope = ?", id, scope).First(&scopeRow)
		scopeErr := scopeResult.Error
		if scopeErr != nil && !errors.Is(scopeErr, gorm.ErrRecordNotFound) {
			return scopeErr
		}
		if scopeErr == nil && (scopeResult.RowsAffected == 0 || scopeRow.ClientID == 0) {
			scopeErr = gorm.ErrRecordNotFound
		}
		if scopeErr == nil && scopeRow.Enabled == enabled {
			view = toClientView(row)
			return nil
		}
		if !enabled && s.revocationStore == nil {
			return ErrRevocationUnavailable
		}
		newScopeEpoch := int64(1)
		if scopeErr == nil {
			newScopeEpoch = scopeRow.ScopeEpoch + 1
		}
		// Advance the client CAS row first. This is the lifecycle
		// linearization point shared with admission; scope writes follow it
		// in the same transaction and roll back together on any failure.
		newRowVersion := row.RowVersion + 1
		result := tx.Model(&models.Client{}).Where("id = ? AND row_version = ?", id, expectedRowVersion).Updates(map[string]any{
			"row_version": newRowVersion,
			"updated_by":  actorID,
			"updated_at":  now,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrConflict
		}
		row.RowVersion = newRowVersion
		row.UpdatedBy = actorID
		row.UpdatedAt = now
		if scopeErr == nil {
			result := tx.Model(&models.ClientScope{}).Where("client_id = ? AND scope = ?", id, scope).Updates(map[string]any{
				"enabled":     enabled,
				"scope_epoch": newScopeEpoch,
				"updated_by":  actorID,
				"updated_at":  now,
			})
			if result.Error != nil || result.RowsAffected != 1 {
				if result.Error != nil {
					return result.Error
				}
				return ErrConflict
			}
		} else {
			scopeRow = models.ClientScope{
				ClientID:   id,
				Scope:      scope,
				Enabled:    enabled,
				ScopeEpoch: newScopeEpoch,
				UpdatedBy:  actorID,
				UpdatedAt:  now,
			}
			if err := tx.Create(&scopeRow).Error; err != nil {
				return err
			}
		}
		if !enabled {
			if err := s.revocationStore.RecordRevocationIntent(normalizeContext(ctx), tx, RevocationIntent{
				ClientID:    row.ID,
				ClientEpoch: row.AuthEpoch,
				Scope:       scope,
				ScopeEpoch:  newScopeEpoch,
				Reason:      "scope.revoke",
				CreatedAt:   now,
			}); err != nil {
				return err
			}
		}
		reason := "scope.grant"
		if !enabled {
			reason = "scope.revoke"
		}
		if err := s.recordAudit(tx, &row, reason, actorID, now); err != nil {
			return err
		}
		view = toClientView(row)
		return nil
	})
	if err != nil {
		return ClientView{}, err
	}
	return view, nil
}

func (s *Service) UpdateOwnerDeptID(_ context.Context, _ int64, _ uint, _ int64, _ uint) error {
	return ErrOwnerDeptImmutable
}

func (s *Service) authorizeClient(ctx context.Context, actorID uint, action string, ownerDeptID uint) error {
	if s.managementBoundary == nil {
		return ErrAuthorizationUnavailable
	}
	return s.managementBoundary.AuthorizeClient(normalizeContext(ctx), actorID, action, ownerDeptID)
}

func (s *Service) recordAudit(tx *gorm.DB, row *models.Client, reason string, actorID uint, now time.Time) error {
	if tx == nil || row == nil || actorID == 0 {
		return ErrDependencyUnavailable
	}
	fingerprint := sha256.Sum256([]byte(row.AK))
	clientID := row.ID
	if err := tx.Create(&models.Audit{
		RequestID:     uuid.NewString(),
		ClientID:      &clientID,
		AKFingerprint: hex.EncodeToString(fingerprint[:]),
		ResourceType:  "openapi_client",
		ResourceID:    strconv.FormatInt(row.ID, 10),
		Result:        "success",
		ReasonClass:   reason,
		Source:        "openapi-client-service",
		CreatedAt:     now,
		CompletedAt:   &now,
	}).Error; err != nil {
		return err
	}
	requestData, err := json.Marshal(struct {
		ClientID int64  `json:"clientId"`
		Action   string `json:"action"`
	}{ClientID: row.ID, Action: reason})
	if err != nil {
		return err
	}
	operation := appmodels.OperationUpdate
	if reason == "client.create" {
		operation = appmodels.OperationCreate
	}
	path := "/api/gb28181/openapi-clients/" + strconv.FormatInt(row.ID, 10)
	return tx.Create(&appmodels.SysOperationLog{
		BaseModel:   appmodels.BaseModel{CreatedAt: now, UpdatedAt: now},
		UserID:      actorID,
		Module:      "openapi-client",
		Operation:   operation,
		Method:      "SERVICE",
		Path:        path,
		RequestData: string(requestData),
		StatusCode:  200,
	}).Error
}

func (s *Service) currentTime() time.Time {
	return s.now().UTC()
}

func normalizeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func loadClient(db *gorm.DB, ctx context.Context, id int64) (models.Client, error) {
	var row models.Client
	result := db.WithContext(ctx).First(&row, id)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return models.Client{}, ErrNotFound
		}
		return models.Client{}, result.Error
	}
	if result.RowsAffected == 0 || row.ID == 0 {
		return models.Client{}, ErrNotFound
	}
	return row, nil
}

func validStatus(status string) bool {
	switch status {
	case models.StatusActive, models.StatusDisabled, models.StatusRevoked:
		return true
	default:
		return false
	}
}

func isSupportedScope(scope string) bool {
	_, ok := supportedScopes[scope]
	return ok
}

func toClientView(row models.Client) ClientView {
	return ClientView{
		ID:                row.ID,
		AK:                row.AK,
		Name:              row.Name,
		OwnerDeptID:       row.OwnerDeptID,
		ResponsibleUserID: row.ResponsibleUserID,
		Status:            row.Status,
		SecretVersion:     row.SecretVersion,
		AuthEpoch:         row.AuthEpoch,
		RateLimit:         row.RateLimit,
		Burst:             row.Burst,
		ViewerQuota:       row.ViewerQuota,
		RowVersion:        row.RowVersion,
		CreatedBy:         row.CreatedBy,
		UpdatedBy:         row.UpdatedBy,
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
	}
}

func toScopeView(row models.ClientScope) ScopeView {
	return ScopeView{
		ClientID:   row.ClientID,
		Scope:      row.Scope,
		Enabled:    row.Enabled,
		ScopeEpoch: row.ScopeEpoch,
		UpdatedBy:  row.UpdatedBy,
		UpdatedAt:  row.UpdatedAt,
	}
}
