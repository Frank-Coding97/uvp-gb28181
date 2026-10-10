import { readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const VERSION_PATTERN = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-rc\.(0|[1-9]\d*))?$/;
const DERIVED_FILES = [
  "server/version.json",
  "web/version.json",
  "web/package.json"
];

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
    const indent = source.match(/\n([ \t]+)"/)?.[1] ?? "  ";
    await writeFile(absolutePath, `${JSON.stringify(value, null, indent)}\n`);
  }
  return version;
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
  throw new Error("usage: node scripts/version.mjs <check|sync>");
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main().catch(error => {
    process.stderr.write(`${error.message}\n`);
    process.exitCode = 1;
  });
}

