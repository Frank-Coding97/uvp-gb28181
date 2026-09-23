package client

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	appmodels "uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

var testDatabaseID atomic.Int64

type recordingRevocationStore struct {
	mu     sync.Mutex
	events []RevocationIntent
	err    error
}

type allowAllManagementBoundary struct{}

func (allowAllManagementBoundary) AuthorizeCreate(context.Context, uint, uint) error {
	return nil
}

func (allowAllManagementBoundary) AuthorizeClient(context.Context, uint, string, uint) error {
	return nil
}

type dataScopeManagementBoundary struct {
	dataScope int8
}

func (dataScopeManagementBoundary) AuthorizeCreate(context.Context, uint, uint) error {
	return nil
}

func (dataScopeManagementBoundary) AuthorizeClient(context.Context, uint, string, uint) error {
	return nil
}

func (b *dataScopeManagementBoundary) AuthorizeCreateWithDataScope(_ context.Context, _ uint, _ uint, dataScope int8) error {
	b.dataScope = dataScope
	return nil
}

func (s *recordingRevocationStore) RecordRevocationIntent(_ context.Context, tx *gorm.DB, intent RevocationIntent) error {
	if tx == nil {
		return errors.New("missing transaction")
	}
	if s.err != nil {
		return s.err
	}
	s.mu.Lock()
	s.events = append(s.events, intent)
	s.mu.Unlock()
	return nil
}

func (s *recordingRevocationStore) snapshot() []RevocationIntent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]RevocationIntent(nil), s.events...)
}

func newClientTestService(t *testing.T, store RevocationIntentStore) (*Service, *gorm.DB) {
	t.Helper()
	dsn := fmt.Sprintf("file:openapi_client_%d?mode=memory&cache=shared", testDatabaseID.Add(1))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Client{}, &models.ClientScope{}, &models.Audit{}))
	require.NoError(t, db.AutoMigrate(&appmodels.SysOperationLog{}))

	masterKey := bytes.Repeat([]byte{0xA5}, 32)
	secrets, err := NewSecretManager(masterKey, "test-key-1")
	require.NoError(t, err)
	service, err := NewService(db, secrets,
		WithClock(func() time.Time { return time.Unix(1790000000, 0).UTC() }),
		WithRevocationIntentStore(store),
		WithManagementBoundary(allowAllManagementBoundary{}),
	)
	require.NoError(t, err)
	return service, db
}

func maskRecordNotFound(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("openapi_mask_record_not_found", func(query *gorm.DB) {
		query.Statement.RaiseErrorOnNotFound = false
	}))
}

func failManagementAuditCreate(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("openapi_fail_management_audit", func(query *gorm.DB) {
		if _, ok := query.Statement.Dest.(*appmodels.SysOperationLog); ok {
			query.AddError(errors.New("management audit unavailable"))
		}
	}))
}

func createTestClient(t *testing.T, service *Service) (ClientView, string) {
	t.Helper()
	view, secret, err := service.Create(context.Background(), CreateRequest{
		Name:              "integration-client",
		OwnerDeptID:       10,
		ResponsibleUserID: 101,
		CreatedBy:         7,
	})
	require.NoError(t, err)
	return view, secret
}

func TestOpenAPIClientCreateGeneratesIndependentCredentials(t *testing.T) {
	service, db := newClientTestService(t, &recordingRevocationStore{})
	first, firstSecret := createTestClient(t, service)
	second, secondSecret := createTestClient(t, service)

	require.NotEqual(t, first.AK, second.AK)
	require.NotEqual(t, firstSecret, secondSecret)
	require.Regexp(t, `^uvp_[0-9a-f]{32}$`, first.AK)
	decoded, err := base64.RawURLEncoding.DecodeString(firstSecret)
	require.NoError(t, err)
	require.Len(t, decoded, 32)
	require.Equal(t, models.StatusActive, first.Status)

	scopes, err := service.ListScopes(context.Background(), first.ID)
	require.NoError(t, err)
	require.Empty(t, scopes)
	require.False(t, db.Migrator().HasTable("sys_user"))
	var auditCount int64
	require.NoError(t, db.Model(&models.Audit{}).Count(&auditCount).Error)
	require.Equal(t, int64(2), auditCount, "creation only writes OpenAPI audit rows")
	var managementLogs []appmodels.SysOperationLog
	require.NoError(t, db.Where("user_id = ?", 7).Find(&managementLogs).Error)
	require.Len(t, managementLogs, 2)
	for _, log := range managementLogs {
		require.Equal(t, appmodels.OperationCreate, log.Operation)
		require.Contains(t, log.RequestData, "clientId")
		require.Contains(t, log.RequestData, "client.create")
		require.NotContains(t, log.RequestData, firstSecret)
		require.NotContains(t, log.RequestData, secondSecret)
	}
}

func TestOpenAPIClientCreateDefaultsAndPersistsDepartmentDataScope(t *testing.T) {
	service, db := newClientTestService(t, &recordingRevocationStore{})
	defaultView, _, err := service.Create(context.Background(), CreateRequest{Name: "default-scope", OwnerDeptID: 10, CreatedBy: 7})
	require.NoError(t, err)
	require.Equal(t, models.DataScopeDepartment, defaultView.DataScope)

	childView, _, err := service.Create(context.Background(), CreateRequest{Name: "child-scope", OwnerDeptID: 10, DataScope: models.DataScopeDepartmentAndChildren, CreatedBy: 7})
	require.NoError(t, err)
	require.Equal(t, models.DataScopeDepartmentAndChildren, childView.DataScope)

	var row models.Client
	require.NoError(t, db.First(&row, childView.ID).Error)
	require.Equal(t, models.DataScopeDepartmentAndChildren, row.DataScope)
	got, err := service.Get(context.Background(), childView.ID)
	require.NoError(t, err)
	require.Equal(t, models.DataScopeDepartmentAndChildren, got.DataScope)
}

func TestOpenAPIClientCreateRejectsUnsupportedDataScope(t *testing.T) {
	service, db := newClientTestService(t, &recordingRevocationStore{})
	for _, dataScope := range []int8{1, 2, 5, -1} {
		_, _, err := service.Create(context.Background(), CreateRequest{Name: "invalid-scope", OwnerDeptID: 10, DataScope: dataScope, CreatedBy: 7})
		require.ErrorIs(t, err, ErrInvalidArgument, "data scope %d", dataScope)
	}
	var count int64
	require.NoError(t, db.Model(&models.Client{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestOpenAPIClientCreatePassesDataScopeToOptionalManagementBoundary(t *testing.T) {
	boundary := &dataScopeManagementBoundary{}
	dsn := fmt.Sprintf("file:openapi_client_scope_boundary_%d?mode=memory&cache=shared", testDatabaseID.Add(1))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Client{}, &models.ClientScope{}, &models.Audit{}, &appmodels.SysOperationLog{}))
	masterKey := bytes.Repeat([]byte{0xA5}, 32)
	secrets, err := NewSecretManager(masterKey, "test-key-1")
	require.NoError(t, err)
	service, err := NewService(db, secrets, WithManagementBoundary(boundary))
	require.NoError(t, err)
	_, _, err = service.Create(context.Background(), CreateRequest{Name: "scoped", OwnerDeptID: 10, DataScope: models.DataScopeDepartmentAndChildren, CreatedBy: 7})
	require.NoError(t, err)
	require.Equal(t, models.DataScopeDepartmentAndChildren, boundary.dataScope)
}

func TestOpenAPIClientSecretIsShownOnlyOnSuccessfulCreateOrRotate(t *testing.T) {
	service, _ := newClientTestService(t, &recordingRevocationStore{})
	created, initialSecret := createTestClient(t, service)

	got, err := service.Get(context.Background(), created.ID)
	require.NoError(t, err)
	encoded, err := json.Marshal(got)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), initialSecret)
	require.NotContains(t, string(encoded), "secretCiphertext")

	material, err := service.LoadVerificationMaterial(context.Background(), created.AK)
	require.NoError(t, err)
	require.Equal(t, initialSecret, material.SecretKey)
	rotated, rotatedSecret, err := service.RotateSecret(context.Background(), created.ID, created.RowVersion, 7)
	require.NoError(t, err)
	require.NotEqual(t, initialSecret, rotatedSecret)
	require.Equal(t, rotatedSecret, mustLoadMaterial(t, service, created.AK).SecretKey)
	require.Equal(t, created.RowVersion+1, rotated.RowVersion)

	_, err = service.Get(context.Background(), created.ID)
	require.NoError(t, err)
	_, err = service.LoadVerificationMaterial(context.Background(), created.AK)
	require.NoError(t, err)
}

func mustLoadMaterial(t *testing.T, service *Service, ak string) VerificationMaterial {
	t.Helper()
	material, err := service.LoadVerificationMaterial(context.Background(), ak)
	require.NoError(t, err)
	return material
}

func TestOpenAPIClientSecretEncryptionBindsAADAndNeverStoresPlaintext(t *testing.T) {
	service, db := newClientTestService(t, &recordingRevocationStore{})
	view, secret := createTestClient(t, service)

	var stored models.Client
	require.NoError(t, db.First(&stored, view.ID).Error)
	require.NotContains(t, string(stored.SecretCiphertext), secret)
	require.NotEmpty(t, stored.SecretIV)
	require.Len(t, stored.SecretIV, 12)

	material := mustLoadMaterial(t, service, view.AK)
	require.Equal(t, secret, material.SecretKey)
	manager := service.secretManager
	_, err := manager.Decrypt(view.ID, view.AK, stored.SecretKeyID, stored.SecretVersion, stored.SecretCiphertext, stored.SecretIV)
	require.NoError(t, err)
	for _, alter := range []struct {
		clientID  int64
		accessKey string
		version   int64
	}{
		{clientID: view.ID + 1, accessKey: stored.AK, version: stored.SecretVersion},
		{clientID: view.ID, accessKey: stored.AK + "x", version: stored.SecretVersion},
		{clientID: view.ID, accessKey: stored.AK, version: stored.SecretVersion + 1},
	} {
		_, err := manager.Decrypt(alter.clientID, alter.accessKey, stored.SecretKeyID, alter.version, stored.SecretCiphertext, stored.SecretIV)
		require.Error(t, err)
	}
	wrongManager, err := NewSecretManager(bytes.Repeat([]byte{0x5A}, 32), stored.SecretKeyID)
	require.NoError(t, err)
	_, err = wrongManager.Decrypt(view.ID, view.AK, stored.SecretKeyID, stored.SecretVersion, stored.SecretCiphertext, stored.SecretIV)
	require.Error(t, err)
	_, err = service.secretManager.Decrypt(view.ID, view.AK, "wrong-key-id", stored.SecretVersion, stored.SecretCiphertext, stored.SecretIV)
	require.Error(t, err)
	_, _, err = manager.Encrypt(view.ID, view.AK, stored.SecretVersion, "not-a-secret")
	require.Error(t, err)
}

func TestOpenAPIClientRotateInvalidatesOldSecretWithoutMediaRevocation(t *testing.T) {
	store := &recordingRevocationStore{}
	service, _ := newClientTestService(t, store)
	view, oldSecret := createTestClient(t, service)

	rotated, newSecret, err := service.RotateSecret(context.Background(), view.ID, view.RowVersion, 7)
	require.NoError(t, err)
	require.Equal(t, view.SecretVersion+1, rotated.SecretVersion)
	require.NoError(t, service.ValidateSecret(context.Background(), view.AK, newSecret))
	require.ErrorIs(t, service.ValidateSecret(context.Background(), view.AK, oldSecret), ErrAuthenticationFailed)
	require.Empty(t, store.snapshot(), "secret rotation must not create media revocation intent")
}

func TestOpenAPIClientStatusEpochsAreTerminalAndRequireRevocationStore(t *testing.T) {
	store := &recordingRevocationStore{}
	service, _ := newClientTestService(t, store)
	denied := *service
	denied.managementBoundary = nil
	_, _, err := denied.Create(context.Background(), CreateRequest{Name: "denied", OwnerDeptID: 10, CreatedBy: 7})
	require.ErrorIs(t, err, ErrAuthorizationUnavailable)
	_, _, err = service.Create(context.Background(), CreateRequest{Name: "no-actor", OwnerDeptID: 10, CreatedBy: 0})
	require.Error(t, err)
	view, secret := createTestClient(t, service)

	disabled, err := service.SetStatus(context.Background(), view.ID, models.StatusDisabled, view.RowVersion, 7)
	require.NoError(t, err)
	require.Equal(t, view.AuthEpoch+1, disabled.AuthEpoch)
	require.ErrorIs(t, service.ValidateSecret(context.Background(), view.AK, secret), ErrAuthenticationFailed)
	require.Len(t, store.snapshot(), 1)

	enabled, err := service.SetStatus(context.Background(), view.ID, models.StatusActive, disabled.RowVersion, 7)
	require.NoError(t, err)
	require.Equal(t, disabled.AuthEpoch+1, enabled.AuthEpoch)
	_, err = service.SetStatus(context.Background(), view.ID, models.StatusActive, disabled.RowVersion, 7)
	require.Error(t, err)

	revoked, err := service.SetStatus(context.Background(), view.ID, models.StatusRevoked, enabled.RowVersion, 7)
	require.NoError(t, err)
	require.Equal(t, enabled.AuthEpoch+1, revoked.AuthEpoch)
	require.Len(t, store.snapshot(), 2)
	_, err = service.SetStatus(context.Background(), view.ID, models.StatusActive, revoked.RowVersion, 7)
	require.ErrorIs(t, err, ErrRevoked)
	_, _, err = service.RotateSecret(context.Background(), view.ID, revoked.RowVersion, 7)
	require.ErrorIs(t, err, ErrRevoked)

	withoutStore, _ := newClientTestService(t, nil)
	noStoreView, _ := createTestClient(t, withoutStore)
	_, err = withoutStore.SetStatus(context.Background(), noStoreView.ID, models.StatusDisabled, noStoreView.RowVersion, 7)
	require.ErrorIs(t, err, ErrRevocationUnavailable)
}

func TestOpenAPIClientScopeEpochsRevokeOnlyOneScope(t *testing.T) {
	store := &recordingRevocationStore{}
	service, _ := newClientTestService(t, store)
	view, _ := createTestClient(t, service)

	withList, err := service.SetScope(context.Background(), view.ID, "device:list", true, view.RowVersion, 7)
	require.NoError(t, err)
	withDetail, err := service.SetScope(context.Background(), view.ID, "device:detail", true, withList.RowVersion, 7)
	require.NoError(t, err)
	listBefore, err := service.GetScope(context.Background(), view.ID, "device:list")
	require.NoError(t, err)
	detailBefore, err := service.GetScope(context.Background(), view.ID, "device:detail")
	require.NoError(t, err)

	withoutList, err := service.SetScope(context.Background(), view.ID, "device:list", false, withDetail.RowVersion, 7)
	require.NoError(t, err)
	listAfter, err := service.GetScope(context.Background(), view.ID, "device:list")
	require.NoError(t, err)
	detailAfter, err := service.GetScope(context.Background(), view.ID, "device:detail")
	require.NoError(t, err)
	require.False(t, listAfter.Enabled)
	require.Equal(t, listBefore.ScopeEpoch+1, listAfter.ScopeEpoch)
	require.Equal(t, detailBefore.ScopeEpoch, detailAfter.ScopeEpoch)
	require.Equal(t, withoutList.AuthEpoch, withDetail.AuthEpoch)
	events := store.snapshot()
	require.Len(t, events, 1)
	require.Equal(t, listAfter.ScopeEpoch, events[0].ScopeEpoch)

	regranted, err := service.SetScope(context.Background(), view.ID, "device:list", true, withoutList.RowVersion, 7)
	require.NoError(t, err)
	regrantedScope, err := service.GetScope(context.Background(), view.ID, "device:list")
	require.NoError(t, err)
	require.True(t, regrantedScope.Enabled)
	require.Equal(t, listAfter.ScopeEpoch+1, regrantedScope.ScopeEpoch)
	require.Equal(t, withoutList.AuthEpoch, regranted.AuthEpoch)

	_, err = service.SetScope(context.Background(), view.ID, "unknown:scope", true, regranted.RowVersion, 7)
	require.ErrorIs(t, err, ErrUnknownScope)
}

func TestOpenAPIClientMutationsUseRowVersionCASAndAuditTransaction(t *testing.T) {
	service, db := newClientTestService(t, &recordingRevocationStore{})
	view, _ := createTestClient(t, service)

	updated, err := service.SetScope(context.Background(), view.ID, "device:list", true, view.RowVersion, 7)
	require.NoError(t, err)
	_, _, err = service.RotateSecret(context.Background(), view.ID, view.RowVersion, 8)
	require.ErrorIs(t, err, ErrConflict)
	current, err := service.Get(context.Background(), view.ID)
	require.NoError(t, err)
	require.Equal(t, updated.RowVersion, current.RowVersion)

	var auditCount int64
	require.NoError(t, db.Model(&models.Audit{}).Where("client_id = ?", view.ID).Count(&auditCount).Error)
	require.Equal(t, int64(2), auditCount, "create and the successful CAS mutation only")

	failingStore := &recordingRevocationStore{err: errors.New("persist intent failed")}
	service, db = newClientTestService(t, failingStore)
	view, _ = createTestClient(t, service)
	_, err = service.SetStatus(context.Background(), view.ID, models.StatusDisabled, view.RowVersion, 7)
	require.Error(t, err)
	got, err := service.Get(context.Background(), view.ID)
	require.NoError(t, err)
	require.Equal(t, models.StatusActive, got.Status, "intent failure must roll back status")
	require.Equal(t, view.RowVersion, got.RowVersion, "intent failure must roll back version")
	require.NoError(t, db.Model(&models.Audit{}).Where("client_id = ? AND reason_class = ?", view.ID, "client.disable").Count(&auditCount).Error)
	require.Zero(t, auditCount)

	service, db = newClientTestService(t, &recordingRevocationStore{})
	view, _ = createTestClient(t, service)
	failManagementAuditCreate(t, db)
	_, err = service.SetStatus(context.Background(), view.ID, models.StatusDisabled, view.RowVersion, 7)
	require.Error(t, err)
	got, err = service.Get(context.Background(), view.ID)
	require.NoError(t, err)
	require.Equal(t, models.StatusActive, got.Status)
	require.Equal(t, view.RowVersion, got.RowVersion)
	require.NoError(t, db.Model(&appmodels.SysOperationLog{}).Where("user_id = ? AND path LIKE ?", 7, "%"+fmt.Sprint(view.ID)+"%").Count(&auditCount).Error)
	require.Equal(t, int64(1), auditCount, "only the successful create audit remains")
}

func TestOpenAPIClientOwnerDepartmentIsImmutableAndResponsibleUserIsNonAuthoritative(t *testing.T) {
	service, db := newClientTestService(t, &recordingRevocationStore{})
	view, secret := createTestClient(t, service)

	require.ErrorIs(t, service.UpdateOwnerDeptID(context.Background(), view.ID, 20, view.RowVersion, 7), ErrOwnerDeptImmutable)
	current, err := service.Get(context.Background(), view.ID)
	require.NoError(t, err)
	require.Equal(t, uint(10), current.OwnerDeptID)

	require.NoError(t, db.Model(&models.Client{}).Where("id = ?", view.ID).Update("responsible_user_id", 999).Error)
	require.NoError(t, service.ValidateSecret(context.Background(), view.AK, secret))
	material := mustLoadMaterial(t, service, view.AK)
	require.Equal(t, int64(view.ID), material.ClientID)
	require.Equal(t, int64(1), material.AuthEpoch)
}

func TestOpenAPIClientHandlesMaskedRecordNotFoundAndMissingScope(t *testing.T) {
	service, db := newClientTestService(t, &recordingRevocationStore{})
	view, _ := createTestClient(t, service)
	maskRecordNotFound(t, db)

	_, err := service.Get(context.Background(), view.ID+100)
	require.ErrorIs(t, err, ErrNotFound)
	_, err = service.GetScope(context.Background(), view.ID, "device:list")
	require.ErrorIs(t, err, ErrNotFound)
	_, err = service.SetStatus(context.Background(), view.ID+100, models.StatusDisabled, 1, 7)
	require.ErrorIs(t, err, ErrNotFound)
	_, _, err = service.RotateSecret(context.Background(), view.ID+100, 1, 7)
	require.ErrorIs(t, err, ErrNotFound)

	updated, err := service.SetScope(context.Background(), view.ID, "device:list", true, view.RowVersion, 7)
	require.NoError(t, err, "first scope grant must create a row when missing is masked")
	require.Equal(t, view.RowVersion+1, updated.RowVersion)
	scope, err := service.GetScope(context.Background(), view.ID, "device:list")
	require.NoError(t, err)
	require.True(t, scope.Enabled)
}
