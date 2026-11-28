package models_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// catalogChannelAttributeColumns 是 B-2(平台侧接收并落库目录通道属性)新增的十列。
// 判定依据:GB/T 28181-2022 附录 A / §9.3.1。
//
// 前六列是两版共有:RoomType/SupplyLightType/DirectionType/Resolution 在 `<Info>` 内,
// IPAddress/Port 在 Catalog Item 层。
//
// 后四列**版本互斥**:PositionType/UseType 是 2016 独有(2022 已删除),
// PhotoelectricImagingType/CapturePositionType 是 2022 独有。
// 它们本身就是"这台设备报的是哪一版目录形态"的证据,所以必须落库而不是只解析。
var catalogChannelAttributeColumns = []string{
	"room_type",
	"supply_light_type",
	"direction_type",
	"resolution",
	"ip_address",
	"port",
	"position_type",
	"use_type",
	"photoelectric_imaging_type",
	"capture_position_type",
}

// 2022 才对 ptz_type 值域补的三种类型名(依据 GB/T 28181-2022 附录 A)。
var ptzTypeTwoThousandTwentyTwoDictNames = []string{
	"遥控半球",
	"多目设备的全景/拼接通道",
	"多目设备的分割通道",
}

func TestChannelCatalogAttributesDDLIsAvailableForEverySupportedDatabase(t *testing.T) {
	root := dualVersionDDLRoot(t)
	migrations := filepath.Join(root, "resource", "database", "gb28181", "migrations")

	for _, migration := range []struct {
		name, up, down, guard string
	}{
		{"mysql", "2026-09-18-channel-catalog-attributes.sql", "2026-09-18-channel-catalog-attributes-down.sql", "information_schema.columns"},
		{"postgresql", "2026-09-18-channel-catalog-attributes-postgresql.sql", "2026-09-18-channel-catalog-attributes-postgresql-down.sql", "if not exists"},
		{"sqlserver", "2026-09-18-channel-catalog-attributes-sqlserver.sql", "2026-09-18-channel-catalog-attributes-sqlserver-down.sql", "col_length"},
	} {
		t.Run(migration.name, func(t *testing.T) {
			for _, file := range []string{migration.up, migration.down} {
				body, err := os.ReadFile(filepath.Join(migrations, file))
				require.NoErrorf(t, err, "迁移文件缺失: %s", file)
				lower := strings.ToLower(string(body))
				for _, column := range catalogChannelAttributeColumns {
					require.Containsf(t, lower, column, "%s 未涉及列 %s", file, column)
				}
			}

			// up 必须带本方言的幂等守卫,否则重复执行会报"列已存在"。
			upBody, err := os.ReadFile(filepath.Join(migrations, migration.up))
			require.NoError(t, err)
			upLower := strings.ToLower(string(upBody))
			require.Contains(t, upLower, migration.guard, "%s 缺少幂等守卫 %s", migration.up, migration.guard)

			// up 补 2022 值域字典项,down 负责删同三项。
			for _, name := range ptzTypeTwoThousandTwentyTwoDictNames {
				require.Contains(t, string(upBody), name, "%s 未补齐字典项 %s", migration.up, name)
			}
			for _, value := range []string{"'5'", "'6'", "'7'"} {
				require.Contains(t, string(upBody), value, "%s 未写入字典值 %s", migration.up, value)
			}

			downBody, err := os.ReadFile(filepath.Join(migrations, migration.down))
			require.NoError(t, err)
			require.Contains(t, string(downBody), "sys_dict_item", "%s 未回收字典项", migration.down)
			for _, value := range []string{"'5'", "'6'", "'7'"} {
				require.Contains(t, string(downBody), value, "%s 未回收字典值 %s", migration.down, value)
			}
		})
	}
}

// 三个全量快照都必须带上新列,否则新建库的 gb_channel 与增量迁移后的库不一致。
func TestChannelCatalogAttributesPresentInEverySnapshot(t *testing.T) {
	root := dualVersionDDLRoot(t)
	for _, file := range []string{"uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql"} {
		body, err := os.ReadFile(filepath.Join(root, "resource", "database", file))
		require.NoError(t, err)
		text := strings.ToLower(string(body))
		for _, column := range catalogChannelAttributeColumns {
			require.Containsf(t, text, column, "%s 缺少列 %s", file, column)
		}
	}
}

// ptz_type 的列注释/enum 必须同步到 2022 值域(1-7),否则人工编辑通道时
// 后端仍按 2016 的 0-4 校验。校验点:迁移三方言 + MySQL 全量快照的字典种子。
func TestChannelPTZTypeEnumCoverTwoThousandTwentyTwo(t *testing.T) {
	root := dualVersionDDLRoot(t)
	for _, file := range []string{
		"2026-09-18-channel-catalog-attributes.sql",
		"2026-09-18-channel-catalog-attributes-postgresql.sql",
		"2026-09-18-channel-catalog-attributes-sqlserver.sql",
	} {
		body, err := os.ReadFile(filepath.Join(root, "resource", "database", "gb28181", "migrations", file))
		require.NoError(t, err)
		require.Contains(t, string(body), "7多目分割通道", "%s 的 ptz_type 注释未同步到 2022 值域", file)
	}

	snapshot, err := os.ReadFile(filepath.Join(root, "resource", "database", "uvp-gb28181.sql"))
	require.NoError(t, err)
	text := string(snapshot)
	for _, name := range ptzTypeTwoThousandTwentyTwoDictNames {
		require.Containsf(t, text, name, "MySQL 全量快照 sys_dict_item 缺少 %s", name)
	}
}
