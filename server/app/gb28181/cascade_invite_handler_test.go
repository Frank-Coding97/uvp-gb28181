package gb28181

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/emiago/sipgo/siptest"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/media"
	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/repository"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

func TestCascadeInvitePeerAcceptsPublishedTargetAndSeparatesPorts(t *testing.T) {
	p := model.GbCascadePlatform{UpstreamServerID: "34020000002000000001", LocalDeviceID: "34020000002000000002", Host: "192.168.10.220", Port: 15060, Transport: "UDP"}
	req := sip.NewRequest(sip.INVITE, sip.Uri{User: "34020000001320000010", Host: "192.168.10.106"})
	req.AppendHeader(&sip.FromHeader{Address: sip.Uri{User: p.UpstreamServerID}})
	req.AppendHeader(&sip.ToHeader{Address: req.Recipient})
	req.SetSource("192.168.10.220:15060")
	req.SetTransport("UDP")
	require.True(t, matchCascadeInvitePeer(req, p))
	p.Port = 16060
	require.False(t, matchCascadeInvitePeer(req, p))
	req.SetSource("192.168.10.220:16060")
	require.True(t, matchCascadeInvitePeer(req, p))
	req.From().Address.User = "other"
	require.False(t, matchCascadeInvitePeer(req, p))
}

type cascadeInviteTestNodes struct{ value *node.Node }

func (n cascadeInviteTestNodes) Get(id int64) (*node.Node, bool) { return n.value, id == n.value.ID }

type cascadeInviteTestSource struct{}

func (cascadeInviteTestSource) Start(context.Context, string, string) (media.StartResult, error) {
	return media.StartResult{NodeID: 1, App: "rtp", StreamID: "shared-source"}, nil
}

type cascadeInviteTestTx struct {
	*siptest.ServerTxRecorder
	responses chan *sip.Response
}

func (tx *cascadeInviteTestTx) Respond(r *sip.Response) error {
	err := tx.ServerTxRecorder.Respond(r)
	tx.responses <- r.Clone()
	return err
}
func cascadeTestRequest(port int, ssrc string) *sip.Request {
	req := sip.NewRequest(sip.INVITE, sip.Uri{User: "34020000001320000010", Host: "192.168.10.106"})
	req.AppendHeader(sip.NewHeader("Via", fmt.Sprintf("SIP/2.0/UDP 192.168.10.220:%d;branch=z9hG4bK-%s", port, ssrc)))
	req.AppendHeader(sip.NewHeader("From", "<sip:34020000002000000001@3402000000>;tag=upper"))
	req.AppendHeader(sip.NewHeader("To", "<sip:34020000001320000010@3402000000>"))
	req.AppendHeader(sip.NewHeader("Contact", fmt.Sprintf("<sip:34020000002000000001@192.168.10.220:%d>", port)))
	req.AppendHeader(sip.NewHeader("Subject", "34020000001320000010:"+ssrc+",34020000002000000001:0"))
	cid := sip.CallIDHeader("same-call-id")
	req.AppendHeader(&cid)
	req.AppendHeader(&sip.CSeqHeader{SeqNo: 1, MethodName: sip.INVITE})
	req.SetSource(fmt.Sprintf("192.168.10.220:%d", port))
	req.SetTransport("UDP")
	req.SetBody([]byte("v=0\r\no=34020000002000000001 0 0 IN IP4 192.168.10.220\r\ns=Play\r\nc=IN IP4 192.168.10.220\r\nt=0 0\r\nm=video 30000 RTP/AVP 96\r\na=recvonly\r\na=rtpmap:96 PS/90000\r\ny=" + ssrc + "\r\n"))
	return req
}
func cascadeAwaitResponse(t *testing.T, tx *cascadeInviteTestTx) *sip.Response {
	t.Helper()
	for {
		select {
		case res := <-tx.responses:
			if res.StatusCode >= 200 {
				return res
			}
		case <-time.After(3 * time.Second):
			t.Fatal("no final SIP response")
			return nil
		}
	}
}
func cascadeDialogRequest(req *sip.Request, res *sip.Response, method sip.RequestMethod) *sip.Request {
	r := req.Clone()
	r.Method = method
	r.CSeq().MethodName = method
	if method == sip.BYE {
		r.CSeq().SeqNo++
	}
	r.ReplaceHeader(sip.HeaderClone(res.To()))
	r.SetBody(nil)
	return r
}

func newCascadeVideoTestRuntime(t *testing.T, stopFailures ...int) (*cascadeVideoRuntime, *gorm.DB, chan string) {
	db := cascadeInviteTestDB(t)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &model.GbCascadePlatform{}, &model.GbCascadeDeviceProjection{}, &model.GbCascadeChannelProjection{}, &model.GbCascadeMediaSession{}))
	device := gbmodels.GbDevice{DeviceID: "37010301021180000007", Status: 1}
	require.NoError(t, db.Create(&device).Error)
	channel := gbmodels.GbChannel{DeviceID: device.DeviceID, ChannelID: "34020000001320000010", Status: 1}
	require.NoError(t, db.Create(&channel).Error)
	for i, port := range []int{15060, 16060} {
		p := model.GbCascadePlatform{Name: fmt.Sprintf("upper%d", i), UpstreamServerID: "34020000002000000001", LocalDeviceID: "34020000002000000002", LocalDomain: "3402000000", Host: "192.168.10.220", Port: port, Transport: "UDP", LocalSIPIP: "192.168.10.106", LocalSIPPort: 5062, Enabled: true}
		require.NoError(t, db.Create(&p).Error)
		d := model.GbCascadeDeviceProjection{PlatformID: p.ID, SourceDeviceID: uint64(device.ID), PublishedDeviceID: device.DeviceID, Active: true}
		require.NoError(t, db.Create(&d).Error)
		c := model.GbCascadeChannelProjection{PlatformID: p.ID, DeviceProjectionID: d.ID, SourceChannelID: uint64(channel.ID), PublishedChannelID: channel.ChannelID, Active: true}
		require.NoError(t, db.Create(&c).Error)
	}
	stops := make(chan string, 10)
	var failures atomic.Int32
	if len(stopFailures) > 0 {
		failures.Store(int32(stopFailures[0]))
	}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if strings.HasSuffix(r.URL.Path, "stopSendRtp") {
			stops <- r.Form.Get("ssrc")
			if failures.Add(-1) >= 0 {
				fmt.Fprint(w, `{"code":-1,"msg":"temporarily unavailable"}`)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"code":0,"local_port":31000}`)
	}))
	t.Cleanup(api.Close)
	address := strings.TrimPrefix(api.URL, "http://")
	host, portString, _ := net.SplitHostPort(address)
	port, _ := strconv.Atoi(portString)
	nodes := cascadeInviteTestNodes{&node.Node{ID: 1, Host: host, APIPort: port, ReceiveHost: "192.168.10.220"}}
	ua, err := sipgo.NewUA()
	require.NoError(t, err)
	t.Cleanup(func() { _ = ua.Close() })
	client, err := sipgo.NewClient(ua)
	require.NoError(t, err)
	h := &cascadeVideoRuntime{client: client, store: repository.NewGormRepository(db), sources: media.NewProvider(cascadeInviteTestSource{}), sender: media.NewSender(nodes, nil), nodes: nodes, sessions: map[string]*cascadeVideoSession{}, senders: map[string]bool{}}
	return h, db, stops
}

func cascadeInviteTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "cascade-invite.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	oldDB, oldConfig := app.GormDbMysql, app.ConfigYml
	app.GormDbMysql = db
	app.ConfigYml = cascadeInviteTestConfig{}
	t.Cleanup(func() {
		app.GormDbMysql, app.ConfigYml = oldDB, oldConfig
		_ = raw.Close()
	})
	return db
}

type cascadeInviteTestConfig struct{}

func (cascadeInviteTestConfig) ConfigFileChangeListen(...func()) {}
func (cascadeInviteTestConfig) Get(string) interface{}           { return nil }
func (cascadeInviteTestConfig) GetString(key string) string {
	if key == "gormv2.usedbtype" {
		return "mysql"
	}
	return ""
}
func (cascadeInviteTestConfig) GetBool(string) bool              { return false }
func (cascadeInviteTestConfig) GetInt(string) int                { return 0 }
func (cascadeInviteTestConfig) GetInt32(string) int32            { return 0 }
func (cascadeInviteTestConfig) GetInt64(string) int64            { return 0 }
func (cascadeInviteTestConfig) GetFloat64(string) float64        { return 0 }
func (cascadeInviteTestConfig) GetDuration(string) time.Duration { return 0 }
func (cascadeInviteTestConfig) GetStringSlice(string) []string   { return nil }
func (cascadeInviteTestConfig) GetUintSlice(string) []uint       { return nil }
func (cascadeInviteTestConfig) Set(string, interface{})          {}
func (cascadeInviteTestConfig) SaveConfig() error                { return nil }

func TestCascadeVideoTwoUpstreamsSameCallIDAndIndependentBye(t *testing.T) {
	h, db, stops := newCascadeVideoTestRuntime(t)
	requests := []*sip.Request{cascadeTestRequest(15060, "0200000001"), cascadeTestRequest(16060, "0200000002")}
	responses := make([]*sip.Response, 2)
	finished := make([]chan struct{}, 2)
	for i, req := range requests {
		tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 10)}
		defer tx.Terminate()
		finished[i] = make(chan struct{})
		go func(i int) { h.Handle(req, tx); close(finished[i]) }(i)
		responses[i] = cascadeAwaitResponse(t, tx)
		require.Equal(t, 200, responses[i].StatusCode)
		ack := cascadeDialogRequest(req, responses[i], sip.ACK)
		require.True(t, h.Handle(ack, tx))
	}
	require.Equal(t, 2, h.sources.LeaseCount("shared-source"))
	// An ACK/BYE with correct tags from the other upstream must not end this dialog.
	bad := cascadeDialogRequest(requests[0], responses[0], sip.BYE)
	bad.SetSource("192.168.10.220:16060")
	badTx := siptest.NewServerTxRecorder(bad)
	defer badTx.Terminate()
	require.True(t, h.Handle(bad, badTx))
	require.Equal(t, 403, badTx.Result()[0].StatusCode)
	for i, req := range requests {
		bye := cascadeDialogRequest(req, responses[i], sip.BYE)
		tx := siptest.NewServerTxRecorder(bye)
		defer tx.Terminate()
		require.True(t, h.Handle(bye, tx))
		require.Equal(t, 200, tx.Result()[0].StatusCode)
		select {
		case <-finished[i]:
		case <-time.After(3 * time.Second):
			t.Fatal("session not released")
		}
		require.Equal(t, 1-i, h.sources.LeaseCount("shared-source"))
		select {
		case ssrc := <-stops:
			require.Equal(t, fmt.Sprintf("020000000%d", i+1), ssrc)
		case <-time.After(time.Second):
			t.Fatal("sender not stopped")
		}
	}
	var rows []model.GbCascadeMediaSession
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 2)
	for _, row := range rows {
		require.Equal(t, model.CascadeMediaSessionStateClosed, row.State)
	}
}

func TestCascadeVideoRejectsSameSenderWithoutStoppingFirst(t *testing.T) {
	h, _, stops := newCascadeVideoTestRuntime(t)
	req := cascadeTestRequest(15060, "0200000001")
	tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 10)}
	defer tx.Terminate()
	done := make(chan struct{})
	go func() { h.Handle(req, tx); close(done) }()
	res := cascadeAwaitResponse(t, tx)
	require.Equal(t, 200, res.StatusCode)
	require.True(t, h.Handle(cascadeDialogRequest(req, res, sip.ACK), tx))
	other := cascadeTestRequest(16060, "0200000001")
	otherTx := siptest.NewServerTxRecorder(other)
	defer otherTx.Terminate()
	require.True(t, h.Handle(other, otherTx))
	out := otherTx.Result()
	require.Equal(t, 486, out[len(out)-1].StatusCode)
	require.Equal(t, 1, h.sources.LeaseCount("shared-source"))
	require.Empty(t, stops)
	bye := cascadeDialogRequest(req, res, sip.BYE)
	byeTx := siptest.NewServerTxRecorder(bye)
	defer byeTx.Terminate()
	h.Handle(bye, byeTx)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cleanup timed out")
	}
}

type cascadeBlockingSource struct{ started chan struct{} }

func (s cascadeBlockingSource) Start(ctx context.Context, _, _ string) (media.StartResult, error) {
	close(s.started)
	<-ctx.Done()
	return media.StartResult{}, ctx.Err()
}
func TestCascadeVideoCancelDuringSourceAcquisition(t *testing.T) {
	h, db, stops := newCascadeVideoTestRuntime(t)
	started := make(chan struct{})
	h.sources = media.NewProvider(cascadeBlockingSource{started})
	req := cascadeTestRequest(15060, "0200000001")
	tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 10)}
	defer tx.Terminate()
	done := make(chan struct{})
	go func() { h.Handle(req, tx); close(done) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("source not requested")
	}
	cancel := req.Clone()
	cancel.Method = sip.CANCEL
	cancel.CSeq().MethodName = sip.CANCEL
	require.NoError(t, tx.Receive(cancel))
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("CANCEL did not release acquisition")
	}
	require.Empty(t, stops)
	require.Zero(t, h.sources.LeaseCount("shared-source"))
	var count int64
	require.NoError(t, db.Model(&model.GbCascadeMediaSession{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestCascadeVideoCrashRecoveryStopsOnlyPersistedSender(t *testing.T) {
	h, db, stops := newCascadeVideoTestRuntime(t)
	row := model.GbCascadeMediaSession{PlatformID: 1, DialogKey: "old-dialog", CallID: "old", State: model.CascadeMediaSessionStateActive, ZLMNodeID: 1, ZLMVHost: "__defaultVhost__", ZLMApp: "rtp", ZLMStream: "shared-source", SenderSSRC: "0200000009"}
	require.NoError(t, db.Create(&row).Error)
	require.NoError(t, h.recoverSenders())
	require.Equal(t, "0200000009", <-stops)
	require.NoError(t, db.First(&row, row.ID).Error)
	require.Equal(t, model.CascadeMediaSessionStateAborted, row.State)
	require.NoError(t, h.recoverSenders())
	require.Empty(t, stops)
}

func TestCascadeVideoRetainsLeaseUntilFailedStopIsRetried(t *testing.T) {
	h, _, stops := newCascadeVideoTestRuntime(t, 1)
	req := cascadeTestRequest(15060, "0200000001")
	tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 10)}
	defer tx.Terminate()
	done := make(chan struct{})
	go func() { h.Handle(req, tx); close(done) }()
	res := cascadeAwaitResponse(t, tx)
	require.Equal(t, 200, res.StatusCode)
	h.Handle(cascadeDialogRequest(req, res, sip.ACK), tx)
	bye := cascadeDialogRequest(req, res, sip.BYE)
	byeTx := siptest.NewServerTxRecorder(bye)
	defer byeTx.Terminate()
	h.Handle(bye, byeTx)
	select {
	case <-stops:
	case <-time.After(time.Second):
		t.Fatal("cleanup not attempted")
	}
	require.Equal(t, 1, h.sources.LeaseCount("shared-source"))
	h.mu.Lock()
	require.Len(t, h.sessions, 1)
	require.Len(t, h.senders, 1)
	h.mu.Unlock()
	select {
	case <-done:
		t.Fatal("released uncertain sender")
	case <-time.After(30 * time.Millisecond):
	}
	select {
	case <-done:
	case <-time.After(7 * time.Second):
		t.Fatal("cleanup retry did not converge")
	}
	require.Zero(t, h.sources.LeaseCount("shared-source"))
	require.Equal(t, "0200000001", <-stops)
}

func TestCascadeVideoPlatformCapacityAndShareRevocation(t *testing.T) {
	h, db, _ := newCascadeVideoTestRuntime(t)
	req := cascadeTestRequest(15060, "0200000001")
	tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 10)}
	defer tx.Terminate()
	done := make(chan struct{})
	go func() { h.Handle(req, tx); close(done) }()
	res := cascadeAwaitResponse(t, tx)
	require.Equal(t, 200, res.StatusCode)
	h.Handle(cascadeDialogRequest(req, res, sip.ACK), tx)
	other := cascadeTestRequest(15060, "0200000002")
	otherTx := siptest.NewServerTxRecorder(other)
	defer otherTx.Terminate()
	h.Handle(other, otherTx)
	out := otherTx.Result()
	require.Equal(t, 486, out[len(out)-1].StatusCode)
	require.NoError(t, db.Model(&model.GbCascadePlatform{}).Where("port = ?", 15060).Update("projection_revision", 7).Error)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	require.NoError(t, h.revalidate(ctx))
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("revoked sharing left session active")
	}
	require.Zero(t, h.sources.LeaseCount("shared-source"))
}

func TestCascadeVideoUnacknowledgedTransactionFailureReleasesSender(t *testing.T) {
	h, db, stops := newCascadeVideoTestRuntime(t)
	req := cascadeTestRequest(15060, "0200000001")
	tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 10)}
	defer tx.Terminate()
	done := make(chan struct{})
	go func() { h.Handle(req, tx); close(done) }()
	require.Equal(t, 200, cascadeAwaitResponse(t, tx).StatusCode)
	tx.Terminate()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("unacknowledged sender leaked")
	}
	require.Zero(t, h.sources.LeaseCount("shared-source"))
	require.Equal(t, "0200000001", <-stops)
	var row model.GbCascadeMediaSession
	require.NoError(t, db.First(&row).Error)
	require.Equal(t, model.CascadeMediaSessionStateFailed, row.State)
}
