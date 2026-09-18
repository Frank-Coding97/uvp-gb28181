import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

test("release build checks the canonical version before compiling", async () => {
  const source = await readFile(new URL("../deploy/test/build-release.sh", import.meta.url), "utf8");
  assert.match(source, /node .*scripts\/version\.mjs["']? check/);
});

test("release build keeps stdout reserved for the archive path", async () => {
  const source = await readFile(new URL("../deploy/test/build-release.sh", import.meta.url), "utf8");
  assert.match(source, /node .*scripts\/version\.mjs["']? check >&2/);
});

test("release build keeps the production heap default but permits an explicit override", async () => {
  const source = await readFile(new URL("../deploy/test/build-release.sh", import.meta.url), "utf8");
  assert.match(source, /NODE_OPTIONS="\$\{NODE_OPTIONS:---max-old-space-size=1536\}"/);
});

test("release archive carries backend and frontend version evidence", async () => {
  const source = await readFile(new URL("../deploy/test/assemble-release.sh", import.meta.url), "utf8");
  assert.match(source, /node .*scripts\/version\.mjs["']? check >&2/);
  assert.match(source, /server\/version\.json/);
  assert.match(source, /frontend\/dist\/version\.json/);
});
