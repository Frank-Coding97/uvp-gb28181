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
      expect(source, file).toContain('scope === "all"');
      expect(source, file).toContain('scope !== "all"');
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

  it("uses the system list button and a card-free pagination bar", () => {
    for (const file of ["ProxyPanel.vue", "FFmpegPanel.vue", "RTPPanel.vue"]) {
      const source = readFileSync(resolve(root, file), "utf8");
      expect(source, file).toContain('class="uvp-page-action-btn uvp-refresh-btn"');
      expect(source, file).toContain('class="uvp-page-action-btn uvp-create-btn"');
      // 分页器挂在卡片外的独立分页条上，不再由表格自带（否则会被卡片边框框住）
      expect(source, file).toContain(':pagination="false"');
      expect(source, file).not.toContain("tablePagination");
      expect(source, file).toContain('class="ingress-pagination uvp-pagination-bar"');
      expect(source, file).toContain("<a-pagination");
      expect(source, file).toContain("show-total");
      expect(source, file).toContain("show-page-size");
      expect(source, file).toContain("show-jumper");
      expect(source, file).toContain(':page-size-options="[10, 20, 50, 100]"');
      // 分页条必须在 data-panel 卡片之外
      const panelStart = source.indexOf('class="data-panel"');
      const panelEnd = source.indexOf("</section>", panelStart);
      expect(source.indexOf('class="ingress-pagination'), file).toBeGreaterThan(panelEnd);
    }
  });
});
