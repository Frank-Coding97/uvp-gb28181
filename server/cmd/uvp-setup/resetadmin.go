package main

// reset-admin —— 把某个账号的口令**还原成出厂初始口令**，并重新标记「首次登录必须改密」。
//
// 为什么需要它（2026-10-15）：
//
//	绿色包装好、管理员在首次登录时把口令改成了自己的、然后忘了 —— 平台上**没有任何
//	自助找回入口**：登录页的「忘记密码」是注释掉的死链，后端也只有
//	`PUT /users/changeInitialPassword`（前提是账号仍处于「待改密」状态）。
//	⇒ 唯一出路是改库。原先只能让客户在目标机上自己跑一小段 Python：
//	  · Linux 包本来就有 python3（建库就是它跑的），照文档抄一段还行；
//	  · ⛔ Windows 客户机**不保证有 python3，包里也不带 sqlite3.exe** ⇒ 现场无解，
//	    只能让客户把 data\uvp.db（含全部业务数据）发回来，或者远程连进去手改。
//	⇒ 交付一个**离线、免依赖**的子命令：Windows 包直接调 bin\uvp-setup.exe。
//	  （Linux 包没有 uvp-setup，对应入口是 ctl.sh 的 `reset-password`，
//	  语义与本命令逐条一致 —— 见 deploy/standalone/uvp-gb28181-ctl.sh。）
//
// ⛔⛔ 语义必须与「出厂状态」逐字对齐，否则重置完客户还是登不进去：
//
//	① 口令取 config.yml 的 `server.demoaccount.defaultpassword`。出厂就是它 ——
//	   seed 里 admin 的 bcrypt 哈希就是该口令的哈希，契约锁在
//	   server/resource/database/password_bootstrap_contract_test.go。
//	② `must_change_password` 必须置 1。这是全新部署的契约（同上测试），
//	   也是唯一能让客户"重置完还得自己再设一个"的机制。
//	③ 前端 `/api/config/get` 在 must_change_password=1 时会把**配置里那个值**
//	   作为登录页提示回显 ⇒ 「重置成配置里的口令」是唯一自洽的选择。
//	   用 --password 指定别的口令时必须显式提醒：页面提示会对不上。
//
// ⛔ 刻意**不做**的三件事：
//   - 不重置其它账号、不动 `status`（把停用账号改成启用是另一件事，别混进来）；
//   - 不改 config.yml：它里面那个值就是"出厂口令"，改了等于改交付契约；
//   - 不删库、不重建库：那会连带把客户的设备/录像数据一起清掉。

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	_ "github.com/glebarez/sqlite" // 与后端同一个纯 Go SQLite 驱动，保证读写同一份文件
	"github.com/go-redis/redis/v8"
	"golang.org/x/crypto/bcrypt"
)

// resetAdminOptions 是 reset-admin 的全部入参（解析完、补全默认值之后）。
type resetAdminOptions struct {
	dbPath     string
	configPath string
	username   string
	password   string
	passwordBy string // 口令来源说明，直接进输出，供现场核对
	forceOff   bool   // 不强制首次改密（--no-force-change）
	noUnlock   bool   // 不去清 Redis 锁定键（--no-unlock）
}

// runResetAdmin 解析参数并执行重置。
//
//	uvp-setup reset-admin [--root <包根>] [--db <uvp.db>] [--config <config.yml>]
//	                      [--username admin] [--password <明文>]
//	                      [--no-force-change] [--no-unlock]
func runResetAdmin(args []string) error {
	fs := newFlagSet("reset-admin")
	root := fs.String("root", ".", "包根目录（默认当前目录；库与配置都在它下面）")
	dbPath := fs.String("db", "", "SQLite 库路径（默认 <root>/data/uvp.db）")
	cfgPath := fs.String("config", "", "config.yml 路径（默认 <root>/config/config.yml）")
	username := fs.String("username", "admin", "要重置口令的账号名")
	password := fs.String("password", "", "新口令（留空=取 config.yml 的 server.demoaccount.defaultpassword）")
	forceOff := fs.Bool("no-force-change", false, "不要求下次登录改密（默认强制改密）")
	noUnlock := fs.Bool("no-unlock", false, "不去清 Redis 里的登录失败/锁定键")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// ⛔ 三个路径都以「包根」为基准推默认值：客户是从包目录里执行脚本，
	//   但 Windows 双击 .cmd 时 cwd 未必是包根 —— 所以 --root 可以显式给，
	//   ctl.ps1 会传绝对路径进来。
	absRoot, err := resolvePath(*root)
	if err != nil {
		return fmt.Errorf("解析 --root 失败: %w", err)
	}
	opts := resetAdminOptions{
		dbPath:     strings.TrimSpace(*dbPath),
		configPath: strings.TrimSpace(*cfgPath),
		username:   strings.TrimSpace(*username),
		password:   *password,
		forceOff:   *forceOff,
		noUnlock:   *noUnlock,
	}
	if opts.dbPath == "" {
		opts.dbPath = filepath.Join(absRoot, "data", "uvp.db")
	}
	if opts.configPath == "" {
		opts.configPath = filepath.Join(absRoot, "config", "config.yml")
	}
	if opts.dbPath, err = resolvePath(opts.dbPath); err != nil {
		return fmt.Errorf("解析 --db 失败: %w", err)
	}
	if opts.configPath, err = resolvePath(opts.configPath); err != nil {
		return fmt.Errorf("解析 --config 失败: %w", err)
	}
	if opts.username == "" {
		return errors.New("--username 不能为空")
	}

	// 口令来源三选一：--password > config.yml。都没有就**报错退出**，
	// ⛔ 绝不回落某个写死的口令 —— 那会让"重置成了什么"变成不可知。
	if opts.password == "" {
		value, ok, err := readConfigString(opts.configPath, "demoaccount", "defaultpassword")
		if err != nil {
			return err
		}
		if !ok || value == "" {
			return fmt.Errorf("没在 %s 里读到 server.demoaccount.defaultpassword，请显式指定 --password",
				opts.configPath)
		}
		opts.password = value
		opts.passwordBy = "config.yml 的 server.demoaccount.defaultpassword"
	} else {
		opts.passwordBy = "--password 参数"
	}
	return executeResetAdmin(opts)
}

func executeResetAdmin(opts resetAdminOptions) error {
	if _, err := os.Stat(opts.dbPath); err != nil {
		return fmt.Errorf("找不到数据库 %s —— 确认 --root 指向的是包根，或这台机器还没执行过 start", opts.dbPath)
	}

	// ⛔ 只读地先开一次，确认「这确实是本平台的库」再动手：把 sys_users 写错的代价是
	//   客户整台机器的登录全废，宁可多检查一步。
	db, err := sql.Open("sqlite", sqliteDSN(opts.dbPath))
	if err != nil {
		return fmt.Errorf("打开数据库失败: %w", err)
	}
	defer func() { _ = db.Close() }()
	// PRAGMA 是**连接级**状态，池里换一条连接就等于没设 ⇒ 钉成 1 条连接。
	db.SetMaxOpenConns(1)

	if ok, err := tableExists(db, "sys_users"); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("%s 里没有 sys_users 表 —— 这不是本平台的数据库", opts.dbPath)
	}

	ctx := context.Background()
	if err := ensureMustChangePasswordColumn(ctx, db); err != nil {
		return err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(opts.password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("生成口令哈希失败: %w", err)
	}

	res, err := db.ExecContext(ctx,
		`UPDATE "sys_users" SET "password" = ?, "must_change_password" = ? WHERE "username" = ?`,
		string(hash), boolToInt(!opts.forceOff), opts.username)
	if err != nil {
		return fmt.Errorf("重置口令失败: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("重置口令失败（读不到影响行数）: %w", err)
	}
	if affected == 0 {
		names, _ := listUsernames(ctx, db)
		return fmt.Errorf("库里没有账号 %q（现有账号：%s）", opts.username, strings.Join(names, ", "))
	}

	// 落盘后回读校验：光看 UPDATE 成功不够 —— 列名写错/被触发器改写都会"成功但没生效"，
	// 而症状是"重置了还是登不进去"，现场只会怀疑是别的原因。
	var stored string
	var mustChange int
	if err := db.QueryRowContext(ctx,
		`SELECT "password", "must_change_password" FROM "sys_users" WHERE "username" = ?`, opts.username).
		Scan(&stored, &mustChange); err != nil {
		return fmt.Errorf("回读校验失败: %w", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(stored), []byte(opts.password)); err != nil {
		return errors.New("回读校验失败：库里的口令哈希与新口令对不上（本次写入未生效）")
	}

	// ⛔ 必须把**口令明文**打出来：现场拿到的就是这一行，看不到值就还得去翻
	//   config.yml（而 Windows 客户机那台机器上未必有人会看 YAML）。
	success("账号 %s 的口令已重置：%s", opts.username, opts.password)
	note("口令来源：%s", opts.passwordBy)
	if opts.forceOff {
		warn("已按 --no-force-change 处理：下次登录**不会**被要求改密")
	} else {
		note("已标记「首次登录必须改密」：用上面这个口令登录后会立刻要求设新口令")
	}
	if opts.passwordBy == "--password 参数" {
		// 出厂口令与配置值不一致时，登录页的提示仍是配置里那个值 —— 必须说清楚，
		// 否则现场会认为"提示的口令登不进去=重置没成功"。
		warn("登录页提示用的是 config.yml 里的出厂口令，与本次设置的可能是两回事")
	}

	if !opts.noUnlock {
		unlockLoginLock(opts)
	}
	note("改完即可直接登录（无需重启）；仍登不上就重启平台后再试")
	return nil
}

// ensureMustChangePasswordColumn 兼容 2026-10-08 之前出的老包：那时库表里还没有这一列。
//
// ⛔ DDL 必须与平台侧迁移**逐字一致**
// （resource/database/gb28181/migrations/2026-10-08-initial-admin-password-change-sqlite.sql）：
// 同一列由两个地方建，类型/默认值/CHECK 只差一个字就会出现"两个包建出的库不一样"。
func ensureMustChangePasswordColumn(ctx context.Context, db *sql.DB) error {
	ok, err := columnExists(db, "sys_users", "must_change_password")
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	const ddl = `ALTER TABLE "sys_users"
  ADD COLUMN "must_change_password" INTEGER NOT NULL DEFAULT 0
  CHECK ("must_change_password" IN (0, 1))`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("补建 must_change_password 列失败（老版本库）: %w", err)
	}
	warn("检测到老版本库（sys_users 缺 must_change_password 列），已按平台同款 DDL 补建")
	return nil
}

// unlockLoginLock 尽力清除 Redis 里的登录锁定键。
//
// 为什么值得做：连错 3 次（safe.loginlockthreshold）会锁 600 秒，而现场"重置完登不进去"
// 的第一嫌疑就是它 —— 这一步省掉一次来回。
// ⛔ 全程 **best-effort**：Redis 没起、端口不通、口令不对都不算失败（改库已经成功了），
//
//	只打一行提示把手工命令给出去。锁键在 config.yml 的 redis.indexdb（默认 1 号库，
//	不是 0），且**不带** token.cachekeyprefix 前缀 —— 见 controllers/auth.go
//	里 `"account_locked:" + req.Username` 的拼法。
func unlockLoginLock(opts resetAdminOptions) {
	// config.yml 读不到时**不报错**：重置口令这件事已经成功了，锁定键清不掉只该是提示。
	host, _, _ := readConfigString(opts.configPath, "redis", "host")
	port, _, _ := readConfigString(opts.configPath, "redis", "port")
	password, _, _ := readConfigString(opts.configPath, "redis", "password")
	indexRaw, _, _ := readConfigString(opts.configPath, "redis", "indexdb")
	if host == "" {
		host = "127.0.0.1"
	}
	if port == "" {
		port = "6379"
	}
	index, err := strconv.Atoi(strings.TrimSpace(indexRaw))
	if err != nil {
		index = 1
	}
	addr := host + ":" + port
	cli := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       index,
		// 客户机上 Redis 可能没起：超时必须短，不能让人以为工具卡死了。
		DialTimeout:  800 * time.Millisecond,
		ReadTimeout:  800 * time.Millisecond,
		WriteTimeout: 800 * time.Millisecond,
	})
	defer func() { _ = cli.Close() }()

	keys := []string{"account_locked:" + opts.username, "login_fail_count:" + opts.username}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	removed, err := cli.Del(ctx, keys...).Result()
	if err != nil {
		warn("未能连接 Redis（%s），登录锁定键请手工清：redis-cli -n %d del %s",
			addr, index, strings.Join(keys, " "))
		return
	}
	if removed > 0 {
		note("已清除 Redis %d 号库的登录锁定键（%d 个）", index, removed)
	} else {
		note("Redis %d 号库里本来就没有锁定键（账号未被锁）", index)
	}
}

// ---- 数据库小工具 ----------------------------------------------------------

func tableExists(db *sql.DB, name string) (bool, error) {
	var n int
	err := db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("检查表 %s 失败: %w", name, err)
	}
	return n > 0, nil
}

func columnExists(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.QueryContext(context.Background(), fmt.Sprintf(`PRAGMA table_info("%s")`, table))
	if err != nil {
		return false, fmt.Errorf("读取 %s 表结构失败: %w", table, err)
	}
	defer func() { _ = rows.Close() }()
	cols, err := rows.Columns()
	if err != nil {
		return false, err
	}
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return false, err
		}
		for i, c := range cols {
			if strings.EqualFold(c, "name") {
				if got, ok := vals[i].(string); ok && got == column {
					return true, nil
				}
			}
		}
	}
	return false, rows.Err()
}

func listUsernames(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT "username" FROM "sys_users" ORDER BY "id" LIMIT 20`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return out, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// ---- config.yml 读取 -------------------------------------------------------
//
// ⛔ 只读**标量**、且必须**限定在段落内**：config.yml 里 `port` 这类键在
// httpserver/redis/zlm/gormv2 多段下重名，全局第一个命中很可能不是你要的那个。
// 这里不复用 portsync.go 的 replaceYAMLScalar：那个是"写"，语义（找不到就报错、保留缩进）
// 与"读"不同，硬凑到一起只会让两边的注释都说不清。

var yamlScalarRe = regexp.MustCompile(`^(\s*)([A-Za-z_][\w-]*)\s*:\s*(.*?)\s*$`)

// readConfigString 读 config.yml 里 section（可任意缩进）下 key 的标量值。
// 找不到段落或键时返回 ok=false（**不报错**），只有文件本身读不了才报错。
func readConfigString(path, section, key string) (string, bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", false, fmt.Errorf("读取 config.yml 失败（%s）: %w", path, err)
	}
	lines := splitLines(string(raw), detectEOL(string(raw)))
	sectionIndent, start := -1, -1
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if m := yamlScalarRe.FindStringSubmatch(line); m != nil && m[2] == section &&
			strings.TrimSpace(cutYAMLComment(m[3])) == "" {
			start, sectionIndent = i, len(m[1])
			break
		}
	}
	if start < 0 {
		return "", false, nil
	}
	for i := start + 1; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		cur := leadingSpace(lines[i])
		if cur <= sectionIndent {
			return "", false, nil // 走出段落了
		}
		m := yamlScalarRe.FindStringSubmatch(lines[i])
		if m == nil || m[2] != key {
			continue
		}
		return stripQuotes(cutYAMLComment(m[3])), true, nil
	}
	return "", false, nil
}

// cutYAMLComment 砍掉**引号外**的行尾注释：`"123456"  # 登录页默认密码` → `"123456"  `。
// ⛔ 不能简单 Split("#")：口令本身可能含 #（客户把出厂口令改成 P#ssw0rd 是合法的）。
func cutYAMLComment(value string) string {
	var quote byte
	for i := 0; i < len(value); i++ {
		c := value[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			quote = c
		case '#':
			return value[:i]
		}
	}
	return value
}

func stripQuotes(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}
