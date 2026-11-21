package sip

import (
	"net/http"
	"testing"
	"time"

	siplib "github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
)

type timeSyncCaptureTx struct{ response *siplib.Response }

func (t *timeSyncCaptureTx) Terminate()                            {}
func (t *timeSyncCaptureTx) OnTerminate(siplib.FnTxTerminate) bool { return true }
func (t *timeSyncCaptureTx) Done() <-chan struct{}                 { return make(chan struct{}) }
func (t *timeSyncCaptureTx) Err() error                            { return nil }
func (t *timeSyncCaptureTx) Respond(response *siplib.Response) error {
	t.response = response
	return nil
}
func (t *timeSyncCaptureTx) Acks() <-chan *siplib.Request    { return make(chan *siplib.Request) }
func (t *timeSyncCaptureTx) OnCancel(siplib.FnTxCancel) bool { return true }

// timeSyncOriginTx 额外实现 Origin()，用于验证包装层不吞掉底层能力。
type timeSyncOriginTx struct {
	timeSyncCaptureTx
	origin *siplib.Request
}

func (t *timeSyncOriginTx) Origin() *siplib.Request { return t.origin }

func timeSyncRegisterRequest() *siplib.Request {
	req := siplib.NewRequest(siplib.REGISTER, siplib.Uri{User: "34020000001320000001", Host: "3402000000"})
	return req
}

func TestDeviceTimeSyncAddsDateOnSuccess(t *testing.T) {
	inner := &timeSyncCaptureTx{}
	tx := newDeviceTimeSyncTx(inner, true)
	_, wrapped := tx.(deviceTimeSyncTx)
	require.True(t, wrapped, "开关打开时应当包装事务")

	req := timeSyncRegisterRequest()
	res := siplib.NewResponseFromRequest(req, siplib.StatusOK, "OK", nil)
	require.NoError(t, tx.Respond(res))

	header := inner.response.GetHeader("Date")
	require.NotNil(t, header, "成功应答必须带 Date 头")

	parsed, err := time.Parse(http.TimeFormat, header.Value())
	require.NoError(t, err, "Date 必须是 SIP/RFC1123 格式")
	require.WithinDuration(t, time.Now().UTC(), parsed, time.Minute)
}

func TestDeviceTimeSyncDisabledKeepsTransactionUntouched(t *testing.T) {
	inner := &timeSyncCaptureTx{}
	tx := newDeviceTimeSyncTx(inner, false)
	require.Same(t, inner, tx, "开关关闭时必须原样返回事务，报文零变化")

	req := timeSyncRegisterRequest()
	res := siplib.NewResponseFromRequest(req, siplib.StatusOK, "OK", nil)
	require.NoError(t, tx.Respond(res))

	require.Nil(t, inner.response.GetHeader("Date"))
}

func TestDeviceTimeSyncSkipsErrorResponses(t *testing.T) {
	inner := &timeSyncCaptureTx{}
	tx := newDeviceTimeSyncTx(inner, true)

	req := timeSyncRegisterRequest()
	res := siplib.NewResponseFromRequest(req, siplib.StatusUnauthorized, "Unauthorized", nil)
	require.NoError(t, tx.Respond(res))

	require.Nil(t, inner.response.GetHeader("Date"), "401 不带 Date，避免干扰摘要质询")
}

func TestDeviceTimeSyncKeepsExplicitDate(t *testing.T) {
	inner := &timeSyncCaptureTx{}
	tx := newDeviceTimeSyncTx(inner, true)

	req := timeSyncRegisterRequest()
	res := siplib.NewResponseFromRequest(req, siplib.StatusOK, "OK", nil)
	res.AppendHeader(siplib.NewHeader("Date", "Mon, 02 Jan 2006 15:04:05 GMT"))
	require.NoError(t, tx.Respond(res))

	require.Equal(t, "Mon, 02 Jan 2006 15:04:05 GMT", inner.response.GetHeader("Date").Value())
}

func TestDeviceTimeSyncForwardsOrigin(t *testing.T) {
	origin := timeSyncRegisterRequest()
	inner := &timeSyncOriginTx{origin: origin}
	tx := newDeviceTimeSyncTx(inner, true)

	provider, ok := tx.(interface{ Origin() *siplib.Request })
	require.True(t, ok)
	require.Same(t, origin, provider.Origin(), "包装层必须透传底层 Origin，否则级联取消校验会退化")
}

func TestDeviceTimeSyncOriginNilWhenUnsupported(t *testing.T) {
	inner := &timeSyncCaptureTx{}
	tx := newDeviceTimeSyncTx(inner, true)

	provider, ok := tx.(interface{ Origin() *siplib.Request })
	require.True(t, ok)
	require.Nil(t, provider.Origin(), "底层不支持 Origin 时返回 nil，与断言失败分支一致")
}
