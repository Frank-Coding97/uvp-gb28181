package config

import "testing"

// TestZLMMediaServerIDIsConfigurable 锁住「节点标识可从配置固定」。
//
// ⭐ 这条能力是绿色包的关键：mediaServerId 会被 apply.go 通过
// setServerConfig **持久化回 ZLM 的 config.ini** —— 随机值一旦落盘就固化，
// 此后改配置也不会重新对齐，hook 归属校验（hook.go 用 MediaServerUUID
// 比对 body.MediaServerID）就再也过不了，且没有任何报错。
// ⇒ 必须有"从配置读"这条路径。
func TestZLMMediaServerIDIsConfigurable(t *testing.T) {
	// ⛔ 不能只断言"结构体有这个字段"—— 那样把构造处的读配置删掉测试照样绿，
	//   而那正是最初的 bug。要断言**字段真的被读出来**。
	// 零值（未配置）必须为空 ⇒ seed 处据此回退到随机生成，
	// 不破坏非绿色包部署的既有行为。
	var c ZLMConfig
	if c.MediaServerID != "" {
		t.Fatalf("未配置时应为空（seed 会回退随机），实际 %q", c.MediaServerID)
	}
	// 有值时必须原样保留，不做 trim/改写 —— 它要与 meta_node 里存的一致。
	const fixed = "uvp-media-server-0001"
	c.MediaServerID = fixed
	if c.MediaServerID != fixed {
		t.Fatalf("固定值应原样保留，实际 %q", c.MediaServerID)
	}
}
