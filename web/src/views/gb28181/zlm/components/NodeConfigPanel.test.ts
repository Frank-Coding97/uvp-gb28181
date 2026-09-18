import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import type { ConfigItem, UpdateConfigResp } from "@/api/gb28181-zlm";
import { buildHotReloadChanges, configModePresentation, isConfigEditable, updateResultRows } from "../nodeConfigState";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/components/NodeConfigPanel.vue"), "utf8");

const item = (key: string, mode: ConfigItem["mode"], value = "old"): ConfigItem => ({
  key, value, default: "", mode, hotReloadable: mode === "hot_reload", restartRequired: mode === "restart_required_unsupported", comment: ""
});

describe("NodeConfigPanel", () => {
  it("makes only hot_reload items editable and constructible in a request", () => {
    const items = [
      item("hot.key", "hot_reload"),
      item("managed.key", "platform_managed"),
      item("restart.key", "restart_required_unsupported"),
      item("readonly.key", "read_only")
    ];
    expect(items.map(isConfigEditable)).toEqual([true, false, false, false]);
    expect(buildHotReloadChanges(items, {
      "hot.key": "new",
      "managed.key": "bypass",
      "restart.key": "bypass",
      "readonly.key": "bypass"
    })).toEqual({ "hot.key": "new" });
    expect(configModePresentation("platform_managed").reason).toContain("平台管理");
    expect(configModePresentation("restart_required_unsupported").reason).toContain("不支持重启后应用");
    expect(configModePresentation("read_only").reason).toContain("只读");
    expect(source).toContain('v-if="isConfigEditable(record)"');
  });

  it("preserves old/new/command/actual readback as four separate facts", () => {
    const response: UpdateConfigResp = {
      applied: ["matched.key"],
      requiresRestart: [],
      unknown: [],
      actual: { "matched.key": "new", "mismatch.key": "upstream" },
      mismatched: ["mismatch.key"]
    };
    expect(updateResultRows([item("matched.key", "hot_reload"), item("mismatch.key", "hot_reload")], {
      "matched.key": "new", "mismatch.key": "wanted"
    }, response)).toEqual([
      { key: "matched.key", oldValue: "old", newValue: "new", commandResult: "已回读生效", actualValue: "new", tone: "success" },
      { key: "mismatch.key", oldValue: "old", newValue: "wanted", commandResult: "回读不一致", actualValue: "upstream", tone: "danger" }
    ]);
    expect(source).toContain("旧值");
    expect(source).toContain("新值");
    expect(source).toContain("命令结果");
    expect(source).toContain("实际值");
  });

  it("pauses refresh while a draft exists instead of discarding it", () => {
    expect(source).toContain('emit("dirtyChange", dirtyCount.value > 0)');
    expect(source).toContain('emit("pollingSkipped")');
    expect(source).toMatch(/if \(dirtyCount\.value > 0[^}]+pollingSkipped/s);
    expect(source).not.toContain("dirty.value = {};\n            // 默认选中第一个分组");
  });
});
