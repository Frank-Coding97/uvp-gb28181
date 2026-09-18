package routes

import (
	"testing"

	"github.com/stretchr/testify/require"

	gbplay "uvplatform.cn/uvp-gb28181/app/gb28181/play"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

type autoRuntimeRegistry struct {
	mediaNode *node.Node
}

func (r autoRuntimeRegistry) IDForUUID(uuid string) (int64, bool) {
	if r.mediaNode == nil || r.mediaNode.MediaServerUUID != uuid {
		return 0, false
	}
	return r.mediaNode.ID, true
}

func (r autoRuntimeRegistry) ResolveAutoOnDemandNode(uuid string) (*node.Node, bool) {
	if _, ok := r.IDForUUID(uuid); !ok {
		return nil, false
	}
	copy := *r.mediaNode
	return &copy, true
}

func (r autoRuntimeRegistry) Get(id int64) (*node.Node, bool) {
	if r.mediaNode == nil || r.mediaNode.ID != id {
		return nil, false
	}
	copy := *r.mediaNode
	return &copy, true
}

func (r autoRuntimeRegistry) IsAutoOnDemandReady(id int64) bool {
	return r.mediaNode != nil && r.mediaNode.ID == id
}

func (r autoRuntimeRegistry) List() []*node.Node {
	if r.mediaNode == nil {
		return nil
	}
	copy := *r.mediaNode
	return []*node.Node{&copy}
}

func TestPlayServiceAndHookRegistryWireBoundedAutoOnDemandRuntime(t *testing.T) {
	SetPlayService(nil)
	t.Cleanup(func() {
		SetPlayService(nil)
		SetHookMultiNode(nil, nil)
	})

	service := new(gbplay.Service)
	SetPlayService(service)
	require.Nil(t, autoOnDemandDispatcher, "节点 Registry 未装配前必须保持 fail-closed")

	registry := autoRuntimeRegistry{mediaNode: &node.Node{
		ID: 7, Host: "192.0.2.1", MediaServerUUID: "node-a", State: node.StateActive,
		RTPPortStart: 30000, RTPPortEnd: 30100,
	}}
	SetHookMultiNode(registry, nil)
	require.NotNil(t, autoOnDemandDispatcher)
	require.True(t, autoOnDemandDispatcher.Available())

	SetPlayService(nil)
	require.Nil(t, autoOnDemandDispatcher)
}
