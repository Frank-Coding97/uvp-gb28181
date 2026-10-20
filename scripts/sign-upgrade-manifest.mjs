#!/usr/bin/env node
/**
 * 生成 UVP 服务升级签名清单和可导入的离线升级包。
 *
 * 用法：node scripts/sign-upgrade-manifest.mjs \
 *   --payload release.json --key-id release-2026 --private-key ed25519.pem \
 *   --artifact 'standalone/linux/amd64|dist/uvp.tar.gz|https://updates.example/uvp.tar.gz' \
 *   --out manifest.json [--offline-out uvp-offline.tar]
 *
 * 私钥只从本地 PEM 文件读取；脚本不会上传文件，也不会访问 GitHub。
 */
import { createHash, createPrivateKey, randomUUID, sign } from "node:crypto";
import { once } from "node:events";
import { createReadStream, createWriteStream } from "node:fs";
import { readFile, rename, rm, stat, writeFile } from "node:fs/promises";
import { basename, resolve } from "node:path";
import { finished } from "node:stream/promises";
import { fileURLToPath } from "node:url";

const DEPLOYMENTS = new Set(["standalone", "systemd", "compose"]);
const TOKEN = /^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/;
const KEY_ID = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/;
const VERSION = /^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-rc\.(0|[1-9][0-9]*))?$/;
const SHA256 = /^[a-f0-9]{64}$/;
const SHA256_REFERENCE = /^sha256:[a-f0-9]{64}$/;
const CHANNELS = new Set(["stable", "beta"]);
const TAR_BLOCK_SIZE = 512;
const MAX_MANIFEST_ENVELOPE_BYTES = 1 << 20;
const MAX_RELEASE_NOTES_BYTES = 256 << 10;
const RFC3339_TIMESTAMP = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d+)?(Z|[+-]\d{2}:\d{2})$/;

const manifestFields = new Set([
  "schema_version",
  "channel",
  "sequence",
  "release_id",
  "version",
  "build_id",
  "published_at",
  "release_notes",
  "min_version",
  "min_updater_protocol",
  "compatibility",
  "artifacts"
]);
const imageFields = new Set(["reference", "archive", "digest", "config_id"]);
const artifactFields = new Set(["deployment", "os", "arch", "filename", "url", "size", "sha256", "images"]);
const compatibilityFields = new Set(["schema_version", "automatic_allowed", "source_builds", "database", "rollback"]);
const releaseIdentityFields = new Set(["version", "build_id"]);
const databaseCompatibilityFields = new Set(["dialect", "allowed_source_migration_digests", "target_migration_digest"]);
const rollbackCompatibilityFields = new Set(["automatic_allowed", "database_compatible", "dependencies_compatible"]);
const channelPointerFields = new Set([
  "schema_version",
  "channel",
  "sequence",
  "release_id",
  "published_at",
  "expires_at",
  "manifest_url",
  "manifest_sha256"
]);
const DATABASE_DIALECTS = new Set(["mysql", "postgresql", "sqlite", "sqlserver"]);

function isRecord(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function selectorKey({ deployment, os, arch }) {
  return `${deployment}\0${os}\0${arch}`;
}

function assertToken(value, name) {
  if (typeof value !== "string" || !TOKEN.test(value)) {
    throw new Error(`${name} 无效`);
  }
  return value;
}

function assertSafeFilename(value) {
  if (
    typeof value !== "string" ||
    value.length === 0 ||
    Buffer.byteLength(value, "utf8") > 512 ||
    value === "." ||
    value === ".." ||
    value.includes("/") ||
    value.includes("\\")
  ) {
    throw new Error("制品文件名必须是安全的单个路径组件");
  }
  return value;
}

function assertSafeArchivePath(value) {
  if (
    typeof value !== "string" ||
    value.length === 0 ||
    value.startsWith("/") ||
    value.includes("\\") ||
    value.split("/").some(part => part === "" || part === "." || part === "..")
  ) {
    throw new Error("compose 镜像 archive 路径无效");
  }
  return value;
}

function assertHttpsURL(value) {
  let parsed;
  try {
    parsed = new URL(value);
  } catch {
    throw new Error("制品 URL 必须是 HTTPS 地址");
  }
  if (parsed.protocol !== "https:" || !parsed.hostname || parsed.username || parsed.password) {
    throw new Error("制品 URL 必须是不含凭据的 HTTPS 地址");
  }
  return value;
}

function assertKnownFields(value, allowed, name) {
  for (const key of Object.keys(value)) {
    if (!allowed.has(key)) throw new Error(`${name} 包含不支持的字段 ${key}`);
  }
}

function isLeapYear(year) {
  return year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
}

function assertRFC3339Timestamp(value) {
  if (value === "") return value;
  if (typeof value !== "string") throw new Error("release published_at 无效");
  const match = RFC3339_TIMESTAMP.exec(value);
  if (!match) throw new Error("release published_at 必须是 RFC3339 时间");
  const [, yearText, monthText, dayText, hourText, minuteText, secondText, zone] = match;
  const year = Number(yearText);
  const month = Number(monthText);
  const day = Number(dayText);
  const hour = Number(hourText);
  const minute = Number(minuteText);
  const second = Number(secondText);
  const monthDays = [31, isLeapYear(year) ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  if (month < 1 || month > 12 || day < 1 || day > monthDays[month - 1] || hour > 23 || minute > 59 || second > 59) {
    throw new Error("release published_at 必须是 RFC3339 时间");
  }
  if (zone !== "Z") {
    const [offsetHour, offsetMinute] = zone.slice(1).split(":").map(Number);
    if (offsetHour > 23 || offsetMinute > 59) throw new Error("release published_at 必须是 RFC3339 时间");
  }
  return value;
}

function assertChannelManifestURL(value) {
  let parsed;
  try {
    parsed = new URL(value);
  } catch {
    throw new Error("channel manifest_url 必须是 HTTPS 地址");
  }
  if (parsed.protocol !== "https:" || !parsed.hostname || parsed.username || parsed.password || parsed.hash) {
    throw new Error("channel manifest_url 必须是不含凭据或片段的 HTTPS 地址");
  }
  return value;
}

export function validateChannelPointer(payload) {
  if (!isRecord(payload)) throw new Error("channel pointer 必须是 JSON 对象");
  assertKnownFields(payload, channelPointerFields, "channel pointer");
  if (payload.schema_version !== 1 || !CHANNELS.has(payload.channel)) {
    throw new Error("channel pointer schema_version 或 channel 无效");
  }
  if (!Number.isSafeInteger(payload.sequence) || payload.sequence < 1) {
    throw new Error("channel pointer sequence 必须为正整数");
  }
  assertToken(payload.release_id, "channel pointer release_id");
  const publishedAt = assertRFC3339Timestamp(payload.published_at);
  const expiresAt = assertRFC3339Timestamp(payload.expires_at);
  if (Date.parse(expiresAt) <= Date.parse(publishedAt)) {
    throw new Error("channel pointer expiry must follow publication time");
  }
  assertChannelManifestURL(payload.manifest_url);
  if (typeof payload.manifest_sha256 !== "string" || !SHA256.test(payload.manifest_sha256)) {
    throw new Error("channel pointer manifest_sha256 无效");
  }
}

function signPayload(payload, privateKey, keyId) {
  if (typeof keyId !== "string" || !KEY_ID.test(keyId)) throw new Error("key_id 无效");
  assertEd25519PrivateKey(privateKey);
  const payloadBytes = Buffer.from(JSON.stringify(payload));
  if (payloadBytes.length === 0 || payloadBytes.length > MAX_MANIFEST_ENVELOPE_BYTES) {
    throw new Error("签名 payload 超过 1 MiB 限制");
  }
  const envelope = {
    payload: payloadBytes.toString("base64"),
    signatures: [
      {
        key_id: keyId,
        signature: sign(null, payloadBytes, privateKey).toString("base64")
      }
    ]
  };
  const envelopeBytes = Buffer.from(JSON.stringify(envelope));
  if (envelopeBytes.length > MAX_MANIFEST_ENVELOPE_BYTES) {
    throw new Error("签名清单超过 1 MiB 限制");
  }
  return { payloadBytes, envelope, envelopeBytes };
}

function validateImages(images, deployment) {
  if (!Array.isArray(images)) throw new Error("compose 镜像 metadata 必须是数组");
  if (deployment !== "compose" && images.length > 0) {
    throw new Error("只有 compose 制品允许镜像 metadata");
  }
  const references = new Set();
  const archives = new Set();
  return images.map((image, index) => {
    if (!isRecord(image)) throw new Error(`compose 镜像 ${index} 无效`);
    assertKnownFields(image, imageFields, `compose 镜像 ${index}`);
    if (typeof image.reference !== "string" || image.reference.trim() === "") {
      throw new Error(`compose 镜像 ${index} reference 无效`);
    }
    const archive = assertSafeArchivePath(image.archive);
    if (references.has(image.reference) || archives.has(archive)) {
      throw new Error(`compose 镜像 ${index} 重复`);
    }
    references.add(image.reference);
    archives.add(archive);
    const normalized = { reference: image.reference, archive };
    for (const field of ["digest", "config_id"]) {
      if (image[field] === undefined || image[field] === "") continue;
      if (typeof image[field] !== "string" || !SHA256_REFERENCE.test(image[field])) {
        throw new Error(`compose 镜像 ${index} ${field} 必须是 SHA-256 标识`);
      }
      normalized[field] = image[field];
    }
    return normalized;
  });
}

function validateCompatibility(value) {
  if (!isRecord(value)) throw new Error("compatibility 必须是 JSON 对象");
  assertKnownFields(value, compatibilityFields, "compatibility");
  if (value.schema_version !== 1) throw new Error("compatibility schema_version 必须为 1");
  if (typeof value.automatic_allowed !== "boolean") {
    throw new Error("compatibility automatic_allowed 必须是布尔值");
  }

  const sourceBuilds = value.source_builds;
  if (sourceBuilds !== undefined && !Array.isArray(sourceBuilds)) {
    throw new Error("compatibility source_builds 必须是数组");
  }
  const sourceKeys = new Set();
  for (const [index, source] of (sourceBuilds ?? []).entries()) {
    if (!isRecord(source)) throw new Error(`compatibility source_builds ${index} 无效`);
    assertKnownFields(source, releaseIdentityFields, `compatibility source_builds ${index}`);
    if (typeof source.version !== "string" || !VERSION.test(source.version)) {
      throw new Error(`compatibility source_builds ${index} version 无效`);
    }
    if (typeof source.build_id !== "string" || source.build_id.trim() === "") {
      throw new Error(`compatibility source_builds ${index} build_id 无效`);
    }
    const key = `${source.version}\0${source.build_id}`;
    if (sourceKeys.has(key)) throw new Error(`compatibility source_builds ${index} 重复`);
    sourceKeys.add(key);
  }

  const database = value.database;
  if (database !== undefined && !isRecord(database)) {
    throw new Error("compatibility database 必须是 JSON 对象");
  }
  if (database !== undefined) {
    assertKnownFields(database, databaseCompatibilityFields, "compatibility database");
    if (!DATABASE_DIALECTS.has(database.dialect)) {
      throw new Error("compatibility database dialect 无效");
    }
    if (!Array.isArray(database.allowed_source_migration_digests) || database.allowed_source_migration_digests.length === 0) {
      throw new Error("compatibility database allowed_source_migration_digests 无效");
    }
    const digests = new Set();
    for (const [index, digest] of database.allowed_source_migration_digests.entries()) {
      if (typeof digest !== "string" || !SHA256.test(digest)) {
        throw new Error(`compatibility database migration digest ${index} 无效`);
      }
      if (digests.has(digest)) throw new Error(`compatibility database migration digest ${index} 重复`);
      digests.add(digest);
    }
    if (typeof database.target_migration_digest !== "string" || !SHA256.test(database.target_migration_digest)) {
      throw new Error("compatibility database target_migration_digest 无效");
    }
  }

  if (value.rollback !== undefined) {
    if (!isRecord(value.rollback)) throw new Error("compatibility rollback 必须是 JSON 对象");
    assertKnownFields(value.rollback, rollbackCompatibilityFields, "compatibility rollback");
    for (const field of rollbackCompatibilityFields) {
      if (typeof value.rollback[field] !== "boolean") {
        throw new Error(`compatibility rollback ${field} 必须是布尔值`);
      }
    }
  }
  if (value.automatic_allowed && ((sourceBuilds ?? []).length === 0 || database === undefined)) {
    throw new Error("自动升级 compatibility 必须声明 source_builds 和 database");
  }
}

function validatePayload(payload) {
  if (!isRecord(payload)) throw new Error("release payload 必须是 JSON 对象");
  assertKnownFields(payload, manifestFields, "release payload");
  if (payload.schema_version !== 1) throw new Error("只支持 schema_version 1");
  if (typeof payload.channel !== "string" || payload.channel === "") throw new Error("release channel 无效");
  if (!Number.isSafeInteger(payload.sequence) || payload.sequence < 1) throw new Error("release sequence 必须为正整数");
  for (const field of ["release_id", "build_id"]) {
    if (typeof payload[field] !== "string" || payload[field].trim() === "") throw new Error(`release ${field} 无效`);
  }
  if (typeof payload.version !== "string" || !VERSION.test(payload.version)) throw new Error("release version 无效");
  if (payload.min_version !== undefined && (typeof payload.min_version !== "string" || !VERSION.test(payload.min_version))) {
    throw new Error("release min_version 无效");
  }
  if (!Number.isSafeInteger(payload.min_updater_protocol) || payload.min_updater_protocol < 1) {
    throw new Error("release min_updater_protocol 必须为正整数");
  }
  if (payload.published_at !== undefined) assertRFC3339Timestamp(payload.published_at);
  if (
    payload.release_notes !== undefined &&
    (typeof payload.release_notes !== "string" || Buffer.byteLength(payload.release_notes, "utf8") > MAX_RELEASE_NOTES_BYTES)
  ) {
    throw new Error("release release_notes 无效或超过 256 KiB");
  }
  if (payload.compatibility !== undefined) validateCompatibility(payload.compatibility);
  if (payload.artifacts !== undefined && !Array.isArray(payload.artifacts)) {
    throw new Error("release artifacts 必须是数组");
  }
}

function sourceArtifactsBySelector(artifacts = []) {
  const result = new Map();
  for (const [index, source] of artifacts.entries()) {
    if (!isRecord(source)) throw new Error(`payload 制品 ${index} 无效`);
    const selector = parseSelector(`${source.deployment}/${source.os}/${source.arch}`);
    const key = selectorKey(selector);
    if (result.has(key)) throw new Error(`payload 包含重复制品 ${source.deployment}/${source.os}/${source.arch}`);
    const metadata = {};
    if (Object.hasOwn(source, "images")) metadata.images = validateImages(source.images, selector.deployment);
    result.set(key, metadata);
  }
  return result;
}

// This validates the final artifact-bearing payload. buildSignedManifest also
// accepts a smaller input form whose artifacts only carry compose metadata,
// so that input cannot use this validator until its files have been hashed.
export function validateSignedReleasePayload(payload) {
  validatePayload(payload);
  if (!Array.isArray(payload.artifacts) || payload.artifacts.length === 0) {
    throw new Error("release artifacts 必须至少包含一个制品");
  }
  const selectors = new Set();
  for (const [index, artifact] of payload.artifacts.entries()) {
    if (!isRecord(artifact)) throw new Error(`release 制品 ${index} 无效`);
    assertKnownFields(artifact, artifactFields, `release 制品 ${index}`);
    const selector = parseSelector(`${artifact.deployment}/${artifact.os}/${artifact.arch}`);
    const key = selectorKey(selector);
    if (selectors.has(key)) {
      throw new Error(`release 制品 ${index} selector 重复`);
    }
    selectors.add(key);
    assertSafeFilename(artifact.filename);
    assertHttpsURL(artifact.url);
    if (!Number.isSafeInteger(artifact.size) || artifact.size < 0) {
      throw new Error(`release 制品 ${index} size 无效`);
    }
    if (typeof artifact.sha256 !== "string" || !SHA256.test(artifact.sha256)) {
      throw new Error(`release 制品 ${index} SHA-256 无效`);
    }
    if (artifact.images !== undefined) validateImages(artifact.images, selector.deployment);
  }
  return payload;
}

export function parseSelector(value) {
  if (typeof value !== "string") throw new Error("制品 selector 无效");
  const parts = value.split("/");
  if (parts.length !== 3) throw new Error("制品 selector 必须是 deployment/os/arch");
  const [deployment, os, arch] = parts;
  if (!DEPLOYMENTS.has(deployment)) throw new Error("制品 selector deployment 无效");
  return {
    deployment,
    os: assertToken(os, "制品 selector OS"),
    arch: assertToken(arch, "制品 selector 架构")
  };
}

export function parseArtifactSpec(value) {
  if (typeof value !== "string") throw new Error("制品参数无效");
  const parts = value.split("|");
  if (parts.length !== 3) throw new Error(`制品参数无效: ${value}`);
  const [selector, filePath, url] = parts;
  if (!filePath) throw new Error(`制品参数无效: ${value}`);
  return { ...parseSelector(selector), filePath, url: assertHttpsURL(url) };
}

export function assertEd25519PrivateKey(key) {
  if (!key || key.type !== "private") throw new Error("必须提供 Ed25519 私钥");
  if (key.asymmetricKeyType !== "ed25519") throw new Error("升级清单仅支持 Ed25519 私钥");
  return key;
}

export function loadEd25519PrivateKey(pem) {
  if (typeof pem !== "string" || !pem.includes("-----BEGIN") || !pem.includes("PRIVATE KEY-----")) {
    throw new Error("私钥必须是 PEM 编码的 Ed25519 私钥");
  }
  try {
    return assertEd25519PrivateKey(createPrivateKey(pem));
  } catch (error) {
    if (error instanceof Error && /Ed25519|私钥/.test(error.message)) throw error;
    throw new Error("无法读取 Ed25519 私钥");
  }
}

// hashFile intentionally never buffers release artifacts. A release can be
// close to the importer's 8 GiB upper bound, so its SHA-256 is streamed.
export async function hashFile(filePath) {
  const info = await stat(filePath);
  if (!info.isFile() || !Number.isSafeInteger(info.size) || info.size < 0) {
    throw new Error(`制品不是可读取的普通文件: ${filePath}`);
  }
  const hash = createHash("sha256");
  for await (const chunk of createReadStream(filePath)) hash.update(chunk);
  return { size: info.size, sha256: hash.digest("hex") };
}

export async function buildSignedManifest({ payload, artifactSpecs, privateKey, keyId }) {
  validatePayload(payload);
  if (typeof keyId !== "string" || !KEY_ID.test(keyId)) throw new Error("key_id 无效");
  assertEd25519PrivateKey(privateKey);
  if (!Array.isArray(artifactSpecs) || artifactSpecs.length === 0) throw new Error("至少需要一个 --artifact");

  const existing = sourceArtifactsBySelector(payload.artifacts ?? []);
  const artifactSelectors = new Set();
  const artifacts = [];
  for (const value of artifactSpecs) {
    const source = parseArtifactSpec(value);
    const key = selectorKey(source);
    if (artifactSelectors.has(key)) {
      throw new Error(`重复制品 selector: ${source.deployment}/${source.os}/${source.arch}`);
    }
    artifactSelectors.add(key);
    const filename = assertSafeFilename(basename(source.filePath));
    const { size, sha256 } = await hashFile(source.filePath);
    const metadata = existing.get(key);
    const artifact = {
      deployment: source.deployment,
      os: source.os,
      arch: source.arch,
      filename,
      url: source.url,
      size,
      sha256
    };
    if (metadata?.images !== undefined) artifact.images = metadata.images;
    artifacts.push({ ...artifact, filePath: source.filePath });
  }

  const signedPayload = {
    ...payload,
    artifacts: artifacts.map(({ filePath, ...artifact }) => artifact)
  };
  validateSignedReleasePayload(signedPayload);
  return { payload: signedPayload, ...signPayload(signedPayload, privateKey, keyId), artifacts };
}

export function buildSignedChannel({
  release,
  manifestURL,
  manifestSHA256,
  publishedAt,
  expiresAt,
  channelSequence,
  privateKey,
  keyId
}) {
  if (!isRecord(release) || !CHANNELS.has(release.channel)) {
    throw new Error("release channel 无效");
  }
  if (!Number.isSafeInteger(release.sequence) || release.sequence < 1) {
    throw new Error("release sequence 必须为正整数");
  }
  assertToken(release.release_id, "release_id");
  const payload = {
    schema_version: 1,
    channel: release.channel,
    sequence: channelSequence === undefined ? release.sequence : channelSequence,
    release_id: release.release_id,
    published_at: publishedAt,
    expires_at: expiresAt,
    manifest_url: manifestURL,
    manifest_sha256: manifestSHA256
  };
  validateChannelPointer(payload);
  return { payload, ...signPayload(payload, privateKey, keyId) };
}

function writeTarNumber(header, offset, length, value) {
  const encoded = `${value.toString(8).padStart(length - 1, "0")}\0`;
  if (Buffer.byteLength(encoded, "ascii") > length) throw new Error("离线制品过大，无法写入 tar");
  header.write(encoded, offset, length, "ascii");
}

// ⛔ mtime 不能写 0。tar 头的 mtime=0 就是 1970-01-01，CentOS 7 的 GNU tar
// 解包时会刷一行「implausibly old time stamp 1970-01-01」（中文即「不可信的旧
// 时间戳」），现场会误判成包坏了。用制品的实际 mtime 填，既真实又稳定。
function tarHeader(name, size, type = "0", mtime = Math.floor(Date.now() / 1000)) {
  if (Buffer.byteLength(name, "utf8") > 100) throw new Error("tar 条目名称过长");
  const header = Buffer.alloc(TAR_BLOCK_SIZE);
  header.write(name, 0, 100, "utf8");
  writeTarNumber(header, 100, 8, 0o644);
  writeTarNumber(header, 108, 8, 0);
  writeTarNumber(header, 116, 8, 0);
  writeTarNumber(header, 124, 12, size);
  writeTarNumber(header, 136, 12, mtime);
  header.fill(0x20, 148, 156);
  header.write(type, 156, 1, "ascii");
  header.write("ustar\0", 257, 6, "ascii");
  header.write("00", 263, 2, "ascii");
  const checksum = header.reduce((sum, byte) => sum + byte, 0);
  header.write(`${checksum.toString(8).padStart(6, "0")}\0 `, 148, 8, "ascii");
  return header;
}

async function writeChunk(output, value) {
  if (!output.write(value)) await once(output, "drain");
}

async function writeTarData(output, source, size, expectedSHA256) {
  let written = 0;
  const hash = expectedSHA256 ? createHash("sha256") : undefined;
  if (Buffer.isBuffer(source)) {
    hash?.update(source);
    await writeChunk(output, source);
    written = source.length;
  } else {
    for await (const chunk of createReadStream(source)) {
      hash?.update(chunk);
      written += chunk.length;
      await writeChunk(output, chunk);
    }
  }
  if (written !== size || (hash && hash.digest("hex") !== expectedSHA256)) {
    throw new Error("制品在打包期间发生变化");
  }
  const padding = (TAR_BLOCK_SIZE - (written % TAR_BLOCK_SIZE)) % TAR_BLOCK_SIZE;
  if (padding > 0) await writeChunk(output, Buffer.alloc(padding));
}

async function writeTarEntry(output, name, source, size, type = "0", expectedSHA256, mtime) {
  if (Buffer.byteLength(name, "utf8") > 100) throw new Error("离线升级包 tar 条目名称不得超过 100 字节");
  await writeChunk(output, tarHeader(name, size, type, mtime));
  await writeTarData(output, source, size, expectedSHA256);
}

// ImportArchive accepts a plain tar with exactly manifest.json and one regular
// artifact. The artifact stream is copied directly into the tar output.
export async function createOfflinePackage({ outputPath, envelopeBytes, artifact }) {
  if (!outputPath || !Buffer.isBuffer(envelopeBytes) || !artifact?.filePath) throw new Error("离线升级包参数无效");
  if (envelopeBytes.length === 0 || envelopeBytes.length > MAX_MANIFEST_ENVELOPE_BYTES) {
    throw new Error("签名清单超过 1 MiB 限制");
  }
  const filename = assertSafeFilename(artifact.filename);
  if (!Number.isSafeInteger(artifact.size) || artifact.size < 0) throw new Error("离线制品大小无效");
  if (typeof artifact.sha256 !== "string" || !SHA256.test(artifact.sha256)) throw new Error("离线制品 SHA-256 无效");
  // 两个条目共用制品 mtime，避免条目之间出现先后不一致。
  let mtime = Math.floor(Date.now() / 1000);
  try {
    const info = await stat(artifact.filePath);
    if (Number.isFinite(info.mtimeMs) && info.mtimeMs > 0) mtime = Math.floor(info.mtimeMs / 1000);
  } catch {
    // 制品读不到时交给下面的流式读取报错，这里保留当前时间即可
  }
  const temporary = `${outputPath}.tmp-${process.pid}-${randomUUID()}`;
  let output;
  let outputFinished;
  try {
    output = createWriteStream(temporary, { flags: "wx", mode: 0o644 });
    outputFinished = finished(output);
    outputFinished.catch(() => {});
    await once(output, "open");
    await writeTarEntry(output, "manifest.json", envelopeBytes, envelopeBytes.length, "0", undefined, mtime);
    await writeTarEntry(output, filename, artifact.filePath, artifact.size, "0", artifact.sha256, mtime);
    await writeChunk(output, Buffer.alloc(TAR_BLOCK_SIZE * 2));
    output.end();
    await outputFinished;
    await rename(temporary, outputPath);
  } catch (error) {
    output?.destroy();
    await outputFinished?.catch(() => {});
    await rm(temporary, { force: true });
    throw error;
  }
}

function parseArguments(argv) {
  const single = new Map();
  const artifacts = [];
  const known = new Set(["--payload", "--key-id", "--private-key", "--artifact", "--out", "--offline-out"]);
  for (let index = 0; index < argv.length; index += 1) {
    const name = argv[index];
    if (!known.has(name)) throw new Error(`不支持的参数 ${name}`);
    const value = argv[index + 1];
    if (!value || value.startsWith("--")) throw new Error(`缺少参数 ${name}`);
    index += 1;
    if (name === "--artifact") {
      artifacts.push(value);
      continue;
    }
    if (single.has(name)) throw new Error(`参数 ${name} 不可重复`);
    single.set(name, value);
  }
  for (const name of ["--payload", "--key-id", "--private-key", "--out"]) {
    if (!single.has(name)) throw new Error(`缺少参数 ${name}`);
  }
  if (artifacts.length === 0) throw new Error("至少需要一个 --artifact");
  return {
    payloadPath: single.get("--payload"),
    keyId: single.get("--key-id"),
    privateKeyPath: single.get("--private-key"),
    artifactSpecs: artifacts,
    outPath: single.get("--out"),
    offlineOutPath: single.get("--offline-out")
  };
}

export async function runCli(argv, { log = console.log } = {}) {
  const options = parseArguments(argv);
  if (options.offlineOutPath && resolve(options.offlineOutPath) === resolve(options.outPath)) {
    throw new Error("--offline-out 不能与 --out 相同");
  }
  const payload = JSON.parse(await readFile(options.payloadPath, "utf8"));
  const privateKey = loadEd25519PrivateKey(await readFile(options.privateKeyPath, "utf8"));
  const result = await buildSignedManifest({
    payload,
    artifactSpecs: options.artifactSpecs,
    privateKey,
    keyId: options.keyId
  });
  if (options.offlineOutPath && result.artifacts.length !== 1) {
    throw new Error("--offline-out 仅支持一个 --artifact；导入器一次只接受一个目标制品");
  }
  await writeFile(options.outPath, result.envelopeBytes, {
    mode: 0o644
  });
  if (options.offlineOutPath) {
    await createOfflinePackage({
      outputPath: options.offlineOutPath,
      envelopeBytes: result.envelopeBytes,
      artifact: result.artifacts[0]
    });
    log(`已生成 ${options.outPath} 和 ${options.offlineOutPath}`);
  } else {
    log(`已生成 ${options.outPath}`);
  }
  return result;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  runCli(process.argv.slice(2)).catch(error => {
    console.error(error instanceof Error ? error.message : String(error));
    process.exitCode = 1;
  });
}
