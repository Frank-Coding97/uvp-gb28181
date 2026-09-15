import assert from "node:assert/strict";
import { mkdtemp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import { checkVersions, readProductVersion, syncVersions } from "./version.mjs";

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

