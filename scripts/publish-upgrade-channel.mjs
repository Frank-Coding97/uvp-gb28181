#!/usr/bin/env node
/**
 * 把一版已构建的安装包发布成官网上的在线升级通道。
 *
 * 只生成两个签名 JSON，**不搬运安装包**：包留在 Gitee Releases，客户端按清单
 * 里的地址去下载，官网只承担几百字节的流量。私钥仅在本机使用，产物提交进官网
 * 仓库后由既有流水线部署。
 *
 * 用法：
 *   node scripts/publish-upgrade-channel.mjs \
 *     --site-dir ~/code/uvp/uvplatform-website/docs/public/upgrades \
 *     --private-key ~/.uvp-release-keys/uvp-release-v1.pem \
 *     --notes-from ~/code/uvp/uvplatform-website/data/releases.json
 *
 * 常用参数：
 *   --version <x.y.z>        默认读 VERSION
 *   --release-id <id>        默认等于版本号，发布后不可变
 *   --channel stable|beta    默认 stable
 *   --artifact-dir <dir>     默认 release-output/<version>
 *   --download-base <url>    默认 Gitee 该 tag 的下载前缀
 *   --base-url <url>         默认 https://uvplatform.com/upgrades
 *   --expires-days <n>       通道指针有效期，默认 90 天
 *   --refresh                只续期通道指针，不发布新版本
 *   --dry-run                只打印计划，不落盘
 *   --no-verify-remote       跳过对下载地址的远程大小校验
 */
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdir, readdir, readFile, rename, rm, stat, writeFile } from "node:fs/promises";
import { basename, dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import {
  buildSignedChannel,
  buildSignedManifest,
  hashFile,
  loadEd25519PrivateKey
} from "./sign-upgrade-manifest.mjs";

const REPO_ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const DEFAULT_BASE_URL = "https://uvplatform.com/upgrades";
const DEFAULT_DOWNLOAD_BASE = "https://gitee.com/Frank-Coding/uvp-gb28181/releases/download";
// ⛔⛔ 这里**不要**加 compose。Linux 绿色包只对 standalone / systemd 成立：
//   ComposeAdapter.PreparePackage 要求包内解出注册的 compose.yml，并按 images 逐个
//   docker load；绿色包里两样都没有（顶层只有 bin/ config/ data/ logs/ resource/ run/
//   updater/ vendor/），硬标成 compose 会在**准备阶段**报
//   `upgrade package is missing registered compose file`，而且现场会先看到
//   "有新版本"再失败，比直接说"没有可用制品"更难查。
//   ⇒ 容器版要单独出 compose 升级包（compose.yml + images/*.tar），用 --deployments compose 显式指定。
const DEFAULT_LINUX_DEPLOYMENTS = ["standalone", "systemd"];
const VERSION_PATTERN = /^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-rc\.(0|[1-9][0-9]*))?$/;
const PACKAGE_PATTERN = /^uvp-gb28181-(linux|windows)-(amd64|arm64)-.+\.(tar\.gz|zip)$/;

function fail(message) {
  throw new Error(message);
}

function rfc3339(date) {
  return new Date(Math.floor(date.getTime() / 1000) * 1000).toISOString().replace(".000Z", "Z");
}

function parseArguments(argv) {
  const values = new Map();
  const flags = new Set();
  const booleanFlags = new Set(["--refresh", "--dry-run", "--no-verify-remote", "--include-windows"]);
  for (let index = 0; index < argv.length; index += 1) {
    const name = argv[index];
    if (booleanFlags.has(name)) {
      flags.add(name);
      continue;
    }
    if (!name.startsWith("--")) fail(`无法识别的参数：${name}`);
    const value = argv[index + 1];
    if (value === undefined || value.startsWith("--")) fail(`参数 ${name} 缺少值`);
    values.set(name, value);
    index += 1;
  }
  return { values, flags };
}

function requireValue(values, name) {
  const value = values.get(name);
  if (value === undefined || value.trim() === "") fail(`缺少必填参数 ${name}`);
  return value.trim();
}

function normalizeBaseURL(value, name) {
  let parsed;
  try {
    parsed = new URL(value);
  } catch {
    fail(`${name} 不是合法 URL：${value}`);
  }
  if (parsed.protocol !== "https:") fail(`${name} 必须使用 HTTPS`);
  if (parsed.username || parsed.password) fail(`${name} 不能携带凭据`);
  return parsed.toString().replace(/\/$/, "");
}

function joinURL(base, ...parts) {
  const encoded = parts.map(part => encodeURIComponent(part)).join("/");
  return `${base}/${encoded}`;
}

// 包内 version.json 是构建号的权威来源：清单里的 build_id 必须与它一致，
// 否则自动升级的「源版本」匹配会永远对不上。
function readPackageVersionJSON(filePath) {
  const listing = execFileSync("tar", ["-tzf", filePath], { encoding: "utf8", maxBuffer: 64 << 20 });
  const entry = listing.split("\n").find(name => name.endsWith("/version.json"));
  if (!entry) fail(`安装包内缺少 version.json：${basename(filePath)}`);
  const raw = execFileSync("tar", ["-xzOf", filePath, entry], { encoding: "utf8", maxBuffer: 1 << 20 });
  const parsed = JSON.parse(raw);
  if (typeof parsed.version !== "string" || typeof parsed.build_id !== "string") {
    fail(`安装包 version.json 格式不符：${basename(filePath)}`);
  }
  return { version: parsed.version, buildID: parsed.build_id };
}

async function discoverArtifacts(artifactDir, { includeWindows }) {
  const entries = await readdir(artifactDir);
  const packages = entries.filter(name => PACKAGE_PATTERN.test(name)).sort();
  const linux = packages.filter(name => name.includes("-linux-"));
  const windows = includeWindows ? packages.filter(name => name.includes("-windows-")) : [];
  if (linux.length === 0) fail(`目录中没有 Linux 安装包：${artifactDir}`);
  return { linux, windows, ignored: packages.length - linux.length - windows.length };
}

async function readReleaseNotes({ values, version }) {
  const notesFile = values.get("--notes-file");
  if (notesFile) return (await readFile(notesFile, "utf8")).trim();

  const notesFrom = values.get("--notes-from");
  if (!notesFrom) return "";
  const raw = JSON.parse(await readFile(notesFrom, "utf8"));
  const entries = raw.filter(item => item.productId === "uvp-gb28181" && item.version === version);
  const notes = [];
  for (const entry of entries) {
    for (const note of entry.notes ?? []) {
      if (!notes.includes(note)) notes.push(note);
    }
  }
  return notes.map(note => `- ${note}`).join("\n");
}

async function readExistingPointer(pointerPath) {
  let bytes;
  try {
    bytes = await readFile(pointerPath);
  } catch (error) {
    if (error && typeof error === "object" && error.code === "ENOENT") return undefined;
    throw error;
  }
  const envelope = JSON.parse(bytes.toString("utf8"));
  if (typeof envelope.payload !== "string") fail(`通道指针格式不符：${pointerPath}`);
  const payload = JSON.parse(Buffer.from(envelope.payload, "base64").toString("utf8"));
  if (!Number.isSafeInteger(payload.sequence) || payload.sequence < 1) {
    fail(`通道指针 sequence 无效：${pointerPath}`);
  }
  return payload;
}

async function writeAtomic(path, bytes) {
  await mkdir(dirname(path), { recursive: true });
  const temporary = `${path}.tmp-${process.pid}`;
  try {
    await writeFile(temporary, bytes, { flag: "wx", mode: 0o644 });
    await rename(temporary, path);
  } finally {
    await rm(temporary, { force: true });
  }
}

// 已发布的版本清单是不可变的。同一版本重复发布必须字节一致，否则说明包被
// 重新构建过——那会静默改变已经在客户端的校验结果，必须人工处理。
async function writeImmutableRelease(path, bytes, dryRun) {
  let existing;
  try {
    existing = await readFile(path);
  } catch (error) {
    if (!error || error.code !== "ENOENT") throw error;
  }
  if (existing !== undefined) {
    if (createHash("sha256").update(existing).digest("hex") === createHash("sha256").update(bytes).digest("hex")) {
      return "unchanged";
    }
    fail(`${path} 已存在且内容不同。同一版本号不允许重新签名，请提升版本号。`);
  }
  if (!dryRun) await writeAtomic(path, bytes);
  return "written";
}

async function verifyRemoteSize(url, expectedSize) {
  const response = await fetch(url, { method: "HEAD", redirect: "follow" });
  if (!response.ok) fail(`下载地址不可用（HTTP ${response.status}）：${url}`);
  const length = Number(response.headers.get("content-length"));
  if (!Number.isSafeInteger(length) || length !== expectedSize) {
    fail(`下载地址上的文件大小 ${length} 与本地安装包 ${expectedSize} 不一致：${url}`);
  }
}

export async function runCli(argv, { log = console.log } = {}) {
  const { values, flags } = parseArguments(argv);
  const dryRun = flags.has("--dry-run");
  const refresh = flags.has("--refresh");

  const siteDir = resolve(requireValue(values, "--site-dir"));
  const privateKey = loadEd25519PrivateKey(await readFile(requireValue(values, "--private-key"), "utf8"));
  const keyId = requireValue(values, "--key-id");
  const channel = values.get("--channel") ?? "stable";
  if (channel !== "stable" && channel !== "beta") fail(`不支持的通道：${channel}`);

  const baseURL = normalizeBaseURL(values.get("--base-url") ?? DEFAULT_BASE_URL, "--base-url");
  const channelPath = join(siteDir, "channels", channel, "manifest.json");
  const existing = await readExistingPointer(channelPath);
  const sequence = values.has("--sequence")
    ? Number(values.get("--sequence"))
    : (existing?.sequence ?? 0) + 1;
  if (!Number.isSafeInteger(sequence) || sequence < 1) fail("--sequence 必须是正整数");
  if (existing && sequence <= existing.sequence) {
    fail(`通道序号必须递增：现有 ${existing.sequence}，本次 ${sequence}`);
  }

  const publishedAt = rfc3339(new Date());
  const expiresDays = Number(values.get("--expires-days") ?? 90);
  if (!Number.isFinite(expiresDays) || expiresDays <= 0) fail("--expires-days 必须是正数");
  const expiresAt = rfc3339(new Date(Date.now() + expiresDays * 86400_000));

  if (refresh) {
    if (!existing) fail("没有可续期的通道指针");
    const pointer = buildSignedChannel({
      release: { channel, sequence, release_id: existing.release_id },
      manifestURL: existing.manifest_url,
      manifestSHA256: existing.manifest_sha256,
      publishedAt,
      expiresAt,
      channelSequence: sequence,
      privateKey,
      keyId
    });
    if (!dryRun) await writeAtomic(channelPath, pointer.envelopeBytes);
    log(`通道指针已续期：sequence ${existing.sequence} -> ${sequence}`);
    log(`  生效至 ${expiresAt}`);
    log(`  文件   ${channelPath}`);
    return;
  }

  const version = values.get("--version") ?? (await readFile(join(REPO_ROOT, "VERSION"), "utf8")).trim();
  if (!VERSION_PATTERN.test(version)) fail(`版本号格式不正确：${version}`);
  const releaseID = values.get("--release-id") ?? version;
  const artifactDir = resolve(values.get("--artifact-dir") ?? join(REPO_ROOT, "release-output", version));
  const downloadBase = normalizeBaseURL(
    values.get("--download-base") ?? `${DEFAULT_DOWNLOAD_BASE}/v${version}`,
    "--download-base"
  );

  const { linux, windows, ignored } = await discoverArtifacts(artifactDir, {
    includeWindows: flags.has("--include-windows")
  });

  const localVersions = new Set();
  const artifactSpecs = [];
  const planned = [];
  const deployments = values.has("--deployments")
    ? values.get("--deployments").split(",").map(item => item.trim()).filter(Boolean)
    : DEFAULT_LINUX_DEPLOYMENTS;

  const addPackage = async (filename, os, arch, deploymentList, { readVersion = true } = {}) => {
    const filePath = join(artifactDir, filename);
    const info = await stat(filePath);
    if (readVersion) {
      const versionJSON = readPackageVersionJSON(filePath);
      if (versionJSON.version !== version) {
        fail(`${filename} 内版本为 ${versionJSON.version}，与清单版本 ${version} 不一致`);
      }
      localVersions.add(versionJSON.buildID);
    }
    const url = joinURL(downloadBase, filename);
    for (const deployment of deploymentList) {
      artifactSpecs.push(`${deployment}/${os}/${arch}|${filePath}|${url}`);
    }
    planned.push({ filename, size: info.size, url });
  };

  for (const filename of linux) {
    const match = /^uvp-gb28181-linux-(amd64|arm64)-/.exec(filename);
    await addPackage(filename, "linux", match[1], deployments);
  }
  // Windows 包是 zip，读不了 tar 内的 version.json；构建号由 Linux 包提供。
  for (const filename of windows) {
    const match = /^uvp-gb28181-windows-(amd64|arm64)-/.exec(filename);
    if (!filename.includes(`-${version}.`)) fail(`${filename} 的文件名与版本 ${version} 不一致`);
    await addPackage(filename, "windows", match[1], ["standalone"], { readVersion: false });
  }

  if (localVersions.size !== 1) {
    fail(`各平台安装包的 build_id 不一致：${[...localVersions].join(", ")}`);
  }
  const buildID = values.get("--build-id") ?? [...localVersions][0];
  if (!localVersions.has(buildID)) {
    fail(`--build-id ${buildID} 与安装包内的构建号不一致（包内为 ${[...localVersions][0]}）`);
  }

  const releaseNotes = await readReleaseNotes({ values, version });
  const payload = {
    schema_version: 1,
    channel,
    sequence,
    release_id: releaseID,
    version,
    build_id: buildID,
    published_at: publishedAt,
    min_updater_protocol: 1
  };
  if (releaseNotes) payload.release_notes = releaseNotes;
  if (values.has("--min-version")) {
    const minVersion = values.get("--min-version");
    if (!VERSION_PATTERN.test(minVersion)) fail(`--min-version 格式不正确：${minVersion}`);
    payload.min_version = minVersion;
  }

  const signed = await buildSignedManifest({ payload, artifactSpecs, privateKey, keyId });
  const releaseDir = join(siteDir, "releases", releaseID);
  const releaseManifestPath = join(releaseDir, "manifest.json");
  const manifestSHA256 = createHash("sha256").update(signed.envelopeBytes).digest("hex");

  log(`发布 ${version}（build ${buildID}）到 ${channel} 通道`);
  for (const item of planned) {
    log(`  制品 ${item.filename}  ${item.size} 字节  ${item.url}`);
  }
  if (ignored > 0) log(`  跳过 ${ignored} 个未纳入在线升级的包（Windows 需 --include-windows）`);

  if (!flags.has("--no-verify-remote")) {
    for (const item of planned) await verifyRemoteSize(item.url, item.size);
    log("  远程校验：下载地址与本地大小一致");
  }

  const manifestState = await writeImmutableRelease(releaseManifestPath, signed.envelopeBytes, dryRun);
  const manifestLabel =
    manifestState === "unchanged" ? "已存在且一致" : dryRun ? "待写入（dry-run）" : "已写入";
  log(`  版本清单 ${manifestLabel}  ${releaseManifestPath}`);

  const pointer = buildSignedChannel({
    release: { channel, sequence, release_id: releaseID },
    manifestURL: joinURL(`${baseURL}/releases`, releaseID, "manifest.json"),
    manifestSHA256,
    publishedAt,
    expiresAt,
    channelSequence: sequence,
    privateKey,
    keyId
  });
  if (!dryRun) await writeAtomic(channelPath, pointer.envelopeBytes);
  log(`  通道指针 sequence ${sequence}，生效至 ${expiresAt}`);
  log(`  文件     ${channelPath}`);
  if (dryRun) log("  （dry-run：未写入任何文件）");
  if (!payload.compatibility) {
    log("  说明：本清单未声明自动升级兼容性，仅支持管理员确认后升级。");
  }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  runCli(process.argv.slice(2)).catch(error => {
    console.error(error instanceof Error ? error.message : String(error));
    process.exitCode = 1;
  });
}
