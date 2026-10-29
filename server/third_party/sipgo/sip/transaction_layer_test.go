package sip

import (
	"context"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testCreateAddr(t *testing.T, addr string) Addr {
	a := Addr{}
	require.NoError(t, a.parseAddr(addr))
	return a
}

func TestIntegrationTransactionLayerServerTx(t *testing.T) {
	if os.Getenv("TEST_INTEGRATION") == "" {
		t.Skip("Use TEST_INTEGRATION env value to run this test")
		return
	}

	tp := NewTransportLayer(net.DefaultResolver, NewParser(), nil)
	txl := NewTransactionLayer(tp)

	req := testCreateRequest(t, "OPTIONS", "sip:192.168.0.1", "UDP", "127.0.0.1:15069")
	key, _ := ServerTxKeyMake(req)

	var count int32 = 0
	txl.OnRequest(func(req *Request, tx *ServerTx) {
		atomic.AddInt32(&count, 1)
		t.Log("Request")
	})

	// Connection will be created
	err := txl.handleRequest(req)
	require.NoError(t, err)

	// Now create connection and test multiple concurent received request
	tp.udp.CreateConnection(context.TODO(),
		testCreateAddr(t, "127.0.0.1:15069"),
		testCreateAddr(t, "192.168.0.1:1234"),
		tp.handleMessage,
	)

	wg := sync.WaitGroup{}
	wg.Add(3)
	for range []int{0, 1, 2} {
		go func() {
			defer wg.Done()
			err := txl.handleRequest(req)
			if err != nil {
				t.Log("Request failed with err", err)
			}
		}()
	}

	wg.Wait()
	require.EqualValues(t, 1, atomic.LoadInt32(&count))
	require.EqualValues(t, 1, len(txl.serverTransactions.items))

	// After termination of transaction, it  must be removed from list
	tx := txl.serverTransactions.items[key]
	require.NotNil(t, tx)
	tx.Terminate()
	require.EqualValues(t, 0, len(txl.serverTransactions.items))
}

func TestTransactionLayerMalformedRequestStateless400(t *testing.T) {
	if os.Getenv("TEST_INTEGRATION") == "" {
		t.Skip("Use TEST_INTEGRATION env value to run this test")
		return
	}

	// Listen on a UDP port to receive the stateless 400 response.
	receiverAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	require.NoError(t, err)
	receiverConn, err := net.ListenUDP("udp", receiverAddr)
	require.NoError(t, err)
	defer receiverConn.Close()
	receiverActualAddr := receiverConn.LocalAddr().String()

	// Set up the transaction layer with a real transport.
	tp := NewTransportLayer(net.DefaultResolver, NewParser(), nil)
	txl := NewTransactionLayer(tp)

	var handlerCalled int32
	txl.OnRequest(func(req *Request, tx *ServerTx) {
		atomic.AddInt32(&handlerCalled, 1)
	})

	// Create a UDP connection in the transport pool so WriteMsg can find it.
	localAddr := "127.0.0.1:15071"
	_, err = tp.udp.CreateConnection(
		context.TODO(),
		testCreateAddr(t, localAddr),
		testCreateAddr(t, receiverActualAddr),
		tp.handleMessage,
	)
	require.NoError(t, err)

	// Build a malformed request: valid Via, From, To, Call-ID, but NO CSeq.
	raw := strings.Join([]string{
		"REGISTER sip:192.168.100.30:5060 SIP/2.0",
		"Via: SIP/2.0/UDP " + receiverActualAddr + ";branch=z9hG4bK-test123",
		"From: <sip:alice@example.com>;tag=from1",
		"To: <sip:alice@example.com>",
		"Call-ID: malformed-test-call-id",
		"Content-Length: 0",
		"",
		"",
	}, "\r\n")

	msg, err := ParseMessage([]byte(raw))
	require.NoError(t, err)

	req := msg.(*Request)
	req.SetTransport("UDP")
	req.SetSource(receiverActualAddr)

	// handleRequest should return an error because CSeq is missing.
	err = txl.handleRequest(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CSeq")

	// The request handler should NOT have been called.
	assert.EqualValues(t, 0, atomic.LoadInt32(&handlerCalled))

	// Read the stateless 400 response that should have been sent.
	buf := make([]byte, 4096)
	receiverConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, readErr := receiverConn.ReadFromUDP(buf)
	require.NoError(t, readErr, "expected to receive a stateless 400 response")

	respMsg, err := ParseMessage(buf[:n])
	require.NoError(t, err)

	resp, ok := respMsg.(*Response)
	require.True(t, ok, "expected a SIP response")
	assert.Equal(t, 400, resp.StatusCode)
	assert.Equal(t, "Bad Request", resp.Reason)
}

func TestTransactionLayerCancelValidatorRejectsBeforeBuiltInTermination(t *testing.T) {
	origin := testCreateRequest(t, "INVITE", "sip:34020000001320000010", "UDP", "192.0.2.20:5060")
	key, err := makeServerTxKey(origin, "")
	require.NoError(t, err)
	conn := &validatorTestConnection{}
	tx := NewServerTx(key, origin, conn, DefaultLogger())
	require.NoError(t, tx.Init())
	txl := &TransactionLayer{serverTransactions: newTransactionStore[*ServerTx]()}
	txl.serverTransactions.items[key] = tx
	txl.SetCancelValidator(func(*Request, *ServerTx) bool { return false })

	cancel := origin.Clone()
	cancel.Method = CANCEL
	cancel.CSeq().MethodName = CANCEL
	require.NoError(t, txl.handleRequest(cancel))
	require.Len(t, conn.messages, 1)
	response, ok := conn.messages[0].(*Response)
	require.True(t, ok)
	require.Equal(t, StatusForbidden, response.StatusCode)
	select {
	case <-tx.Done():
		t.Fatal("rejected CANCEL must not terminate the original INVITE transaction")
	default:
	}
	tx.Terminate()
}

type validatorTestConnection struct{ messages []Message }

func (c *validatorTestConnection) LocalAddr() net.Addr { return nil }
func (c *validatorTestConnection) WriteMsg(message Message) error {
	c.messages = append(c.messages, message)
	return nil
}
func (c *validatorTestConnection) Ref(int) int            { return 0 }
func (c *validatorTestConnection) TryClose() (int, error) { return 0, nil }
func (c *validatorTestConnection) Close() error           { return nil }

func TestTransactionLayerClientTx(t *testing.T) {
	if os.Getenv("TEST_INTEGRATION") == "" {
		t.Skip("Use TEST_INTEGRATION env value to run this test")
		return
	}
	tp := NewTransportLayer(net.DefaultResolver, NewParser(), nil)
	txl := NewTransactionLayer(tp)

	req := testCreateRequest(t, "OPTIONS", "sip:127.0.0.1:9876", "UDP", "127.0.0.1:15070")

	wg := sync.WaitGroup{}
	wg.Add(3)
	var count int32
	for range []int{0, 1, 2} {
		go func() {
			defer wg.Done()
			tx, err := txl.Request(context.TODO(), req)
			if err != nil {
				t.Log("Request failed with err", err)
				return
			}
			atomic.AddInt32(&count, 1)
			require.Equal(t, req, tx.origin)
		}()
	}

	wg.Wait()
	// Only one transaction will be created and executed
	require.EqualValues(t, 1, atomic.LoadInt32(&count))
	require.Equal(t, 2, tp.udp.pool.Size())
	assert.True(t, tp.udp.pool.Get("127.0.0.1:9876") != nil)
}

// 现场报文：GB28181 设备的 Via branch **不带** RFC3261 magic cookie，且 From **漏了 tag**。
// 两个缺陷叠加时，事务层必须仍能建出事务键 —— 否则请求会在业务处理之前被判成畸形报文，
// 回一条 stateless 400，设备永远注册不上（2026-10-13 实测：某 luna 系 SDK 模拟客户端
// 往平台注册，每条都被 400 打回，平台侧没有 401 挑战、没有 register.* 事件、没有安全事件）。
//
// 这条用例锁的是"键可算"，**不是**"报文合规"：合规判定归业务层。
func TestServerTxKeyToleratesNonRFC3261BranchAndMissingFromTag(t *testing.T) {
	raw := strings.Join([]string{
		"REGISTER sip:37010000002000000001@192.168.10.220:51064 SIP/2.0",
		"Via: SIP/2.0/UDP 192.168.1.101:5061;branch=lunaProxy1937319883",
		"From: <sip:34020000001320000011@192.168.1.101:5061>",
		"To: <sip:37010000002000000001@192.168.10.220:51064>",
		"Call-ID: 459c36ca5e89ee24b1b5f27ffda55a55@0:0:0:0:0:0:0:0",
		"CSeq: 6185 REGISTER",
		"Contact: <sip:34020000001320000011@192.168.1.101:5061>",
		"Expires: 3600",
		"Content-Length: 0",
		"",
		"",
	}, "\r\n")

	msg, err := ParseMessage([]byte(raw))
	require.NoError(t, err)
	req, ok := msg.(*Request)
	require.True(t, ok)

	key, err := ServerTxKeyMake(req)
	require.NoError(t, err, "branch 无 z9hG4bK + From 无 tag 不应再被判成畸形报文")
	require.NotEmpty(t, key)

	// 重传要稳定命中同一个键，否则同一请求会被当成新事务重复处理。
	retransKey, err := ServerTxKeyMake(req)
	require.NoError(t, err)
	require.Equal(t, key, retransKey, "同一请求重传必须算出同一个键")

	// 平台回包走同一条计算路径（响应用请求的 Via/From/Call-ID/CSeq），键必须一致，
	// 否则事务层找不到 server transaction，连 401 挑战都发不出去。
	res := NewResponseFromRequest(req, 401, "Unauthorized", nil)
	resKey, err := ServerTxKeyMake(res)
	require.NoError(t, err, "响应侧算了同一个键才算真的修好")
	require.Equal(t, key, resKey)

	// 放宽不能放宽到"不同请求撞键"：只改 Call-ID 就必须算出不同的键。
	otherRaw := strings.Replace(raw,
		"Call-ID: 459c36ca5e89ee24b1b5f27ffda55a55@0:0:0:0:0:0:0:0",
		"Call-ID: 00000000000000000000000000000000@0:0:0:0:0:0:0:0", 1)
	require.NotEqual(t, raw, otherRaw)
	otherMsg, err := ParseMessage([]byte(otherRaw))
	require.NoError(t, err)
	otherKey, err := ServerTxKeyMake(otherMsg.(*Request))
	require.NoError(t, err)
	require.NotEqual(t, key, otherKey, "不同 Call-ID 的请求不能撞同一个事务键")

	// From 缺失仍然是畸形报文（From 是 RFC 强制头，缺了连回包都构造不出来）。
	noFromRaw := strings.Join([]string{
		"REGISTER sip:37010000002000000001@192.168.10.220:51064 SIP/2.0",
		"Via: SIP/2.0/UDP 192.168.1.101:5061;branch=lunaProxy1937319883",
		"To: <sip:37010000002000000001@192.168.10.220:51064>",
		"Call-ID: 459c36ca5e89ee24b1b5f27ffda55a55@0:0:0:0:0:0:0:0",
		"CSeq: 6185 REGISTER",
		"Content-Length: 0",
		"",
		"",
	}, "\r\n")
	noFromMsg, err := ParseMessage([]byte(noFromRaw))
	require.NoError(t, err)
	_, err = ServerTxKeyMake(noFromMsg.(*Request))
	require.Error(t, err, "From 整体缺失仍应判为畸形报文")
	assert.Contains(t, err.Error(), "From")
}
