package uac

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/emiago/sipgo"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

func playbackIntentStoreFixture(t *testing.T) (*UAC, *gorm.DB, *playauth.DeviceOperationIntentStore, playauth.DeviceOperationIntentIdentity, *snapshotTransactionObserver) {
	t.Helper()
	ua, err := sipgo.NewUA()
	require.NoError(t, err)
	t.Cleanup(func() { _ = ua.Close() })
	u, err := New(ua, "34020000002000000001", "3402000000", "192.0.2.1", 5061, false)
	require.NoError(t, err)
	observer := &snapshotTransactionObserver{}
	u.client.TxRequester = observer
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "intent.sqlite")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, db.AutoMigrate(&playauth.DeviceOperationIntent{}))
	require.NoError(t, db.Exec("ALTER TABLE gb_device_operation_intent ADD COLUMN sip_steps_json TEXT NULL").Error)
	require.NoError(t, db.Exec("CREATE TABLE gb_device (id BIGINT PRIMARY KEY, device_id TEXT, access_epoch BIGINT, cleanup_completed_epoch BIGINT, deleted_at DATETIME)").Error)
	require.NoError(t, db.Exec("CREATE TABLE gb_channel (id BIGINT PRIMARY KEY, device_id TEXT, channel_id TEXT, deleted_at DATETIME)").Error)
	in := validPlaybackInvite()
	require.NoError(t, db.Exec("INSERT INTO gb_device VALUES (1,?,1,1,NULL)", in.DeviceID).Error)
	require.NoError(t, db.Exec("INSERT INTO gb_channel VALUES (11,?,?,NULL)", in.DeviceID, in.ChannelID).Error)
	id := playauth.DeviceOperationIntentIdentity{OperationID: strings.Repeat("a", 32), DevicePK: 1, DeviceCode: in.DeviceID, DeviceEpoch: 1, TargetScope: "channel", TargetPK: 11, TargetCode: in.ChannelID, Kind: "playback"}
	store := playauth.NewDeviceOperationIntentStore(db)
	_, err = store.Reserve(context.Background(), id)
	require.NoError(t, err)
	_, err = store.Dispatch(context.Background(), id, 1)
	require.NoError(t, err)
	return u, db, store, id, observer
}

func TestPlaybackIntentStoredPreparationMatchesActualBuilder(t *testing.T) {
	u, db, store, id, observer := playbackIntentStoreFixture(t)
	in := validPlaybackInvite()
	request, stored, err := u.prepareStoredPlaybackInvite(context.Background(), store, id, 2, strings.Repeat("b", 32), in)
	require.NoError(t, err)
	require.Nil(t, observer.request, "preparation must never start a SIP transaction")
	require.Len(t, stored.Steps, 1)
	require.Equal(t, playauth.SIPStepPrepared, stored.Steps[0].State)
	i := stored.Steps[0].Identity
	tag, _ := request.From().Params.Get("tag")
	branch, _ := request.Via().Params.Get("branch")
	rport, hasRport := request.Via().Params.Get("rport")
	digest := sha256.Sum256(request.Body())
	require.Equal(t, playauth.DeviceSIPInviteIdentity{StepID: strings.Repeat("b", 32),
		CallID: string(*request.CallID()), CSeq: request.CSeq().SeqNo, RequestURI: request.Recipient.String(),
		FromURI: request.From().Address.String(), LocalTag: tag, ToURI: request.To().Address.String(), ContactURI: request.Contact().Address.String(),
		Transport: request.Transport(), Destination: request.Destination(), ViaHost: request.Via().Host, ViaPort: request.Via().Port,
		ViaTransport: request.Via().Transport, Branch: branch, RPortPresent: hasRport, RPortValue: rport, MaxForwards: uint32(*request.MaxForwards()),
		ContentType: string(*request.ContentType()), BodyLength: len(request.Body()), BodySHA256: hex.EncodeToString(digest[:])}, i)
	loaded, err := playauth.NewDeviceOperationIntentStore(db).LoadSIPInviteSteps(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, stored, loaded)
	encoded, err := json.Marshal(loaded)
	require.NoError(t, err)
	require.Equal(t, "{}", string(encoded))
	var raw string
	require.NoError(t, db.Table("gb_device_operation_intent").Select("sip_steps_json").Scan(&raw).Error)
	require.NotContains(t, raw, "v=0")
	require.NotContains(t, raw, "s=Playback")
	// Returned preparation is not a reusable dispatch token. Calling the
	// builder again cannot replace the persisted Call-ID under the same StepID.
	request, duplicate, err := u.prepareStoredPlaybackInvite(context.Background(), store, id, 3, i.StepID, in)
	require.ErrorIs(t, err, playauth.ErrDeviceIntentConflict)
	require.Nil(t, request)
	require.Empty(t, duplicate.Intent.OperationID)
	require.Nil(t, observer.request)
}

func TestPlaybackIntentStoredPreparationRejectsWrongBinding(t *testing.T) {
	for _, mismatch := range []string{"device", "channel", "kind", "scope", "step", "nil-store", "nil-context", "nil-uac", "cancelled"} {
		t.Run(mismatch, func(t *testing.T) {
			u, _, store, id, observer := playbackIntentStoreFixture(t)
			in, stepID := validPlaybackInvite(), strings.Repeat("b", 32)
			ctx := context.Background()
			switch mismatch {
			case "device":
				in.DeviceID = "34020000002000000011"
			case "channel":
				in.ChannelID = "34020000001320000002"
			case "kind":
				id.Kind = "live"
			case "scope":
				id.TargetScope = "device"
			case "step":
				stepID = "bad"
			case "nil-store":
				store = nil
			case "nil-context":
				ctx = nil
			case "nil-uac":
				u = nil
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			request, out, err := u.prepareStoredPlaybackInvite(ctx, store, id, 2, stepID, in)
			require.Error(t, err)
			require.Nil(t, request)
			require.Empty(t, out.Intent.OperationID)
			require.Nil(t, observer.request)
		})
	}
}

func TestPlaybackIntentStoredPreparationFailureReturnsNoRequest(t *testing.T) {
	for _, failure := range []string{"db-write", "transfer", "version"} {
		t.Run(failure, func(t *testing.T) {
			u, db, store, id, observer := playbackIntentStoreFixture(t)
			version := int64(2)
			switch failure {
			case "db-write":
				require.NoError(t, db.Exec(`CREATE TRIGGER deny_sip_prepare BEFORE UPDATE ON gb_device_operation_intent BEGIN SELECT RAISE(ABORT,'fixture write failure'); END`).Error)
			case "transfer":
				require.NoError(t, db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
			case "version":
				version = 3
			}
			req, out, err := u.prepareStoredPlaybackInvite(context.Background(), store, id, version, strings.Repeat("b", 32), validPlaybackInvite())
			require.Error(t, err)
			require.Nil(t, req)
			require.Empty(t, out.Intent.OperationID)
			require.Nil(t, observer.request)
			loaded, err := store.LoadSIPInviteSteps(context.Background(), id)
			require.NoError(t, err)
			require.Empty(t, loaded.Steps)
		})
	}
}
