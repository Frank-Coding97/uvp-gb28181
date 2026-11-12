package security

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFirewallAgentReleaseAndDeploymentContract(t *testing.T) {
	_, current, _, ok := runtime.Caller(0)
	require.True(t, ok)
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(current), "../../../.."))
	read := func(path string) string {
		data, err := os.ReadFile(filepath.Join(repoRoot, path))
		require.NoError(t, err, path)
		return string(data)
	}

	workflow := read(".github/workflows/ci-deploy-test.yml")
	require.Contains(t, workflow, "uvp-firewall-agent-linux-amd64")

	build := read("deploy/test/build-release.sh")
	require.Contains(t, build, "uvp-firewall-agent-linux-amd64")
	require.Contains(t, build, "assemble-release.sh")

	assemble := read("deploy/test/assemble-release.sh")
	require.Contains(t, assemble, "uvp-firewall-agent-linux-amd64")
	require.Contains(t, assemble, `install -m 0755 "$SOURCE_ROOT/server/bin/uvp-firewall-agent-linux-amd64" "$release_dir/agent/uvp-firewall-agent"`)
	require.Contains(t, assemble, `install -m 0644 "$SOURCE_ROOT/deploy/test/uvp-firewall-agent.service" "$release_dir/agent/uvp-firewall-agent.service"`)
	require.Contains(t, assemble, `install -m 0644 "$SOURCE_ROOT/deploy/test/uvp-firewall-agent.default" "$release_dir/agent/uvp-firewall-agent.default"`)

	compose := read("deploy/test/compose.yml")
	require.Contains(t, compose, "/run/uvp:/run/uvp")
	require.Contains(t, compose, `"0.0.0.0:56002:56002/tcp"`)
	require.Contains(t, compose, `"0.0.0.0:56002:56002/udp"`)

	deploy := read("deploy/test/deploy-uvp.sh")
	require.Contains(t, deploy, "agent-current")
	require.Contains(t, deploy, `install -m 0644 "$RELEASES/$PREVIOUS/agent/uvp-firewall-agent.service"`)
	require.Contains(t, deploy, "systemctl enable uvp-firewall-agent")
	require.Contains(t, deploy, "systemctl restart uvp-firewall-agent")
	require.Contains(t, deploy, "for _ in {1..50}")
	require.Contains(t, deploy, "delete table inet uvp_sip_guard")
	require.NotContains(t, strings.ToLower(deploy), "flush ruleset")

	unit := read("deploy/test/uvp-firewall-agent.service")
	require.Contains(t, unit, "CapabilityBoundingSet=CAP_NET_ADMIN")
	require.Contains(t, unit, "/opt/uvp-gb28181/agent-current/uvp-firewall-agent")
}

// TestStandalonePackageShipsFirewallAgent 锁住绿色包（单机包）的**四件事**。
//
// ⛔⛔ 背景（2026-10-20 老板报障）：绿色包从来没打过 uvp-firewall-agent，
// 于是「国标接入安全」切保护/严格模式时后端拨 `/run/uvp/firewall-agent.sock`
// 直接 `no such file or directory`，整个保存请求 400 —— 功能等于开不了。
// 只补其中一环都不算修好：
//
//	① 出包脚本要**编并装**进包里（否则现场拿到的是老包）；
//	② ctl.sh 要**起停**它，并把包内 socket 路径 export 给后端（否则后端永远找 /run/uvp）；
//	③ 服务注册脚本要有 **root 单元**（业务服务按普通用户跑，没有 nft 权限）；
//	④ 卸载脚本要一起摘掉（否则内核 nft 表留在机器上持续丢包）。
func TestStandalonePackageShipsFirewallAgent(t *testing.T) {
	_, current, _, ok := runtime.Caller(0)
	require.True(t, ok)
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(current), "../../../.."))
	read := func(path string) string {
		data, err := os.ReadFile(filepath.Join(repoRoot, path))
		require.NoError(t, err, path)
		return string(data)
	}

	build := read("deploy/standalone/build-standalone.sh")
	require.Contains(t, build, "cmd/uvp-firewall-agent")
	require.Contains(t, build, `cp "$SERVER_DIR/bin/uvp-firewall-agent" "$PKG/bin/uvp-firewall-agent"`)
	require.Contains(t, build, `assert_fresh "$SERVER_DIR/bin/uvp-firewall-agent"`)
	require.Contains(t, build, `"$PKG/bin/uvp-firewall-agent" \`)

	ctl := read("deploy/standalone/uvp-gb28181-ctl.sh")
	require.Contains(t, ctl, "start_firewall_agent")
	require.Contains(t, ctl, "stop_firewall_agent")
	require.Contains(t, ctl, "serve_firewall_agent")
	require.Contains(t, ctl, `export UVP_GB28181_FIREWALL_SOCKET="$FIREWALL_AGENT_SOCKET"`)
	require.Contains(t, ctl, "--sip-port")
	// 非 root 时只能提示、不能 fail：应用层拦截仍然有效，平台不该起不来。
	require.Contains(t, ctl, `if [ "$(id -u)" -ne 0 ]; then`)

	install := read("deploy/standalone/uvp-gb28181-service-install.sh")
	require.Contains(t, install, `FIREWALL_UNIT_NAME="uvp-gb28181-firewall-agent.service"`)
	require.Contains(t, install, "AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW")
	require.Contains(t, install, "Group=${RUN_GROUP}")
	// ⛔ 单元不能写死版本路径（升级后 releases/<id> 会变），必须走 ctl 的 serve 子命令。
	require.Contains(t, install, `ExecStart=/bin/bash "${CTL}" serve-firewall-agent`)
	require.Contains(t, install, "systemctl enable \"$FIREWALL_UNIT_NAME\"")

	uninstall := read("deploy/standalone/uvp-gb28181-service-uninstall.sh")
	require.Contains(t, uninstall, `FIREWALL_UNIT_NAME="uvp-gb28181-firewall-agent.service"`)
	require.Contains(t, uninstall, "nft delete table inet uvp_sip_guard")

	// Go 侧：没有代理时**不许**把"切保护模式"变成错误（否则绿色包永远开不了）。
	bootstrap := read("server/app/gb28181/bootstrap.go")
	require.Contains(t, bootstrap, "NewSocketPresenceClient")
	require.Contains(t, bootstrap, "SetSIPPort")
}
