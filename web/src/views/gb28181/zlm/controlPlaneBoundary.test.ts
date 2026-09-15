import { readdirSync, readFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { describe, expect, it } from "vitest";

function sourceFiles(root: string): string[] {
  return readdirSync(root, { withFileTypes: true }).flatMap(entry => {
    const path = join(root, entry.name);
    if (entry.isDirectory()) return sourceFiles(path);
    if (!/\.(?:ts|vue)$/.test(entry.name) || entry.name.endsWith(".test.ts")) return [];
    return [path];
  });
}

const zlmRoot = resolve(process.cwd(), "src/views/gb28181/zlm");
const cloudRecordingRoot = resolve(process.cwd(), "src/views/gb28181/cloud-recordings");
const apiFiles = [
  "src/api/gb28181-zlm.ts",
  "src/api/gb28181-zlm-runtime.ts",
  "src/api/gb28181-zlm-ingress.ts"
].map(file => resolve(process.cwd(), file));

describe("ZLM browser control-plane boundary", () => {
  it("never connects to the raw ZLM management API or writes diagnostics to console", () => {
    const bundle = [...sourceFiles(zlmRoot), ...sourceFiles(cloudRecordingRoot), ...apiFiles]
      .map(file => readFileSync(file, "utf8"))
      .join("\n");

    expect(bundle).not.toMatch(/\/index\/api\//i);
    expect(bundle).not.toMatch(/[?&](?:secret|token|access_token)=/i);
    expect(bundle).not.toMatch(/\bconsole\.(?:debug|info|log|warn|error)\s*\(/);
  });

  it.each([
    "NodeList.vue",
    "NodeDetail.vue",
    "NodeForm.vue",
    "ZLMSessionKickDialog.vue",
    "ZLMNodeActionDialog.vue",
    "ServerConfig.vue",
    "components/NodeConfigPanel.vue"
  ])("does not place raw transport or backend messages into the DOM in %s", file => {
    const source = readFileSync(resolve(zlmRoot, file), "utf8");

    expect(source).not.toMatch(/\(error as Error\)\?\.message/);
    expect(source).not.toMatch(/error instanceof Error \? error\.message/);
    expect(source).not.toMatch(/operation\.error\s*\|\|/);
  });
});
