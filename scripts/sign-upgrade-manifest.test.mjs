import assert from "node:assert/strict";
import { createHash, generateKeyPairSync, verify } from "node:crypto";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import {
  assertEd25519PrivateKey,
  buildSignedManifest,
  createOfflinePackage,
  hashFile,
  runCli
} from "./sign-upgrade-manifest.mjs";

const payload = (artifacts = []) => ({
  schema_version: 1,
  channel: "stable",
  sequence: 7,
  release_id: "r-7",
  version: "1.2.3",
  build_id: "build-7",
  min_updater_protocol: 1,
  artifacts
});

async function fixture() {
  const root = await mkdtemp(path.join(os.tmpdir(), "uvp-sign-upgrade-"));
  const artifact = path.join(root, "uvp.tar.gz");
  await writeFile(artifact, "release artifact");
  return { root, artifact };
}

function releaseKey() {
  return generateKeyPairSync("ed25519");
}

function artifactSpec({ selector = "standalone/linux/amd64", filePath, url = "https://updates.example.test/r-7/uvp.tar.gz" }) {
  return `${selector}|${filePath}|${url}`;
}

function automaticCompatibility() {
  return {
    schema_version: 1,
    automatic_allowed: true,
    source_builds: [{ version: "1.1.0", build_id: "installed-build-1" }],
    database: {
      dialect: "sqlite",
      allowed_source_migration_digests: ["a".repeat(64)],
      target_migration_digest: "b".repeat(64)
    },
    rollback: {
      automatic_allowed: true,
      database_compatible: true,
      dependencies_compatible: true
    }
  };
}

function composePayloadWithImageReference(reference) {
  return payload([
    {
      deployment: "compose",
      os: "linux",
      arch: "amd64",
      images: [{ reference, archive: "images/backend.tar" }]
    }
  ]);
}

function tarEntries(data) {
  const entries = [];
  let offset = 0;
  while (offset + 512 <= data.length) {
    const header = data.subarray(offset, offset + 512);
    if (header.every(byte => byte === 0)) break;
    const name = header.subarray(0, 100).toString("utf8").replace(/\0.*$/, "");
    const sizeText = header.subarray(124, 136).toString("utf8").replace(/\0.*$/, "").trim();
    const size = Number.parseInt(sizeText || "0", 8);
    const type = header.subarray(156, 157).toString("utf8") || "0";
    const bodyStart = offset + 512;
    entries.push({
      name,
      type,
      data: data.subarray(bodyStart, bodyStart + size)
    });
    offset = bodyStart + Math.ceil(size / 512) * 512;
  }
  return entries;
}

test("只接受 Ed25519 私钥", () => {
  const release = releaseKey();
  assert.equal(assertEd25519PrivateKey(release.privateKey), release.privateKey);

  const rsa = generateKeyPairSync("rsa", { modulusLength: 2048 });
  assert.throws(() => assertEd25519PrivateKey(rsa.privateKey), /Ed25519/);
  assert.throws(() => assertEd25519PrivateKey(release.publicKey), /私钥/);
});

test("拒绝不安全的 selector、非 HTTPS 地址和重复目标制品", async () => {
  const { root, artifact } = await fixture();
  const { privateKey } = releaseKey();
  try {
    await assert.rejects(
      () =>
        buildSignedManifest({
          payload: payload(),
          artifactSpecs: [
            artifactSpec({
              selector: "compose/linux/amd64/extra",
              filePath: artifact
            })
          ],
          privateKey,
          keyId: "release-2026"
        }),
      /selector/i
    );
    await assert.rejects(
      () =>
        buildSignedManifest({
          payload: payload(),
          artifactSpecs: [
            artifactSpec({
              filePath: artifact,
              url: "http://updates.example.test/uvp.tar.gz"
            })
          ],
          privateKey,
          keyId: "release-2026"
        }),
      /HTTPS/i
    );
    await assert.rejects(
      () =>
        buildSignedManifest({
          payload: payload(),
          artifactSpecs: [artifactSpec({ filePath: artifact }), artifactSpec({ filePath: artifact })],
          privateKey,
          keyId: "release-2026"
        }),
      /重复/
    );
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("保留并校验匹配 compose selector 的镜像元数据", async () => {
  const { root, artifact } = await fixture();
  const { privateKey } = releaseKey();
  const images = [
    {
      reference: "registry.example.test/uvp/backend:1.2.3",
      archive: "images/backend.tar",
      digest: `sha256:${"a".repeat(64)}`,
      config_id: `sha256:${"b".repeat(64)}`
    }
  ];
  try {
    const compatibility = automaticCompatibility();
    const result = await buildSignedManifest({
      payload: {
        ...payload([{ deployment: "compose", os: "linux", arch: "amd64", images }]),
        compatibility
      },
      artifactSpecs: [artifactSpec({ selector: "compose/linux/amd64", filePath: artifact })],
      privateKey,
      keyId: "release-2026"
    });
    assert.deepEqual(result.payload.artifacts[0].images, images);
    assert.deepEqual(result.payload.compatibility, compatibility);

    await assert.rejects(
      () =>
        buildSignedManifest({
          payload: {
            ...payload([{ deployment: "compose", os: "linux", arch: "amd64", images }]),
            migration_compatibility: { backup_required: true }
          },
          artifactSpecs: [artifactSpec({ selector: "compose/linux/amd64", filePath: artifact })],
          privateKey,
          keyId: "release-2026"
        }),
      /不支持/
    );

    await assert.rejects(
      () =>
        buildSignedManifest({
          payload: payload([
            {
              deployment: "compose",
              os: "linux",
              arch: "amd64",
              images: [{ ...images[0], archive: "../images/backend.tar" }]
            }
          ]),
          artifactSpecs: [
            artifactSpec({
              selector: "compose/linux/amd64",
              filePath: artifact
            })
          ],
          privateKey,
          keyId: "release-2026"
        }),
      /archive/i
    );
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("拒绝无效的自动升级兼容声明", async () => {
  const { root, artifact } = await fixture();
  const { privateKey } = releaseKey();
  try {
    await assert.rejects(
      () =>
        buildSignedManifest({
          payload: {
            ...payload(),
            compatibility: { ...automaticCompatibility(), unexpected: true }
          },
          artifactSpecs: [artifactSpec({ filePath: artifact })],
          privateKey,
          keyId: "release-2026"
        }),
      /不支持的字段/
    );
    await assert.rejects(
      () =>
        buildSignedManifest({
          payload: {
            ...payload(),
            compatibility: {
              ...automaticCompatibility(),
              database: {
                ...automaticCompatibility().database,
                target_migration_digest: "not-a-digest"
              }
            }
          },
          artifactSpecs: [artifactSpec({ filePath: artifact })],
          privateKey,
          keyId: "release-2026"
        }),
      /target_migration_digest/
    );
    await assert.rejects(
      () =>
        buildSignedManifest({
          payload: {
            ...payload(),
            compatibility: {
              ...automaticCompatibility(),
              database: {
                ...automaticCompatibility().database,
                dialect: "postgres"
              }
            }
          },
          artifactSpecs: [artifactSpec({ filePath: artifact })],
          privateKey,
          keyId: "release-2026"
        }),
      /dialect/
    );
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("拒绝 payload 中重复 selector 和错误的镜像摘要", async () => {
  const { root, artifact } = await fixture();
  const { privateKey } = releaseKey();
  const composeArtifact = { deployment: "compose", os: "linux", arch: "amd64" };
  try {
    await assert.rejects(
      () =>
        buildSignedManifest({
          payload: payload([composeArtifact, composeArtifact]),
          artifactSpecs: [artifactSpec({ selector: "compose/linux/amd64", filePath: artifact })],
          privateKey,
          keyId: "release-2026"
        }),
      /重复/
    );
    await assert.rejects(
      () =>
        buildSignedManifest({
          payload: payload([
            {
              ...composeArtifact,
              images: [{ reference: "registry.example.test/uvp:1.2.3", archive: "images/uvp.tar", digest: "sha256:not-a-digest" }]
            }
          ]),
          artifactSpecs: [artifactSpec({ selector: "compose/linux/amd64", filePath: artifact })],
          privateKey,
          keyId: "release-2026"
        }),
      /SHA-256/
    );
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("拒绝后端不会接受的发布时间、说明与签名清单大小", async () => {
  const { root, artifact } = await fixture();
  const { privateKey } = releaseKey();
  try {
    await assert.rejects(
      () =>
        buildSignedManifest({
          payload: { ...payload(), published_at: "tomorrow" },
          artifactSpecs: [artifactSpec({ filePath: artifact })],
          privateKey,
          keyId: "release-2026"
        }),
      /published_at/
    );
    await assert.rejects(
      () =>
        buildSignedManifest({
          payload: { ...payload(), release_notes: "x".repeat((256 << 10) + 1) },
          artifactSpecs: [artifactSpec({ filePath: artifact })],
          privateKey,
          keyId: "release-2026"
        }),
      /release_notes/
    );
    await assert.rejects(
      () =>
        buildSignedManifest({
          payload: composePayloadWithImageReference(`registry.example.test/${"x".repeat(1024 * 1024)}`),
          artifactSpecs: [artifactSpec({ selector: "compose/linux/amd64", filePath: artifact })],
          privateKey,
          keyId: "release-2026"
        }),
      /payload/
    );
    await assert.rejects(
      () =>
        buildSignedManifest({
          payload: composePayloadWithImageReference(`registry.example.test/${"x".repeat(780 * 1024)}`),
          artifactSpecs: [artifactSpec({ selector: "compose/linux/amd64", filePath: artifact })],
          privateKey,
          keyId: "release-2026"
        }),
      /签名清单/
    );
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("制品摘要通过流式读取计算", async () => {
  const { root, artifact } = await fixture();
  const body = Buffer.alloc(3 * 1024 * 1024 + 17, 0x5a);
  await writeFile(artifact, body);
  try {
    assert.deepEqual(await hashFile(artifact), {
      size: body.length,
      sha256: createHash("sha256").update(body).digest("hex")
    });
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("离线输出遵守 ImportArchive 的 manifest 加单制品 tar 契约", async () => {
  const { root, artifact } = await fixture();
  const { publicKey, privateKey } = releaseKey();
  const offline = path.join(root, "uvp-offline.tar");
  try {
    const result = await buildSignedManifest({
      payload: payload(),
      artifactSpecs: [artifactSpec({ filePath: artifact })],
      privateKey,
      keyId: "release-2026"
    });
    await createOfflinePackage({
      outputPath: offline,
      envelopeBytes: result.envelopeBytes,
      artifact: result.artifacts[0]
    });

    const entries = tarEntries(await readFile(offline));
    assert.deepEqual(
      entries.map(entry => [entry.name, entry.type]),
      [
        ["manifest.json", "0"],
        ["uvp.tar.gz", "0"]
      ]
    );
    assert.deepEqual(entries[1].data, await readFile(artifact));

    const envelope = JSON.parse(entries[0].data.toString("utf8"));
    const signedPayload = Buffer.from(envelope.payload, "base64");
    assert.equal(verify(null, signedPayload, publicKey, Buffer.from(envelope.signatures[0].signature, "base64")), true);
    assert.equal(JSON.parse(signedPayload).artifacts[0].sha256, result.artifacts[0].sha256);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("离线打包期间制品被替换时拒绝写出不匹配的签名包", async () => {
  const { root, artifact } = await fixture();
  const { privateKey } = releaseKey();
  try {
    const result = await buildSignedManifest({
      payload: payload(),
      artifactSpecs: [artifactSpec({ filePath: artifact })],
      privateKey,
      keyId: "release-2026"
    });
    await writeFile(artifact, "forged! artifact");

    await assert.rejects(
      () =>
        createOfflinePackage({
          outputPath: path.join(root, "offline.tar"),
          envelopeBytes: result.envelopeBytes,
          artifact: result.artifacts[0]
        }),
      /发生变化/
    );
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("CLI 同时生成签名清单和单制品离线包", async () => {
  const { root, artifact } = await fixture();
  const { privateKey } = releaseKey();
  const payloadPath = path.join(root, "release.json");
  const privateKeyPath = path.join(root, "release.pem");
  const manifestPath = path.join(root, "manifest.json");
  const offlinePath = path.join(root, "offline.tar");
  try {
    await writeFile(payloadPath, JSON.stringify(payload()));
    await writeFile(privateKeyPath, privateKey.export({ format: "pem", type: "pkcs8" }));
    await runCli(
      [
        "--payload",
        payloadPath,
        "--key-id",
        "release-2026",
        "--private-key",
        privateKeyPath,
        "--artifact",
        artifactSpec({ filePath: artifact }),
        "--out",
        manifestPath,
        "--offline-out",
        offlinePath
      ],
      { log: () => {} }
    );

    const manifestBytes = await readFile(manifestPath);
    assert.ok(JSON.parse(manifestBytes).signatures[0].signature);
    const entries = tarEntries(await readFile(offlinePath));
    assert.deepEqual(
      entries.map(entry => entry.name),
      ["manifest.json", "uvp.tar.gz"]
    );
    assert.deepEqual(entries[0].data, manifestBytes);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
