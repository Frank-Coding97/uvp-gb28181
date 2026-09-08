package gb28181

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/recording"

	"github.com/stretchr/testify/require"
)

const recoveryGenerationHelperEnv = "UVP_RECOVERY_GENERATION_HELPER"

type recoveryGenerationHelperResult struct {
	Token string `json:"token,omitempty"`
	Valid bool   `json:"valid"`
}

func TestRecoveryGenerationBindsPlayAcrossFreshProcesses(t *testing.T) {
	active := strings.Repeat("a", 32)
	previous := strings.Repeat("b", 32)
	old := runRecoveryGenerationHelper(t, map[string]string{
		"UVP_RECOVERY_GENERATION_MODE": "play-issue", "UVP_RECOVERY_GENERATION": "old",
		"UVP_RECOVERY_PLAY_ACTIVE": active,
	})
	require.NotEmpty(t, old.Token)
	require.True(t, runRecoveryGenerationHelper(t, map[string]string{
		"UVP_RECOVERY_GENERATION_MODE": "play-verify", "UVP_RECOVERY_GENERATION": "old",
		"UVP_RECOVERY_PLAY_ACTIVE": active, "UVP_RECOVERY_PLAY_TOKEN": old.Token,
	}).Valid)
	require.False(t, runRecoveryGenerationHelper(t, map[string]string{
		"UVP_RECOVERY_GENERATION_MODE": "play-verify", "UVP_RECOVERY_GENERATION": "new",
		"UVP_RECOVERY_PLAY_ACTIVE": active, "UVP_RECOVERY_PLAY_TOKEN": old.Token,
	}).Valid)
	newToken := runRecoveryGenerationHelper(t, map[string]string{
		"UVP_RECOVERY_GENERATION_MODE": "play-issue", "UVP_RECOVERY_GENERATION": "new",
		"UVP_RECOVERY_PLAY_ACTIVE": active,
	}).Token
	require.True(t, runRecoveryGenerationHelper(t, map[string]string{
		"UVP_RECOVERY_GENERATION_MODE": "play-verify", "UVP_RECOVERY_GENERATION": "new",
		"UVP_RECOVERY_PLAY_ACTIVE": active, "UVP_RECOVERY_PLAY_TOKEN": newToken,
	}).Valid)

	previousToken := runRecoveryGenerationHelper(t, map[string]string{
		"UVP_RECOVERY_GENERATION_MODE": "play-issue", "UVP_RECOVERY_GENERATION": "old",
		"UVP_RECOVERY_PLAY_ACTIVE": previous,
	}).Token
	for _, generation := range []string{"old", "new"} {
		result := runRecoveryGenerationHelper(t, map[string]string{
			"UVP_RECOVERY_GENERATION_MODE": "play-verify", "UVP_RECOVERY_GENERATION": generation,
			"UVP_RECOVERY_PLAY_ACTIVE": active, "UVP_RECOVERY_PLAY_PREVIOUS": previous,
			"UVP_RECOVERY_PLAY_TOKEN": previousToken,
		})
		require.Equal(t, generation == "old", result.Valid, "previous key generation=%s", generation)
	}
}

func TestRecoveryGenerationBindsRecordingEnvAcrossFreshProcesses(t *testing.T) {
	for _, source := range []string{"UVP_CLOUD_RECORDING_CAPABILITY_KEY", "UVP_RECOVERY_JWT_ROOT"} {
		t.Run(source, func(t *testing.T) { testRecordingRecoveryGeneration(t, source) })
	}
}

func testRecordingRecoveryGeneration(t *testing.T, source string) {
	root := strings.Repeat("c", 32)
	issue := func(generation string) string {
		return runRecoveryGenerationHelper(t, map[string]string{
			"UVP_RECOVERY_GENERATION_MODE": "recording-issue", "UVP_RECOVERY_GENERATION": generation,
			source: root,
		}).Token
	}
	verify := func(generation, token string) bool {
		return runRecoveryGenerationHelper(t, map[string]string{
			"UVP_RECOVERY_GENERATION_MODE": "recording-verify", "UVP_RECOVERY_GENERATION": generation,
			source: root, "UVP_RECOVERY_RECORDING_TOKEN": token,
		}).Valid
	}
	old := issue("old")
	require.True(t, verify("old", old))
	require.False(t, verify("new", old))
	require.True(t, verify("new", issue("new")))
	legacy := issue("")
	require.True(t, verify("", legacy))
	require.False(t, verify("new", legacy))
}

func TestRecoveryGenerationDerivationDomainAndLegacyCompatibility(t *testing.T) {
	root := []byte("fixed-recovery-root")
	mac := hmac.New(sha256.New, root)
	_, err := mac.Write([]byte("uvp-gb28181/recovery-generation/play/v1\x00generation-a"))
	require.NoError(t, err)
	require.Equal(t, mac.Sum(nil), bindRecoveryGeneration(root, "play", "generation-a"))
	require.Equal(t, root, bindRecoveryGeneration(root, "play", ""))
	require.Equal(t, root, bindRecoveryGeneration(root, "play", "  "))
	require.NotEqual(t, bindRecoveryGeneration(root, "play", "generation-a"), bindRecoveryGeneration(root, "recording", "generation-a"))
}

func TestRecoveryGenerationDoesNotUpgradeWeakRoots(t *testing.T) {
	settings := gbconfig.PlayAuthSettings{Enabled: true}
	_, err := buildPlaySigner(settings, "short", "", "jwt-root", "zlm-root", "generation-a")
	require.ErrorIs(t, err, playauth.ErrKeyInvalid)
	_, err = buildPlaySigner(settings, strings.Repeat("a", 32), "short", "jwt-root", "zlm-root", "generation-a")
	require.ErrorIs(t, err, playauth.ErrKeyInvalid)
	_, err = buildRecordingCapabilitySigner([]byte("short"), "generation-a")
	require.ErrorIs(t, err, recording.ErrCapabilityKey)
}

func recoveryGenerationBinding() playauth.Binding {
	return playauth.Binding{
		DeviceID: "37010301021320000014", ChannelID: "37010301021320000001",
		App: "rtp", Stream: "37010301021320000014_37010301021320000001", MediaServerID: "node-a",
	}
}

func runRecoveryGenerationHelper(t *testing.T, values map[string]string) recoveryGenerationHelperResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRecoveryGenerationHelper$", "-test.count=1")
	overrides := map[string]string{recoveryGenerationHelperEnv: "1", "UVP_RECOVERY_JWT_ROOT": "", "UVP_CLOUD_RECORDING_CAPABILITY_KEY": "", "UVP_RECOVERY_PLAY_PREVIOUS": ""}
	for key, value := range values {
		overrides[key] = value
	}
	for _, entry := range os.Environ() {
		key := entry
		if separator := strings.IndexByte(entry, '='); separator >= 0 {
			key = entry[:separator]
		}
		if _, overridden := overrides[key]; !overridden {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	for key, value := range overrides {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	raw, err := cmd.Output()
	if err != nil {
		t.Fatalf("recovery generation helper failed: %v", err)
	}
	var result recoveryGenerationHelperResult
	require.NoError(t, json.NewDecoder(bytes.NewReader(raw)).Decode(&result))
	return result
}

func TestRecoveryGenerationHelper(t *testing.T) {
	if os.Getenv(recoveryGenerationHelperEnv) != "1" {
		t.Skip("subprocess helper")
	}
	mode := os.Getenv("UVP_RECOVERY_GENERATION_MODE")
	generation := os.Getenv("UVP_RECOVERY_GENERATION")
	result := recoveryGenerationHelperResult{}
	switch mode {
	case "play-issue", "play-verify":
		active := os.Getenv("UVP_RECOVERY_PLAY_ACTIVE")
		previous := os.Getenv("UVP_RECOVERY_PLAY_PREVIOUS")
		signer, err := buildPlaySigner(gbconfig.PlayAuthSettings{Enabled: true}, active, previous, "jwt-root", "zlm-root", generation)
		require.NoError(t, err)
		if mode == "play-issue" {
			grant, issueErr := signer.IssueDirect(recoveryGenerationBinding())
			require.NoError(t, issueErr)
			result.Token = grant.Token
		} else {
			_, verifyErr := signer.Verify(os.Getenv("UVP_RECOVERY_PLAY_TOKEN"), recoveryGenerationBinding())
			result.Valid = verifyErr == nil
		}
	case "recording-issue", "recording-verify":
		root := []byte(os.Getenv("UVP_CLOUD_RECORDING_CAPABILITY_KEY"))
		if len(root) == 0 {
			derived, err := recording.DeriveCapabilityKey([]byte(os.Getenv("UVP_RECOVERY_JWT_ROOT")))
			require.NoError(t, err)
			root = derived
		}
		signer, err := buildRecordingCapabilitySigner(root, generation)
		require.NoError(t, err)
		if mode == "recording-issue" {
			grant, issueErr := signer.Issue("41", 7, recording.CapabilityModePlay, nil)
			require.NoError(t, issueErr)
			result.Token = grant.Token
		} else {
			_, verifyErr := signer.Verify(os.Getenv("UVP_RECOVERY_RECORDING_TOKEN"), "41", recording.CapabilityModePlay)
			result.Valid = verifyErr == nil
		}
	default:
		t.Fatalf("unknown recovery generation helper mode")
	}
	require.NoError(t, json.NewEncoder(os.Stdout).Encode(result))
}
