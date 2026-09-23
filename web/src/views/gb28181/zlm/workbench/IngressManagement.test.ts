import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const root = resolve(process.cwd(), "src/views/gb28181/zlm");

describe("ingress workbench composition", () => {
  it("uses the extracted ingress panels for every view", () => {
    const source = readFileSync(resolve(root, "workbench/IngressManagement.vue"), "utf8");

    expect(source).toContain("ProxyPanel");
    expect(source).toContain("FFmpegPanel");
    expect(source).toContain("RTPPanel");
    expect(source).toContain(":active=\"active\"");
    expect(source).toContain(":scope=\"workspace.scope.value\"");
  });

  it("keeps the old pages as compatibility shells", () => {
    const pages = ["ProxyManagement.vue", "FFmpegSources.vue", "RTPServices.vue"];
    for (const page of pages) {
      const source = readFileSync(resolve(root, page), "utf8");
      expect(source, page).toContain("LegacyIngressShell");
      expect(source, page).toContain("Panel");
      expect(source, page).not.toContain("listZLMNodes");
      expect(source, page).not.toContain("useZLMRuntimePolling");
    }
  });
});
