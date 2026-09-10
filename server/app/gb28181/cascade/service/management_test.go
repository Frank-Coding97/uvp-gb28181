package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/repository"
	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/securestore"
)

func TestManagementServiceCreatesAndUpdatesWriteOnlyPassword(t *testing.T) {
	store := newManagementStoreFake()
	sealer := &credentialSealerFake{}
	runtime := &managementRuntimeFake{}
	service := NewManagementService(store, sealer, runtime, fakeClock{now: time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)})
	password := "密钥-one"

	created, err := service.Create(context.Background(), validPlatformInput(&password))
	require.NoError(t, err)
	require.NotZero(t, created.ID)
	require.True(t, created.HasPassword)
	encoded, err := json.Marshal(created)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), password)
	require.Equal(t, []string{password}, sealer.plaintexts)
	require.Equal(t, 1, runtime.reloads)

	empty := ""
	update := validPlatformInput(&empty)
	update.Host = "198.51.100.20"
	updated, err := service.Update(context.Background(), created.ID, created.ConfigRevision, update)
	require.NoError(t, err)
	require.Equal(t, "198.51.100.20", updated.Host)
	require.True(t, updated.HasPassword)
	require.Equal(t, []string{password}, sealer.plaintexts, "empty password must not replace the stored credential")
	require.Equal(t, 2, runtime.reloads)

	stored, err := store.FindPlatform(context.Background(), created.ID)
	require.NoError(t, err)
	require.Equal(t, []byte("cipher:"+password), stored.SecretCiphertext)
}

func TestManagementServiceValidatesConfigBeforePersistence(t *testing.T) {
	store := newManagementStoreFake()
	service := NewManagementService(store, &credentialSealerFake{}, nil, fakeClock{now: time.Now()})

	invalid := validPlatformInput(nil)
	invalid.LocalDeviceID = "not-a-gb-id"
	_, err := service.Create(context.Background(), invalid)
	require.ErrorIs(t, err, ErrInvalidPlatformConfig)
	require.Empty(t, store.platforms)

	invalid = validPlatformInput(nil)
	invalid.LocalSIPIP = "not-an-ip"
	_, err = service.Create(context.Background(), invalid)
	require.ErrorIs(t, err, ErrInvalidPlatformConfig)
	require.Empty(t, store.platforms)

	invalid = validPlatformInput(nil)
	invalid.Port = 70000
	_, err = service.Create(context.Background(), invalid)
	require.ErrorIs(t, err, ErrInvalidPlatformConfig)
	require.Empty(t, store.platforms)
}

func TestManagementServiceEnableReconnectAndDeleteRespectRuntimeAndSessions(t *testing.T) {
	store := newManagementStoreFake()
	runtime := &managementRuntimeFake{}
	service := NewManagementService(store, &credentialSealerFake{}, runtime, fakeClock{now: time.Now()})
	input := validPlatformInput(nil)
	input.Enabled = false
	created, err := service.Create(context.Background(), input)
	require.NoError(t, err)
	require.Zero(t, runtime.reloads)

	enabled, err := service.SetEnabled(context.Background(), created.ID, created.ConfigRevision, true)
	require.NoError(t, err)
	require.True(t, enabled.Enabled)
	require.Equal(t, 1, runtime.reloads)
	runtime.ids = []uint64{created.ID}
	require.NoError(t, service.Reconnect(context.Background(), created.ID))
	require.Equal(t, []uint64{created.ID}, runtime.reconnects)

	store.sessions[created.ID] = []model.GbCascadeMediaSession{{PlatformID: created.ID, State: model.CascadeMediaSessionStateActive}}
	err = service.Delete(context.Background(), created.ID)
	require.ErrorIs(t, err, ErrPlatformHasActiveSessions)
	_, err = store.FindPlatform(context.Background(), created.ID)
	require.NoError(t, err)

	store.sessions[created.ID] = nil
	err = service.Delete(context.Background(), created.ID)
	require.NoError(t, err)
	_, err = store.FindPlatform(context.Background(), created.ID)
	require.ErrorIs(t, err, repository.ErrPlatformNotFound)
}

func TestManagementServiceProjectionOperationsStayPlatformScoped(t *testing.T) {
	store := newManagementStoreFake()
	runtime := &managementRuntimeFake{}
	service := NewManagementService(store, &credentialSealerFake{}, runtime, fakeClock{now: time.Now()})
	inputA := validPlatformInput(nil)
	inputA.Enabled = false
	inputB := inputA
	inputB.Name = "upstream-b"
	inputB.LocalDeviceID = "34020000002000000002"
	a, err := service.Create(context.Background(), inputA)
	require.NoError(t, err)
	b, err := service.Create(context.Background(), inputB)
	require.NoError(t, err)

	devices := []repository.DeviceProjectionInput{{SourceDeviceID: 1, PublishedDeviceID: "34020000001320000001"}}
	channels := []repository.ChannelProjectionInput{{SourceDeviceID: 1, SourceChannelID: 2, PublishedChannelID: "34020000001320000011"}}
	beforeReload := runtime.reloads
	require.NoError(t, service.ReplaceProjection(context.Background(), a.ID, 0, devices, channels))
	require.Equal(t, beforeReload+1, runtime.reloads)
	snapshotA, err := service.Projection(context.Background(), a.ID)
	require.NoError(t, err)
	require.Len(t, snapshotA.Channels, 1)
	snapshotB, err := service.Projection(context.Background(), b.ID)
	require.NoError(t, err)
	require.Empty(t, snapshotB.Channels)

	err = service.ReplaceProjection(context.Background(), 999, 0, devices, channels)
	require.ErrorIs(t, err, repository.ErrPlatformNotFound)
}

func validPlatformInput(password *string) PlatformConfigInput {
	return PlatformConfigInput{
		Name: "upstream-a", UpstreamServerID: "34020000002000000001", UpstreamDomain: "3402000000",
		Host: "192.0.2.10", Port: 5060, LocalDeviceID: "34020000002000000009", LocalDomain: "3402000000",
		LocalSIPIP: "192.0.2.20", LocalSIPPort: 5060, AuthUsername: "34020000002000000009", Password: password,
		ProfileOverride: model.CascadeProfileOverrideAuto, Transport: "UDP", RegisterExpires: 3600,
		KeepaliveInterval: 60, CatalogBatchSize: 100, MaxStreams: 4, Enabled: true,
	}
}

type credentialSealerFake struct {
	plaintexts []string
}

func (s *credentialSealerFake) Encrypt(_ string, plaintext []byte) (securestore.Envelope, error) {
	s.plaintexts = append(s.plaintexts, string(plaintext))
	return securestore.Envelope{Ciphertext: []byte("cipher:" + string(plaintext)), Nonce: []byte("nonce"), Algorithm: securestore.AlgorithmAES256GCM, KeyVersion: "v1"}, nil
}

type managementRuntimeFake struct {
	reloads    int
	reconnects []uint64
	ids        []uint64
	err        error
}

func (r *managementRuntimeFake) Reload(context.Context) error {
	r.reloads++
	return r.err
}
func (r *managementRuntimeFake) Reconnect(id uint64)   { r.reconnects = append(r.reconnects, id) }
func (r *managementRuntimeFake) PlatformIDs() []uint64 { return append([]uint64(nil), r.ids...) }

type managementStoreFake struct {
	nextID      uint64
	platforms   map[uint64]model.GbCascadePlatform
	projections map[uint64]repository.ProjectionSnapshot
	sessions    map[uint64][]model.GbCascadeMediaSession
}

func newManagementStoreFake() *managementStoreFake {
	return &managementStoreFake{
		platforms: make(map[uint64]model.GbCascadePlatform), projections: make(map[uint64]repository.ProjectionSnapshot),
		sessions: make(map[uint64][]model.GbCascadeMediaSession),
	}
}

func (s *managementStoreFake) CreatePlatform(_ context.Context, platform *model.GbCascadePlatform) error {
	s.nextID++
	platform.ID = s.nextID
	if platform.ConfigRevision == 0 {
		platform.ConfigRevision = 1
	}
	s.platforms[platform.ID] = *platform
	return nil
}

func (s *managementStoreFake) FindPlatform(_ context.Context, id uint64) (*model.GbCascadePlatform, error) {
	platform, ok := s.platforms[id]
	if !ok {
		return nil, repository.ErrPlatformNotFound
	}
	copy := platform
	return &copy, nil
}

func (s *managementStoreFake) ListPlatforms(context.Context) ([]model.GbCascadePlatform, error) {
	items := make([]model.GbCascadePlatform, 0, len(s.platforms))
	for _, platform := range s.platforms {
		items = append(items, platform)
	}
	return items, nil
}

func (s *managementStoreFake) UpdatePlatformConfig(_ context.Context, platform *model.GbCascadePlatform, revision uint64) (*model.GbCascadePlatform, error) {
	stored, ok := s.platforms[platform.ID]
	if !ok {
		return nil, repository.ErrPlatformNotFound
	}
	if stored.ConfigRevision != revision {
		return nil, repository.ErrRevisionConflict
	}
	copy := *platform
	copy.ConfigRevision = revision + 1
	s.platforms[platform.ID] = copy
	return &copy, nil
}

func (s *managementStoreFake) SoftDeletePlatform(_ context.Context, id uint64) error {
	if _, ok := s.platforms[id]; !ok {
		return repository.ErrPlatformNotFound
	}
	delete(s.platforms, id)
	delete(s.projections, id)
	return nil
}

func (s *managementStoreFake) ReplaceProjection(_ context.Context, platformID uint64, _ uint64, devices []repository.DeviceProjectionInput, channels []repository.ChannelProjectionInput) error {
	platform, ok := s.platforms[platformID]
	if !ok {
		return repository.ErrPlatformNotFound
	}
	snapshot := repository.ProjectionSnapshot{Platform: platform, Revision: s.projections[platformID].Revision + 1}
	for index, input := range devices {
		snapshot.Devices = append(snapshot.Devices, model.GbCascadeDeviceProjection{ID: uint64(index + 1), PlatformID: platformID, SourceDeviceID: input.SourceDeviceID, PublishedDeviceID: input.PublishedDeviceID, Active: true})
	}
	for index, input := range channels {
		snapshot.Channels = append(snapshot.Channels, model.GbCascadeChannelProjection{ID: uint64(index + 1), PlatformID: platformID, SourceChannelID: input.SourceChannelID, PublishedChannelID: input.PublishedChannelID, Active: true})
	}
	s.projections[platformID] = snapshot
	return nil
}

func (s *managementStoreFake) ProjectionSnapshot(_ context.Context, platformID uint64) (*repository.ProjectionSnapshot, error) {
	platform, ok := s.platforms[platformID]
	if !ok {
		return nil, repository.ErrPlatformNotFound
	}
	snapshot := s.projections[platformID]
	snapshot.Platform = platform
	return &snapshot, nil
}

func (s *managementStoreFake) ListNonterminalMediaSessions(_ context.Context, platformID uint64) ([]model.GbCascadeMediaSession, error) {
	return append([]model.GbCascadeMediaSession(nil), s.sessions[platformID]...), nil
}

var _ ManagementStore = (*managementStoreFake)(nil)
var _ ManagementRuntime = (*managementRuntimeFake)(nil)
var _ CredentialSealer = (*credentialSealerFake)(nil)

func TestManagementServiceTypedNilCipherReturnsCredentialUnavailable(t *testing.T) {
	var cipher *securestore.Cipher
	svc := NewManagementService(newManagementStoreFake(), cipher, &managementRuntimeFake{}, fakeClock{})
	password := "test-password"
	result, err := svc.Create(context.Background(), validPlatformInput(&password))
	require.Nil(t, result)
	require.ErrorIs(t, err, ErrCredentialUnavailable)
	require.ErrorIs(t, err, securestore.ErrKeyUnavailable)
}

func TestManagementViewDetectsLostKeyAndPasswordReplacement(t *testing.T) {
	oldKey, err := securestore.NewCipher([]byte("01234567890123456789012345678901"), "v1")
	require.NoError(t, err)
	newKey, err := securestore.NewCipher([]byte("11234567890123456789012345678901"), "v1")
	require.NoError(t, err)
	svc := NewManagementService(newManagementStoreFake(), oldKey, &managementRuntimeFake{}, fakeClock{})
	platform := model.GbCascadePlatform{}
	password := "original"
	require.NoError(t, svc.applyCredential(&platform, &password))
	require.False(t, svc.view(platform).CredentialNeedsReset)
	svc.sealer = newKey
	require.True(t, svc.view(platform).CredentialNeedsReset)
	password = "replacement"
	require.NoError(t, svc.applyCredential(&platform, &password))
	require.False(t, svc.view(platform).CredentialNeedsReset)
}
