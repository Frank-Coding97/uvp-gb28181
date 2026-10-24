package main

import (
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"golang.org/x/crypto/bcrypt"
)

// 这些用例锁的是 reset-admin 的**交付语义**（现场"重置完还是登不进去"都出在这里）：
//
//	① 口令写进去后必须能用新口令**比对通过**（不是"UPDATE 没报错"就算成功）；
//	② `must_change_password` 必须置 1 —— 出厂契约，也是客户改回自己口令的唯一入口；
//	③ 其它账号、其它列**一行都不能动**；
//	④ 口令来源：--password 优先，其次 config.yml 的 server.demoaccount.defaultpassword；
//	   两者都没有必须**报错**（绝不偷偷回落一个写死的口令）；
//	⑤ 老版本库（缺 must_change_password 列）要能补齐再用，而不是报 no such column；
//	⑥ 认错库（没有 sys_users）必须拒绝，不能把别人的 SQLite 改坏。

const testUsersDDL = `CREATE TABLE "sys_users" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "username" TEXT NOT NULL,
  "password" TEXT NOT NULL DEFAULT '',
  "status" INTEGER NOT NULL DEFAULT 1,
  "must_change_password" INTEGER NOT NULL DEFAULT 0
);`

// 2026-10-08 之前出的包建出来的库：没有 must_change_password 列。
const legacyUsersDDL = `CREATE TABLE "sys_users" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "username" TEXT NOT NULL,
  "password" TEXT NOT NULL DEFAULT '',
  "status" INTEGER NOT NULL DEFAULT 1
);`

const testConfig = `server:
  systemname: 测试平台
  demoaccount:
    enabled: false
    defaultusername: "admin"   # 登录页默认账号
    defaultpassword: "123456"  # 登录页默认密码
redis:
  host: "127.0.0.1"
  port: 6379
  password: ""
  indexdb: 1
`

func mustHash(t *testing.T, password string) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("生成测试哈希失败: %v", err)
	}
	return string(h)
}

// newInstall 造一个"包根"：data/uvp.db + config/config.yml + resource/version.json。
// 返回 (root, dbPath)。
func newInstall(t *testing.T, ddl, config string, rows ...string) (string, string) {
	t.Helper()
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	confDir := filepath.Join(root, "config")
	for _, d := range []string{dataDir, confDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("建目录失败: %v", err)
		}
	}
	dbPath := filepath.Join(dataDir, "uvp.db")
	db, err := sql.Open("sqlite", sqliteDSN(dbPath))
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	if _, err := db.Exec(ddl); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	for _, row := range rows {
		if _, err := db.Exec(row); err != nil {
			t.Fatalf("写入测试数据失败（%s）: %v", row, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("关闭测试库失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(confDir, "config.yml"), []byte(config), 0o644); err != nil {
		t.Fatalf("写 config.yml 失败: %v", err)
	}
	return root, dbPath
}

// readUser 用**新连接**回读（别复用被测代码的连接，那会把连接级 PRAGMA 的坑带进来）。
func readUser(t *testing.T, dbPath, username string) (hash string, mustChange int, status int) {
	t.Helper()
	db, err := sql.Open("sqlite", sqliteDSN(dbPath))
	if err != nil {
		t.Fatalf("回读打开库失败: %v", err)
	}
	defer func() { _ = db.Close() }()
	err = db.QueryRow(`SELECT "password", "must_change_password", "status" FROM "sys_users" WHERE "username" = ?`,
		username).Scan(&hash, &mustChange, &status)
	if err != nil {
		t.Fatalf("回读 %s 失败: %v（是不是被删了/列没建出来？）", username, err)
	}
	return hash, mustChange, status
}

func TestResetAdminUsesConfigDefaultPassword(t *testing.T) {
	root, dbPath := newInstall(t, testUsersDDL, testConfig,
		`INSERT INTO "sys_users" ("username","password","must_change_password") VALUES ('admin','`+mustHash(t, "OldPwd#1")+`',0)`,
		`INSERT INTO "sys_users" ("username","password","must_change_password") VALUES ('operator','`+mustHash(t, "KeepMe")+`',0)`,
	)
	otherBefore, _, _ := readUser(t, dbPath, "operator")

	// 只给 --root：库与配置都按包根推默认值（ctl 脚本就是这么调的）。
	if err := runResetAdmin([]string{"--root", root, "--no-unlock"}); err != nil {
		t.Fatalf("reset-admin 应成功，实际报错: %v", err)
	}

	hash, mustChange, status := readUser(t, dbPath, "admin")
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte("123456")); err != nil {
		t.Errorf("admin 的口令不是 config.yml 里的出厂口令 123456: %v", err)
	}
	if mustChange != 1 {
		t.Errorf("must_change_password = %d，期望 1（出厂契约：登录后强制改密）", mustChange)
	}
	if status != 1 {
		t.Errorf("status = %d，期望 1 —— 重置口令不得顺手改启用状态", status)
	}
	otherAfter, otherMust, _ := readUser(t, dbPath, "operator")
	if otherAfter != otherBefore {
		t.Error("operator 的口令被连带改了 —— 本命令只应动 --username 指定的账号")
	}
	if otherMust != 0 {
		t.Error("operator 的 must_change_password 被连带改了")
	}
}

// ⛔ 行尾注释、引号、以及"另一个段里同名键"三件事一起测：配置读错值的表现是
// 「重置成了一个谁也猜不到的口令」，只能靠这个用例拦住。
func TestResetAdminConfigValueIsSectionScopedAndUnquoted(t *testing.T) {
	cfg := `media:
  defaultpassword: "DECOY-不应被采用"
server:
  demoaccount:
    defaultusername: "admin"
    defaultpassword: "From-Config"   # 行尾注释不能算进值里
redis:
  host: "127.0.0.1"
  port: 6379
  indexdb: 1
`
	root, dbPath := newInstall(t, testUsersDDL, cfg,
		`INSERT INTO "sys_users" ("username","password") VALUES ('admin','`+mustHash(t, "OldPwd#1")+`')`,
	)
	if err := runResetAdmin([]string{"--root", root, "--no-unlock"}); err != nil {
		t.Fatalf("reset-admin 应成功，实际报错: %v", err)
	}
	hash, _, _ := readUser(t, dbPath, "admin")
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte("From-Config")); err != nil {
		t.Errorf("口令不是 demoaccount.defaultpassword 的值（可能取到了别的段或吞了行尾注释）: %v", err)
	}
}

func TestResetAdminExplicitPasswordWins(t *testing.T) {
	root, dbPath := newInstall(t, testUsersDDL, testConfig,
		`INSERT INTO "sys_users" ("username","password") VALUES ('admin','`+mustHash(t, "OldPwd#1")+`')`,
	)
	if err := runResetAdmin([]string{"--root", root, "--password", "P#ss-w0rd", "--no-unlock"}); err != nil {
		t.Fatalf("reset-admin 应成功，实际报错: %v", err)
	}
	hash, _, _ := readUser(t, dbPath, "admin")
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte("P#ss-w0rd")); err != nil {
		t.Errorf("--password 未被采用: %v", err)
	}
}

func TestResetAdminRejectsWhenNoPasswordSource(t *testing.T) {
	// config.yml 里没有 demoaccount 段，也没给 --password ⇒ 必须报错，且库不变。
	root, dbPath := newInstall(t, testUsersDDL, "server:\n  systemname: 测试\n",
		`INSERT INTO "sys_users" ("username","password") VALUES ('admin','`+mustHash(t, "OldPwd#1")+`')`,
	)
	before, _, _ := readUser(t, dbPath, "admin")

	err := runResetAdmin([]string{"--root", root, "--no-unlock"})
	if err == nil {
		t.Fatal("没有口令来源时必须报错（不能偷偷回落写死的口令）")
	}
	if !strings.Contains(err.Error(), "defaultpassword") {
		t.Errorf("报错没告诉运维怎么修（应提到 defaultpassword / --password）: %v", err)
	}
	if after, _, _ := readUser(t, dbPath, "admin"); after != before {
		t.Error("报错的路径居然改了库")
	}
}

func TestResetAdminUnknownUserFailsWithoutTouchingOthers(t *testing.T) {
	root, dbPath := newInstall(t, testUsersDDL, testConfig,
		`INSERT INTO "sys_users" ("username","password") VALUES ('admin','`+mustHash(t, "OldPwd#1")+`')`,
	)
	before, _, _ := readUser(t, dbPath, "admin")

	err := runResetAdmin([]string{"--root", root, "--username", "ghost", "--no-unlock"})
	if err == nil {
		t.Fatal("账号不存在时必须报错")
	}
	if !strings.Contains(err.Error(), "ghost") || !strings.Contains(err.Error(), "admin") {
		t.Errorf("报错应同时给出「要改的账号」和「现有账号」两个信息，实际: %v", err)
	}
	if after, _, _ := readUser(t, dbPath, "admin"); after != before {
		t.Error("账号名写错时不能顺手把别的账号重置了")
	}
}

func TestResetAdminAddsMustChangeColumnOnLegacyDB(t *testing.T) {
	root, dbPath := newInstall(t, legacyUsersDDL, testConfig,
		`INSERT INTO "sys_users" ("username","password") VALUES ('admin','`+mustHash(t, "OldPwd#1")+`')`,
	)
	if err := runResetAdmin([]string{"--root", root, "--no-unlock"}); err != nil {
		t.Fatalf("老版本库（缺 must_change_password 列）应当能补齐后再重置，实际报错: %v", err)
	}
	hash, mustChange, _ := readUser(t, dbPath, "admin")
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte("123456")); err != nil {
		t.Errorf("老库上的口令没重置成功: %v", err)
	}
	if mustChange != 1 {
		t.Errorf("补出来的列没被置 1，must_change_password = %d", mustChange)
	}
}

func TestResetAdminRejectsForeignDatabase(t *testing.T) {
	root, dbPath := newInstall(t, `CREATE TABLE "something_else" ("id" INTEGER PRIMARY KEY);`, testConfig)
	err := runResetAdmin([]string{"--root", root, "--no-unlock"})
	if err == nil {
		t.Fatal("没有 sys_users 表时必须拒绝，别把别人的库改了")
	}
	if !strings.Contains(err.Error(), "sys_users") {
		t.Errorf("报错没说清是「这个库不是本平台的」，实际: %v", err)
	}
	if _, statErr := os.Stat(dbPath); statErr != nil {
		t.Errorf("拒绝的路径不应删库: %v", statErr)
	}
}

func TestResetAdminMissingDatabaseFails(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config", "config.yml"), []byte(testConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	err := runResetAdmin([]string{"--root", root, "--no-unlock"})
	if err == nil {
		t.Fatal("库不存在时必须报错")
	}
	if !strings.Contains(err.Error(), "uvp.db") {
		t.Errorf("报错应指出期望的库路径，实际: %v", err)
	}
}

// 锁定键必须被清掉：现场"重置完登不进去"的头号原因就是它（连错 3 次锁 600 秒）。
func TestResetAdminClearsRedisLoginLock(t *testing.T) {
	mr := miniredis.RunT(t)
	mr.DB(1).Set("account_locked:admin", "1")
	mr.DB(1).Set("login_fail_count:admin", "3")
	// 另一个账号的锁不能被连带清掉。
	mr.DB(1).Set("account_locked:operator", "1")

	cfg := "server:\n  demoaccount:\n    defaultpassword: \"123456\"\n" +
		"redis:\n  host: \"127.0.0.1\"\n  port: " + mr.Port() + "\n  password: \"\"\n  indexdb: 1\n"
	root, dbPath := newInstall(t, testUsersDDL, cfg,
		`INSERT INTO "sys_users" ("username","password") VALUES ('admin','`+mustHash(t, "OldPwd#1")+`')`,
	)
	if err := runResetAdmin([]string{"--root", root}); err != nil {
		t.Fatalf("reset-admin 应成功，实际报错: %v", err)
	}
	if hash, _, _ := readUser(t, dbPath, "admin"); bcrypt.CompareHashAndPassword([]byte(hash), []byte("123456")) != nil {
		t.Error("口令没重置成功")
	}
	if mr.DB(1).Exists("account_locked:admin") || mr.DB(1).Exists("login_fail_count:admin") {
		t.Error("admin 的登录锁定键没被清掉（客户会看到「账户已被锁定」，以为重置失败）")
	}
	if !mr.DB(1).Exists("account_locked:operator") {
		t.Error("把别人的锁定键也清了")
	}
	// 0 号库不该被动过：平台的缓存库是 indexdb 指定的那个，不是 0。
	if mr.DB(0).Exists("account_locked:admin") {
		t.Error("清到了 0 号库")
	}
}

// Redis 连不上**不算失败**：口令已经改成功了，清锁只是顺手。
func TestResetAdminRedisUnavailableStillSucceeds(t *testing.T) {
	// 这个端口上没有监听者（用 1 端口，普通权限下必然连不上）。
	cfg := "server:\n  demoaccount:\n    defaultpassword: \"123456\"\n" +
		"redis:\n  host: \"127.0.0.1\"\n  port: 1\n  indexdb: 1\n"
	root, dbPath := newInstall(t, testUsersDDL, cfg,
		`INSERT INTO "sys_users" ("username","password") VALUES ('admin','`+mustHash(t, "OldPwd#1")+`')`,
	)
	if err := runResetAdmin([]string{"--root", root}); err != nil {
		t.Fatalf("Redis 连不上时不应报错（只该提示），实际: %v", err)
	}
	hash, _, _ := readUser(t, dbPath, "admin")
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte("123456")) != nil {
		t.Error("Redis 连不上时口令没重置成功")
	}
}

func TestResetAdminNoForceChangeKeepsFlagOff(t *testing.T) {
	root, dbPath := newInstall(t, testUsersDDL, testConfig,
		`INSERT INTO "sys_users" ("username","password") VALUES ('admin','`+mustHash(t, "OldPwd#1")+`')`,
	)
	if err := runResetAdmin([]string{"--root", root, "--no-force-change", "--no-unlock"}); err != nil {
		t.Fatalf("reset-admin 应成功，实际报错: %v", err)
	}
	if _, mustChange, _ := readUser(t, dbPath, "admin"); mustChange != 0 {
		t.Errorf("--no-force-change 下 must_change_password 应为 0，实际 %d", mustChange)
	}
}

// 现场拿到的就是这一行输出：必须把**口令明文**打在终端上。
// ⛔ 曾经只打了"口令来源"（config.yml 的某个键）而没打值 —— 客户得自己去翻 YAML，
//
//	而 Windows 客户机上未必有人会看 YAML；等于命令跑成功但没人知道拿什么登录。
func TestResetAdminPrintsTheNewPassword(t *testing.T) {
	root, _ := newInstall(t, testUsersDDL, testConfig,
		`INSERT INTO "sys_users" ("username","password") VALUES ('admin','`+mustHash(t, "OldPwd#1")+`')`,
	)
	out := captureStdout(t, func() {
		if err := runResetAdmin([]string{"--root", root, "--no-unlock"}); err != nil {
			t.Errorf("reset-admin 应成功，实际报错: %v", err)
		}
	})
	if !strings.Contains(out, "123456") {
		t.Errorf("输出里没有新口令明文，现场不知道该拿什么登录：\n%s", out)
	}
	if !strings.Contains(out, "admin") {
		t.Errorf("输出里没写清是哪个账号被重置：\n%s", out)
	}
}

// captureStdout 把 fn 期间写到 os.Stdout 的内容收回来。
// ⛔ 必须**先关写端再读**，否则 io.ReadAll 会一直等（管道没关就永远读不到 EOF）。
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("建管道失败: %v", err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	_ = w.Close()
	os.Stdout = old
	return <-done
}
