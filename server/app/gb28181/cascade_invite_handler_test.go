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
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/media"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/repository"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.com/uvp-gb28181/app/global/app"
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

func TestCascadeInvitePeerRequiresConfiguredSourceAddress(t *testing.T) {
	p := model.GbCascadePlatform{UpstreamServerID: "35020000002000000001", LocalDeviceID: "34020000002000000002", Host: "192.168.10.220", Port: 8160, Transport: "UDP"}
	req := sip.NewRequest(sip.INVITE, sip.Uri{User: "34020000001320000001", Host: "10.8.0.3"})
	req.AppendHeader(&sip.FromHeader{Address: sip.Uri{User: p.UpstreamServerID}})
	req.AppendHeader(&sip.ToHeader{Address: req.Recipient})
	req.SetSource("10.8.0.2:8160")
	req.SetTransport("UDP")

	require.False(t, matchCascadeInvitePeer(req, p))

	peer, ok := cascadePeerFromRequest(req)
	require.True(t, ok)
	require.True(t, peer.identify(p))

	// 端口是同一上级平台多接入实例之间唯一的分辨依据，不能松。
	p.Port = 5060
	require.False(t, peer.identify(p))
	p.Port = 8160
	// From 用户是身份本身，同样不能松。
	p.UpstreamServerID = "34020000002000000001"
	require.False(t, peer.identify(p))
	p.UpstreamServerID = "35020000002000000001"
	// 传输层也要一致，否则 UDP 报文能冒充同端口的 TCP 接入。
	p.Transport = "TCP"
	require.False(t, peer.identify(p))
}

func TestCascadePeerMatchesAllResolvedDomainAddressesAndRejectsResolutionFailure(t *testing.T) {
	previous := cascadePeerDNS.lookup
	cascadePeerDNS.Lock()
	cascadePeerDNS.lookup = func(host string) ([]net.IP, error) {
		if host != "upstream.example.test" {
			return nil, fmt.Errorf("dns lookup failed for %s", host)
		}
		return []net.IP{net.ParseIP("192.0.2.10"), net.ParseIP("2001:db8::10")}, nil
	}
	cascadePeerDNS.Unlock()
	t.Cleanup(func() {
		cascadePeerDNS.Lock()
		cascadePeerDNS.lookup = previous
		cascadePeerDNS.Unlock()
	})

	p := model.GbCascadePlatform{UpstreamServerID: "35020000002000000001", Host: "upstream.example.test", Port: 8160, Transport: "UDP"}
	for _, source := range []string{"192.0.2.10:8160", "[2001:db8::10]:8160"} {
		req := sip.NewRequest(sip.INVITE, sip.Uri{User: "34020000001320000001", Host: "local"})
		req.AppendHeader(&sip.FromHeader{Address: sip.Uri{User: p.UpstreamServerID}})
		req.SetSource(source)
		req.SetTransport("UDP")
		peer, ok := cascadePeerFromRequest(req)
		require.True(t, ok)
		require.True(t, peer.match(p), source)
	}

	p.Host = "missing.example.test"
	require.False(t, (&cascadePeer{host: "192.0.2.10", port: 8160, transport: "udp", fromUser: p.UpstreamServerID}).match(p))
}

func TestCascadePeerKeepsEstablishedSourceBindingWhenDNSChanges(t *testing.T) {
	original := cascadePeer{host: "192.0.2.10", port: 8160, transport: "udp", fromUser: "35020000002000000001"}
	observed := original
	// sameSession compares the actual peer captured by the dialog, not the
	// current DNS answer. A later address change cannot migrate an established
	// session to another source.
	require.True(t, original.sameSession(observed))
	observed.host = "192.0.2.11"
	require.False(t, original.sameSession(observed))
}

func TestCascadeVideoRejectsUpstreamSignallingFromAnotherAddress(t *testing.T) {
	h, _, stops := newCascadeVideoTestRuntime(t)
	req := cascadeTestRequestFrom("10.8.0.2", 15060, "0200000001")
	tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 10)}
	defer tx.Terminate()
	go h.Handle(req, tx)
	require.Equal(t, 403, cascadeAwaitResponse(t, tx).StatusCode)
	select {
	case ssrc := <-stops:
		t.Fatalf("unexpected sender stop for rejected request: %s", ssrc)
	default:
	}
}

// 退回身份匹配不等于「谁的报文都收」：同一上级 ID 的两个接入实例必须仍然分得清。
func TestCascadeVideoRejectsAmbiguousUpstreamIdentity(t *testing.T) {
	h, db, _ := newCascadeVideoTestRuntime(t)
	require.NoError(t, db.Create(&model.GbCascadePlatform{
		Name: "upper1-shadow", UpstreamServerID: "34020000002000000001", LocalDeviceID: "34020000002000000002",
		LocalDomain: "3402000000", Host: "10.9.9.9", Port: 15060, Transport: "UDP",
		LocalSIPIP: "192.168.10.106", LocalSIPPort: 5062, Enabled: true,
	}).Error)

	req := cascadeTestRequestFrom("10.8.0.2", 15060, "0200000001")
	tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 10)}
	defer tx.Terminate()
	go h.Handle(req, tx)
	require.Equal(t, 403, cascadeAwaitResponse(t, tx).StatusCode)
}

// 源地址对不上就退回身份匹配，但身份本身（From 用户）仍是硬要求。
func TestCascadeVideoRejectsUpstreamWithUnknownFromUser(t *testing.T) {
	h, _, _ := newCascadeVideoTestRuntime(t)
	req := cascadeTestRequestFrom("10.8.0.2", 15060, "0200000001")
	req.ReplaceHeader(sip.NewHeader("From", "<sip:35020000002000000001@3502000000>;tag=upper"))
	tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 10)}
	defer tx.Terminate()
	go h.Handle(req, tx)
	require.Equal(t, 403, cascadeAwaitResponse(t, tx).StatusCode)
}

func TestCascadeCancelValidatorBindsVideoTransactionToOriginalPeer(t *testing.T) {
	h, _, _ := newCascadeVideoTestRuntime(t)
	origin := cascadeTestRequest(15060, "0200000001")
	tx := siptest.NewServerTxRecorder(origin)
	defer tx.Terminate()

	cancel := origin.Clone()
	cancel.Method = sip.CANCEL
	cancel.CSeq().MethodName = sip.CANCEL
	cancel.SetSource("10.8.0.2:15060")
	require.False(t, h.ValidateCancel(cancel, tx), "异源 CANCEL 不能终止级联视频 INVITE")

	cancel.SetSource("192.168.10.220:15060")
	require.True(t, h.ValidateCancel(cancel, tx), "同源 CANCEL 应交给 SIP 事务层处理")
}

func TestCascadeCancelKeepsOriginalPeerAfterPlatformEndpointChanges(t *testing.T) {
	h, db, _ := newCascadeVideoTestRuntime(t)
	origin := cascadeTestRequest(15060, "0200000001")
	tx := siptest.NewServerTxRecorder(origin)
	defer tx.Terminate()
	// Changing the configured endpoint must not detach a pending transaction
	// from the peer that actually sent its INVITE.
	require.NoError(t, db.Model(&model.GbCascadePlatform{}).Where("id = 1").Update("host", "192.0.2.99").Error)
	cancel := origin.Clone()
	cancel.Method = sip.CANCEL
	cancel.CSeq().MethodName = sip.CANCEL
	cancel.SetSource("10.8.0.2:15060")
	require.False(t, h.ValidateCancel(cancel, tx))
	cancel.SetSource(origin.Source())
	require.True(t, h.ValidateCancel(cancel, tx))
}

type cascadeInviteTestNodes struct{ value *node.Node }

func (n cascadeInviteTestNodes) Get(id int64) (*node.Node, bool) { return n.value, id == n.value.ID }

type cascadeInviteTestSource struct{}

func (cascadeInviteTestSource) Start(context.Context, media.AcquireRequest) (media.StartResult, error) {
	return media.StartResult{NodeID: 1, App: "rtp", StreamID: "shared-source"}, nil
}

type cascadeCallbackSource struct {
	onStart func(media.AcquireRequest)
}

func (s cascadeCallbackSource) Start(_ context.Context, request media.AcquireRequest) (media.StartResult, error) {
	if s.onStart != nil {
		s.onStart(request)
	}
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
	return cascadeTestRequestFrom("192.168.10.220", port, ssrc)
}

// cascadeTestRequestFrom 允许指定上游的源地址：上级平台双网卡/走 VPN 时，
// 它主动发 INVITE 的源 IP 可以不是配置里的 Host。
func cascadeTestRequestFrom(sourceHost string, port int, ssrc string) *sip.Request {
	req := sip.NewRequest(sip.INVITE, sip.Uri{User: "34020000001320000010", Host: "192.168.10.106"})
	req.AppendHeader(sip.NewHeader("Via", fmt.Sprintf("SIP/2.0/UDP %s:%d;branch=z9hG4bK-%s", sourceHost, port, ssrc)))
	req.AppendHeader(sip.NewHeader("From", "<sip:34020000002000000001@3402000000>;tag=upper"))
	req.AppendHeader(sip.NewHeader("To", "<sip:34020000001320000010@3402000000>"))
	req.AppendHeader(sip.NewHeader("Contact", fmt.Sprintf("<sip:34020000002000000001@%s:%d>", sourceHost, port)))
	req.AppendHeader(sip.NewHeader("Subject", "34020000001320000010:"+ssrc+",34020000002000000001:0"))
	cid := sip.CallIDHeader("same-call-id")
	req.AppendHeader(&cid)
	req.AppendHeader(&sip.CSeqHeader{SeqNo: 1, MethodName: sip.INVITE})
	req.SetSource(fmt.Sprintf("%s:%d", sourceHost, port))
	req.SetTransport("UDP")
	req.SetBody([]byte("v=0\r\no=34020000002000000001 0 0 IN IP4 " + sourceHost + "\r\ns=Play\r\nc=IN IP4 " + sourceHost + "\r\nt=0 0\r\nm=video 30000 RTP/AVP 96\r\na=recvonly\r\na=rtpmap:96 PS/90000\r\ny=" + ssrc + "\r\n"))
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
	return newCascadeVideoTestRuntimeWithSource(t, cascadeInviteTestSource{}, stopFailures...)
}

func newCascadeVideoTestRuntimeWithSource(t *testing.T, source media.SourceStarter, stopFailures ...int) (*cascadeVideoRuntime, *gorm.DB, chan string) {
	db := cascadeInviteTestDB(t)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &model.GbCascadePlatform{}, &model.GbCascadeDeviceProjection{}, &model.GbCascadeChannelProjection{}, &model.GbCascadeBusinessNode{}, &model.GbCascadeBusinessBinding{}, &model.GbCascadeMediaSession{}))
	require.NoError(t, db.Exec("ALTER TABLE gb_device ADD COLUMN access_epoch BIGINT NOT NULL DEFAULT 1").Error)
	require.NoError(t, db.Exec("ALTER TABLE gb_device ADD COLUMN cleanup_completed_epoch BIGINT NOT NULL DEFAULT 1").Error)
	require.NoError(t, db.Exec("ALTER TABLE gb_device ADD COLUMN legacy_revoked_before DATETIME NULL").Error)
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
				if _, err := fmt.Fprint(w, `{"code":-400,"msg":"temporarily unavailable"}`); err != nil {
					panic(err)
				}
				return
			}
		}
		if strings.HasSuffix(r.URL.Path, "listRtpSender") {
			_, _ = fmt.Fprint(w, `{"code":0,"data":["0200000001","0200000002","1200000001","1200000002"],"rtp_info":{"0200000001":{"seq":9,"rtptime":9000},"1200000001":{"seq":9,"rtptime":9000}}}`)
			return
		}
		if strings.HasSuffix(r.URL.Path, "resetRtpSenderInfo") {
			_, _ = fmt.Fprint(w, `{"code":0}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := fmt.Fprint(w, `{"code":0,"local_port":31000}`); err != nil {
			panic(err)
		}
	}))
	t.Cleanup(api.Close)
	address := strings.TrimPrefix(api.URL, "http://")
	host, portString, _ := net.SplitHostPort(address)
	port, _ := strconv.Atoi(portString)
	nodes := cascadeInviteTestNodes{&node.Node{ID: 1, Host: host, APIPort: port, SDPIP: "192.168.10.220"}}
	ua, err := sipgo.NewUA()
	require.NoError(t, err)
	t.Cleanup(func() { _ = ua.Close() })
	client, err := sipgo.NewClient(ua)
	require.NoError(t, err)
	h := &cascadeVideoRuntime{client: client, store: repository.NewGormRepository(db), sources: media.NewProvider(source), sender: media.NewSender(nodes, nil), nodes: nodes, sessions: map[string]*cascadeVideoSession{}, senders: map[string]bool{}}
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

func (s cascadeBlockingSource) Start(ctx context.Context, _ media.AcquireRequest) (media.StartResult, error) {
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

func TestCascadeVideoCrashRecoveryIgnoresBroadcastSessions(t *testing.T) {
	h, db, stops := newCascadeVideoTestRuntime(t)
	row := model.GbCascadeMediaSession{Kind: model.CascadeMediaSessionKindBroadcast, PlatformID: 1, DialogKey: "broadcast-call", CallID: "broadcast-call", State: model.CascadeMediaSessionStateAnswered, ZLMNodeID: 1, ZLMVHost: "__defaultVhost__", ZLMApp: "rtp", ZLMStream: "broadcast-source", SenderSSRC: "0200000008"}
	require.NoError(t, db.Create(&row).Error)
	require.NoError(t, h.recoverSenders())
	require.Empty(t, stops, "video recovery must not stop a broadcast-owned RTP sender")
	require.NoError(t, db.First(&row, row.ID).Error)
	require.Equal(t, model.CascadeMediaSessionStateAnswered, row.State, "video recovery must not rewrite broadcast signaling state")
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
	require.NoError(t, db.Model(&model.GbCascadePlatform{}).Where("port = ?", 15060).Update("max_streams", 1).Error)
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

func TestCascadeVideoRejectsUnavailableDeviceAuthorization(t *testing.T) {
	for _, change := range []string{
		"UPDATE gb_device SET access_epoch = 2",
		"UPDATE gb_device SET access_epoch = 0",
		"UPDATE gb_device SET cleanup_completed_epoch = 0",
		"ALTER TABLE gb_device DROP COLUMN access_epoch",
	} {
		t.Run(change, func(t *testing.T) {
			h, db, _ := newCascadeVideoTestRuntime(t)
			require.NoError(t, db.Exec(change).Error)
			req := cascadeTestRequest(15060, "0200000001")
			tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 10)}
			defer tx.Terminate()
			done := make(chan struct{})
			go func() { h.Handle(req, tx); close(done) }()
			res := cascadeAwaitResponse(t, tx)
			if res.StatusCode == 200 {
				h.Handle(cascadeDialogRequest(req, res, sip.ACK), tx)
				byeTx := siptest.NewServerTxRecorder(req)
				defer byeTx.Terminate()
				h.Handle(cascadeDialogRequest(req, res, sip.BYE), byeTx)
			}
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("INVITE did not finish")
			}
			require.Equal(t, 503, res.StatusCode)
			require.False(t, h.sources.HasLease("shared-source"))
			var count int64
			require.NoError(t, db.Model(&model.GbCascadeMediaSession{}).Count(&count).Error)
			require.Zero(t, count)
		})
	}
}

func TestCascadeVideoRejectsProjectionRevocationBeforeSender(t *testing.T) {
	var source cascadeCallbackSource
	h, db, _ := newCascadeVideoTestRuntimeWithSource(t, source)
	source.onStart = func(media.AcquireRequest) {
		// Simulate a transfer/removal that clears the channel projection without
		// changing the platform projection revision.
		_ = db.Where("platform_id = ?", uint64(1)).Delete(&model.GbCascadeChannelProjection{}).Error
	}
	h.sources = media.NewProvider(source)
	req := cascadeTestRequest(15060, "0200000001")
	tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 10)}
	defer tx.Terminate()
	done := make(chan struct{})
	go func() { h.Handle(req, tx); close(done) }()
	res := cascadeAwaitResponse(t, tx)
	require.Equal(t, 403, res.StatusCode)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("INVITE did not finish")
	}
	require.False(t, h.sources.HasLease("shared-source"))
	var count int64
	require.NoError(t, db.Model(&model.GbCascadeMediaSession{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestCascadeVideoDefaultAllowsMultipleStreamsForOnePlatform(t *testing.T) {
	h, _, _ := newCascadeVideoTestRuntime(t)
	t.Cleanup(func() {
		h.mu.Lock()
		for _, session := range h.sessions {
			session.cancel()
		}
		h.mu.Unlock()
		h.wg.Wait()
	})
	for i := 1; i <= 2; i++ {
		req := cascadeTestRequest(15060, fmt.Sprintf("020000000%d", i))
		callID := sip.CallIDHeader(fmt.Sprintf("unlimited-%d", i))
		req.ReplaceHeader(&callID)
		tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 10)}
		t.Cleanup(tx.Terminate)
		go h.Handle(req, tx)
		res := cascadeAwaitResponse(t, tx)
		require.Equal(t, 200, res.StatusCode)
		require.True(t, h.Handle(cascadeDialogRequest(req, res, sip.ACK), tx))
	}
	require.Equal(t, 2, h.sources.LeaseCount("shared-source"))
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
