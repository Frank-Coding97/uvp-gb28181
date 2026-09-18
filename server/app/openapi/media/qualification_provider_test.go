package media

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
	"uvplatform.cn/uvp-gb28181/app/openapi/config"
)

func qualifiedProviderFixture(t *testing.T) (*NodeQualificationProvider, *trustedFactoryFixture) {
	t.Helper()
	f := newTrustedFactoryFixture(t)
	bindings := &FileNodeControlBindings{bindings: map[string]NodeControlBinding{"node-a": f.binding}, qualifications: map[string]deploymentQualification{
		"node-a": {ID: "aef3e620-28b8-4ba3-a19c-378387d86a15", Topology: "direct-http1", MediaOrigins: map[string]string{"https-flv": "https://media.example.test"}},
	}}
	p := NewNodeQualificationProvider(f.factory.registry, bindings, f.store)
	return p, f
}

func TestNodeQualificationProviderCapacityIsAtomicAndExpires(t *testing.T) {
	p, _ := qualifiedProviderFixture(t)
	now := time.Now()
	for i := 0; i < qualificationCapacity-1; i++ {
		p.tickets[fmt.Sprint(i)] = qualifiedTicketRecord{ticket: QualificationTicket{ExpiresAt: now.Add(time.Minute)}}
	}
	var workers sync.WaitGroup
	var admitted atomic.Int32
	for i := 0; i < 16; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if _, err := p.Prepare(context.Background(), qualificationTarget()); err == nil {
				admitted.Add(1)
			}
		}()
	}
	workers.Wait()
	require.EqualValues(t, 1, admitted.Load())
	require.Len(t, p.tickets, qualificationCapacity)
	require.Zero(t, p.pending)
	p.now = func() time.Time { return now.Add(time.Hour) }
	_, err := p.Prepare(context.Background(), qualificationTarget())
	require.NoError(t, err)
	require.Len(t, p.tickets, 1, "expired tickets cannot permanently exhaust capacity")
	require.Zero(t, p.pending)
}

func TestNodeQualificationProviderRejectsDrift(t *testing.T) {
	for _, kind := range []string{"revision", "offline", "recovery", "boot", "unknown", "binding", "origin", "ticket-id", "cross-provider"} {
		t.Run(kind, func(t *testing.T) {
			p, f := qualifiedProviderFixture(t)
			ctx := context.Background()
			target := qualificationTarget()
			ticket, err := p.Prepare(ctx, target)
			require.NoError(t, err)
			switch kind {
			case "revision":
				f.mu.Lock()
				f.n.Revision++
				f.mu.Unlock()
			case "offline":
				f.mu.Lock()
				f.n.State = "offline"
				f.mu.Unlock()
			case "recovery":
				f.mu.Lock()
				f.n.RecoveryRequired = true
				f.mu.Unlock()
			case "boot":
				require.NoError(t, f.db.Exec("UPDATE meta_node SET current_boot_nonce=? WHERE id=1", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa").Error)
			case "unknown":
				_, err = f.store.MarkUnknown(ctx, config.NodeRuntimeRef{NodeID: ticket.NodeID, NodeUUID: ticket.NodeUUID, NodeRevision: ticket.NodeRevision})
				require.NoError(t, err)
			case "binding":
				b := p.bindings.bindings["node-a"]
				b.BindingRevision++
				p.bindings.bindings["node-a"] = b
			case "origin":
				ticket.MediaOrigin = "https://other.example.test"
			case "ticket-id":
				ticket.QualificationID = "aef3e620-28b8-4ba3-a19c-378387d86a15"
			case "cross-provider":
				p = NewNodeQualificationProvider(p.registry, p.bindings, p.store)
			}
			require.Error(t, p.Validate(ctx, target, ticket))
		})
	}
}

func TestNodeQualificationProviderAdmissionBeforeProbe(t *testing.T) {
	for _, kind := range []string{"no-declaration", "wrong-protocol", "full", "cancelled", "bad-target"} {
		t.Run(kind, func(t *testing.T) {
			p, f := qualifiedProviderFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			target := qualificationTarget()
			switch kind {
			case "no-declaration":
				p.bindings.qualifications = nil
			case "wrong-protocol":
				target.Protocol = "wss-flv"
			case "full":
				p.pending = qualificationCapacity
			case "cancelled":
				cancel()
			case "bad-target":
				target.DeviceID = "invalid"
			}
			ticket, err := p.Prepare(ctx, target)
			require.Error(t, err)
			require.Empty(t, ticket)
			f.mu.Lock()
			require.Empty(t, f.requests, "rejected qualification must not even start a control probe")
			f.mu.Unlock()
		})
	}
}

func qualificationTarget() QualificationRequest {
	return QualificationRequest{DeviceID: "34020000001320000001", ChannelID: "34020000001320000002", Protocol: "https-flv"}
}

func TestNodeQualificationProviderFreshBoundTicket(t *testing.T) {
	p, f := qualifiedProviderFixture(t)
	ctx := context.Background()
	target := qualificationTarget()
	ticket, err := p.Prepare(ctx, target)
	require.NoError(t, err)
	require.Equal(t, f.boot, ticket.BootNonce)
	require.Equal(t, uint64(9), ticket.NodeRevision)
	require.Equal(t, "https://media.example.test", ticket.MediaOrigin)
	require.NoError(t, p.Validate(ctx, target, ticket))
	request := play.Request{DeviceID: target.DeviceID, ChannelID: target.ChannelID, RequiredNode: ticket.NodeID, RequiredProtocol: target.Protocol, QualificationID: ticket.QualificationID}
	snapshot := play.NodeQualificationSnapshot{ID: ticket.NodeID, Revision: ticket.NodeRevision, MediaServerUUID: ticket.NodeUUID, State: "active"}
	f.mu.Lock()
	before := len(f.requests)
	f.mu.Unlock()
	require.NoError(t, p.PlayValidator().Validate(ctx, request, snapshot))
	f.mu.Lock()
	require.Len(t, f.requests, before, "play mutex validator cannot perform control I/O")
	f.mu.Unlock()
	changed := ticket
	changed.MediaOrigin = "https://other.example.test"
	require.Error(t, p.Validate(ctx, target, changed))
	target.ChannelID = "34020000001320000003"
	require.Error(t, p.Validate(ctx, target, ticket))
	p.now = func() time.Time { return ticket.ExpiresAt }
	require.Error(t, p.Validate(ctx, qualificationTarget(), ticket))
}
