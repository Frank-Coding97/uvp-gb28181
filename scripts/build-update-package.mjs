#!/usr/bin/env node
/**
 * 增量升级包（update package）生成器。
 *
 * 输入两个已解包的完整发行目录（旧版 / 新版），输出一个只含**变化文件**的升级包：
 *
 *   Linux ：uvp-gb28181-update-<平台>-<旧>-to-<新>.tar.gz
 *   Windows：uvp-gb28181-update-<平台>-<旧>-to-<新>.zip
 *   └── uvp-gb28181-update-<平台>-<旧>-to-<新>/
 *       ├── apply.sh / apply.ps1 + apply.cmd   目标机上执行：校验 → 停服 → 备份 → 落文件 → 删旧 → 起服
 *       ├── manifest.json   人读的清单（版本、逐文件 sha256、动作）
 *       ├── files.sha256    sha256sum -c / Get-FileHash 消费的清单
 *       ├── deletes.txt     需要在目标机删除的旧文件
 *       └── payload/        与安装根同构的新文件
 *
 * ⛔⛔ 绝不打包实例状态：`config/`（含现场改过的 config.yml、随机 JWT 密钥）、`data/`
 *    （数据库）、`logs/`、`run/`。覆盖它们等于把现场机换成一个新环境 —— 已登录会话全失效、
 *    SIP 追踪密文再也解不开。这些目录的内容变化会被丢弃，只在报告里点名。
 *
 * ⛔⛔ `resource/public/uploads/`、`gb-channel-snapshot/` 是**现场运行时数据**，既不覆盖
 *    也不删除（见 PROTECTED_PREFIXES）。
 *
 * ⛔ 升级助手（updater/uvp-updater[.exe]）在目标机上**正在运行**，就地覆盖只会换掉 inode
 *    （Linux：旧进程继续跑旧代码）/ 撞文件锁（Windows：sharing violation）。所以 payload
 *    里直接落成 `.new` 旁挂，交给控制脚本在下次启动时替换。
 */
import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import {
  chmodSync,
  copyFileSync,
  existsSync,
  mkdirSync,
  readFileSync,
  readdirSync,
  rmSync,
  statSync,
  writeFileSync
} from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { deflateRawSync } from "node:zlib";

/** 目标机上的实例状态目录：一律不进增量包。 */
export const EXCLUDED_TOP_DIRS = Object.freeze(["config", "data", "logs", "run"]);

/** 打包时忽略的噪音文件。 */
export const IGNORED_NAMES = Object.freeze([".DS_Store"]);

/**
 * ⛔⛔ 现场运行时数据：两个版本之间"少了一个"也**绝不能删**。
 *
 * 这些路径躺在 resource/public/ 里，看起来像随包资源，实际是平台跑起来以后写进去的
 * 用户数据（通道抓拍图、行业上传件）。发行包偶尔会把开发机上的这类目录一起打进去
 * （2026-11-05 的 Windows 1.1.16 包就带了 36 个），逐文件对比时它们会表现为
 * "新版本没有了 ⇒ 删除"，照单执行就是删客户数据。
 */
export const PROTECTED_PREFIXES = Object.freeze([
  "resource/public/uploads/",
  "resource/public/gb-channel-snapshot/"
]);

/** 同上，按文件名模式识别：Python 字节码缓存之类的构建残留。 */
export const PROTECTED_PATTERNS = Object.freeze([/(^|\/)__pycache__(\/|$)/, /\.pyc$/i]);

/**
 * 构建期渲染产物，不随包下发。
 *
 * `vendor/nginx/conf/nginx.conf` 里的路径是**构建机**的临时目录，现场每次启动都会由
 * ctl 从 `nginx.conf.template` 重新渲染。覆盖它没有任何收益，反而在渲染失败时
 * 把一份指向别人机器的配置留在现场。
 */
export const SKIPPED_PATHS = Object.freeze(["vendor/nginx/conf/nginx.conf"]);

/** 助手镜像在安装根里的相对路径（与 server/internal/upgrade/helper_update.go 一致）。 */
export const HELPER_IMAGES = Object.freeze(["updater/uvp-updater", "updater/uvp-updater.exe"]);

const APPLY_TEMPLATE = `#!/bin/sh
# UVP-GB28181 增量升级包
#
# 用法：
#   ./apply.sh -r <安装目录>    升级指定安装根（目录里应有 uvp-gb28181-ctl.sh）
#   ./apply.sh                  从当前目录向上自动探测安装根
#   ./apply.sh -n               演练：只校验 + 打印，不动任何文件
#
# 顺序固定，任何一步失败即中止；文件改动之前一定先做备份。
set -eu

FROM_VERSION="__FROM_VERSION__"
TO_VERSION="__TO_VERSION__"

HERE=\$(cd "\$(dirname "\$0")" && pwd)
ROOT=""
DRY_RUN=0

say() { printf '%s\\n' "\$*"; }
die() { printf '[失败] %s\\n' "\$*" >&2; exit 1; }

while [ \$# -gt 0 ]; do
  case "\$1" in
    -r|--root) [ \$# -ge 2 ] || die "-r 缺少目录参数"; ROOT="\$2"; shift 2 ;;
    -n|--dry-run) DRY_RUN=1; shift ;;
    -h|--help) sed -n '2,10p' "\$0"; exit 0 ;;
    *) die "未知参数 \$1" ;;
  esac
done

[ -f "\$HERE/manifest.json" ] || die "包不完整：缺少 manifest.json"
[ -d "\$HERE/payload" ] || die "包不完整：缺少 payload/"

# ---- 1 定位安装根与程序目录 -------------------------------------------------
if [ -z "\$ROOT" ]; then
  probe="\$HERE"
  while [ "\$probe" != "/" ]; do
    probe=\$(dirname "\$probe")
    if [ -f "\$probe/uvp-gb28181-ctl.sh" ]; then ROOT="\$probe"; break; fi
  done
fi
[ -n "\$ROOT" ] || die "没找到安装根，请用 -r <安装目录> 指定（目录里应有 uvp-gb28181-ctl.sh）"
ROOT=\$(cd "\$ROOT" && pwd)
[ -f "\$ROOT/uvp-gb28181-ctl.sh" ] || die "\$ROOT 不是安装根：缺少 uvp-gb28181-ctl.sh"

# 升级过的安装根有 current 软链指向 releases/<task_id>：程序与随包依赖从那边读，
# config/data/logs/run 仍固定在安装根。版本号也在程序目录里。
ACTIVE="\$ROOT"
if [ -L "\$ROOT/current" ]; then
  ACTIVE=\$(readlink -f "\$ROOT/current" 2>/dev/null || true)
  [ -n "\$ACTIVE" ] || die "current 版本指针不可解析，请先修复 \$ROOT/current"
fi
[ -f "\$ACTIVE/version.json" ] || die "\$ACTIVE 缺少 version.json，无法确认当前版本"

current=\$(sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\\([^"]*\\)".*/\\1/p' "\$ACTIVE/version.json" | head -1)
[ -n "\$current" ] || die "读不出 \$ACTIVE/version.json 里的版本号"
if [ "\$current" != "\$FROM_VERSION" ]; then
  die "版本不匹配：当前 \${current}，本包只能升级 \${FROM_VERSION} → \${TO_VERSION}。
     请换对应版本的升级包，或直接用完整安装包覆盖安装。"
fi

# ---- 2 校验 payload ---------------------------------------------------------
say "==> 校验升级包完整性"
if [ -s "\$HERE/files.sha256" ]; then
  if command -v sha256sum >/dev/null 2>&1; then
    ( cd "\$HERE" && sha256sum -c files.sha256 >/dev/null ) || die "校验失败：包已损坏，请重新下载"
  elif command -v shasum >/dev/null 2>&1; then
    ( cd "\$HERE" && shasum -a 256 -c files.sha256 >/dev/null ) || die "校验失败：包已损坏，请重新下载"
  else
    die "目标机既没有 sha256sum 也没有 shasum，无法校验，已中止"
  fi
fi
say "    通过"

if [ "\$DRY_RUN" -eq 1 ]; then
  # 演练也要给出「会动多少东西」的量化结论，否则用户看不出这次升级的规模。
  payload_bytes=\$(find "\$HERE/payload" -type f -exec wc -c {} + 2>/dev/null | awk '\$2 != "total" { s += \$1 } END { print s + 0 }')
  overwrite=\$(cd "\$HERE/payload" && find . -type f | sed 's|^\\./||' | sed 's|\\.new\$||' | while IFS= read -r rel; do
    if [ -e "\$ACTIVE/\$rel" ]; then printf 'x\\n'; fi
  done | wc -l)
  remove_count=0
  if [ -s "\$HERE/deletes.txt" ]; then
    while IFS= read -r rel; do
      [ -n "\$rel" ] || continue
      if [ -e "\$ACTIVE/\$rel" ]; then remove_count=\$((remove_count + 1)); fi
    done < "\$HERE/deletes.txt"
  fi
  c_add=\$(sed -n 's/.*"add":[[:space:]]*\\([0-9][0-9]*\\).*/\\1/p' "\$HERE/manifest.json" | head -1)
  c_mod=\$(sed -n 's/.*"modify":[[:space:]]*\\([0-9][0-9]*\\).*/\\1/p' "\$HERE/manifest.json" | head -1)
  c_del=\$(sed -n 's/.*"delete":[[:space:]]*\\([0-9][0-9]*\\).*/\\1/p' "\$HERE/manifest.json" | head -1)
  mb=\$(awk -v b="\$payload_bytes" 'BEGIN { printf "%.1f", b / 1000000 }')
  say "==> 演练模式（不做任何改动）"
  say "    安装根   \$ROOT"
  say "    程序目录 \$ACTIVE"
  say "    版本     \$FROM_VERSION → \$TO_VERSION"
  if [ -n "\$c_add" ] && [ -n "\$c_mod" ] && [ -n "\$c_del" ]; then
    say "    包内容   新增 \$c_add / 更新 \$c_mod / 删除 \$c_del，解开约 \${mb} MB"
  fi
  say "    将执行   覆盖 \$overwrite 个现有文件，清理 \$remove_count 个旧文件"
  say "    备份位置 \$ROOT/../uvp-update-backup-<时间戳>.tar.gz"
  exit 0
fi

# ---- 3 停服 ----------------------------------------------------------------
# systemd 托管的安装（uvp-gb28181-service-install.sh 注册过）不能用 ctl.sh stop：
# 单元会立刻把服务拉回来，覆盖二进制时报 Text file busy。必须先让 systemd 放手。
SYSTEMD_UNIT="uvp-gb28181.service"
USE_SYSTEMD=0
if command -v systemctl >/dev/null 2>&1 && systemctl is-enabled --quiet "\$SYSTEMD_UNIT" 2>/dev/null; then
  USE_SYSTEMD=1
  [ "\$(id -u)" -eq 0 ] || die "本机以 systemd 服务运行（\${SYSTEMD_UNIT}），请用 sudo 执行本脚本"
fi

say "==> 停止服务"
if [ "\$USE_SYSTEMD" -eq 1 ]; then
  systemctl stop uvp-gb28181-updater.service 2>/dev/null || true
  systemctl stop "\$SYSTEMD_UNIT" || die "停服失败，已中止（未改动任何文件）"
else
  "\$ROOT/uvp-gb28181-ctl.sh" stop-all || die "停服失败，已中止（未改动任何文件）"
fi

waited=0
while [ "\$waited" -lt 30 ]; do
  if ! pgrep -f "\$ACTIVE/bin/uvp-server" >/dev/null 2>&1; then break; fi
  sleep 1; waited=\$((waited + 1))
done
if pgrep -f "\$ACTIVE/bin/uvp-server" >/dev/null 2>&1; then
  die "uvp-server 等待 30 秒仍在运行，请手工停止后重试（未改动任何文件）"
fi

# ---- 4 备份 ----------------------------------------------------------------
STAMP=\$(date -u +%Y%m%d%H%M%S)
BACKUP="\$ROOT/../uvp-update-backup-\$STAMP.tar.gz"
LIST="\$ROOT/.update-backup-list"
: > "\$LIST"

say "==> 记录将被覆盖 / 删除的文件"
( cd "\$HERE/payload" && find . -type f -print ) | sed 's|^\\./||' | while IFS= read -r rel; do
  case "\$rel" in
    updater/uvp-updater.new|updater/uvp-updater.exe.new) target="\$ROOT/\${rel%.new}" ;;
    *) target="\$ACTIVE/\$rel" ;;
  esac
  if [ -e "\$target" ]; then printf '%s\\n' "\${target#\$ROOT/}" >> "\$LIST"; fi
done || true
if [ -s "\$HERE/deletes.txt" ]; then
  while IFS= read -r rel; do
    [ -n "\$rel" ] || continue
    if [ -e "\$ACTIVE/\$rel" ]; then printf '%s\\n' "\$rel" >> "\$LIST"; fi
  done < "\$HERE/deletes.txt"
fi
sort -u "\$LIST" -o "\$LIST"

if [ -s "\$LIST" ]; then
  say "    备份 \$(wc -l < "\$LIST" | tr -d ' ') 个文件到 \$BACKUP"
  tar -czf "\$BACKUP" -C "\$ROOT" -T "\$LIST" || { rm -f "\$LIST"; die "备份失败，已中止（未改动任何文件）"; }
else
  say "    没有需要备份的现有文件"
  BACKUP=""
fi
rm -f "\$LIST"

# ---- 5 落新文件 ------------------------------------------------------------
say "==> 写入新文件"
tar -cf - -C "\$HERE/payload" . | tar -xf - -C "\$ACTIVE" || die "写入失败，请用备份回滚：\${BACKUP:-（无备份）}"

# 助手镜像必须落在安装根的 updater/ —— 那是 ctl.sh 里写死的 UPDATER_BIN 位置，
# 不随 current 指向的版本目录变化。
if [ "\$ACTIVE" != "\$ROOT" ]; then
  for name in uvp-updater uvp-updater.exe; do
    if [ -f "\$ACTIVE/updater/\$name.new" ]; then
      mkdir -p "\$ROOT/updater"
      mv -f "\$ACTIVE/updater/\$name.new" "\$ROOT/updater/\$name.new"
    fi
  done
fi

# ---- 6 删除旧文件 ----------------------------------------------------------
if [ -s "\$HERE/deletes.txt" ]; then
  removed=0
  while IFS= read -r rel; do
    [ -n "\$rel" ] || continue
    if [ -f "\$ACTIVE/\$rel" ]; then rm -f "\$ACTIVE/\$rel" && removed=\$((removed + 1)); fi
  done < "\$HERE/deletes.txt"
  say "==> 清理本版本已移除的文件：\$removed 个"
fi
chmod 0755 "\$ROOT/uvp-gb28181-ctl.sh" 2>/dev/null || true

# ---- 7 起服 + 校验 ---------------------------------------------------------
say "==> 启动服务"
if [ "\$USE_SYSTEMD" -eq 1 ]; then
  systemctl start uvp-gb28181-updater.service 2>/dev/null || true
  systemctl start "\$SYSTEMD_UNIT" || die "启动失败。文件已更新，排查请见 \$ROOT/logs；备份在 \${BACKUP:-（无）}"
else
  "\$ROOT/uvp-gb28181-ctl.sh" start || die "启动失败。文件已更新，排查请见 \$ROOT/logs；备份在 \${BACKUP:-（无）}"
fi

after=\$(sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\\([^"]*\\)".*/\\1/p' "\$ACTIVE/version.json" | head -1)
[ "\$after" = "\$TO_VERSION" ] || die "升级后版本号是 \$after，期望 \$TO_VERSION（文件可能没覆盖成功）"

say ""
say "升级完成：\$FROM_VERSION → \$TO_VERSION"
say "安装根：\$ROOT"
if [ -n "\$BACKUP" ]; then say "回滚备份：\$BACKUP"; fi
say "配置与数据库未改动（config/ 与 data/ 不在升级包内）。"
`;

const CONTROL_SAFE = /^[A-Za-z0-9._-]+$/;

/** PowerShell 版 apply 脚本的模板目录（与 Linux 的内嵌模板同源思路，只是拆成独立文件便于语法检查）。 */
const templateDir = join(dirname(fileURLToPath(import.meta.url)), "templates");

export function sha256File(path) {
  const hash = createHash("sha256");
  hash.update(readFileSync(path));
  return hash.digest("hex");
}

function isIgnored(name) {
  return IGNORED_NAMES.includes(name) || name.startsWith("._");
}

/** 现场运行时数据 —— 只能"不动"，不能"删除"。 */
export function isProtectedPath(rel) {
  const normalized = rel.replaceAll("\\", "/");
  if (PROTECTED_PREFIXES.some((prefix) => normalized.startsWith(prefix))) return true;
  return PROTECTED_PATTERNS.some((pattern) => pattern.test(normalized));
}

/** 构建期产物 —— 既不进包，也不参与差异计算。 */
export function isSkippedPath(rel) {
  return SKIPPED_PATHS.includes(rel.replaceAll("\\", "/"));
}

/** 收集目录下所有文件（相对路径、正斜杠），跳过实例状态目录与噪音文件。 */
export function collectFiles(root) {
  const out = [];
  const skipped = [];
  const walk = (dir, prefix) => {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      if (isIgnored(entry.name)) continue;
      const rel = prefix ? `${prefix}/${entry.name}` : entry.name;
      if (isSkippedPath(rel)) continue;
      if (entry.isDirectory()) {
        if (!prefix && EXCLUDED_TOP_DIRS.includes(entry.name)) {
          skipped.push(rel);
          continue;
        }
        walk(join(dir, entry.name), rel);
      } else if (entry.isFile()) {
        out.push(rel);
      }
    }
  };
  walk(root, "");
  out.sort();
  return { files: out, skipped };
}

/** 助手镜像在 payload 里的落点名：加 .new，避免覆盖正在运行的程序。 */
export function payloadNameFor(rel) {
  return HELPER_IMAGES.includes(rel) ? `${rel}.new` : rel;
}

export function readVersionFile(root) {
  const path = join(root, "version.json");
  if (!existsSync(path)) throw new Error(`${root} 缺少 version.json`);
  const data = JSON.parse(readFileSync(path, "utf8"));
  if (!data || typeof data.version !== "string" || !data.version) {
    throw new Error(`${path} 没有有效的 version 字段`);
  }
  return data;
}

/** 对比两个发行目录，产出版本清单。 */
export function diffReleases(fromDir, toDir) {
  const from = readVersionFile(fromDir);
  const to = readVersionFile(toDir);
  const a = collectFiles(fromDir);
  const b = collectFiles(toDir);
  const fromSet = new Set(a.files);
  const toSet = new Set(b.files);

  // ⛔ 现场运行时数据一律不参与：既不下发覆盖，也不计入删除。
  const add = b.files.filter((rel) => !fromSet.has(rel) && !isProtectedPath(rel));
  const modify = b.files.filter((rel) => {
    if (!fromSet.has(rel)) return false;
    if (isProtectedPath(rel)) return false;
    const oldPath = join(fromDir, rel);
    const newPath = join(toDir, rel);
    if (statSync(oldPath).size !== statSync(newPath).size) return true;
    return sha256File(oldPath) !== sha256File(newPath);
  });
  const removedAll = a.files.filter((rel) => !toSet.has(rel));
  const remove = removedAll.filter((rel) => !isProtectedPath(rel));
  const protectedDropped = removedAll.filter((rel) => isProtectedPath(rel));

  return {
    from,
    to,
    add,
    modify,
    remove,
    protectedDropped,
    skipped: a.skipped,
    toFiles: b.files.filter((rel) => !isProtectedPath(rel))
  };
}

function tarFlags() {
  // macOS 的 bsdtar 要屏蔽 AppleDouble / xattr，否则客户机解包会刷几百行警告；
  // GNU tar 没有 --no-xattrs。
  return process.platform === "darwin" ? ["--no-xattrs", "--format=ustar"] : ["--format=gnu"];
}

// ---------------------------------------------------------------- ZIP ----
// Windows 包必须是 zip：现场机双击就能解开，不用额外装 7-Zip / tar。
// 这里自己拼最小 ZIP 结构，不依赖系统 zip 命令 —— macOS 的 Info-ZIP 不写 UTF-8 标志，
// 包内那批中文名的前端资源到了 Windows 会变成乱码文件名（浏览器 404，页面白屏）。
const CRC_TABLE = (() => {
  const table = new Int32Array(256);
  for (let index = 0; index < 256; index += 1) {
    let value = index;
    for (let bit = 0; bit < 8; bit += 1) {
      value = value & 1 ? 0xedb88320 ^ (value >>> 1) : value >>> 1;
    }
    table[index] = value;
  }
  return table;
})();

function crc32(buffer) {
  let value = 0 ^ -1;
  for (let index = 0; index < buffer.length; index += 1) {
    value = (value >>> 8) ^ CRC_TABLE[(value ^ buffer[index]) & 0xff];
  }
  return (value ^ -1) >>> 0;
}

// 固定 2026-01-01 00:00:00 的 DOS 时间：同样输入产出同样字节，便于比对与复现。
const DOS_TIME = 0;
const DOS_DATE = ((2026 - 1980) << 9) | (1 << 5) | 1;

/** 把 [{ name, data }] 写成 zip（deflate，文件名按 UTF-8 存并打 EFS 标志）。 */
export function writeZip(archivePath, entries) {
  const chunks = [];
  const central = [];
  let offset = 0;

  for (const entry of entries) {
    const nameBuffer = Buffer.from(entry.name, "utf8");
    const digest = crc32(entry.data);
    const deflated = deflateRawSync(entry.data, { level: 6 });
    const useDeflate = deflated.length < entry.data.length;
    const body = useDeflate ? deflated : entry.data;
    const method = useDeflate ? 8 : 0;

    const local = Buffer.alloc(30);
    local.writeUInt32LE(0x04034b50, 0);
    local.writeUInt16LE(20, 4);
    local.writeUInt16LE(0x0800, 6); // bit 11：文件名是 UTF-8
    local.writeUInt16LE(method, 8);
    local.writeUInt16LE(DOS_TIME, 10);
    local.writeUInt16LE(DOS_DATE, 12);
    local.writeUInt32LE(digest, 14);
    local.writeUInt32LE(body.length, 18);
    local.writeUInt32LE(entry.data.length, 22);
    local.writeUInt16LE(nameBuffer.length, 26);
    local.writeUInt16LE(0, 28);
    chunks.push(local, nameBuffer, body);

    const header = Buffer.alloc(46);
    header.writeUInt32LE(0x02014b50, 0);
    header.writeUInt16LE(20, 4);
    header.writeUInt16LE(20, 6);
    header.writeUInt16LE(0x0800, 8);
    header.writeUInt16LE(method, 10);
    header.writeUInt16LE(DOS_TIME, 12);
    header.writeUInt16LE(DOS_DATE, 14);
    header.writeUInt32LE(digest, 16);
    header.writeUInt32LE(body.length, 20);
    header.writeUInt32LE(entry.data.length, 24);
    header.writeUInt16LE(nameBuffer.length, 28);
    header.writeUInt16LE(0, 30);
    header.writeUInt16LE(0, 32);
    header.writeUInt16LE(0, 34);
    header.writeUInt16LE(0, 36);
    header.writeUInt32LE(0, 38);
    header.writeUInt32LE(offset, 42);
    central.push(Buffer.concat([header, nameBuffer]));

    offset += local.length + nameBuffer.length + body.length;
  }

  const directory = Buffer.concat(central);
  const end = Buffer.alloc(22);
  end.writeUInt32LE(0x06054b50, 0);
  end.writeUInt16LE(0, 4);
  end.writeUInt16LE(0, 6);
  end.writeUInt16LE(entries.length, 8);
  end.writeUInt16LE(entries.length, 10);
  end.writeUInt32LE(directory.length, 12);
  end.writeUInt32LE(offset, 16);
  end.writeUInt16LE(0, 20);

  writeFileSync(archivePath, Buffer.concat([...chunks, directory, end]));
}

/** 生成增量升级包。 */
export function buildUpdatePackage(options) {
  const fromDir = resolve(options.fromDir);
  const toDir = resolve(options.toDir);
  const platform = options.platform;
  if (!CONTROL_SAFE.test(platform)) throw new Error(`平台名不合法：${platform}`);
  const isWindows = platform.startsWith("windows");

  const diff = diffReleases(fromDir, toDir);
  if (diff.from.version === diff.to.version) {
    throw new Error(`源目录与目标目录版本相同（${diff.to.version}），做不出增量包`);
  }
  if (diff.toFiles.length === 0) throw new Error(`${toDir} 里没有任何文件`);

  const stem = `uvp-gb28181-update-${platform}-${diff.from.version}-to-${diff.to.version}`;
  const outDir = resolve(options.outDir);
  mkdirSync(outDir, { recursive: true });
  const staging = join(outDir, stem);
  rmSync(staging, { recursive: true, force: true });
  mkdirSync(join(staging, "payload"), { recursive: true });

  const entries = [];
  const checksumLines = [];
  const addedSet = new Set(diff.add);

  for (const rel of [...diff.add, ...diff.modify]) {
    const src = join(toDir, rel);
    const payloadRel = payloadNameFor(rel);
    const dst = join(staging, "payload", payloadRel);
    mkdirSync(dirname(dst), { recursive: true });
    copyFileSync(src, dst);
    const mode = statSync(src).mode & 0o777;
    chmodSync(dst, mode);
    const digest = sha256File(dst);
    checksumLines.push(`${digest}  payload/${payloadRel}`);
    const entry = {
      path: payloadRel,
      action: addedSet.has(rel) ? "add" : "modify",
      sha256: digest,
      size: statSync(dst).size,
      mode: mode.toString(8).padStart(4, "0")
    };
    if (payloadRel !== rel) entry.target = rel;
    entries.push(entry);
  }

  for (const rel of diff.remove) {
    entries.push({
      path: rel,
      action: "delete",
      sha256: sha256File(join(fromDir, rel)),
      size: statSync(join(fromDir, rel)).size
    });
  }
  entries.sort((x, y) => (x.path < y.path ? -1 : x.path > y.path ? 1 : 0));

  const stateTouched = diff.remove.filter((rel) => EXCLUDED_TOP_DIRS.includes(rel.split("/")[0]));

  const manifest = {
    schema: 1,
    product: "uvp-gb28181",
    platform,
    from_version: diff.from.version,
    to_version: diff.to.version,
    from_build_id: diff.from.build_id || null,
    to_build_id: diff.to.build_id || null,
    created_at: options.createdAt || new Date().toISOString(),
    excluded_top_dirs: EXCLUDED_TOP_DIRS,
    protected_paths: PROTECTED_PREFIXES,
    counts: { add: diff.add.length, modify: diff.modify.length, delete: diff.remove.length },
    notes: [
      "config/、data/、logs/、run/ 属于实例状态，不进增量包：升级不会改动现场配置与数据库。",
      "resource/public/uploads/、gb-channel-snapshot/ 是现场运行时数据，既不覆盖也不删除。",
      `${isWindows ? HELPER_IMAGES[1] : HELPER_IMAGES[0]} 会落成 .new 旁挂文件，由控制脚本在下次启动时替换。`
    ],
    files: entries
  };

  writeFileSync(join(staging, "manifest.json"), `${JSON.stringify(manifest, null, 2)}\n`);
  writeFileSync(join(staging, "files.sha256"), checksumLines.length ? `${checksumLines.join("\n")}\n` : "");
  writeFileSync(join(staging, "deletes.txt"), diff.remove.length ? `${diff.remove.join("\n")}\n` : "");

  let archive;
  let archiveName;
  if (isWindows) {
    // ⛔ apply.ps1 必须带 UTF-8 BOM：Windows PowerShell 5.1 读无 BOM 的 .ps1 会按系统
    //   ANSI 代码页（简中=GBK）解码，中文提示变乱码，严重时引号被吃掉直接语法错误。
    const psTemplate = readFileSync(join(templateDir, "update-apply.ps1"), "utf8");
    const psScript = psTemplate
      .split("__FROM_VERSION__")
      .join(diff.from.version)
      .split("__TO_VERSION__")
      .join(diff.to.version);
    writeFileSync(join(staging, "apply.ps1"), `\ufeff${psScript}`, { mode: 0o644 });
    // 双击入口：cmd 里的 chcp 让控制台按 UTF-8 显示，执行策略由 -ExecutionPolicy Bypass 兜住。
    writeFileSync(
      join(staging, "apply.cmd"),
      [
        "@echo off",
        "setlocal",
        "chcp 65001 >nul",
        'powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0apply.ps1" %*',
        "if errorlevel 1 pause",
        "endlocal",
        ""
      ].join("\r\n")
    );

    archiveName = `${stem}.zip`;
    archive = join(outDir, archiveName);
    rmSync(archive, { force: true });
    const zipEntries = [];
    const collect = (dir, prefix) => {
      for (const entry of readdirSync(dir, { withFileTypes: true })) {
        const rel = prefix ? `${prefix}/${entry.name}` : entry.name;
        if (entry.isDirectory()) collect(join(dir, entry.name), rel);
        else if (entry.isFile()) zipEntries.push({ name: `${stem}/${rel}`, data: readFileSync(join(dir, entry.name)) });
      }
    };
    collect(staging, "");
    zipEntries.sort((left, right) => (left.name < right.name ? -1 : left.name > right.name ? 1 : 0));
    writeZip(archive, zipEntries);
  } else {
    const applyScript = APPLY_TEMPLATE.split("__FROM_VERSION__")
      .join(diff.from.version)
      .split("__TO_VERSION__")
      .join(diff.to.version);
    writeFileSync(join(staging, "apply.sh"), applyScript, { mode: 0o755 });
    chmodSync(join(staging, "apply.sh"), 0o755);

    archiveName = `${stem}.tar.gz`;
    archive = join(outDir, archiveName);
    rmSync(archive, { force: true });
    execFileSync("tar", [...tarFlags(), "-czf", archive, "-C", dirname(staging), stem], {
      stdio: "inherit"
    });
  }

  const digest = sha256File(archive);
  writeFileSync(`${archive}.sha256`, `${digest}  ${archiveName}\n`);

  return {
    stem,
    archive,
    sha256: digest,
    size: statSync(archive).size,
    staging,
    manifest,
    stateTouched,
    protectedDropped: diff.protectedDropped,
    skipped: diff.skipped,
    unpackedBytes: entries.filter((e) => e.action !== "delete").reduce((sum, e) => sum + e.size, 0)
  };
}

export function runCli(argv, io = console) {
  const args = argv.slice(0);
  const opts = { outDir: "release-output" };
  while (args.length) {
    const flag = args.shift();
    if (flag === "--from") opts.fromDir = args.shift();
    else if (flag === "--to") opts.toDir = args.shift();
    else if (flag === "--platform") opts.platform = args.shift();
    else if (flag === "--out") opts.outDir = args.shift();
    else if (flag === "-h" || flag === "--help") {
      io.log("用法: build-update-package.mjs --from <旧版目录> --to <新版目录> --platform <linux-amd64> [--out release-output]");
      return 0;
    } else throw new Error(`未知参数 ${flag}`);
  }
  for (const [key, label] of [["fromDir", "--from"], ["toDir", "--to"], ["platform", "--platform"]]) {
    if (!opts[key]) throw new Error(`缺少必填参数 ${label}`);
  }
  if (!existsSync(opts.fromDir)) throw new Error(`源目录不存在：${opts.fromDir}`);
  if (!existsSync(opts.toDir)) throw new Error(`目标目录不存在：${opts.toDir}`);

  const result = buildUpdatePackage(opts);
  io.log("");
  io.log(`增量升级包：${result.archive.replace(/^.*[\\/]/, "")}`);
  io.log(`  版本    ${result.manifest.from_version} → ${result.manifest.to_version}（${result.manifest.platform}）`);
  io.log(
    `  内容    新增 ${result.manifest.counts.add} / 改动 ${result.manifest.counts.modify} / 删除 ${result.manifest.counts.delete}`
  );
  io.log(`  体积    ${(result.size / 1e6).toFixed(1)} MB（解开约 ${(result.unpackedBytes / 1e6).toFixed(1)} MB）`);
  io.log(`  校验值  ${result.sha256}`);
  if (result.stateTouched.length) {
    io.log(`  ⚠ 实例状态目录里有文件被移除，但不会随包处理：${result.stateTouched.slice(0, 5).join(", ")}`);
  }
  if (result.protectedDropped.length) {
    io.log(`  ⚠ 现场运行时数据不参与删除（${result.protectedDropped.length} 个）：${result.protectedDropped.slice(0, 3).join(", ")} …`);
  }
  return 0;
}

const invokedDirectly =
  process.argv[1] && resolve(process.argv[1]) === resolve(fileURLToPath(import.meta.url));
if (invokedDirectly) {
  try {
    process.exitCode = runCli(process.argv.slice(2));
  } catch (error) {
    console.error(`错误：${error.message}`);
    process.exitCode = 1;
  }
}
