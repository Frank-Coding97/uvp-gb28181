import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { generateKeyPairSync, verify } from "node:crypto";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import { runCli } from "./publish-upgrade-channel.mjs";

const silent = { log: () => {} };

function verifyEnvelope(bytes, publicKey) {
  const envelope = JSON.parse(bytes.toString("utf8"));
  const payload = Buffer.from(envelope.payload, "base64");
  const signature = Buffer.from(envelope.signatures[0].signature, "base64");
  assert.ok(verify(null, payload, publicKey, signature), "envelope signature is not valid");
  return JSON.parse(payload.toString("utf8"));
}

async function fixture() {
  const root = await mkdtemp(path.join(os.tmpdir(), "uvp-channel-publish-"));
  const { privateKey, publicKey } = generateKeyPairSync("ed25519");
  const keyPath = path.join(root, "release.pem");
  await writeFile(keyPath, privateKey.export({ format: "pem", type: "pkcs8" }), { mode: 0o600 });

  const packageName = "uvp-gb28181-linux-amd64-9.9.9";
  const packageDir = path.join(root, packageName);
  await mkdir(packageDir, { recursive: true });
  await writeFile(
    path.join(packageDir, "version.json"),
    JSON.stringify({ name: "uvp-gb28181", version: "9.9.9", build_id: "9.9.9-abc1234" })
  );
  const artifactDir = path.join(root, "release-output");
  await mkdir(artifactDir, { recursive: true });
  execFileSync("tar", ["-czf", path.join(artifactDir, `${packageName}.tar.gz`), "-C", root, packageName]);

  return {
    root,
    keyPath,
    publicKey,
    artifactDir,
    siteDir: path.join(root, "site"),
    args: [
      "--version", "9.9.9",
      "--artifact-dir", artifactDir,
      "--site-dir", path.join(root, "site"),
      "--private-key", keyPath,
      "--key-id", "test-key",
      "--download-base", "https://dl.example.test/v9.9.9",
      "--base-url", "https://updates.example.test/upgrades",
      "--no-verify-remote"
    ]
  };
}

test("发布时同时写出签名清单与通道指针", async () => {
  const f = await fixture();
  try {
    await runCli(f.args, silent);

    const pointerPayload = verifyEnvelope(
      await readFile(path.join(f.siteDir, "channels", "stable", "manifest.json")),
      f.publicKey
    );
    assert.equal(pointerPayload.channel, "stable");
    assert.equal(pointerPayload.sequence, 1);
    assert.equal(pointerPayload.release_id, "9.9.9");
    assert.equal(pointerPayload.manifest_url, "https://updates.example.test/upgrades/releases/9.9.9/manifest.json");

    const releasePayload = verifyEnvelope(
      await readFile(path.join(f.siteDir, "releases", "9.9.9", "manifest.json")),
      f.publicKey
    );
    assert.equal(releasePayload.version, "9.9.9");
    assert.equal(releasePayload.build_id, "9.9.9-abc1234");
    assert.deepEqual(
      releasePayload.artifacts.map(item => `${item.deployment}/${item.os}/${item.arch}`).sort(),
      ["compose/linux/amd64", "standalone/linux/amd64", "systemd/linux/amd64"]
    );
    for (const artifact of releasePayload.artifacts) {
      assert.equal(artifact.url, "https://dl.example.test/v9.9.9/uvp-gb28181-linux-amd64-9.9.9.tar.gz");
      assert.match(artifact.sha256, /^[a-f0-9]{64}$/);
    }
  } finally {
    await rm(f.root, { recursive: true, force: true });
  }
});

test("同一版本重复发布必须拒绝", async () => {
  const f = await fixture();
  try {
    await runCli(f.args, silent);
    await assert.rejects(() => runCli(f.args, silent), /已存在且内容不同/);
  } finally {
    await rm(f.root, { recursive: true, force: true });
  }
});

test("通道指针序号必须递增", async () => {
  const f = await fixture();
  try {
    await runCli(f.args, silent);
    await assert.rejects(
      () => runCli([...f.args, "--sequence", "1"], silent),
      /通道序号必须递增/
    );
  } finally {
    await rm(f.root, { recursive: true, force: true });
  }
});

test("续期只换指针、不动版本清单", async () => {
  const f = await fixture();
  try {
    await runCli(f.args, silent);
    const releasePath = path.join(f.siteDir, "releases", "9.9.9", "manifest.json");
    const before = await readFile(releasePath);
    const beforePointer = verifyEnvelope(
      await readFile(path.join(f.siteDir, "channels", "stable", "manifest.json")),
      f.publicKey
    );

    await runCli([...f.args, "--refresh"], silent);

    const after = await readFile(releasePath);
    assert.deepEqual(after, before, "续期不应改动已发布的版本清单");
    const pointer = verifyEnvelope(
      await readFile(path.join(f.siteDir, "channels", "stable", "manifest.json")),
      f.publicKey
    );
    assert.equal(pointer.sequence, 2);
    assert.equal(pointer.manifest_url, beforePointer.manifest_url);
    assert.equal(pointer.manifest_sha256, beforePointer.manifest_sha256);
  } finally {
    await rm(f.root, { recursive: true, force: true });
  }
});

test("包内版本与清单版本不一致时拒绝发布", async () => {
  const f = await fixture();
  try {
    await assert.rejects(() => runCli([...f.args, "--version", "9.9.8"], silent), /不一致|没有 Linux 安装包/);
  } finally {
    await rm(f.root, { recursive: true, force: true });
  }
});
