import assert from "node:assert/strict";
import { createHash, generateKeyPairSync, verify } from "node:crypto";
import { access, mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import { buildSignedChannel, buildSignedManifest } from "./sign-upgrade-manifest.mjs";
import { DEFAULT_EXPECTED_TARGETS, publishLocalRelease as publishRelease, runCli } from "./publish-upgrade-release.mjs";

const baseURL = "https://updates.example.test/uvp-gb28181";
const oneTarget = ["standalone/linux/amd64"];

function publishLocalRelease(options) {
  return publishRelease({ ...options, expectedTargets: options.expectedTargets ?? oneTarget });
}

async function fixture() {
  const root = await mkdtemp(path.join(os.tmpdir(), "uvp-local-publish-"));
  const artifact = path.join(root, "uvp.tar.gz");
  await writeFile(artifact, "release artifact");
  return { root, artifact, site: path.join(root, "site") };
}

function payload() {
  return {
    schema_version: 1,
    channel: "stable",
    sequence: 7,
    release_id: "r-7",
    version: "1.2.3",
    build_id: "build-7",
    min_updater_protocol: 1,
    artifacts: []
  };
}

function artifactSpec(filePath) {
  return `standalone/linux/amd64|${filePath}|${baseURL}/releases/r-7/uvp.tar.gz`;
}

function matrixArtifactSpec(selector, filePath) {
  return `${selector}|${filePath}|${baseURL}/releases/r-7/${path.basename(filePath)}`;
}

async function matrixArtifacts(root, buildID) {
  const artifacts = [];
  for (const selector of DEFAULT_EXPECTED_TARGETS) {
    const filename = `${selector.replaceAll("/", "-")}.tar.gz`;
    const filePath = path.join(root, filename);
    await writeFile(filePath, `${buildID}:${selector}`);
    artifacts.push({ selector, filePath });
  }
  return artifacts;
}

test("signs a channel pointer bound to an immutable release manifest", () => {
  const { publicKey, privateKey } = generateKeyPairSync("ed25519");
  const release = Buffer.from("signed release envelope");
  const result = buildSignedChannel({
    release: { channel: "stable", sequence: 7, release_id: "r-7" },
    manifestURL: `${baseURL}/releases/r-7/manifest.json`,
    manifestSHA256: createHash("sha256").update(release).digest("hex"),
    publishedAt: "2026-10-16T00:00:00Z",
    expiresAt: "2026-10-17T00:00:00Z",
    privateKey,
    keyId: "release-2026"
  });

  const envelope = JSON.parse(result.envelopeBytes);
  const signed = Buffer.from(envelope.payload, "base64");
  assert.equal(verify(null, signed, publicKey, Buffer.from(envelope.signatures[0].signature, "base64")), true);
  assert.deepEqual(JSON.parse(signed), {
    schema_version: 1,
    channel: "stable",
    sequence: 7,
    release_id: "r-7",
    published_at: "2026-10-16T00:00:00Z",
    expires_at: "2026-10-17T00:00:00Z",
    manifest_url: `${baseURL}/releases/r-7/manifest.json`,
    manifest_sha256: createHash("sha256").update(release).digest("hex")
  });
  assert.throws(
    () =>
      buildSignedChannel({
        release: { channel: "stable", sequence: 7, release_id: "r-7" },
        manifestURL: `${baseURL}/releases/r-7/manifest.json`,
        manifestSHA256: createHash("sha256").update(release).digest("hex"),
        publishedAt: "2026-10-17T00:00:00Z",
        expiresAt: "2026-10-16T00:00:00Z",
        privateKey,
        keyId: "release-2026"
      }),
    /expiry/i
  );
});

test("requires an explicit release target matrix", async () => {
  const { root, artifact, site } = await fixture();
  const { privateKey } = generateKeyPairSync("ed25519");
  try {
    const signed = await buildSignedManifest({
      payload: payload(),
      artifactSpecs: [artifactSpec(artifact)],
      privateKey,
      keyId: "release-2026"
    });
    const manifest = path.join(root, "manifest.json");
    await writeFile(manifest, signed.envelopeBytes);
    const options = {
      siteDir: site,
      baseURL,
      releaseManifestPath: manifest,
      artifactPaths: [artifact],
      privateKey,
      keyId: "release-2026",
      publishedAt: "2026-10-16T00:00:00Z",
      expiresAt: "2026-10-17T00:00:00Z",
      dryRun: true
    };
    await assert.rejects(() => publishRelease(options), /expectedTargets/);
    await assert.rejects(() => publishRelease({ ...options, expectedTargets: DEFAULT_EXPECTED_TARGETS }), /缺少预期目标/);
    const plan = await publishLocalRelease(options);
    assert.deepEqual(plan.release.expectedTargets, oneTarget);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("CLI defaults to the full matrix and requires an explicit target to publish a limited fixture", async () => {
  const { root, artifact, site } = await fixture();
  const { privateKey } = generateKeyPairSync("ed25519");
  try {
    const signed = await buildSignedManifest({
      payload: payload(),
      artifactSpecs: [artifactSpec(artifact)],
      privateKey,
      keyId: "release-2026"
    });
    const manifest = path.join(root, "manifest.json");
    const privateKeyPath = path.join(root, "release.pem");
    await writeFile(manifest, signed.envelopeBytes);
    await writeFile(privateKeyPath, privateKey.export({ format: "pem", type: "pkcs8" }));
    const cliArguments = [
      "--site-dir",
      site,
      "--base-url",
      baseURL,
      "--release-manifest",
      manifest,
      "--artifact",
      artifact,
      "--private-key",
      privateKeyPath,
      "--key-id",
      "release-2026",
      "--published-at",
      "2026-10-16T00:00:00Z",
      "--expires-at",
      "2026-10-17T00:00:00Z",
      "--dry-run"
    ];
    await assert.rejects(() => runCli(cliArguments, { log: () => {} }), /缺少预期目标/);
    const plan = await runCli([...cliArguments, "--target", oneTarget[0]], { log: () => {} });
    assert.equal(plan.dryRun, true);
    await assert.rejects(() => access(site));
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("full release matrix rejects a missing target and a stale artifact before publishing", async () => {
  const { root, site } = await fixture();
  const { privateKey } = generateKeyPairSync("ed25519");
  try {
    const artifacts = await matrixArtifacts(root, "build-7");
    const manifest = path.join(root, "manifest.json");
    const options = {
      siteDir: site,
      baseURL,
      releaseManifestPath: manifest,
      artifactPaths: artifacts.map(artifact => artifact.filePath),
      expectedTargets: DEFAULT_EXPECTED_TARGETS,
      privateKey,
      keyId: "release-2026",
      publishedAt: "2026-10-16T00:00:00Z",
      expiresAt: "2026-10-17T00:00:00Z",
      dryRun: true
    };

    const incomplete = await buildSignedManifest({
      payload: payload(),
      artifactSpecs: artifacts.slice(0, -1).map(artifact => matrixArtifactSpec(artifact.selector, artifact.filePath)),
      privateKey,
      keyId: "release-2026"
    });
    await writeFile(manifest, incomplete.envelopeBytes);
    await assert.rejects(() => publishRelease(options), /缺少预期目标/);

    const complete = await buildSignedManifest({
      payload: payload(),
      artifactSpecs: artifacts.map(artifact => matrixArtifactSpec(artifact.selector, artifact.filePath)),
      privateKey,
      keyId: "release-2026"
    });
    await writeFile(manifest, complete.envelopeBytes);
    await writeFile(artifacts[4].filePath, `build-6:${artifacts[4].selector}`);
    await assert.rejects(() => publishRelease(options), /SHA-256|大小/);
    await assert.rejects(() => access(site));
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("publishes verified release files before atomically replacing the channel pointer", async () => {
  const { root, artifact, site } = await fixture();
  const { privateKey } = generateKeyPairSync("ed25519");
  try {
    const signed = await buildSignedManifest({
      payload: payload(),
      artifactSpecs: [artifactSpec(artifact)],
      privateKey,
      keyId: "release-2026"
    });
    const manifest = path.join(root, "manifest.json");
    await writeFile(manifest, signed.envelopeBytes);

    const result = await publishLocalRelease({
      siteDir: site,
      baseURL,
      releaseManifestPath: manifest,
      artifactPaths: [artifact],
      privateKey,
      keyId: "release-2026",
      publishedAt: "2026-10-16T00:00:00Z",
      expiresAt: "2026-10-17T00:00:00Z"
    });

    assert.equal(result.dryRun, false);
    assert.deepEqual(await readFile(path.join(site, "releases/r-7/manifest.json")), signed.envelopeBytes);
    assert.deepEqual(await readFile(path.join(site, "releases/r-7/uvp.tar.gz")), await readFile(artifact));
    const pointer = JSON.parse(await readFile(path.join(site, "channels/stable/manifest.json"), "utf8"));
    const channel = JSON.parse(Buffer.from(pointer.payload, "base64"));
    assert.equal(channel.release_id, "r-7");
    assert.equal(channel.manifest_url, `${baseURL}/releases/r-7/manifest.json`);
    assert.equal(channel.manifest_sha256, createHash("sha256").update(signed.envelopeBytes).digest("hex"));
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("digest failure leaves the old channel pointer untouched", async () => {
  const { root, artifact, site } = await fixture();
  const { privateKey } = generateKeyPairSync("ed25519");
  try {
    const signed = await buildSignedManifest({
      payload: payload(),
      artifactSpecs: [artifactSpec(artifact)],
      privateKey,
      keyId: "release-2026"
    });
    const manifest = path.join(root, "manifest.json");
    const pointerPath = path.join(site, "channels/stable/manifest.json");
    await writeFile(manifest, signed.envelopeBytes);
    await mkdir(path.dirname(pointerPath), { recursive: true });
    await writeFile(pointerPath, "old signed pointer");
    await writeFile(artifact, "tampered release artifact");

    await assert.rejects(
      () =>
        publishLocalRelease({
          siteDir: site,
          baseURL,
          releaseManifestPath: manifest,
          artifactPaths: [artifact],
          privateKey,
          keyId: "release-2026",
          publishedAt: "2026-10-16T00:00:00Z",
          expiresAt: "2026-10-17T00:00:00Z"
        }),
      /SHA-256/i
    );
    assert.equal(await readFile(pointerPath, "utf8"), "old signed pointer");
    await assert.rejects(() => access(path.join(site, "releases/r-7")));
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("rejects an untrusted or duplicate-field release envelope before it writes the site", async () => {
  const { root, artifact, site } = await fixture();
  const { privateKey } = generateKeyPairSync("ed25519");
  try {
    const signed = await buildSignedManifest({
      payload: payload(),
      artifactSpecs: [artifactSpec(artifact)],
      privateKey,
      keyId: "release-2026"
    });
    const manifest = path.join(root, "manifest.json");
    const tampered = JSON.parse(signed.envelopeBytes);
    tampered.signatures[0].signature = "A".repeat(tampered.signatures[0].signature.length);
    await writeFile(manifest, JSON.stringify(tampered));

    await assert.rejects(
      () =>
        publishLocalRelease({
          siteDir: site,
          baseURL,
          releaseManifestPath: manifest,
          artifactPaths: [artifact],
          privateKey,
          keyId: "release-2026",
          publishedAt: "2026-10-16T00:00:00Z",
          expiresAt: "2026-10-17T00:00:00Z"
        }),
      /签名/
    );
    await assert.rejects(() => access(site));

    await writeFile(manifest, '{"payload":"aGVsbG8=","payload":"aGVsbG8=","signatures":[]}');
    await assert.rejects(
      () =>
        publishLocalRelease({
          siteDir: site,
          baseURL,
          releaseManifestPath: manifest,
          artifactPaths: [artifact],
          privateKey,
          keyId: "release-2026",
          publishedAt: "2026-10-16T00:00:00Z",
          expiresAt: "2026-10-17T00:00:00Z"
        }),
      /重复字段/
    );
    await assert.rejects(() => access(site));
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("refreshes only the channel pointer with an independently increasing sequence", async () => {
  const { root, artifact, site } = await fixture();
  const { privateKey } = generateKeyPairSync("ed25519");
  try {
    const signed = await buildSignedManifest({
      payload: payload(),
      artifactSpecs: [artifactSpec(artifact)],
      privateKey,
      keyId: "release-2026"
    });
    const manifest = path.join(root, "manifest.json");
    await writeFile(manifest, signed.envelopeBytes);
    await publishLocalRelease({
      siteDir: site,
      baseURL,
      releaseManifestPath: manifest,
      artifactPaths: [artifact],
      privateKey,
      keyId: "release-2026",
      publishedAt: "2026-10-16T00:00:00Z",
      expiresAt: "2026-10-17T00:00:00Z"
    });
    const releasePath = path.join(site, "releases/r-7/manifest.json");
    const releaseBefore = await readFile(releasePath);

    await publishLocalRelease({
      siteDir: site,
      baseURL,
      releaseManifestPath: manifest,
      artifactPaths: [],
      privateKey,
      keyId: "release-2026",
      publishedAt: "2026-10-16T01:00:00Z",
      expiresAt: "2026-10-18T00:00:00Z",
      channelSequence: 8,
      refresh: true
    });

    assert.deepEqual(await readFile(releasePath), releaseBefore);
    const pointer = JSON.parse(await readFile(path.join(site, "channels/stable/manifest.json"), "utf8"));
    assert.equal(JSON.parse(Buffer.from(pointer.payload, "base64")).sequence, 8);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("refuses to overwrite an existing release and keeps dry-run write-free", async () => {
  const { root, artifact, site } = await fixture();
  const { privateKey } = generateKeyPairSync("ed25519");
  try {
    const signed = await buildSignedManifest({
      payload: payload(),
      artifactSpecs: [artifactSpec(artifact)],
      privateKey,
      keyId: "release-2026"
    });
    const manifest = path.join(root, "manifest.json");
    await writeFile(manifest, signed.envelopeBytes);

    const plan = await publishLocalRelease({
      siteDir: site,
      baseURL,
      releaseManifestPath: manifest,
      artifactPaths: [artifact],
      privateKey,
      keyId: "release-2026",
      publishedAt: "2026-10-16T00:00:00Z",
      expiresAt: "2026-10-17T00:00:00Z",
      dryRun: true
    });
    assert.equal(plan.dryRun, true);
    await assert.rejects(() => access(site));

    await publishLocalRelease({
      siteDir: site,
      baseURL,
      releaseManifestPath: manifest,
      artifactPaths: [artifact],
      privateKey,
      keyId: "release-2026",
      publishedAt: "2026-10-16T00:00:00Z",
      expiresAt: "2026-10-17T00:00:00Z"
    });
    await assert.rejects(
      () =>
        publishLocalRelease({
          siteDir: site,
          baseURL,
          releaseManifestPath: manifest,
          artifactPaths: [artifact],
          privateKey,
          keyId: "release-2026",
          publishedAt: "2026-10-16T00:00:00Z",
          expiresAt: "2026-10-17T00:00:00Z"
        }),
      /already exists/i
    );
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
