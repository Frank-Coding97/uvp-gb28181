import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { promisify } from "node:util";
import test from "node:test";
import { fileURLToPath } from "node:url";

const execFileAsync = promisify(execFile);
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const serverRoot = path.join(root, "server");
const versionScript = path.join(root, "scripts", "version.mjs");
const buildIDVariable = "uvplatform.com/uvp-gb28181/app/global/app.BuildID";

const probeSource = `package main

import (
  "encoding/json"
  "fmt"
  "uvplatform.com/uvp-gb28181/app/global/app"
)

func main() {
  if err := app.LoadVersionInfo(); err != nil {
    panic(err)
  }
  version, buildID := app.RuntimeVersionIdentity()
  result, err := json.Marshal(map[string]string{"version": version, "build_id": buildID})
  if err != nil {
    panic(err)
  }
  fmt.Println(string(result))
}
`;

async function runVersion(...args) {
  return execFileAsync(process.execPath, [versionScript, ...args], {
    cwd: root,
    maxBuffer: 1024 * 1024
  });
}

async function buildProbe(rootDir, buildID) {
  const source = path.join(rootDir, "identity-probe.go");
  const binary = path.join(rootDir, "identity-probe");
  await writeFile(source, probeSource, { mode: 0o600 });
  await execFileAsync(
    "go",
    ["build", "-trimpath", "-ldflags", `-X ${buildIDVariable}=${buildID}`, "-o", binary, source],
    { cwd: serverRoot, maxBuffer: 1024 * 1024 }
  );
  return binary;
}

async function runProbe(binary, versionPath) {
  const { stdout } = await execFileAsync(binary, [], {
    env: { ...process.env, UVP_VERSION_PATH: versionPath },
    maxBuffer: 1024 * 1024
  });
  return JSON.parse(stdout);
}

test("generated release metadata and an ldflags binary must share one runtime build identity", async () => {
  const temporary = await mkdtemp(path.join(os.tmpdir(), "uvp-build-identity-"));
  const metadata = path.join(temporary, "metadata");
  const buildID = "build-20261016-runtime";
  try {
    const version = (await readFile(path.join(root, "VERSION"), "utf8")).trim();
    await runVersion("release-metadata", buildID, metadata);
    await runVersion("check-release-metadata", buildID, metadata);

    const versionPath = path.join(metadata, "server", "version.json");
    const versionInfo = JSON.parse(await readFile(versionPath, "utf8"));
    assert.equal(versionInfo.version, version);
    assert.equal(versionInfo.build_id, buildID);

    const binary = await buildProbe(temporary, buildID);
    assert.deepEqual(await runProbe(binary, versionPath), { version, build_id: buildID });

    await writeFile(versionPath, JSON.stringify({ ...versionInfo, build_id: "different-build" }));
    await assert.rejects(
      () => runVersion("check-release-metadata", buildID, metadata),
      error => {
        assert.match(`${error.message}\n${error.stderr ?? ""}`, /release metadata mismatch/);
        return true;
      }
    );
    assert.deepEqual(await runProbe(binary, versionPath), { version: "", build_id: "" });
  } finally {
    await rm(temporary, { recursive: true, force: true });
  }
});
