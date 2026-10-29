import { lstat, mkdir, readdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const VERSION_PATTERN = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-rc\.(0|[1-9]\d*))?$/;
const DERIVED_FILES = [
  "server/version.json",
  "web/version.json",
  "web/package.json"
];
const RELEASE_METADATA_FILES = ["server/version.json", "web/version.json"];
const BUILD_ID_PATTERN = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/;
const FRONTEND_JAVASCRIPT_EXTENSIONS = new Set([".js", ".mjs"]);

export async function readProductVersion(root) {
  const version = (await readFile(path.join(root, "VERSION"), "utf8")).trim();
  if (!VERSION_PATTERN.test(version)) {
    throw new Error(`invalid product version: ${version || "<empty>"}`);
  }
  return version;
}

async function readVersionFile(root, relativePath) {
  const absolutePath = path.join(root, relativePath);
  const source = await readFile(absolutePath, "utf8");
  const value = JSON.parse(source);
  return { absolutePath, relativePath, source, value };
}

function normalizeBuildID(buildID) {
  const normalized = typeof buildID === "string" ? buildID.trim() : "";
  if (!BUILD_ID_PATTERN.test(normalized)) {
    throw new Error("invalid release build id");
  }
  return normalized;
}

function stringifyVersionFile(value, source) {
  const indent = source.match(/\n([ \t]+)"/)?.[1] ?? "  ";
  return `${JSON.stringify(value, null, indent)}\n`;
}

export async function checkVersions(root) {
  const expected = await readProductVersion(root);
  for (const relativePath of DERIVED_FILES) {
    const { value } = await readVersionFile(root, relativePath);
    if (value.version !== expected) {
      throw new Error(`version mismatch: ${relativePath} has ${value.version}, expected ${expected}`);
    }
  }
  return expected;
}

export async function syncVersions(root) {
  const version = await readProductVersion(root);
  for (const relativePath of DERIVED_FILES) {
    const { absolutePath, source, value } = await readVersionFile(root, relativePath);
    value.version = version;
    await writeFile(absolutePath, stringifyVersionFile(value, source));
  }
  return version;
}

export async function writeReleaseMetadata(root, buildID, outputRoot) {
  const version = await checkVersions(root);
  const normalizedBuildID = normalizeBuildID(buildID);
  const destinationRoot = path.resolve(outputRoot);

  for (const relativePath of RELEASE_METADATA_FILES) {
    const { source, value } = await readVersionFile(root, relativePath);
    const destination = path.join(destinationRoot, relativePath);
    await mkdir(path.dirname(destination), { recursive: true });
    await writeFile(destination, stringifyVersionFile({ ...value, build_id: normalizedBuildID }, source));
  }

  return { version, buildID: normalizedBuildID };
}

export async function checkReleaseMetadata(root, buildID, outputRoot) {
  const version = await checkVersions(root);
  const normalizedBuildID = normalizeBuildID(buildID);
  const destinationRoot = path.resolve(outputRoot);

  for (const relativePath of RELEASE_METADATA_FILES) {
    const source = await readVersionFile(root, relativePath);
    const generated = await readVersionFile(destinationRoot, relativePath);
    if (
      generated.value.name !== source.value.name ||
      generated.value.version !== version ||
      generated.value.build_id !== normalizedBuildID
    ) {
      throw new Error(`release metadata mismatch: ${relativePath}`);
    }
  }

  return { version, buildID: normalizedBuildID };
}

async function frontendJavaScriptAssets(directory) {
  const info = await lstat(directory);
  if (!info.isDirectory() || info.isSymbolicLink()) {
    throw new Error(`frontend build directory is invalid: ${directory}`);
  }
  const assets = [];
  const entries = await readdir(directory, { withFileTypes: true });
  entries.sort((left, right) => left.name.localeCompare(right.name));
  for (const entry of entries) {
    const candidate = path.join(directory, entry.name);
    if (entry.isDirectory()) {
      assets.push(...(await frontendJavaScriptAssets(candidate)));
    } else if (entry.isFile() && FRONTEND_JAVASCRIPT_EXTENSIONS.has(path.extname(entry.name))) {
      assets.push(candidate);
    }
  }
  return assets;
}

// The source tree can say anything about a build. The release gate inspects
// emitted JavaScript because only that is served to a browser after activation.
export async function checkFrontendBuildIdentity(root, buildID, distRoot) {
  const version = await checkVersions(root);
  const normalizedBuildID = normalizeBuildID(buildID);
  const destinationRoot = path.resolve(distRoot);
  const index = path.join(destinationRoot, "index.html");
  const indexInfo = await lstat(index);
  if (!indexInfo.isFile() || indexInfo.isSymbolicLink()) {
    throw new Error(`frontend build index is invalid: ${index}`);
  }
  const assets = await frontendJavaScriptAssets(destinationRoot);
  if (assets.length === 0) {
    throw new Error(`frontend build has no JavaScript assets: ${destinationRoot}`);
  }
  const marker = Buffer.from(normalizedBuildID, "utf8");
  for (const asset of assets) {
    if ((await readFile(asset)).includes(marker)) {
      return { version, buildID: normalizedBuildID, assets };
    }
  }
  throw new Error(`frontend build identity ${normalizedBuildID} is absent from compiled JavaScript assets`);
}

async function main() {
  const command = process.argv[2];
  const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
  if (command === "check") {
    const version = await checkVersions(root);
    process.stdout.write(`versions match ${version}\n`);
    return;
  }
  if (command === "sync") {
    const version = await syncVersions(root);
    process.stdout.write(`versions synchronized to ${version}\n`);
    return;
  }
  if (command === "release-metadata") {
    const [buildID, outputRoot] = process.argv.slice(3);
    if (!outputRoot) throw new Error("usage: node scripts/version.mjs release-metadata <build-id> <output-dir>");
    const metadata = await writeReleaseMetadata(root, buildID, outputRoot);
    process.stdout.write(`release metadata prepared ${metadata.version} ${metadata.buildID}\n`);
    return;
  }
  if (command === "check-release-metadata") {
    const [buildID, outputRoot] = process.argv.slice(3);
    if (!outputRoot) throw new Error("usage: node scripts/version.mjs check-release-metadata <build-id> <output-dir>");
    const metadata = await checkReleaseMetadata(root, buildID, outputRoot);
    process.stdout.write(`release metadata match ${metadata.version} ${metadata.buildID}\n`);
    return;
  }
  if (command === "check-frontend-build-id") {
    const [buildID, distRoot] = process.argv.slice(3);
    if (!distRoot) throw new Error("usage: node scripts/version.mjs check-frontend-build-id <build-id> <dist-dir>");
    const result = await checkFrontendBuildIdentity(root, buildID, distRoot);
    process.stdout.write(`frontend build identity match ${result.version} ${result.buildID}\n`);
    return;
  }
  throw new Error("usage: node scripts/version.mjs <check|sync|release-metadata|check-release-metadata|check-frontend-build-id>");
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main().catch(error => {
    process.stderr.write(`${error.message}\n`);
    process.exitCode = 1;
  });
}

