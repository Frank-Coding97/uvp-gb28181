package sqlitebaseline

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"uvplatform.cn/uvp-gb28181/internal/sqlitedialect"
)

const expectedBaselineSHA256 = "8a0fb8b4575ea7d9dffe749a7d7d0d764e64f79dd8a2018ee23737c848786a61"

func openBaselineDB(t *testing.T) (*gorm.DB, *sql.DB) {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "baseline.db") + "?_pragma=foreign_keys(1)"
	db, err := gorm.Open(sqlitedialect.Open(dsn), &gorm.Config{TranslateError: true, Logger: logger.Discard})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	raw.SetMaxIdleConns(1)
	t.Cleanup(func() { require.NoError(t, raw.Close()) })
	require.NoError(t, db.Exec(SQL).Error)
	return db, raw
}

func TestBaselineArtifactAndManifestAreLocked(t *testing.T) {
	require.Equal(t, "sqlite-baseline-20260907-ren-r3", Version)
	require.Equal(t, expectedBaselineSHA256, SHA256)
	require.NotEmpty(t, SQL)
	digest := sha256.Sum256([]byte(SQL))
	require.Equal(t, expectedBaselineSHA256, hex.EncodeToString(digest[:]))

	body, err := os.ReadFile("manifest.json")
	require.NoError(t, err)
	var manifest struct {
		Version       string   `json:"version"`
		SourceCommit  string   `json:"source_commit"`
		SHA256        string   `json:"sha256"`
		Tables        int      `json:"tables"`
		TableNames    []string `json:"table_names"`
		Indexes       int      `json:"indexes"`
		IndexManifest []struct {
			Table string `json:"table"`
			Name  string `json:"name"`
		} `json:"index_manifest"`
		SeedStatements int               `json:"seed_statements"`
		SeedInserts    int               `json:"seed_inserts"`
		SeedUpdates    int               `json:"seed_updates"`
		SeedDeletes    int               `json:"seed_deletes"`
		Inputs         map[string]string `json:"inputs"`
	}
	require.NoError(t, json.Unmarshal(body, &manifest))
	require.Equal(t, Version, manifest.Version)
	require.Equal(t, "af558ce57b1805e05fa7fa107d73afb3fde460a0", manifest.SourceCommit)
	require.Equal(t, expectedBaselineSHA256, manifest.SHA256)
	require.Equal(t, 90, manifest.Tables)
	require.Len(t, manifest.TableNames, 90)
	require.Equal(t, 242, manifest.Indexes)
	require.Len(t, manifest.IndexManifest, 242)
	require.Equal(t, 1156, manifest.SeedStatements)
	require.Equal(t, 925, manifest.SeedInserts)
	require.Equal(t, 224, manifest.SeedUpdates)
	require.Equal(t, 7, manifest.SeedDeletes)
	require.Equal(t, map[string]string{
		"uvp-gb28181.sql":                      "40beabbe59e4d62e6790c3d315b45f3f33047b51bcb4a66bbf1b0629548ba665",
		"2026-08-15-device-grant-table.sql":    "1bed8dabb71f168b72fde28540f25b53b9c92bcddae5d06400d61a1d84dd4bfb",
		"2026-08-15-device-traffic.sql":        "9fdaef7450f0de93eaf0467a761acd13bfd468c2b0d7bfa28ad84afd67c3fdfe",
		"2026-08-21-device-traffic-hourly.sql": "6f5ab75f0bef394cbee8dd1e542cb28eb33de207773506c7873454210997bd9b",
		"2026-09-02-home-dashboard.sql":        "b265e4f9c155b7eda27330d1b45a6c2310c3382d81490a7242c67e0fd2d4feec",
	}, manifest.Inputs)
}

func TestBaselineCreatesCompleteSchemaAndSafeSeeds(t *testing.T) {
	db, raw := openBaselineDB(t)

	var tableCount int
	require.NoError(t, raw.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`).Scan(&tableCount))
	require.Equal(t, 90, tableCount)
	var sqliteVersion string
	require.NoError(t, raw.QueryRow(`SELECT sqlite_version()`).Scan(&sqliteVersion))
	require.Equal(t, "3.53.4", sqliteVersion)
	var foreignKeys int
	require.NoError(t, raw.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys))
	require.Equal(t, 1, foreignKeys)

	manifestBody, err := os.ReadFile("manifest.json")
	require.NoError(t, err)
	var manifest struct {
		TableNames    []string `json:"table_names"`
		IndexManifest []struct {
			Table   string `json:"table"`
			Name    string `json:"name"`
			Columns string `json:"columns"`
			Unique  bool   `json:"unique"`
		} `json:"index_manifest"`
	}
	require.NoError(t, json.Unmarshal(manifestBody, &manifest))
	var actualTables []string
	rows, err := raw.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`)
	require.NoError(t, err)
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		actualTables = append(actualTables, name)
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	sort.Strings(actualTables)
	expectedTables := append([]string(nil), manifest.TableNames...)
	sort.Strings(expectedTables)
	require.Equal(t, expectedTables, actualTables)

	type indexSpec struct {
		columns []string
		unique  bool
	}
	actualIndexes := map[string]indexSpec{}
	type indexKey struct {
		table string
		name  string
	}
	var indexKeys []indexKey
	indexRows, err := raw.Query(`SELECT tbl_name, name FROM sqlite_master WHERE type='index' AND name NOT LIKE 'sqlite_%'`)
	require.NoError(t, err)
	for indexRows.Next() {
		var table, name string
		require.NoError(t, indexRows.Scan(&table, &name))
		indexKeys = append(indexKeys, indexKey{table: table, name: name})
	}
	require.NoError(t, indexRows.Err())
	require.NoError(t, indexRows.Close())
	for _, index := range indexKeys {
		table, name := index.table, index.name
		var unique int
		require.NoError(t, raw.QueryRow(`SELECT "unique" FROM pragma_index_list(?) WHERE name=?`, table, name).Scan(&unique))
		columnRows, queryErr := raw.Query(`SELECT name FROM pragma_index_info(?) ORDER BY seqno`, name)
		require.NoError(t, queryErr)
		var columns []string
		for columnRows.Next() {
			var column string
			require.NoError(t, columnRows.Scan(&column))
			columns = append(columns, column)
		}
		require.NoError(t, columnRows.Err())
		require.NoError(t, columnRows.Close())
		actualIndexes[table+"\x00"+name] = indexSpec{columns: columns, unique: unique != 0}
	}
	expectedIndexes := map[string]indexSpec{}
	for _, index := range manifest.IndexManifest {
		var columns []string
		for _, column := range strings.Split(index.Columns, ",") {
			columns = append(columns, strings.Trim(strings.TrimSpace(column), `"`))
		}
		expectedIndexes[index.Table+"\x00"+index.Name] = indexSpec{columns: columns, unique: index.Unique}
	}
	require.Equal(t, expectedIndexes, actualIndexes)

	for _, table := range []string{
		"gb_alarm_binding", "gb_alarm_event", "gb_alarm_resource", "gb_alarm_resource_parent", "gb_anomaly_record",
		"gb_cascade_channel_projection", "gb_cascade_device_projection", "gb_cascade_media_session", "gb_cascade_platform",
		"gb_catalog_node", "gb_channel", "gb_channel_mount", "gb_custom_group", "gb_channel_favorite_item",
		"gb_channel_favorite_group", "gb_custom_group_device", "gb_device", "gb_device_control_state", "gb_device_status_event",
		"gb_device_subscription", "gb_mobile_position_history", "gb_mobile_position_latest", "gb_playback_scheme",
		"gb_playback_scheme_slot", "gb_ptz_cruise_track", "gb_ptz_home_position", "gb_ptz_operation", "gb_ptz_operation_attempt",
		"gb_ptz_preset", "gb_ptz_state", "gb_recording_plan_gap", "gb_recording_plan_execution", "gb_recording_plan_channel_state",
		"gb_recording_plan_binding", "gb_recording_plan_period", "gb_recording_plan", "gb_recording_file", "gb_recording_reconcile_state",
		"gb_recording_session", "gb_sip_config", "gb_sip_security_access_rule", "gb_sip_security_audit", "gb_sip_security_ban",
		"gb_sip_security_event", "gb_sip_security_policy", "gb_sip_trace_capture", "gb_sip_trace_message", "gb_sip_trace_session_diagnosis",
		"gb_talk_session", "meta_node", "gb_zlm_managed_resource", "scheduler_log", "scheduler_setting", "sys_affix",
		"sys_affix_chunk", "sys_api", "sys_casbin_rule", "sys_civil_code", "sys_department", "sys_dict", "sys_dict_item",
		"sys_gen", "sys_gen_field", "sys_job_results", "sys_jobs", "sys_menu", "sys_menu_api", "sys_operation_logs", "sys_param",
		"sys_role", "sys_role_menu", "sys_user_role", "sys_users", "sys_user_sessions", "sys_login_logs", "gb_device_firmware_upgrade",
		"gb_device_grant", "gb_device_traffic_session", "gb_device_traffic_daily", "gb_device_traffic_gap", "gb_device_traffic_hourly",
		"gb_dashboard_layout", "gb_sip_metric_minute", "gb_sip_metric_flush", "gb_sip_metric_gap", "gb_play_attempt",
	} {
		var exists int
		require.NoError(t, raw.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&exists), table)
		require.Equal(t, 1, exists, table)
	}

	for _, table := range []string{"gb_device", "gb_channel", "gb_sip_config", "gb_cascade_platform", "meta_node", "sys_operation_logs"} {
		var rows int
		require.NoError(t, raw.QueryRow(`SELECT COUNT(*) FROM `+table).Scan(&rows), table)
		require.Zero(t, rows, table)
	}
	for _, table := range []string{"sys_department", "sys_role", "sys_api", "sys_menu", "sys_menu_api", "sys_role_menu", "sys_casbin_rule", "sys_dict", "sys_dict_item", "gb_sip_security_policy"} {
		var rows int
		require.NoError(t, raw.QueryRow(`SELECT COUNT(*) FROM `+table).Scan(&rows), table)
		require.NotZero(t, rows, table)
	}
	var users, userRoles int
	require.NoError(t, raw.QueryRow(`SELECT COUNT(*) FROM sys_users`).Scan(&users))
	require.NoError(t, raw.QueryRow(`SELECT COUNT(*) FROM sys_user_role`).Scan(&userRoles))
	require.Zero(t, users)
	require.Zero(t, userRoles)

	for _, query := range []string{
		`SELECT COUNT(*) FROM sys_menu WHERE parent_id<>0 AND NOT EXISTS (SELECT 1 FROM sys_menu p WHERE p.id=sys_menu.parent_id)`,
		`SELECT COUNT(*) FROM sys_menu_api ma WHERE NOT EXISTS (SELECT 1 FROM sys_menu m WHERE m.id=ma.menu_id) OR NOT EXISTS (SELECT 1 FROM sys_api a WHERE a.id=ma.api_id)`,
		`SELECT COUNT(*) FROM sys_role_menu rm WHERE NOT EXISTS (SELECT 1 FROM sys_role r WHERE r.id=rm.role_id) OR NOT EXISTS (SELECT 1 FROM sys_menu m WHERE m.id=rm.menu_id)`,
		`SELECT COUNT(*) FROM sys_dict_item di WHERE NOT EXISTS (SELECT 1 FROM sys_dict d WHERE d.id=di.dict_id)`,
	} {
		var dangling int
		require.NoError(t, raw.QueryRow(query).Scan(&dangling))
		require.Zero(t, dangling, query)
	}

	var recoveryIndex int
	require.NoError(t, raw.QueryRow(`SELECT COUNT(*) FROM pragma_index_list('meta_node') WHERE name='idx_recovery_required'`).Scan(&recoveryIndex))
	require.Equal(t, 1, recoveryIndex)

	var autoIncrementDDL string
	require.NoError(t, raw.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='gb_device_grant'`).Scan(&autoIncrementDDL))
	require.Contains(t, autoIncrementDDL, `AUTOINCREMENT`)

	// SQLite's default collation is deliberately BINARY.  The installer and
	// login lock use the original username bytes, so admin and Admin differ.
	var collation string
	require.NoError(t, raw.QueryRow(`SELECT coll FROM pragma_index_xinfo('username') WHERE key=1 LIMIT 1`).Scan(&collation))
	require.Equal(t, "BINARY", collation)
	require.NoError(t, db.Exec(`INSERT INTO sys_users(id,username,password,status) VALUES (1,'admin','test-hash',1),(2,'Admin','test-hash',1),(3,'管理员','test-hash',1),(4,'管理員','test-hash',1)`).Error)
	require.Error(t, db.Exec(`INSERT INTO sys_users(id,username,password,status) VALUES (5,'admin','test-hash',1)`).Error)
	require.Error(t, db.Exec(`INSERT INTO sys_users(id,username,password,status) VALUES (6,?,'test-hash',1)`, strings.Repeat("u", 51)).Error)
	require.NoError(t, db.Exec(`INSERT INTO gb_device(id,device_id) VALUES (200,'device-20-char-12345')`).Error)
	require.Error(t, db.Exec(`INSERT INTO gb_device(id,device_id) VALUES (201,?)`, strings.Repeat("d", 21)).Error)

	// JSON is stored as TEXT, with NULL still accepted and malformed values rejected.
	require.NoError(t, db.Exec(`INSERT INTO sys_jobs(id,"group",name,executor_name,cron_expression) VALUES ('json-null','g','n','e','* * * * *')`).Error)
	require.NoError(t, db.Exec(`INSERT INTO sys_jobs(id,"group",name,executor_name,cron_expression,parameters,created_by) VALUES ('json-valid','g','n','e','* * * * *','{"ok":true}',1)`).Error)
	require.Error(t, db.Exec(`INSERT INTO sys_jobs(id,"group",name,executor_name,cron_expression,parameters,created_by) VALUES ('json-invalid','g','n','e','* * * * *','not-json',1)`).Error)
	// SQLite must not accept a REAL or negative value in a MySQL INT UNSIGNED column.
	require.Error(t, db.Exec(`INSERT INTO sys_jobs(id,"group",name,executor_name,cron_expression,created_by) VALUES ('unsigned-negative','g','n','e','* * * * *',-1)`).Error)
	require.Error(t, db.Exec(`INSERT INTO sys_jobs(id,"group",name,executor_name,cron_expression,created_by) VALUES ('unsigned-real','g','n','e','* * * * *',0.5)`).Error)
	// The source singleton and deployment-mode checks remain enforced.
	require.Error(t, db.Exec(`INSERT INTO gb_sip_config(id,deployment_mode,listen_ip,advertise_ip,port,domain,server_id,password) VALUES (1,'invalid','','',5060,'','','')`).Error)

	var fkCount int
	require.NoError(t, raw.QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_list('sys_job_results')`).Scan(&fkCount))
	require.Equal(t, 1, fkCount)
	require.Error(t, db.Exec(`INSERT INTO sys_job_results(job_id,status,start_time,end_time,duration) VALUES ('missing-job','failed','2026-09-07 00:00:00','2026-09-07 00:00:00',0)`).Error)
}

func TestBaselineRejectsContaminatedOrUnconvertedInputs(t *testing.T) {
	lower := strings.ToLower(SQL)
	for _, forbidden := range []string{
		"drop table", "auto_increment", "engine=", "using btree", "collate ", "_utf8mb4", "now()", "concat(", "@recording_",
		"insert into sys_users", "insert or ignore into sys_users", "insert into sys_user_role", "insert or ignore into sys_user_role",
		"$2a$10$0as9fxwloz/pxiqzsbr7huy.dqdwucyb795qiwca6fsn0lu.gla.c",
	} {
		require.NotContains(t, lower, forbidden)
	}
	// Check both unquoted and quoted identifiers so a contaminated source
	// cannot evade this contract by changing MySQL identifier quoting.
	for _, table := range []string{"sys_users", "sys_user_role", "gb_device", "gb_channel", "meta_node", "sys_operation_logs"} {
		pattern := regexp.MustCompile(`(?is)\binsert\s+(?:or\s+\w+\s+)?into\s+["` + "`" + `]?` + table + `["` + "`" + `]?\b`)
		require.NotRegexp(t, pattern, SQL, table)
	}
	require.NotRegexp(t, regexp.MustCompile(`(?i)\bUNSIGNED\b`), SQL)
}

func TestBaselinePreservesModernSQLiteTemporalReadWrite(t *testing.T) {
	db, raw := openBaselineDB(t)
	value := time.Date(2026, 9, 7, 12, 34, 56, 123456789, time.FixedZone("offset", 8*60*60))
	require.NoError(t, db.Exec(`INSERT INTO sys_users(id,username,password,status,created_at) VALUES (100,'time-contract','test-hash',1,?)`, value).Error)
	var loaded time.Time
	require.NoError(t, raw.QueryRow(`SELECT created_at FROM sys_users WHERE id=100`).Scan(&loaded))
	require.True(t, value.Equal(loaded), "stored=%v loaded=%v", value, loaded)

	var dateType string
	require.NoError(t, raw.QueryRow(`SELECT type FROM pragma_table_info('gb_recording_file') WHERE name='record_date'`).Scan(&dateType))
	require.Equal(t, "DATE", dateType)
}

func TestSecretNonceUsesBlobAffinityAndPreservesBytes(t *testing.T) {
	db, raw := openBaselineDB(t)
	var declaredType string
	require.NoError(t, raw.QueryRow(`SELECT type FROM pragma_table_info('gb_cascade_platform') WHERE name='secret_nonce'`).Scan(&declaredType))
	require.Equal(t, "BLOB", declaredType)

	payload := bytes.Repeat([]byte{0xff}, 64)
	payload[0] = 0x00
	payload[1] = 0x80
	payload[2] = 0xc3
	payload[3] = 0x28
	insert := `INSERT INTO gb_cascade_platform(name,upstream_server_id,upstream_domain,host,port,local_device_id,local_domain,local_sip_ip,local_sip_port,secret_nonce,created_at,updated_at) VALUES ('blob-test','upstream','domain','127.0.0.1',5060,'local-device','local-domain','127.0.0.1',5061,?,'2026-09-07 00:00:00','2026-09-07 00:00:00')`
	require.NoError(t, db.Exec(insert, payload).Error)
	var id int64
	require.NoError(t, raw.QueryRow(`SELECT last_insert_rowid()`).Scan(&id))
	require.NotZero(t, id)
	update := `UPDATE gb_cascade_platform SET secret_nonce=? WHERE id=?`
	var storageClass string
	require.NoError(t, raw.QueryRow(`SELECT typeof(secret_nonce) FROM gb_cascade_platform WHERE id=?`, id).Scan(&storageClass))
	require.Equal(t, "blob", storageClass)
	var roundTripped []byte
	require.NoError(t, raw.QueryRow(`SELECT secret_nonce FROM gb_cascade_platform WHERE id=?`, id).Scan(&roundTripped))
	require.Equal(t, payload, roundTripped)
	err := db.Exec(update, bytes.Repeat([]byte{0xff}, 65), id).Error
	require.Error(t, err)
	require.Contains(t, strings.ToLower(err.Error()), "check")
	var retained []byte
	require.NoError(t, raw.QueryRow(`SELECT secret_nonce FROM gb_cascade_platform WHERE id=?`, id).Scan(&retained))
	require.Equal(t, payload, retained)
}

func TestBaselineHasNoTransactionBoundary(t *testing.T) {
	lower := strings.ToLower(SQL)
	require.NotContains(t, lower, "begin;")
	require.NotContains(t, lower, "commit;")
}
