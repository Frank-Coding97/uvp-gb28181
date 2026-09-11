import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/ServerConfig.vue"), "utf8");

describe("server config page", () => {
  it("shares the extracted config panel and locks node switching while drafts exist", () => {
    expect(source).toContain("NodeConfigPanel");
    expect(source).toContain('@dirty-change="configDirty = $event"');
    expect(source).toContain(':disabled="configDirty || restartPolling"');
    expect(source).toContain("存在未保存草稿时，自动轮询与节点切换都会暂停");
  });

  it("treats restart as an accepted operation and polls every recovery phase", () => {
    expect(source).toContain("restartZLMNode");
    expect(source).toContain("getZLMNodeRestartStatus");
    for (const state of ["accepted", "waiting_offline", "waiting_heartbeat", "converging", "ready", "failed", "unknown"]) {
      expect(source).toContain(state);
    }
    expect(source).toContain("后端已受理，不代表节点已经恢复");
    expect(source).toMatch(/if \(operation\.status === "ready"\)[\s\S]+Message\.success/);
    expect(source).not.toMatch(/restartZLMNode[\s\S]{0,400}Message\.success/);
  });

  it("keeps update and restart under independent permissions", () => {
    expect(source).toContain('gb28181:zlm:config:update');
    expect(source).toContain('gb28181:zlm:restart');
  });
});
