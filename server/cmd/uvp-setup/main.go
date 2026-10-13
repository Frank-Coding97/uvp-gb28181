// Command uvp-setup 是绿色安装包的「首次运行辅助工具」。
//
// 为什么要有它（而不是继续用包内脚本）：
//   - Linux 绿色包的内嵌 Python（deploy/standalone/uvp-gb28181-ctl.sh 的 DB_INIT_PY）
//     依赖目标机有 python3；**Windows 客户机不能这么假设**。
//   - Linux 版的自签证书用 openssl CLI 生成；Windows 自带没有 openssl。
//
// 两个子命令：
//
//	uvp-setup db-init  --baseline <baseline.sql> --db <uvp.db> [--quiet]
//	uvp-setup certgen  --dir <conf 目录> --zlm-pem <输出.pem> [--cn uvp-local] [--ip 1.2.3.4]... [--force]
//	uvp-setup db-init  --baseline <baseline.sql> --split-digest   # 只打印切分指纹，不碰库
//
// ⛔ db-init 的建库语义必须与 Linux 版**逐条等价**（同一份 baseline.sql、同一种切分、
// 同样「PRAGMA 必须在事务外」、同样「重复索引名被 SQLite 静默跳过要显式报出」、
// 同样的建库后对账）。契约锁在 baseline_test.go：切分指纹与 generate.py 的
// split_sql() 逐字节比对（`stmts=N sha256=…`，与 ctl.sh 的 UVP_DB_SPLIT_DIGEST 同格式）。
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "db-init":
		err = runDBInit(os.Args[2:])
	case "certgen":
		err = runCertgen(os.Args[2:])
	case "ports-sync":
		err = runPortsSync(os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "未知子命令: %s\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err.Error())
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, strings.Join([]string{
		"uvp-setup —— 绿色安装包首次运行辅助工具",
		"",
		"用法:",
		"  uvp-setup db-init --baseline <baseline.sql> --db <uvp.db> [--quiet]",
		"  uvp-setup db-init --baseline <baseline.sql> --split-digest",
		"  uvp-setup certgen --dir <目录> [--zlm-pem <file.pem>] [--cn uvp-local] [--ip A]... [--days 3650] [--force]",
		"  uvp-setup ports-sync --env <config.env> --yml <config.yml> --ini <config.ini> \\",
		"      --http P --redis P --https P --nginx-http P --zlm-http P --zlm-rtp-proxy P [--user-set K1,K2]",
		"",
	}, "\n"))
}

// indent 与 Linux 版 ctl 的 det()/ok() 同宽（"[uvp] " 恰好 6 个字符），
// 这样两个平台的安装输出缩进一致。
const indent = "      "

func note(format string, a ...any) {
	fmt.Fprintf(os.Stdout, indent+"· "+format+"\n", a...)
}

func success(format string, a ...any) {
	fmt.Fprintf(os.Stdout, indent+"✓ "+format+"\n", a...)
}

// resolvePath 把相对路径按当前工作目录补全，并确保父目录存在。
// ⛔ Windows 上双击启动时 cwd 不可预期，调用方一律传绝对路径；这里只做兜底。
func resolvePath(p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", fmt.Errorf("路径为空")
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	return fs
}

// ipList 让 --ip 可以重复出现。
type ipList []string

func (l *ipList) String() string { return strings.Join(*l, ",") }
func (l *ipList) Set(v string) error {
	for _, part := range strings.Split(v, ",") {
		if s := strings.TrimSpace(part); s != "" {
			*l = append(*l, s)
		}
	}
	return nil
}
