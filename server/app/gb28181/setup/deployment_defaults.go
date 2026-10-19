package setup

import (
	"os"
	"strconv"
	"strings"
)

// DefaultSIPPortEnv 是「部署层告知引导页默认 SIP 端口」的环境变量名。
const DefaultSIPPortEnv = "UVP_DEFAULT_SIP_PORT"

// DeploymentDefaultSIPPort 返回部署层指定的引导页默认 SIP 端口，没配/非法时返回 0（表示"不知道"）。
//
// ⛔⛔ 为什么默认端口不能只在**构建期**定：SIP 端口不经过 config.yml、也不经过
//     容器端口映射 —— 它是管理员首次登录时在引导页里**手工录入**、存进 gb_sip_config 的。
//     前端为了"预填一个段内端口"，把默认值烤进了产物（VITE_DEFAULT_SIP_PORT），
//     于是默认值是**打镜像那一刻**决定的常量。
//     但安装脚本允许整段平移端口（`--https-port 53000` ⇒ 规划段 53000-53064），
//     段一变，烤进去的 51064 就落在**段外**：
//       · 防火墙只放行了段内一段（一条规则），51064 根本不通；
//       · 设备注册到 51064 必然失败，而现场看到的只是"注册不上"，查不到端口段这层原因。
//     踩过：220 上用 53000 段部署，引导页默认值仍是 51064。
//
// ⇒ 正解是**部署事实由部署层给**：install.sh 按段基址算出 SIP 端口写进 .env → compose
//   注入本进程 → 本函数读出来 → Status 下发给前端。前端拿不到才退回构建期常量。
//
// ⚠️ 返回 0 而不是某个兜底数字：本函数**不知道**当前部署用哪个段，编一个数字只会
//    让前端放弃它自己的构建期默认值去用一个更错的。0 在 JSON 里配合 omitempty
//    就是"这条信息不存在"，前端照旧走自己的回退链。
func DeploymentDefaultSIPPort() int {
	raw := strings.TrimSpace(os.Getenv(DefaultSIPPortEnv))
	if raw == "" {
		return 0
	}
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		return 0
	}
	return port
}
