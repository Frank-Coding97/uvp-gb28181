import assert from "node:assert/strict";
import { mkdtemp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import {
  checkFrontendBuildIdentity,
  checkReleaseMetadata,
  checkVersions,
  readProductVersion,
  syncVersions,
  writeReleaseMetadata
} from "./version.mjs";

async function createFixture(version = "1.2.3") {
  const root = await mkdtemp(path.join(os.tmpdir(), "uvp-version-"));
  await mkdir(path.join(root, "server"), { recursive: true });
  await mkdir(path.join(root, "web"), { recursive: true });
  await writeFile(path.join(root, "VERSION"), `${version}\n`);
  await writeFile(path.join(root, "server/version.json"), JSON.stringify({ name: "uvp-gb28181", version: "0.0.0" }, null, 2));
  await writeFile(path.join(root, "web/version.json"), JSON.stringify({ name: "uvp-gb28181-ui", version: "0.0.0" }, null, 2));
  await writeFile(path.join(root, "web/package.json"), JSON.stringify({ name: "uvp-gb28181-ui", private: true, version: "0.0.0" }, null, 2));
  return root;
}

test("reads the canonical semantic version", async () => {
  const root = await createFixture("2.0.0-rc.3");
  try {
    assert.equal(await readProductVersion(root), "2.0.0-rc.3");
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("rejects unsupported version formats", async () => {
  const root = await createFixture("v1.2.3");
  try {
    await assert.rejects(() => readProductVersion(root), /invalid product version/i);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("synchronizes every derived version while preserving other metadata", async () => {
  const root = await createFixture();
  try {
    await syncVersions(root);
    assert.deepEqual(JSON.parse(await readFile(path.join(root, "server/version.json"), "utf8")), {
      name: "uvp-gb28181",
      version: "1.2.3"
    });
    assert.deepEqual(JSON.parse(await readFile(path.join(root, "web/version.json"), "utf8")), {
      name: "uvp-gb28181-ui",
      version: "1.2.3"
    });
    assert.deepEqual(JSON.parse(await readFile(path.join(root, "web/package.json"), "utf8")), {
      name: "uvp-gb28181-ui",
      private: true,
      version: "1.2.3"
    });
    assert.equal(await checkVersions(root), "1.2.3");
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("fails the gate when a derived version drifts", async () => {
  const root = await createFixture();
  try {
    await assert.rejects(() => checkVersions(root), /version mismatch/i);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("writes release metadata outside the source tree with one build identity", async () => {
  const root = await createFixture();
  const output = path.join(root, "release-metadata");
  try {
    await syncVersions(root);
    assert.deepEqual(await writeReleaseMetadata(root, "build-20261016", output), {
      version: "1.2.3",
      buildID: "build-20261016"
    });
    assert.deepEqual(JSON.parse(await readFile(path.join(output, "server/version.json"), "utf8")), {
      name: "uvp-gb28181",
      version: "1.2.3",
      build_id: "build-20261016"
    });
    assert.deepEqual(JSON.parse(await readFile(path.join(output, "web/version.json"), "utf8")), {
      name: "uvp-gb28181-ui",
      version: "1.2.3",
      build_id: "build-20261016"
    });
    assert.deepEqual(JSON.parse(await readFile(path.join(root, "server/version.json"), "utf8")), {
      name: "uvp-gb28181",
      version: "1.2.3"
    });
    assert.deepEqual(await checkReleaseMetadata(root, "build-20261016", output), {
      version: "1.2.3",
      buildID: "build-20261016"
    });
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("checks the injected build identity in a compiled frontend JavaScript asset", async () => {
  const root = await createFixture();
  const dist = path.join(root, "web", "dist");
  const buildID = "build-20261016-frontend";
  try {
    await syncVersions(root);
    await mkdir(path.join(dist, "static", "js"), { recursive: true });
    await writeFile(path.join(dist, "index.html"), '<script type="module" src="/static/js/app.js"></script>');
    await writeFile(path.join(dist, "static", "js", "app.js"), `document.documentElement.dataset.uvpBuildId = "${buildID}";`);
    await writeFile(path.join(dist, "version.json"), JSON.stringify({ name: "uvp-gb28181-ui", version: "1.2.3", build_id: buildID }));

    assert.deepEqual(await checkFrontendBuildIdentity(root, buildID, dist), {
      version: "1.2.3",
      buildID,
      assets: [path.join(dist, "static", "js", "app.js")]
    });

    await writeFile(path.join(dist, "static", "js", "app.js"), 'document.documentElement.dataset.uvpBuildId = "old-build";');
    await assert.rejects(() => checkFrontendBuildIdentity(root, buildID, dist), /frontend build identity/i);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

