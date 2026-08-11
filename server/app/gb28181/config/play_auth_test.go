package config

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type playAuthMutableSource struct {
	values  map[string]interface{}
	saveErr error
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
func (s *playAuthMutableSource) GetInt(string) int                 { return 0 }
func (s *playAuthMutableSource) GetStringSlice(string) []string    { return nil }
func (s *playAuthMutableSource) Set(key string, value interface{}) { s.values[key] = value }
func (s *playAuthMutableSource) SaveConfig() error                 { return s.saveErr }

func TestPlayAuthSettingsDefaultAndDependencies(t *testing.T) {
	require.Equal(t, PlayAuthSettings{}, PlayAuthSettingsFrom(&playAuthMutableSource{values: map[string]interface{}{}}))
	require.Error(t, ValidatePlayAuthSettings(PlayAuthSettings{BindClientIP: true}, FixedAddressPlaybackSettings{}))
	require.Error(t, ValidatePlayAuthSettings(PlayAuthSettings{}, FixedAddressPlaybackSettings{FixedAddressEnabled: true, AutoOnDemandEnabled: true}))
	require.NoError(t, ValidatePlayAuthSettings(PlayAuthSettings{Enabled: true, BindClientIP: true}, FixedAddressPlaybackSettings{FixedAddressEnabled: true, AutoOnDemandEnabled: true}))
}

func TestSavePlayAuthSettingsRollsBackOnFailure(t *testing.T) {
	source := &playAuthMutableSource{values: map[string]interface{}{
		PlayAuthEnabledConfigKey:      false,
		PlayAuthBindClientIPConfigKey: false,
		FixedAddressEnabledConfigKey:  true,
		AutoOnDemandEnabledConfigKey:  false,
	}, saveErr: errors.New("disk full")}
	err := SavePlayAuthSettings(source, PlayAuthSettings{Enabled: true, BindClientIP: true})
	require.Error(t, err)
	require.False(t, source.GetBool(PlayAuthEnabledConfigKey))
	require.False(t, source.GetBool(PlayAuthBindClientIPConfigKey))
}
