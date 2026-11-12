import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { cp, lstat, mkdtemp, rm, symlink } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

import { checkFrontendBuildIdentity } from "./version.mjs";

const execFileAsync = promisify(execFile);
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const sourceWeb = path.join(root, "web");
const viteBinary = path.join(sourceWeb, "node_modules", "vite", "bin", "vite.js");

async function copyWebForBuild(destination) {
  await cp(sourceWeb, destination, {
    recursive: true,
    filter(source) {
      const name = path.basename(source);
      return name !== "node_modules" && name !== "dist" && !name.endsWith(".local");
    }
  });
  await symlink(path.join(sourceWeb, "node_modules"), path.join(destination, "node_modules"), "dir");
}

test("a real Vite build retains its injected build identity in compiled JavaScript", { timeout: 180_000 }, async t => {
  try {
    await lstat(viteBinary);
  } catch {
    t.skip("web/node_modules is unavailable; install web dependencies to run this release-build test");
    return;
  }

  const temporary = await mkdtemp(path.join(os.tmpdir(), "uvp-frontend-build-id-"));
  const webRoot = path.join(temporary, "web");
  const output = path.join(webRoot, "release-dist");
  const buildID = "build-20261016-vite-runtime";
  try {
    await copyWebForBuild(webRoot);
    await execFileAsync(process.execPath, [path.join(webRoot, "node_modules", "vite", "bin", "vite.js"), "build", "--mode", "production", "--outDir", output], {
      cwd: webRoot,
      env: { ...process.env, VITE_BUILD_ID: buildID },
      maxBuffer: 8 * 1024 * 1024
    });

    const result = await checkFrontendBuildIdentity(root, buildID, output);
    assert.equal(result.buildID, buildID);
    assert.ok(result.assets.length > 0);
    await assert.rejects(() => checkFrontendBuildIdentity(root, "other-build", output), /frontend build identity/i);
  } finally {
    await rm(temporary, { recursive: true, force: true });
  }
});
