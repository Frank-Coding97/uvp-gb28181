#!/usr/bin/env node
/**
 * Publish a locally built, signed release to a self-hosted static site.
 *
 * This command never contacts a network service. It validates every input
 * first, publishes an immutable release directory, then atomically replaces
 * the signed channel pointer.
 */
import { createHash, createPublicKey, randomUUID, verify } from "node:crypto";
import { copyFile, lstat, mkdir, open, readFile, rename, rm, writeFile } from "node:fs/promises";
import { basename, dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import {
  assertEd25519PrivateKey,
  buildSignedChannel,
  hashFile,
  loadEd25519PrivateKey,
  parseSelector,
  validateChannelPointer,
  validateSignedReleasePayload
} from "./sign-upgrade-manifest.mjs";

const CHANNELS = new Set(["stable", "beta"]);
const TOKEN = /^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/;
const SHA256 = /^[a-f0-9]{64}$/;
const MAX_MANIFEST_ENVELOPE_BYTES = 1 << 20;
const envelopeFields = new Set(["payload", "signatures"]);
const signatureFields = new Set(["key_id", "signature"]);

export const DEFAULT_EXPECTED_TARGETS = Object.freeze([
  "standalone/linux/amd64",
  "systemd/linux/amd64",
  "compose/linux/amd64",
  "standalone/linux/arm64",
  "systemd/linux/arm64",
  "compose/linux/arm64"
]);

function isRecord(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function assertKnownFields(value, allowed, name) {
  for (const key of Object.keys(value)) {
    if (!allowed.has(key)) throw new Error(`${name} 包含不支持的字段 ${key}`);
  }
}

function isJSONWhitespace(character) {
  return character === " " || character === "\n" || character === "\r" || character === "\t";
}

// JSON.parse intentionally accepts duplicate object keys by keeping the last
// one. The updater rejects them, so publication uses this small strict parser.
function parseStrictJSON(bytes, name) {
  if (!Buffer.isBuffer(bytes)) throw new Error(`${name} 必须是 UTF-8 JSON`);
  const source = bytes.toString("utf8");
  if (!Buffer.from(source, "utf8").equals(bytes)) throw new Error(`${name} 不是有效 UTF-8 JSON`);
  let position = 0;

  function fail() {
    throw new Error(`${name} 不是有效且无重复字段的 JSON`);
  }

  function skipWhitespace() {
    while (position < source.length && isJSONWhitespace(source[position])) position += 1;
  }

  function parseString() {
    if (source[position] !== '"') fail();
    const start = position;
    position += 1;
    while (position < source.length) {
      const character = source[position];
      if (character === '"') {
        position += 1;
        try {
          return JSON.parse(source.slice(start, position));
        } catch {
          fail();
        }
      }
      if (character === "\\") {
        position += 1;
        if (position >= source.length) fail();
        position += source[position] === "u" ? 5 : 1;
        continue;
      }
      if (character.charCodeAt(0) < 0x20) fail();
      position += 1;
    }
    fail();
  }

  function parsePrimitive() {
    const start = position;
    while (position < source.length && !isJSONWhitespace(source[position]) && !",]}".includes(source[position])) {
      position += 1;
    }
    if (start === position) fail();
    try {
      return JSON.parse(source.slice(start, position));
    } catch {
      fail();
    }
  }

  function parseArray() {
    const result = [];
    position += 1;
    skipWhitespace();
    if (source[position] === "]") {
      position += 1;
      return result;
    }
    while (true) {
      result.push(parseValue());
      skipWhitespace();
      if (source[position] === "]") {
        position += 1;
        return result;
      }
      if (source[position] !== ",") fail();
      position += 1;
      skipWhitespace();
    }
  }

  function parseObject() {
    const result = Object.create(null);
    const keys = new Set();
    position += 1;
    skipWhitespace();
    if (source[position] === "}") {
      position += 1;
      return result;
    }
    while (true) {
      const key = parseString();
      if (keys.has(key)) throw new Error(`${name} 包含重复字段 ${key}`);
      keys.add(key);
      skipWhitespace();
      if (source[position] !== ":") fail();
      position += 1;
      const value = parseValue();
      Object.defineProperty(result, key, { value, enumerable: true, configurable: true, writable: true });
      skipWhitespace();
      if (source[position] === "}") {
        position += 1;
        return result;
      }
      if (source[position] !== ",") fail();
      position += 1;
      skipWhitespace();
    }
  }

  function parseValue() {
    skipWhitespace();
    switch (source[position]) {
      case "{":
        return parseObject();
      case "[":
        return parseArray();
      case '"':
        return parseString();
      default:
        return parsePrimitive();
    }
  }

  const value = parseValue();
  skipWhitespace();
  if (position !== source.length) fail();
  return value;
}

function decodeBase64(value, name) {
  if (typeof value !== "string" || value === "") throw new Error(`${name} 必须是 Base64`);
  const decoded = Buffer.from(value, "base64");
  if (decoded.length === 0 || decoded.toString("base64") !== value) {
    throw new Error(`${name} 必须是标准 Base64`);
  }
  return decoded;
}

function verifySignedEnvelope(envelopeBytes, privateKey, keyId, name) {
  if (!Buffer.isBuffer(envelopeBytes) || envelopeBytes.length === 0 || envelopeBytes.length > MAX_MANIFEST_ENVELOPE_BYTES) {
    throw new Error(`${name} 签名信封大小无效`);
  }
  assertEd25519PrivateKey(privateKey);
  const envelope = parseStrictJSON(envelopeBytes, `${name} 签名信封`);
  if (!isRecord(envelope)) throw new Error(`${name} 签名信封必须是 JSON 对象`);
  assertKnownFields(envelope, envelopeFields, `${name} 签名信封`);
  if (!Array.isArray(envelope.signatures) || envelope.signatures.length === 0) {
    throw new Error(`${name} 签名信封缺少 signatures`);
  }
  const payloadBytes = decodeBase64(envelope.payload, `${name} payload`);
  if (payloadBytes.length > MAX_MANIFEST_ENVELOPE_BYTES) {
    throw new Error(`${name} payload 超过 1 MiB 限制`);
  }
  const publicKey = createPublicKey(privateKey);
  let trusted = false;
  for (const signature of envelope.signatures) {
    if (!isRecord(signature)) throw new Error(`${name} signature 无效`);
    assertKnownFields(signature, signatureFields, `${name} signature`);
    if (typeof signature.key_id !== "string" || typeof signature.signature !== "string") {
      throw new Error(`${name} signature 无效`);
    }
    if (signature.key_id !== keyId) continue;
    let bytes;
    try {
      bytes = decodeBase64(signature.signature, `${name} signature`);
    } catch {
      continue;
    }
    if (verify(null, payloadBytes, publicKey, bytes)) {
      trusted = true;
      break;
    }
  }
  if (!trusted) throw new Error(`${name} 签名不受提供的 key_id 信任`);
  return { envelope, payloadBytes, payload: parseStrictJSON(payloadBytes, `${name} payload`) };
}

function assertReleaseID(value) {
  if (typeof value !== "string" || !TOKEN.test(value)) throw new Error("release_id 无效");
  return value;
}

function normalizeBaseURL(value) {
  let parsed;
  try {
    parsed = new URL(value);
  } catch {
    throw new Error("baseURL 必须是 HTTPS 地址");
  }
  if (parsed.protocol !== "https:" || !parsed.hostname || parsed.username || parsed.password || parsed.search || parsed.hash) {
    throw new Error("baseURL 必须是不含凭据、查询或片段的 HTTPS 地址");
  }
  return `${parsed.origin}${parsed.pathname.replace(/\/+$/, "")}`;
}

function releaseURL(baseURL, releaseID, filename) {
  return new URL(`releases/${encodeURIComponent(releaseID)}/${encodeURIComponent(filename)}`, `${baseURL}/`).toString();
}

function manifestURL(baseURL, releaseID) {
  return new URL(`releases/${encodeURIComponent(releaseID)}/manifest.json`, `${baseURL}/`).toString();
}

function selectorKey(selector) {
  return `${selector.deployment}\0${selector.os}\0${selector.arch}`;
}

function validateExpectedTargets(artifacts, expectedTargets) {
  if (!Array.isArray(expectedTargets) || expectedTargets.length === 0) {
    throw new Error("expectedTargets 必须是非空 deployment/os/arch 数组");
  }
  const expected = new Map();
  for (const value of expectedTargets) {
    const selector = parseSelector(value);
    const key = selectorKey(selector);
    if (expected.has(key)) throw new Error(`expectedTargets 包含重复目标 ${value}`);
    expected.set(key, value);
  }
  const actual = new Map();
  for (const artifact of artifacts) {
    const selector = parseSelector(`${artifact.deployment}/${artifact.os}/${artifact.arch}`);
    const key = selectorKey(selector);
    if (actual.has(key)) throw new Error(`release manifest 包含重复目标 ${artifact.deployment}/${artifact.os}/${artifact.arch}`);
    actual.set(key, artifact);
    if (!expected.has(key)) {
      throw new Error(`release manifest 包含未声明目标 ${artifact.deployment}/${artifact.os}/${artifact.arch}`);
    }
  }
  for (const [key, value] of expected) {
    if (!actual.has(key)) throw new Error(`release manifest 缺少预期目标 ${value}`);
  }
  return [...expected.values()];
}

function expectedArtifactPaths(artifacts, artifactPaths, baseURL, releaseID) {
  if (!Array.isArray(artifactPaths)) throw new Error("artifactPaths 必须是数组");
  if (artifactPaths.length !== artifacts.length) throw new Error("本地制品数量与签名 release manifest 不一致");
  const supplied = new Map();
  for (const filePath of artifactPaths) {
    if (typeof filePath !== "string" || filePath === "") throw new Error("本地制品路径无效");
    const filename = basename(filePath);
    if (filename === "" || supplied.has(filename)) throw new Error(`本地制品文件名重复或无效: ${filePath}`);
    supplied.set(filename, filePath);
  }
  const files = [];
  const filenames = new Set();
  for (const artifact of artifacts) {
    if (filenames.has(artifact.filename)) throw new Error(`release manifest 制品文件名重复: ${artifact.filename}`);
    filenames.add(artifact.filename);
    const expectedURL = releaseURL(baseURL, releaseID, artifact.filename);
    if (new URL(artifact.url).toString() !== expectedURL) {
      throw new Error(`release manifest 制品 URL 不在本次自建发布目录: ${artifact.filename}`);
    }
    const filePath = supplied.get(artifact.filename);
    if (!filePath) throw new Error(`缺少本地制品: ${artifact.filename}`);
    files.push({ ...artifact, filePath });
  }
  return files;
}

async function verifyArtifactInputs(files) {
  for (const artifact of files) {
    const actual = await hashFile(artifact.filePath);
    if (actual.size !== artifact.size || actual.sha256 !== artifact.sha256) {
      throw new Error(`本地制品 SHA-256 或大小与 release manifest 不一致: ${artifact.filename}`);
    }
  }
}

async function lstatOrUndefined(filePath) {
  try {
    return await lstat(filePath);
  } catch (error) {
    if (error && typeof error === "object" && error.code === "ENOENT") return undefined;
    throw error;
  }
}

async function readExistingChannel(pointerPath, privateKey, keyId, expectedChannel) {
  let bytes;
  try {
    bytes = await readFile(pointerPath);
  } catch (error) {
    if (error && typeof error === "object" && error.code === "ENOENT") return undefined;
    throw error;
  }
  const verified = verifySignedEnvelope(bytes, privateKey, keyId, "现有 channel pointer");
  validateChannelPointer(verified.payload);
  if (verified.payload.channel !== expectedChannel) {
    throw new Error("现有 channel pointer 与发布通道不一致");
  }
  return verified.payload;
}

async function validatePublicationState(plan) {
  const releaseInfo = await lstatOrUndefined(plan.releaseDir);
  if (plan.refresh) {
    if (!releaseInfo?.isDirectory() || releaseInfo.isSymbolicLink()) {
      throw new Error(`无法刷新未发布的 release: ${plan.release.release_id}`);
    }
    const storedManifest = await readFile(plan.releaseManifestPath);
    const storedDigest = createHash("sha256").update(storedManifest).digest("hex");
    if (storedDigest !== plan.manifestSHA256) {
      throw new Error("已发布 release manifest 与待刷新 channel 的摘要不一致");
    }
  } else if (releaseInfo) {
    throw new Error(`release_id already exists and is immutable: ${plan.release.release_id}`);
  }

  const existing = await readExistingChannel(plan.channelPath, plan.privateKey, plan.keyId, plan.release.channel);
  if (existing && plan.channel.payload.sequence <= existing.sequence) {
    throw new Error("channel sequence 必须高于现有 channel pointer");
  }
  return existing;
}

async function publishImmutableRelease(plan) {
  const releasesDir = dirname(plan.releaseDir);
  const stagingDir = `${plan.releaseDir}.staging-${process.pid}-${randomUUID()}`;
  await mkdir(releasesDir, { recursive: true, mode: 0o755 });
  await mkdir(stagingDir, { mode: 0o755 });
  try {
    await writeFile(joinRelease(stagingDir, "manifest.json"), plan.releaseEnvelope, { flag: "wx", mode: 0o644 });
    for (const artifact of plan.files) {
      const destination = joinRelease(stagingDir, artifact.filename);
      await copyFile(artifact.filePath, destination);
      const copied = await hashFile(destination);
      if (copied.size !== artifact.size || copied.sha256 !== artifact.sha256) {
        throw new Error(`制品在发布期间发生变化: ${artifact.filename}`);
      }
    }
    await rename(stagingDir, plan.releaseDir);
  } catch (error) {
    await rm(stagingDir, { recursive: true, force: true });
    throw error;
  }
}

function joinRelease(directory, filename) {
  return `${directory}/${filename}`;
}

async function replaceChannelAtomically(plan) {
  const channelDir = dirname(plan.channelPath);
  const temporary = `${plan.channelPath}.tmp-${process.pid}-${randomUUID()}`;
  await mkdir(channelDir, { recursive: true, mode: 0o755 });
  try {
    await writeFile(temporary, plan.channel.envelopeBytes, { flag: "wx", mode: 0o644 });
    await rename(temporary, plan.channelPath);
  } finally {
    await rm(temporary, { force: true });
  }
}

async function acquirePublishLock(siteDir) {
  const lockPath = `${siteDir}/.uvp-publish.lock`;
  try {
    return { path: lockPath, handle: await open(lockPath, "wx", 0o600) };
  } catch (error) {
    if (error && typeof error === "object" && error.code === "EEXIST") {
      throw new Error("已有本地发布任务正在运行");
    }
    throw error;
  }
}

function resultFor(plan, dryRun) {
  return {
    dryRun,
    release: {
      id: plan.release.release_id,
      directory: plan.releaseDir,
      manifestSHA256: plan.manifestSHA256,
      published: !plan.refresh,
      expectedTargets: plan.expectedTargets
    },
    channel: {
      channel: plan.release.channel,
      sequence: plan.channel.payload.sequence,
      path: plan.channelPath,
      manifestURL: plan.channel.payload.manifest_url
    }
  };
}

async function preparePublication(options) {
  if (!isRecord(options)) throw new Error("发布参数无效");
  if (typeof options.siteDir !== "string" || options.siteDir === "") throw new Error("siteDir 无效");
  if (typeof options.releaseManifestPath !== "string" || options.releaseManifestPath === "") {
    throw new Error("releaseManifestPath 无效");
  }
  if (options.refresh !== undefined && typeof options.refresh !== "boolean") throw new Error("refresh 必须是布尔值");
  if (options.dryRun !== undefined && typeof options.dryRun !== "boolean") throw new Error("dryRun 必须是布尔值");
  const refresh = options.refresh === true;
  const baseURL = normalizeBaseURL(options.baseURL);
  const privateKey = assertEd25519PrivateKey(options.privateKey);
  const releaseEnvelope = await readFile(options.releaseManifestPath);
  const verified = verifySignedEnvelope(releaseEnvelope, privateKey, options.keyId, "release manifest");
  validateSignedReleasePayload(verified.payload);
  const release = verified.payload;
  if (!CHANNELS.has(release.channel)) throw new Error("release channel 无效");
  const releaseID = assertReleaseID(release.release_id);
  const expectedTargets = validateExpectedTargets(release.artifacts, options.expectedTargets);
  const files = refresh
    ? (() => {
        if (!Array.isArray(options.artifactPaths) || options.artifactPaths.length !== 0) {
          throw new Error("refresh 只能更新 channel pointer，artifactPaths 必须为空");
        }
        return [];
      })()
    : expectedArtifactPaths(release.artifacts, options.artifactPaths, baseURL, releaseID);
  if (!refresh) await verifyArtifactInputs(files);

  const manifestSHA256 = createHash("sha256").update(releaseEnvelope).digest("hex");
  const channel = buildSignedChannel({
    release,
    manifestURL: manifestURL(baseURL, releaseID),
    manifestSHA256,
    publishedAt: options.publishedAt,
    expiresAt: options.expiresAt,
    channelSequence: options.channelSequence,
    privateKey,
    keyId: options.keyId
  });
  const siteDir = resolve(options.siteDir);
  return {
    siteDir,
    privateKey,
    keyId: options.keyId,
    refresh,
    dryRun: options.dryRun === true,
    release,
    releaseEnvelope,
    manifestSHA256,
    files,
    expectedTargets,
    channel,
    releaseDir: `${siteDir}/releases/${releaseID}`,
    releaseManifestPath: `${siteDir}/releases/${releaseID}/manifest.json`,
    channelPath: `${siteDir}/channels/${release.channel}/manifest.json`
  };
}

export async function publishLocalRelease(options) {
  const plan = await preparePublication(options);
  if (plan.dryRun) {
    await validatePublicationState(plan);
    return resultFor(plan, true);
  }

  await mkdir(plan.siteDir, { recursive: true, mode: 0o755 });
  const lock = await acquirePublishLock(plan.siteDir);
  try {
    await validatePublicationState(plan);
    if (!plan.refresh) await publishImmutableRelease(plan);
    await replaceChannelAtomically(plan);
    return resultFor(plan, false);
  } finally {
    await lock.handle.close();
    await rm(lock.path, { force: true });
  }
}

function parseArguments(argv) {
  const values = new Map();
  const artifacts = [];
  const targets = [];
  const valueArguments = new Set([
    "--site-dir",
    "--base-url",
    "--release-manifest",
    "--artifact",
    "--private-key",
    "--key-id",
    "--published-at",
    "--expires-at",
    "--channel-sequence",
    "--target"
  ]);
  const flagArguments = new Set(["--refresh", "--dry-run"]);
  for (let index = 0; index < argv.length; index += 1) {
    const name = argv[index];
    if (flagArguments.has(name)) {
      if (values.has(name)) throw new Error(`参数 ${name} 不可重复`);
      values.set(name, true);
      continue;
    }
    if (!valueArguments.has(name)) throw new Error(`不支持的参数 ${name}`);
    const value = argv[index + 1];
    if (!value || value.startsWith("--")) throw new Error(`缺少参数 ${name}`);
    index += 1;
    if (name === "--artifact") {
      artifacts.push(value);
      continue;
    }
    if (name === "--target") {
      targets.push(value);
      continue;
    }
    if (values.has(name)) throw new Error(`参数 ${name} 不可重复`);
    values.set(name, value);
  }
  for (const name of ["--site-dir", "--base-url", "--release-manifest", "--private-key", "--key-id", "--published-at", "--expires-at"]) {
    if (!values.has(name)) throw new Error(`缺少参数 ${name}`);
  }
  let channelSequence;
  if (values.has("--channel-sequence")) {
    const text = values.get("--channel-sequence");
    if (!/^[1-9][0-9]*$/.test(text) || !Number.isSafeInteger(Number(text))) {
      throw new Error("--channel-sequence 必须是正整数");
    }
    channelSequence = Number(text);
  }
  return {
    siteDir: values.get("--site-dir"),
    baseURL: values.get("--base-url"),
    releaseManifestPath: values.get("--release-manifest"),
    artifactPaths: artifacts,
    expectedTargets: targets.length > 0 ? targets : [...DEFAULT_EXPECTED_TARGETS],
    privateKeyPath: values.get("--private-key"),
    keyId: values.get("--key-id"),
    publishedAt: values.get("--published-at"),
    expiresAt: values.get("--expires-at"),
    channelSequence,
    refresh: values.has("--refresh"),
    dryRun: values.has("--dry-run")
  };
}

export async function runCli(argv, { log = console.log } = {}) {
  const options = parseArguments(argv);
  const privateKey = loadEd25519PrivateKey(await readFile(options.privateKeyPath, "utf8"));
  const result = await publishLocalRelease({ ...options, privateKey });
  log(
    result.dryRun
      ? `dry-run 已验证 release ${result.release.id} 和 channel ${result.channel.channel}`
      : `已发布 release ${result.release.id}，channel ${result.channel.channel} sequence ${result.channel.sequence}`
  );
  return result;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  runCli(process.argv.slice(2)).catch(error => {
    console.error(error instanceof Error ? error.message : String(error));
    process.exitCode = 1;
  });
}
