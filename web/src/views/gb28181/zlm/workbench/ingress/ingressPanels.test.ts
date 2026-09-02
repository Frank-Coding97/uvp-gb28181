import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const root = resolve(process.cwd(), "src/views/gb28181/zlm/workbench/ingress");

describe("ingress panels", () => {
  it.each([
    ["ProxyPanel.vue", ["listZLMPullProxies", "listZLMPushProxies", "createZLMPullProxy", "createZLMPushProxy"]],
    ["FFmpegPanel.vue", ["listZLMFFmpegSources", "createZLMFFmpegSource"]],
    ["RTPPanel.vue", ["listZLMRTPServers", "createZLMRTPServer"]]
  ])("%s is node-scoped and uses the backend API", (file, apiNames) => {
    const source = readFileSync(resolve(root, file), "utf8");

    expect(source, file).toContain("useZLMRuntimePolling");
    expect(source, file).toContain("active");
    expect(source, file).toContain("scope");
    expect(source, file).toContain("nodeId");
    for (const apiName of apiNames) expect(source, file).toContain(apiName);
    expect(source, file).not.toContain("index/api");
  });

  it("does not issue a list request for the all-node scope", () => {
    const files = ["ProxyPanel.vue", "FFmpegPanel.vue", "RTPPanel.vue"];
    for (const file of files) {
      const source = readFileSync(resolve(root, file), "utf8");
      expect(source, file).toContain("scope === \"all\"");
      expect(source, file).toContain("scope !== \"all\"");
    }
  });

  it("starts every ingress panel with actions instead of a redundant title block", () => {
    for (const file of ["ProxyPanel.vue", "FFmpegPanel.vue", "RTPPanel.vue"]) {
      const source = readFileSync(resolve(root, file), "utf8");
      expect(source, file).not.toMatch(/<header class="panel-toolbar">\s*<div><h2>/);
      expect(source, file).toMatch(/\.panel-toolbar\s*\{[^}]*justify-content:\s*flex-end;/s);
    }
  });

  it("keeps capability checks functional without persistent support banners", () => {
    for (const file of ["ProxyPanel.vue", "FFmpegPanel.vue", "RTPPanel.vue"]) {
      const source = readFileSync(resolve(root, file), "utf8");
      expect(source, file).not.toContain('class="capability-banner"');
      expect(source, file).toContain("capability");
    }
  });

  it("uses the system list button and table pagination language", () => {
    for (const file of ["ProxyPanel.vue", "FFmpegPanel.vue", "RTPPanel.vue"]) {
      const source = readFileSync(resolve(root, file), "utf8");
      expect(source, file).toContain('class="uvp-refresh-btn"');
      expect(source, file).toContain(':pagination="tablePagination"');
      expect(source, file).toContain("showTotal: true");
      expect(source, file).toContain("showJumper: true");
      expect(source, file).toContain("showPageSize: true");
      expect(source, file).toContain("pageSizeOptions: [10, 20, 50, 100]");
      expect(source, file).not.toContain("<a-pagination");
    }
  });
});
