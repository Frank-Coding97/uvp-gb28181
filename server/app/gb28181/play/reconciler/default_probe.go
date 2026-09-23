package reconciler

import (
	"context"
	"errors"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

// defaultProbeImpl 生产 media probe 实现.
// 单独文件 + 反向依赖 zlm 包,让 reconciler.go 保持零 zlm 依赖,测试更纯净.
//
// 单节点场景(n == nil): 目前没有单节点 client 直传路径,由调用方通过
// WithMediaProbe 注入自定义 probe(bootstrap.go 装配时决定).
// 这里返回 error 让调用者知道走错分支.
func defaultProbeImpl(ctx context.Context, n *node.Node, streamID string) (bool, error) {
	if n == nil {
		return false, errors.New("defaultProbeImpl: node is nil (单节点场景请通过 WithMediaProbe 注入自定义 probe)")
	}
	client := zlm.NewClientForNode(n)
	return client.IsMediaOnline(ctx, "rtp", streamID)
}
