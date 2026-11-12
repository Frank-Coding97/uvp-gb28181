package models_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// videoParamColumns 是 gb_device_video_param 的全部列（依据 GB/T 28181-2022 A.2.1.13）。
//
// 前五列 + video_bit_rate 是**设备回读的取值**，原样存附录 G 的码值字符串；
// 中间四列是迟到应答保护（source_operation_seq / source_sn / source_operation_id）
// 与观测时刻；最后三列是行元数据。
var videoParamColumns = []string{
	"id",
	"device_id",
	"target_code",
	"stream_number",
	"video_format",
	"resolution",
	"frame_rate",
	"bit_rate_type",
	"video_bit_rate",
	"source_operation_seq",
	"source_sn",
	"source_operation_id",
	"observed_at",
	"raw_summary",
	"created_at",
	"updated_at",
}

// videoParamAPIPath 是视频参数的读/写接口路径（与迁移、控制器、路由三处必须同名）。
const videoParamAPIPath = "/api/gb28181/device-mgmt/channel/:id/video-params"

func TestVideoParamDDLIsAvailableForEverySupportedDatabase(t *testing.T) {
	root := dualVersionDDLRoot(t)
	migrations := filepath.Join(root, "resource", "database", "gb28181", "migrations")

	for _, migration := range []struct {
		name, up, down, guard string
	}{
		{"mysql", "2026-09-18-channel-video-param.sql", "2026-09-18-channel-video-param-down.sql", "information_schema"},
		{"postgresql", "2026-09-18-channel-video-param-postgresql.sql", "2026-09-18-channel-video-param-postgresql-down.sql", "add column if not exists"},
		{"sqlserver", "2026-09-18-channel-video-param-sqlserver.sql", "2026-09-18-channel-video-param-sqlserver-down.sql", "col_length"},
	} {
		t.Run(migration.name, func(t *testing.T) {
			upBody, err := os.ReadFile(filepath.Join(migrations, migration.up))
			require.NoErrorf(t, err, "迁移文件缺失: %s", migration.up)
			upLower := strings.ToLower(string(upBody))

			require.Contains(t, upLower, "gb_device_video_param", "%s 未建落库表", migration.up)
			for _, column := range videoParamColumns {
				require.Containsf(t, upLower, column, "%s 未涉及列 %s", migration.up, column)
			}
			require.Contains(t, upLower, "uk_video_param_target", "%s 缺少每码流唯一键", migration.up)
			require.Contains(t, upLower, "stream_number_list", "%s 未补 gb_channel.stream_number_list", migration.up)

			// up 必须带本方言的幂等守卫，否则重复执行会报"表/列已存在"。
			require.Contains(t, upLower, migration.guard, "%s 缺少幂等守卫 %s", migration.up, migration.guard)

			// 读接口绑 gb28181:ptz:view、写接口绑 gb28181:ptz:control —— 两者都必须出现，
			// 且都必须走 sys_menu_api 精确绑定 + sys_casbin_rule 去重。
			for _, token := range []string{
				videoParamAPIPath,
				"gb28181:ptz:view",
				"gb28181:ptz:control",
				"sys_menu_api",
				"sys_casbin_rule",
				"select distinct 'p'",
				"not exists",
			} {
				require.Contains(t, upLower, token, "%s 权限段落缺少 %s", migration.up, token)
			}

			downBody, err := os.ReadFile(filepath.Join(migrations, migration.down))
			require.NoErrorf(t, err, "迁移文件缺失: %s", migration.down)
			downLower := strings.ToLower(string(downBody))
			require.Contains(t, downLower, "deleted_at", "%s 必须用软删除语义回收 API", migration.down)
			require.Contains(t, downLower, "sys_menu_api", migration.down)
			require.Contains(t, downLower, "sys_casbin_rule", migration.down)
			require.Contains(t, downLower, videoParamAPIPath, migration.down)
			require.Contains(t, downLower, "gb_device_video_param", "%s 未删落库表", migration.down)
			require.Contains(t, downLower, "stream_number_list", "%s 未回收 gb_channel.stream_number_list", migration.down)
		})
	}
}

// 三个全量快照都必须带上「本表 + 本接口权限」，理由是 runner 的基线语义：
//
//	空版本表 + 基线探测表已存在 ⇒ 全部迁移被直接标记为已应用而**不执行**。
//
// 所以快照里没有的物件在快照建出来的新库上**永远不会出现**，增量迁移补不回来。
// 这条不是风格问题，是新建库能不能用的问题。
func TestVideoParamPresentInEverySnapshot(t *testing.T) {
	root := dualVersionDDLRoot(t)
	for _, file := range []string{"uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql"} {
		t.Run(file, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(root, "resource", "database", file))
			require.NoError(t, err)
			text := strings.ToLower(string(body))

			require.Containsf(t, text, "gb_device_video_param",
				"%s 缺少 gb_device_video_param 建表；空版本表会让增量迁移被整体跳过", file)
			require.Containsf(t, text, videoParamAPIPath,
				"%s 缺少视频参数接口注册；空版本表会让增量迁移被整体跳过", file)
			require.Containsf(t, text, "gb28181:ptz:view", "%s 缺少读接口权限绑定", file)
			require.Containsf(t, text, "gb28181:ptz:control", "%s 缺少写接口权限绑定", file)

			for _, column := range []string{"stream_number", "video_format", "resolution", "frame_rate", "bit_rate_type", "video_bit_rate"} {
				require.Containsf(t, text, column, "%s 缺少列 %s", file, column)
			}
		})
	}
}

// gb_channel.stream_number_list 是 2022 独有的目录属性（A.2.1.9 <Info> 内的 StreamNumberList），
// 是视频参数面板「按码流分段」的出处。三个快照 + 三方言迁移都必须有。
func TestChannelStreamNumberListPresentEverywhere(t *testing.T) {
	root := dualVersionDDLRoot(t)
	for _, file := range []string{"uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql"} {
		body, err := os.ReadFile(filepath.Join(root, "resource", "database", file))
		require.NoError(t, err)
		require.Containsf(t, strings.ToLower(string(body)), "stream_number_list", "%s 缺少 stream_number_list 列", file)
	}

	for _, file := range []string{
		"2026-09-18-channel-video-param.sql",
		"2026-09-18-channel-video-param-postgresql.sql",
		"2026-09-18-channel-video-param-sqlserver.sql",
		"2026-09-18-channel-video-param-down.sql",
		"2026-09-18-channel-video-param-postgresql-down.sql",
		"2026-09-18-channel-video-param-sqlserver-down.sql",
	} {
		body, err := os.ReadFile(filepath.Join(root, "resource", "database", "gb28181", "migrations", file))
		require.NoError(t, err)
		require.Containsf(t, strings.ToLower(string(body)), "stream_number_list", "%s 未涉及 stream_number_list", file)
	}
}
