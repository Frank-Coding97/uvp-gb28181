import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const APP_BUILD_ID = "uvplatform.com/uvp-gb28181/app/global/app.BuildID";

async function source(relativePath) {
  return readFile(new URL(`../${relativePath}`, import.meta.url), "utf8");
}

test("test release builds one backend identity from the verified source revision", async () => {
  const build = await source("deploy/test/build-release.sh");

  assert.match(build, /BUILD_ID="\$SHA"/);
  assert.match(build, new RegExp(`-X ${APP_BUILD_ID}=\\$\\{?BUILD_ID\\}?`));
  assert.match(build, /VITE_BUILD_ID="\$BUILD_ID"/);
  assert.match(build, /check-frontend-build-id "\$BUILD_ID" "\$SOURCE_ROOT\/web\/dist"/);
  assert.match(build, /scripts\/version\.mjs["']? release-metadata "\$BUILD_ID"/);
  assert.match(build, /UVP_RELEASE_METADATA_DIR="\$release_metadata_dir"/);
});

test("release assembly accepts only generated metadata carrying the release build identity", async () => {
  const assemble = await source("deploy/test/assemble-release.sh");

  assert.match(assemble, /UVP_RELEASE_METADATA_DIR/);
  assert.match(assemble, /scripts\/version\.mjs["']? check-release-metadata "\$SHA"/);
  assert.match(assemble, /check-frontend-build-id "\$SHA" "\$SOURCE_ROOT\/web\/dist"/);
  assert.match(assemble, /\$RELEASE_METADATA_DIR\/server\/version\.json/);
  assert.match(assemble, /\$RELEASE_METADATA_DIR\/web\/version\.json/);
  assert.doesNotMatch(assemble, /install -m 0644 "\$SOURCE_ROOT\/server\/version\.json"/);
  assert.doesNotMatch(assemble, /install -m 0644 "\$SOURCE_ROOT\/web\/version\.json"/);
});

test("standalone package embeds and records one explicit build identity", async () => {
  const build = await source("deploy/standalone/build-standalone.sh");

  assert.match(build, /UVP_BUILD_ID/);
  assert.match(build, new RegExp(`-X ${APP_BUILD_ID}=\\$\\{?BUILD_ID\\}?`));
  assert.match(build, /VITE_BUILD_ID=%s/);
  assert.match(build, /check-frontend-build-id "\$BUILD_ID" "\$WEB_DIR\/dist"/);
  assert.match(build, /scripts\/version\.mjs"? release-metadata "\$BUILD_ID"/);
  assert.match(build, /\$RELEASE_METADATA_DIR\/server\/version\.json/);
  assert.doesNotMatch(build, /cp "\$SERVER_DIR\/version\.json" "\$PKG\/version\.json"/);
});

test("standalone package protects updater registration and installation secrets regardless of umask", async () => {
  const build = await source("deploy/standalone/build-standalone.sh");

  assert.match(build, /chmod 0700 "\$PKG\/config"/);
  assert.match(build, /chmod 0600 "\$PKG\/config\/config\.yml" "\$PKG\/config\/config\.env"/);
});
