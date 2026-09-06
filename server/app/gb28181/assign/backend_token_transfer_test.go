package assign

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

func TestOpenAPIDeviceTransferInvalidatesBackendTokensWithoutGlobalRevocation(t *testing.T) {
	db := newAssignTestDB(t)
	device := seedAssignedDeviceWithCode(t, db, "34020000002000100021")
	other := seedAssignedDeviceWithCode(t, db, "34020000002000100022")
	now := fixedTransferClock()
	root := []byte("backend-transfer-fixture-key-00001")
	signer, err := playauth.NewKeyring(playauth.KeyMaterial{ID: "transfer-fixture", Secret: root}, nil,
		playauth.WithNow(func() time.Time { return now }))
	require.NoError(t, err)
	registry := playauth.NewAuthorizationRegistry(playauth.WithAuthorizationRegistryNow(func() time.Time { return now }))
	authorization := playauth.NewAuthorizationService(signer, registry,
		playauth.WithDeviceSecurityAuthority(playauth.NewDeviceSecurityStore(db)))
	ctx := context.Background()
	type tokenCase struct {
		token   string
		binding playauth.Binding
		target  bool
	}
	var cases []tokenCase
	for _, deviceID := range []string{device.DeviceID, other.DeviceID} {
		for _, generation := range []uint64{0, 17} {
			binding := playauth.Binding{DeviceID: deviceID, ChannelID: "34020000002000110021",
				DeviceEpoch: 1, App: "rtp", Stream: deviceID + "-stream", MediaServerID: "backend-node", MediaGeneration: generation}
			grant, err := authorization.IssueDirectContext(ctx, binding)
			require.NoError(t, err)
			cases = append(cases, tokenCase{grant.Token, binding, deviceID == device.DeviceID})
			binding.DeviceEpoch = 0
			legacy := legacyBackendTransferFixture(t, root, signer, registry, binding)
			cases = append(cases, tokenCase{legacy, binding, deviceID == device.DeviceID})
		}
	}
	for _, test := range cases {
		_, err := authorization.VerifyContext(ctx, test.token, test.binding)
		require.NoError(t, err)
	}
	globalCutoff := playauth.RevokedBefore()
	transfer := NewService(db, validatorVisibleDept1, WithTransferClock(func() time.Time { return now }))
	receipt, err := transfer.AssignOneWithReceipt(ctx, device.ID, 2, nil, false)
	require.NoError(t, err)
	require.EqualValues(t, 2, receipt.NewEpoch)
	require.Equal(t, now.Unix()+1, receipt.LegacyRevokedBefore.Unix())
	require.Equal(t, globalCutoff, playauth.RevokedBefore())
	verifyCases := func() {
		for _, test := range cases {
			_, err := authorization.VerifyContext(ctx, test.token, test.binding)
			if test.target {
				require.ErrorIs(t, err, playauth.ErrTokenRevoked)
			} else {
				require.NoError(t, err)
			}
		}
	}
	verifyCases()
	_, err = transfer.AssignOneWithReceipt(ctx, device.ID, 1, nil, false)
	require.NoError(t, err)
	verifyCases()
	// A fresh service/store cannot revive old hot tokens after restart.
	restarted := playauth.NewAuthorizationService(signer, playauth.NewAuthorizationRegistry(),
		playauth.WithDeviceSecurityAuthority(playauth.NewDeviceSecurityStore(db)))
	for _, test := range cases {
		if test.binding.MediaGeneration == 0 {
			continue
		}
		_, err := restarted.VerifyContext(ctx, test.token, test.binding)
		if test.target {
			require.ErrorIs(t, err, playauth.ErrTokenRevoked)
		} else {
			require.NoError(t, err)
		}
	}
	current := cases[0].binding
	current.DeviceEpoch = 3
	_, err = authorization.IssueDirectContext(ctx, current)
	require.NoError(t, err, "a newly authorized snapshot can issue without reviving old tokens")
}

// Legacy issuance exists only in this test fixture. Production issues v4.
func legacyBackendTransferFixture(t *testing.T, root []byte, signer *playauth.Signer, registry *playauth.AuthorizationRegistry, binding playauth.Binding) string {
	t.Helper()
	prepared, err := signer.Prepare()
	require.NoError(t, err)
	if binding.MediaGeneration == 0 {
		require.NoError(t, registry.Register(prepared, binding))
	}
	claims := playauth.Claims{Version: 2, Audience: "gb28181-play", Mode: playauth.ModeDirect, KeyID: "transfer-fixture",
		DeviceID: binding.DeviceID, ChannelID: binding.ChannelID, App: binding.App, Stream: binding.Stream,
		MediaServerID: binding.MediaServerID, MediaGeneration: binding.MediaGeneration,
		IssuedAt: prepared.IssuedAt.Unix(), ExpiresAt: prepared.ExpiresAt.Unix(), Nonce: prepared.Nonce,
		AuthorizationGeneration: prepared.AuthorizationGeneration}
	payload, err := json.Marshal(claims)
	require.NoError(t, err)
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	derive := hmac.New(sha256.New, root)
	_, err = derive.Write([]byte("uvp-gb28181/play-authorization/v2"))
	require.NoError(t, err)
	sign := hmac.New(sha256.New, derive.Sum(nil))
	_, err = sign.Write([]byte(encoded))
	require.NoError(t, err)
	return encoded + "." + base64.RawURLEncoding.EncodeToString(sign.Sum(nil))
}
