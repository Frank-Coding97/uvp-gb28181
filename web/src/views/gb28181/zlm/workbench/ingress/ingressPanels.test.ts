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
});
