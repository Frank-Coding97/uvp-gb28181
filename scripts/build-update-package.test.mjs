import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import {
  chmodSync,
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  statSync,
  writeFileSync
} from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import {
  buildUpdatePackage,
  collectFiles,
  diffReleases,
  payloadNameFor
} from "./build-update-package.mjs";

function write(root, rel, content, mode = 0o644) {
  const full = path.join(root, rel);
  mkdirSync(path.dirname(full), { recursive: true });
  writeFileSync(full, content);
  chmodSync(full, mode);
}

function sha(path) {
  return createHash("sha256").update(readFileSync(path)).digest("hex");
}

/** 造一对假的发行目录：覆盖改动 / 新增 / 删除 / 实例状态四类。 */
function fixture() {
  const root = mkdtempSync(path.join(os.tmpdir(), "uvp-update-pkg-"));
  const from = path.join(root, "from");
  const to = path.join(root, "to");

  for (const dir of [from, to]) {
    writeFileSync(path.join(dir, "..", "keep"), "");
  }
  mkdirSync(from, { recursive: true });
  mkdirSync(to, { recursive: true });

  // 两个版本都有、内容不同 ⇒ modify
  write(from, "bin/uvp-server", "old-server-binary\n", 0o755);
  write(to, "bin/uvp-server", "new-server-binary\n", 0o755);
  write(from, "updater/uvp-updater", "old-helper\n", 0o755);
  write(to, "updater/uvp-updater", "new-helper\n", 0o755);
  write(from, "uvp-gb28181-ctl.sh", "#!/bin/sh\nexit 0\n", 0o755);
  write(to, "uvp-gb28181-ctl.sh", "#!/bin/sh\nexit 0\n", 0o755);
  write(from, "version.json", JSON.stringify({ version: "1.1.16", build_id: "b16" }));
  write(to, "version.json", JSON.stringify({ version: "1.1.17", build_id: "b17" }));

  // 只有新版 ⇒ add
  write(to, "resource/public/static/new-hash.js", "new asset\n");
  // 只有旧版 ⇒ delete
  write(from, "resource/public/static/old-hash.js", "old asset\n");

  // 实例状态：两个版本都不同，但绝不能进包
  write(from, "config/config.yml", "audience: old\n");
  write(to, "config/config.yml", "audience: new\n");
  write(from, "config/config.env", "UVP_KEY=old\n");
  write(to, "config/config.env", "UVP_KEY=new\n");
  write(from, "data/uvp.db", "old db\n");
  write(to, "data/uvp.db", "new db\n");
  write(from, "logs/app.log", "old log\n");
  write(to, "logs/app.log", "new log\n");

  return { root, from, to };
}

test("实例状态目录不进入文件清单", () => {
  const { from } = fixture();
  const { files, skipped } = collectFiles(from);
  assert.deepEqual(skipped.sort(), ["config", "data", "logs"]);
  assert.ok(files.every((rel) => !/^(config|data|logs|run)\//.test(rel)));
  assert.ok(files.includes("bin/uvp-server"));
});

test("助手镜像改名为 .new，其余路径保持不变", () => {
  assert.equal(payloadNameFor("updater/uvp-updater"), "updater/uvp-updater.new");
  assert.equal(payloadNameFor("updater/uvp-updater.exe"), "updater/uvp-updater.exe.new");
  assert.equal(payloadNameFor("bin/uvp-server"), "bin/uvp-server");
});

test("差异分类：新增 / 改动 / 删除", () => {
  const { from, to } = fixture();
  const diff = diffReleases(from, to);
  assert.deepEqual(diff.add, ["resource/public/static/new-hash.js"]);
  assert.deepEqual(diff.remove, ["resource/public/static/old-hash.js"]);
  assert.deepEqual(diff.modify.sort(), ["bin/uvp-server", "updater/uvp-updater", "version.json"]);
  assert.equal(diff.from.version, "1.1.16");
  assert.equal(diff.to.version, "1.1.17");
});

test("同版本拒绝生成", () => {
  const { from } = fixture();
  assert.throws(
    () => buildUpdatePackage({ fromDir: from, toDir: from, platform: "linux-amd64", outDir: os.tmpdir() }),
    /版本相同/
  );
});

test("生成的包结构完整且校验值自洽", () => {
  const { root, from, to } = fixture();
  const result = buildUpdatePackage({
    fromDir: from,
    toDir: to,
    platform: "linux-amd64",
    outDir: path.join(root, "out"),
    createdAt: "2026-11-05T00:00:00.000Z"
  });

  const manifest = JSON.parse(readFileSync(path.join(result.staging, "manifest.json"), "utf8"));
  assert.equal(manifest.from_version, "1.1.16");
  assert.equal(manifest.to_version, "1.1.17");
  assert.deepEqual(manifest.excluded_top_dirs, ["config", "data", "logs", "run"]);
  assert.equal(result.stem, "uvp-1.1.16-to-1.1.17-linux-amd64.incremental");

  // payload 里绝不能出现 config / data / logs
  const payloadFiles = collectFiles(path.join(result.staging, "payload")).files;
  assert.ok(payloadFiles.every((rel) => !/^(config|data|logs|run)\//.test(rel)));
  assert.ok(payloadFiles.includes("updater/uvp-updater.new"));
  assert.ok(!payloadFiles.includes("updater/uvp-updater"));

  // files.sha256 必须与 payload 实际内容逐条对上
  const lines = readFileSync(path.join(result.staging, "files.sha256"), "utf8").trim().split("\n");
  assert.equal(lines.length, payloadFiles.length);
  for (const line of lines) {
    const [digest, relPath] = line.split(/\s+/);
    assert.equal(digest, sha(path.join(result.staging, relPath)), `${relPath} 的校验值对不上`);
  }

  assert.equal(readFileSync(path.join(result.staging, "deletes.txt"), "utf8").trim(), "resource/public/static/old-hash.js");

  const apply = readFileSync(path.join(result.staging, "apply.sh"), "utf8");
  assert.ok(apply.includes('FROM_VERSION="1.1.16"'));
  assert.ok(apply.includes('TO_VERSION="1.1.17"'));
  assert.ok(!apply.includes("__FROM_VERSION__"));
  assert.ok(statSync(path.join(result.staging, "apply.sh")).mode & 0o100, "apply.sh 必须可执行");

  assert.ok(existsSync(result.archive));
  assert.equal(readFileSync(`${result.archive}.sha256`, "utf8").split(/\s+/)[0], result.sha256);

  rmSync(root, { recursive: true, force: true });
});

test("apply.sh 在模拟安装根上完成升级，且不动配置与数据", () => {
  const { root, from, to } = fixture();
  const outDir = path.join(root, "out");
  buildUpdatePackage({ fromDir: from, toDir: to, platform: "linux-amd64", outDir });

  // 解开增量包
  const unpack = path.join(root, "unpack");
  mkdirSync(unpack, { recursive: true });
  execFileSync("tar", ["-xzf", path.join(outDir, "uvp-1.1.16-to-1.1.17-linux-amd64.incremental.tar.gz"), "-C", unpack]);
  const pkg = path.join(unpack, "uvp-1.1.16-to-1.1.17-linux-amd64.incremental");

  // 造一个"现场安装根"：旧版文件 + 现场特有的配置与数据
  const installRoot = path.join(root, "site");
  execFileSync("cp", ["-R", `${from}/.`, installRoot]);
  write(installRoot, "config/config.yml", "audience: site-specific\n");
  write(installRoot, "data/uvp.db", "site database\n");
  // 桩 ctl.sh：stop-all / start 直接返回成功
  write(
    installRoot,
    "uvp-gb28181-ctl.sh",
    `#!/bin/sh\ncase "$1" in stop-all|start) exit 0 ;; *) exit 0 ;; esac\n`,
    0o755
  );

  execFileSync("sh", [path.join(pkg, "apply.sh"), "-r", installRoot], { stdio: "pipe" });

  // 程序文件已替换
  assert.equal(readFileSync(path.join(installRoot, "bin/uvp-server"), "utf8"), "new-server-binary\n");
  assert.equal(JSON.parse(readFileSync(path.join(installRoot, "version.json"), "utf8")).version, "1.1.17");
  assert.ok(statSync(path.join(installRoot, "bin/uvp-server")).mode & 0o100, "二进制必须保持可执行");

  // 助手走旁挂，不就地覆盖
  assert.equal(readFileSync(path.join(installRoot, "updater/uvp-updater"), "utf8"), "old-helper\n");
  assert.equal(readFileSync(path.join(installRoot, "updater/uvp-updater.new"), "utf8"), "new-helper\n");

  // 旧资产被删、新资产已落
  assert.ok(!existsSync(path.join(installRoot, "resource/public/static/old-hash.js")));
  assert.equal(
    readFileSync(path.join(installRoot, "resource/public/static/new-hash.js"), "utf8"),
    "new asset\n"
  );

  // 现场配置与数据库纹丝不动
  assert.equal(readFileSync(path.join(installRoot, "config/config.yml"), "utf8"), "audience: site-specific\n");
  assert.equal(readFileSync(path.join(installRoot, "data/uvp.db"), "utf8"), "site database\n");

  rmSync(root, { recursive: true, force: true });
});

test("apply.sh 版本不匹配时拒绝执行且不改文件", () => {
  const { root, from, to } = fixture();
  const outDir = path.join(root, "out");
  buildUpdatePackage({ fromDir: from, toDir: to, platform: "linux-amd64", outDir });

  const unpack = path.join(root, "unpack");
  mkdirSync(unpack, { recursive: true });
  execFileSync("tar", ["-xzf", path.join(outDir, "uvp-1.1.16-to-1.1.17-linux-amd64.incremental.tar.gz"), "-C", unpack]);
  const pkg = path.join(unpack, "uvp-1.1.16-to-1.1.17-linux-amd64.incremental");

  const installRoot = path.join(root, "site");
  execFileSync("cp", ["-R", `${from}/.`, installRoot]);
  write(installRoot, "version.json", JSON.stringify({ version: "1.1.15" }));
  write(installRoot, "uvp-gb28181-ctl.sh", "#!/bin/sh\nexit 0\n", 0o755);

  let failed = false;
  try {
    execFileSync("sh", [path.join(pkg, "apply.sh"), "-r", installRoot], { stdio: "pipe" });
  } catch (error) {
    failed = true;
    assert.match(String(error.stderr), /版本不匹配/);
  }
  assert.ok(failed, "版本不匹配必须失败");
  assert.equal(readFileSync(path.join(installRoot, "bin/uvp-server"), "utf8"), "old-server-binary\n");
  assert.equal(existsSync(path.join(installRoot, "updater/uvp-updater.new")), false);

  rmSync(root, { recursive: true, force: true });
});

// ---------------------------------------------------------------- Windows ----

/** 造带现场数据的 Windows 发行目录：中文名前端资源、运行时上传件、nginx 渲染产物。 */
function windowsFixture() {
  const base = fixture();
  write(base.from, "bin/uvp-server.exe", "old-server\n");
  write(base.to, "bin/uvp-server.exe", "new-server\n");
  write(base.from, "updater/uvp-updater.exe", "old-helper\n");
  write(base.to, "updater/uvp-updater.exe", "new-helper\n");
  write(base.from, "uvp-gb28181-ctl.ps1", "# ctl\n");
  write(base.to, "uvp-gb28181-ctl.ps1", "# ctl\n");
  // 中文名的前端资源：zip 必须按 UTF-8 存，否则 Windows 解出来是乱码文件名
  write(base.to, "resource/public/static/插件-abc123.js", "cn asset\n");
  // 运行时数据：只在旧版有 ⇒ 绝不能进删除清单
  write(base.from, "resource/public/uploads/2026-09-01/user.jpg", "user data\n");
  write(base.from, "resource/public/gb-channel-snapshot/2026-07/cam.jpg", "snapshot\n");
  write(base.from, "resource/baseline/__pycache__/generate.cpython-312.pyc", "bytecode\n");
  // 构建期渲染产物：两版都不同，但不该进包
  write(base.from, "vendor/nginx/conf/nginx.conf", "pid C:/build-16/run/nginx.pid;\n");
  write(base.to, "vendor/nginx/conf/nginx.conf", "pid C:/build-17/run/nginx.pid;\n");
  return base;
}

test("现场运行时数据与构建期产物都不参与增量", () => {
  const { from, to } = windowsFixture();
  const diff = diffReleases(from, to);
  const all = [...diff.add, ...diff.modify, ...diff.remove];
  assert.ok(all.every((rel) => !rel.startsWith("resource/public/uploads/")));
  assert.ok(all.every((rel) => !rel.startsWith("resource/public/gb-channel-snapshot/")));
  assert.ok(all.every((rel) => !rel.includes("__pycache__")));
  assert.ok(all.every((rel) => rel !== "vendor/nginx/conf/nginx.conf"));
  assert.equal(diff.protectedDropped.length, 3);
});

test("Windows 平台产出 zip，脚本带 BOM 且文件名按 UTF-8 存储", () => {
  const { root, from, to } = windowsFixture();
  const outDir = path.join(root, "out");
  const result = buildUpdatePackage({ fromDir: from, toDir: to, platform: "windows-amd64", outDir });

  assert.ok(result.archive.endsWith(".zip"), "Windows 包必须是 zip");
  assert.equal(result.stem, "uvp-1.1.16-to-1.1.17-windows-amd64.incremental");
  assert.equal(readFileSync(`${result.archive}.sha256`, "utf8").split(/\s+/)[0], result.sha256);

  // 结构自洽：CRC 全对才能通过 unzip -t
  execFileSync("unzip", ["-t", result.archive], { stdio: "pipe" });

  // ⛔ 不用 unzip -Z1 验中文名：macOS 的 Info-ZIP 在非终端环境会把 UTF-8 名字降级成乱码，
  //   测出来的是它的显示问题，不是包的问题。直接查原始字节 —— 名字必须是 UTF-8 编码。
  //   ⚠️ macOS 文件系统存 NFD，所以按 NFD 形态找；两者都是同一串 UTF-8 字节序列。
  const raw = readFileSync(result.archive);
  const has = (name) => raw.includes(Buffer.from(name, "utf8"));
  assert.ok(has("插件-abc123.js"), "中文文件名必须按 UTF-8 存入 zip");
  assert.ok(has("apply.ps1"));
  assert.ok(has("apply.cmd"));
  assert.ok(has("updater/uvp-updater.exe.new"), "助手必须是旁挂 .new");

  // PowerShell 5.1 读无 BOM 的 .ps1 会按系统代码页（简中=GBK）解码 ⇒ 中文乱码甚至语法错
  const psBytes = readFileSync(path.join(result.staging, "apply.ps1"));
  assert.deepEqual([...psBytes.subarray(0, 3)], [0xef, 0xbb, 0xbf], "apply.ps1 必须带 UTF-8 BOM");
  const psText = psBytes.toString("utf8");
  assert.match(psText, /\$FromVersion\s*=\s*'1\.1\.16'/);
  assert.match(psText, /\$ToVersion\s*=\s*'1\.1\.17'/);
  assert.ok(!psText.includes("__FROM_VERSION__"));

  // apply.cmd 是双击入口：CRLF 换行 + 绕过执行策略，否则客户机点开一闪而过
  const cmd = readFileSync(path.join(result.staging, "apply.cmd"), "utf8");
  assert.ok(cmd.includes("-ExecutionPolicy Bypass"));
  assert.ok(cmd.includes("\r\n"));

  rmSync(root, { recursive: true, force: true });
});
