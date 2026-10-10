package playback

import (
	"context"
	"errors"
)

// ResourceAbandoner 由"需要设备侧确认才能释放"的资源实现。
//
// 正常情况下清理保持 fail-closed:设备没有确认拆除之前,平台保留绑定以便安全重试。
// 但设备不一定应答,重试必须有终点 —— 耗尽后 Registry 会先调用本接口解除确认前置
// 条件,再走 RunAbandoned 释放平台侧资源。
type ResourceAbandoner interface {
	AbandonDeviceTeardown(context.Context) error
}

type CleanupRunner struct{}

// Run 是常规清理路径,保持 fail-closed 语义:
// 前一阶段未结算时保留后续绑定,让同一会话可以在下一次尝试里安全重试。
func (CleanupRunner) Run(ctx context.Context, resources CleanupResources) error {
	if resources == nil {
		return nil
	}
	var result error
	result = errors.Join(result, resources.Teardown(ctx))
	result = errors.Join(result, resources.CloseRTP(ctx))
	if result != nil {
		return result
	}
	return resources.Unbind(ctx)
}

// RunAbandoned 在设备侧拆除重试耗尽后强制执行完整清理。
//
// 与 Run 的两点差别都是刻意的:
//  1. 先解除"必须收到设备确认"的前置条件(资源支持时);
//  2. 三个阶段各自执行,不再用前一阶段的失败短路后面的阶段。
//
// 此时"保留绑定换安全重试"已经没有意义:继续等待只会把通道的占位永久锁死
// (对外固定 429「当前通道已有回放会话」),并让清扫器持续向设备灌信令。
func (CleanupRunner) RunAbandoned(ctx context.Context, resources CleanupResources) error {
	if resources == nil {
		return nil
	}
	var result error
	if abandoner, ok := resources.(ResourceAbandoner); ok {
		result = errors.Join(result, abandoner.AbandonDeviceTeardown(ctx))
	}
	result = errors.Join(result, resources.Teardown(ctx))
	result = errors.Join(result, resources.CloseRTP(ctx))
	result = errors.Join(result, resources.Unbind(ctx))
	return result
}
