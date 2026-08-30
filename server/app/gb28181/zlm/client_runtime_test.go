package zlm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestClientRuntimeStatistic(t *testing.T) {
	client, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/index/api/getStatistic", r.URL.Path)
		require.Equal(t, "test-secret", r.URL.Query().Get("secret"))
		_, _ = w.Write([]byte(`{"code":0,"data":{"MediaSource":7,"MultiMediaSourceMuxer":8,"TcpServer":9,"TcpSession":10,"UdpServer":11,"UdpSession":12,"TcpClient":13,"Socket":14,"FrameImp":15,"Frame":16,"Buffer":17,"BufferRaw":18,"BufferLikeString":19,"BufferList":20,"RtpPacket":21,"RtmpPacket":22}}`))
	})
	defer server.Close()

	statistic, err := client.GetStatistic(context.Background())
	require.NoError(t, err)
	require.Equal(t, uint64(7), statistic.MediaSource)
	require.Equal(t, uint64(10), statistic.TcpSession)
	require.Equal(t, uint64(14), statistic.Socket)
	require.Equal(t, uint64(22), statistic.RtmpPacket)
}

func TestClientRuntimeThreadLoadDetailKeepsNameLoadAndFDCount(t *testing.T) {
	client, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/index/api/getThreadsLoad", r.URL.Path)
		_, _ = w.Write([]byte(`{"code":0,"data":[{"name":"event poller 0","load":12,"fd_count":31},{"name":"event poller 1","load":48,"fd_count":17}]}`))
	})
	defer server.Close()

	loads, err := client.GetThreadsLoadDetail(context.Background())
	require.NoError(t, err)
	require.Equal(t, []ThreadLoad{
		{Name: "event poller 0", Load: 12, FDCount: 31},
		{Name: "event poller 1", Load: 48, FDCount: 17},
	}, loads)
	require.InDelta(t, 0.30, AverageThreadLoad(loads), 0.0001)
}

func TestClientRuntimeAllSession(t *testing.T) {
	client, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/index/api/getAllSession", r.URL.Path)
		query := r.URL.Query()
		require.Equal(t, "1935", query.Get("local_port"))
		require.Equal(t, "10.0.0.8", query.Get("peer_ip"))
		require.Equal(t, "test-secret", query.Get("secret"))
		_, _ = w.Write([]byte(`{"code":0,"data":[{"id":"session-1","peer_ip":"10.0.0.8","peer_port":4567,"local_ip":"0.0.0.0","local_port":1935,"identifier":"session-1","type":"tcp","typeid":"RtspSession"}]}`))
	})
	defer server.Close()

	sessions, err := client.GetAllSessions(context.Background(), SessionFilter{LocalPort: 1935, PeerIP: "10.0.0.8"})
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	require.Equal(t, "session-1", sessions[0].ID)
	require.Equal(t, "tcp", sessions[0].Type)
	require.Equal(t, 1935, sessions[0].LocalPort)
}

func TestClientRuntimeAPIList(t *testing.T) {
	client, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/index/api/getApiList", r.URL.Path)
		require.Equal(t, "test-secret", r.URL.Query().Get("secret"))
		_, _ = w.Write([]byte(`{"code":0,"data":["/index/api/getStatistic","/index/api/getAllSession","/index/api/getApiList"]}`))
	})
	defer server.Close()

	apis, err := client.GetAPIList(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"/index/api/getStatistic", "/index/api/getAllSession", "/index/api/getApiList"}, apis)
}

func TestClientCapabilityProfileStates(t *testing.T) {
	tests := []struct {
		name           string
		body           string
		wantStatistic  CapabilityState
		wantAllSession CapabilityState
		wantAPIList    CapabilityState
		wantError      bool
	}{
		{
			name:           "supported and unsupported",
			body:           `{"code":0,"data":["/index/api/getStatistic","/index/api/getApiList"]}`,
			wantStatistic:  CapabilitySupported,
			wantAllSession: CapabilityUnsupported,
			wantAPIList:    CapabilitySupported,
		},
		{
			name:           "unable to probe",
			body:           `{"code":-1,"msg":"Please login first"}`,
			wantStatistic:  CapabilityUnknown,
			wantAllSession: CapabilityUnknown,
			wantAPIList:    CapabilityUnknown,
			wantError:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, server := newMockClient(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tt.body))
			})
			defer server.Close()

			profile, err := client.GetCapabilityProfile(context.Background())
			if tt.wantError {
				require.Error(t, err)
				require.ErrorIs(t, err, ErrCapabilityUnknown)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tt.wantStatistic, profile.GetStatistic)
			require.Equal(t, tt.wantAllSession, profile.GetAllSession)
			require.Equal(t, tt.wantAPIList, profile.GetAPIList)
		})
	}
}

func TestClientCapabilityProfileNormalizesAPIPaths(t *testing.T) {
	client, server := newMockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":["getStatistic","/index/api/getAllSession"]}`))
	})
	defer server.Close()

	profile, err := client.GetCapabilityProfile(context.Background())
	require.NoError(t, err)
	require.Equal(t, CapabilitySupported, profile.Status("/index/api/getStatistic"))
	require.Equal(t, CapabilitySupported, profile.Status("getAllSession"))
	require.Equal(t, CapabilityUnsupported, profile.Status("getMediaList"))
}

func TestClientCapabilityProfileEmptyAPIListIsUnsupported(t *testing.T) {
	client, server := newMockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":[]}`))
	})
	defer server.Close()

	profile, err := client.GetCapabilityProfile(context.Background())
	require.NoError(t, err)
	require.NotNil(t, profile.APIs)
	require.Equal(t, CapabilityUnsupported, profile.GetStatistic)
	require.Equal(t, CapabilityUnsupported, profile.GetAllSession)
	require.Equal(t, CapabilitySupported, profile.GetAPIList)
	require.Equal(t, CapabilityUnsupported, profile.Status("unknownApi"))
}

func TestClientRuntimeZLMCodeDistinguishesUnsupported(t *testing.T) {
	client, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/index/api/getStatistic", r.URL.Path)
		_, _ = w.Write([]byte(`{"code":-404,"msg":"api not found: http://user:password@example.invalid/api?token=should-not-leak"}`))
	})
	defer server.Close()

	_, err := client.GetStatistic(context.Background())
	require.Error(t, err)
	require.ErrorIs(t, err, ErrCapabilityUnsupported)
	require.NotContains(t, err.Error(), "password")
	require.NotContains(t, err.Error(), "token=should-not-leak")
}

func TestClientRuntimeMalformedJSONDoesNotReturnEmptyData(t *testing.T) {
	client, server := newMockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":`))
	})
	defer server.Close()

	statistic, err := client.GetStatistic(context.Background())
	require.Error(t, err)
	require.Zero(t, statistic)
	require.ErrorIs(t, err, ErrNodeFailure)
}

func TestClientRuntimeResponseLimit(t *testing.T) {
	client, server := newMockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `{"code":0,"data":%q}`, strings.Repeat("x", 8<<20))
	})
	defer server.Close()

	_, err := client.GetAPIList(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "超出上限")
	require.ErrorIs(t, err, ErrNodeFailure)
}

func TestClientRuntimeDeadline(t *testing.T) {
	client, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := client.GetStatistic(ctx)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrNodeFailure)
	require.Less(t, time.Since(started), time.Second)
}

func TestClientRuntimeRedactsSecretUserinfoAndQuery(t *testing.T) {
	client, _ := newMockClient(t, func(http.ResponseWriter, *http.Request) {})
	client.http = &http.Client{Transport: runtimeRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		return nil, errors.New("dial failed: GET " + r.URL.String() + " userinfo=http://alice:password@example.invalid/path?token=super-secret")
	})}

	_, err := client.GetStatistic(context.Background())
	require.Error(t, err)
	message := err.Error()
	require.NotContains(t, message, "test-secret")
	require.NotContains(t, message, "alice:password")
	require.NotContains(t, message, "token=super-secret")
	require.Contains(t, message, "userinfo=http://example.invalid/path")
}

func TestClientRuntimeAPIListRedactsUntrustedURLValues(t *testing.T) {
	client, server := newMockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":["http://alice:password@example.invalid/api?token=should-not-leak"]}`))
	})
	defer server.Close()

	apis, err := client.GetAPIList(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"http://example.invalid/api"}, apis)
}

func TestClientRuntimeConcurrentReads(t *testing.T) {
	client, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index/api/getStatistic":
			_, _ = w.Write([]byte(`{"code":0,"data":{"MediaSource":1}}`))
		case "/index/api/getAllSession":
			_, _ = w.Write([]byte(`{"code":0,"data":[]}`))
		case "/index/api/getApiList":
			_, _ = w.Write([]byte(`{"code":0,"data":["/index/api/getStatistic"]}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	})
	defer server.Close()

	const workers = 12
	var wait sync.WaitGroup
	wait.Add(workers)
	for i := 0; i < workers; i++ {
		go func(i int) {
			defer wait.Done()
			ctx := context.Background()
			switch i % 3 {
			case 0:
				_, _ = client.GetStatistic(ctx)
			case 1:
				_, _ = client.GetAllSessions(ctx, SessionFilter{})
			default:
				_, _ = client.GetAPIList(ctx)
			}
		}(i)
	}
	wait.Wait()
}

type runtimeRoundTripFunc func(*http.Request) (*http.Response, error)

func (f runtimeRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
