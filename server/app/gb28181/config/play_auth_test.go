package config

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

type playAuthMutableSource struct {
	values    map[string]interface{}
	saveErr   error
	saveCalls int
}

func (s *playAuthMutableSource) Get(key string) interface{} { return s.values[key] }
func (s *playAuthMutableSource) GetBool(key string) bool {
	value, _ := s.values[key].(bool)
	return value
}
func (s *playAuthMutableSource) GetString(key string) string {
	value, _ := s.values[key].(string)
	return value
}
func (s *playAuthMutableSource) GetInt(key string) int {
	value, _ := s.values[key].(int)
	return value
}
func (s *playAuthMutableSource) GetStringSlice(string) []string    { return nil }
func (s *playAuthMutableSource) Set(key string, value interface{}) { s.values[key] = value }
func (s *playAuthMutableSource) SaveConfig() error                 { s.saveCalls++; return s.saveErr }

func TestPlayAuthSettingsDefaultAndDependencies(t *testing.T) {
	require.Equal(t, PlayAuthSettings{TTLSeconds: DefaultPlayAuthTTLSeconds}, PlayAuthSettingsFrom(&playAuthMutableSource{values: map[string]interface{}{}}))
	require.Error(t, ValidatePlayAuthSettings(PlayAuthSettings{BindClientIP: true}, FixedAddressPlaybackSettings{}))
	require.NoError(t, ValidatePlayAuthSettings(PlayAuthSettings{TTLSeconds: DefaultPlayAuthTTLSeconds}, FixedAddressPlaybackSettings{FixedAddressEnabled: true, AutoOnDemandEnabled: true}))
	require.Error(t, ValidatePlayAuthSettings(PlayAuthSettings{TTLSeconds: MinPlayAuthTTLSeconds - 1}, FixedAddressPlaybackSettings{}))
	require.Error(t, ValidatePlayAuthSettings(PlayAuthSettings{TTLSeconds: MaxPlayAuthTTLSeconds + 1}, FixedAddressPlaybackSettings{}))
	require.NoError(t, ValidatePlayAuthSettings(PlayAuthSettings{Enabled: true, BindClientIP: true, TTLSeconds: 300}, FixedAddressPlaybackSettings{FixedAddressEnabled: true, AutoOnDemandEnabled: true}))
}

func TestSavePlayAuthSettingsRollsBackOnFailure(t *testing.T) {
	source := &playAuthMutableSource{values: map[string]interface{}{
		PlayAuthEnabledConfigKey:      false,
		PlayAuthBindClientIPConfigKey: false,
		PlayAuthTTLSecondsConfigKey:   DefaultPlayAuthTTLSeconds,
		FixedAddressEnabledConfigKey:  true,
		AutoOnDemandEnabledConfigKey:  false,
	}, saveErr: errors.New("disk full")}
	err := SavePlayAuthSettings(source, PlayAuthSettings{Enabled: true, BindClientIP: true, TTLSeconds: 300})
	require.Error(t, err)
	require.False(t, source.GetBool(PlayAuthEnabledConfigKey))
	require.False(t, source.GetBool(PlayAuthBindClientIPConfigKey))
	require.Equal(t, DefaultPlayAuthTTLSeconds, source.GetInt(PlayAuthTTLSecondsConfigKey))
}

func TestEnsurePlayAuthActiveKeyGeneratesAndPersistsMissingKey(t *testing.T) {
	source := &playAuthMutableSource{values: map[string]interface{}{}}
	reader := bytes.NewReader(bytes.Repeat([]byte{0x2a}, 32))

	err := ensurePlayAuthActiveKey(source, reader)

	require.NoError(t, err)
	active := source.GetString(PlayAuthActiveKeyConfigKey)
	require.NotEmpty(t, active)
	require.Equal(t, "KioqKioqKioqKioqKioqKioqKioqKioqKioqKioqKio", active)
	require.Equal(t, 1, source.saveCalls)
	require.Empty(t, source.GetString(PlayAuthPreviousKeyConfigKey))
}

func TestEnsurePlayAuthActiveKeyDoesNotOverwriteExistingKey(t *testing.T) {
	source := &playAuthMutableSource{values: map[string]interface{}{
		PlayAuthActiveKeyConfigKey:   "existing-active-key",
		PlayAuthPreviousKeyConfigKey: "existing-previous-key",
	}}

	err := ensurePlayAuthActiveKey(source, bytes.NewReader(bytes.Repeat([]byte{0x2a}, 32)))

	require.NoError(t, err)
	require.Equal(t, "existing-active-key", source.GetString(PlayAuthActiveKeyConfigKey))
	require.Equal(t, "existing-previous-key", source.GetString(PlayAuthPreviousKeyConfigKey))
	require.Zero(t, source.saveCalls)
}

func TestEnsurePlayAuthActiveKeyDoesNotPersistOnRandomFailure(t *testing.T) {
	source := &playAuthMutableSource{values: map[string]interface{}{}}

	err := ensurePlayAuthActiveKey(source, failingReader{})

	require.Error(t, err)
	require.Empty(t, source.GetString(PlayAuthActiveKeyConfigKey))
	require.Zero(t, source.saveCalls)
}

func TestEnsurePlayAuthActiveKeyRollsBackOnSaveFailure(t *testing.T) {
	source := &playAuthMutableSource{
		values:  map[string]interface{}{PlayAuthActiveKeyConfigKey: ""},
		saveErr: errors.New("disk full"),
	}

	err := ensurePlayAuthActiveKey(source, bytes.NewReader(bytes.Repeat([]byte{0x2a}, 32)))

	require.Error(t, err)
	require.Empty(t, source.GetString(PlayAuthActiveKeyConfigKey))
	require.Equal(t, 1, source.saveCalls)
}

func TestEnsurePlayAuthActiveKeyGeneratesKeyAcceptedByPlaySigner(t *testing.T) {
	source := &playAuthMutableSource{values: map[string]interface{}{}}

	require.NoError(t, ensurePlayAuthActiveKey(source, bytes.NewReader(bytes.Repeat([]byte{0x2a}, 32))))
	active, previous := PlayAuthKeyMaterialFrom(source)

	require.NotEmpty(t, active)
	require.Empty(t, previous)
	_, err := playauth.NewSigner([]byte(active))
	require.NoError(t, err)
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("entropy unavailable") }
