package sip

import (
	"context"
	"errors"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/emiago/sipgo"
	siplib "github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	gbtrace "uvplatform.cn/uvp-gb28181/app/gb28181/trace"
)

type offlineTraceStore struct{}

func (offlineTraceStore) InsertBatch(context.Context, []gbtrace.StoredEvent) error {
	return errors.New("clickhouse offline")
}

func TestClickHouseOfflineDoesNotChangeUDPRegisterResponse(t *testing.T) {
	probe, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	port := probe.LocalAddr().(*net.UDPAddr).Port
	require.NoError(t, probe.Close())

	cfg := testConfig()
	cfg.SIP.Port = port
	cfg.SIP.Transport = []string{"udp"}
	cfg.Trace = gbconfig.TraceConfig{Enabled: true, QueueCapacity: 8, BatchSize: 1, FlushIntervalMS: 5}
	cipher, err := gbtrace.NewCipher([]byte("0123456789abcdef0123456789abcdef"), "test-v1")
	require.NoError(t, err)
	runtime := gbtrace.NewModule(cfg.Trace, offlineTraceStore{}, cipher)
	server, err := NewServer(cfg, WithTraceFactory(func(gbconfig.TraceConfig) gbtrace.Runtime { return runtime }))
	require.NoError(t, err)
	require.NoError(t, server.Start())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		_ = server.Shutdown(ctx)
	})
	time.Sleep(100 * time.Millisecond)

	ua, err := sipgo.NewUA(sipgo.WithUserAgent("trace-offline-test"))
	require.NoError(t, err)
	defer ua.Close()
	client, err := sipgo.NewClient(ua, sipgo.WithClientHostname("127.0.0.1"))
	require.NoError(t, err)
	defer client.Close()

	recipient := siplib.Uri{}
	require.NoError(t, siplib.ParseUri("sip:34020000002000000001@127.0.0.1:"+strconv.Itoa(port), &recipient))
	request := siplib.NewRequest(siplib.REGISTER, recipient)
	request.AppendHeader(siplib.NewHeader("Contact", "<sip:34020000001320000001@127.0.0.1>"))
	request.SetTransport("UDP")
	transaction, err := client.TransactionRequest(context.Background(), request, sipgo.ClientRequestRegisterBuild)
	require.NoError(t, err)
	defer transaction.Terminate()

	select {
	case response := <-transaction.Responses():
		require.NotNil(t, response)
		require.Contains(t, []int{siplib.StatusUnauthorized, siplib.StatusOK}, response.StatusCode)
	case <-time.After(3 * time.Second):
		t.Fatal("SIP REGISTER response timed out while ClickHouse was offline")
	}
	require.Eventually(t, func() bool {
		health := runtime.Health()
		return health.State == gbtrace.HealthDegraded && health.LastError == "clickhouse offline"
	}, time.Second, 10*time.Millisecond)
}
