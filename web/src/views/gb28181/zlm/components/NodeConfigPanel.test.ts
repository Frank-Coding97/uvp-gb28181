import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import type { ConfigGroup, ConfigItem, UpdateConfigResp } from "@/api/gb28181-zlm";
import {
  buildConfigChanges,
  configDictionaryOptions,
  configModePresentation,
  isConfigEditable,
  isHookRoutingChange,
  isNetworkPortChange,
  isWebRTCPortChange,
  orderConfigGroups,
  updateResultRows
} from "../nodeConfigState";
import { hasRuleBlock } from "@/test/source-assert";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/components/NodeConfigPanel.vue"), "utf8");

const item = (key: string, mode: ConfigItem["mode"], value = "old"): ConfigItem => ({
  key,
  value,
  default: "",
  mode,
  hotReloadable: mode === "hot_reload",
  restartRequired: mode === "restart_required" || mode === "restart_required_unsupported",
  comment: ""
});

describe("NodeConfigPanel", () => {
  it("submits and confirms a WebRTC external address as a hot update", () => {
    const externalIP = item("rtc.externIP", "hot_reload", "");
    const changes = buildConfigChanges([externalIP], { "rtc.externIP": "62.234.152.39" });
    expect(isConfigEditable(externalIP)).toBe(true);
    expect(changes).toEqual({ "rtc.externIP": "62.234.152.39" });
    const rows = updateResultRows([externalIP], changes, {
      applied: ["rtc.externIP"],
      requiresRestart: [],
      unknown: []
    });
    expect(rows[0]?.actualValue).toBe("62.234.152.39");
    expect(rows[0]?.tone).toBe("success");
    expect(isNetworkPortChange("rtc.externIP")).toBe(false);
    expect(buildConfigChanges([item("rtc.externIP", "hot_reload", "62.234.152.39")], { "rtc.externIP": "" })).toEqual({
      "rtc.externIP": ""
    });
  });
  it("allows saving the heartbeat interval without claiming immediate activation", () => {
    const interval = item("hook.alive_interval", "restart_required", "30");
    expect(isConfigEditable(interval)).toBe(true);
    expect(buildConfigChanges([interval], { "hook.alive_interval": "45.5" })).toEqual({ "hook.alive_interval": "45.5" });
    expect(configModePresentation(interval.mode).label).toBe("重启后生效");
    expect(
      updateResultRows(
        [interval],
        { "hook.alive_interval": "45.5" },
        {
          applied: [],
          requiresRestart: ["hook.alive_interval"],
          unknown: []
        }
      )
    ).toEqual([
      {
        key: "hook.alive_interval",
        oldValue: "30",
        newValue: "45.5",
        actualValue: "45.5",
        commandResult: "已保存，重启媒体节点后生效",
        tone: "warning"
      }
    ]);
    expect(source).toContain('rows.filter(row => row.tone !== "danger")');
    expect(source).toContain("配置已保存，部分配置需重启媒体节点后生效");
    expect(source.replace(/\s+/g, " ")).toMatch(/>\s*保存配置\s*<\/a-button\s*>/);
  });
  it("edits network ports as restart-required values with an impact confirmation", () => {
    const port = item("http.port", "restart_required", "18080");
    const range = item("rtp_proxy.port_range", "restart_required", "30000-35000");
    expect(isConfigEditable(port)).toBe(true);
    expect(buildConfigChanges([port, range], { "http.port": "28080", "rtp_proxy.port_range": "31000-32000" })).toEqual({
      "http.port": "28080",
      "rtp_proxy.port_range": "31000-32000"
    });
    expect(
      updateResultRows(
        [port],
        { "http.port": "28080" },
        {
          applied: [],
          requiresRestart: ["http.port"],
          unknown: []
        }
      )[0]?.commandResult
    ).toBe("已保存，重启媒体节点后生效");
    expect(isNetworkPortChange("http.port")).toBe(true);
    expect(isNetworkPortChange("rtp_proxy.port_range")).toBe(true);
    expect(isNetworkPortChange("hook.alive_interval")).toBe(false);
    expect(source).toContain("修改网络端口配置");
    expect(source).toContain("节点登记的 API 端口");
    expect(source).toContain("节点登记的 RTP 收流范围");
  });
  it("confirms WebRTC port changes with the persistence and firewall consequences", () => {
    // 四个端口才算「WebRTC 端口」；地址类不算（改了不用重启，也不涉及防火墙）。
    expect(isWebRTCPortChange("rtc.port")).toBe(true);
    expect(isWebRTCPortChange("rtc.tcpPort")).toBe(true);
    expect(isWebRTCPortChange("rtc.icePort")).toBe(true);
    expect(isWebRTCPortChange("rtc.iceTcpPort")).toBe(true);
    expect(isWebRTCPortChange("rtc.externIP")).toBe(false);
    expect(isWebRTCPortChange("rtc.port_range")).toBe(false);
    // 与「网络端口」分开：那一批的后果是平台与节点失联，这一批只是浏览器拿不到画面。
    expect(isNetworkPortChange("rtc.port")).toBe(false);
    expect(source).toContain("修改 WebRTC 端口");
    expect(source).toContain("会自动对齐同号");
    expect(source).toContain("写回部署端口规划");
    expect(source).toContain("防火墙");
    // hints 必须常驻展示，不能只弹一次窗
    expect(source).toContain('v-for="hint in saveHints"');
  });
  it("confirms hook routing changes while allowing URL and switch edits", () => {
    const items = [item("hook.enable", "hot_reload", "1"), item("hook.on_play", "hot_reload")];
    expect(buildConfigChanges(items, { "hook.enable": "0", "hook.on_play": "https://custom.example/play" })).toEqual({
      "hook.enable": "0",
      "hook.on_play": "https://custom.example/play"
    });
    expect(isHookRoutingChange("hook.enable")).toBe(true);
    expect(isHookRoutingChange("hook.on_play")).toBe(true);
    expect(isHookRoutingChange("hook.timeoutSec")).toBe(false);
    expect(isHookRoutingChange("protocol.enable_mp4")).toBe(false);
    expect(source).toContain("Modal.warning");
    expect(source).toContain("修改 Hook 回调配置");
  });
  it("uses dictionary metadata for enum controls", () => {
    const checkSource = item("rtp_proxy.checkSource", "hot_reload", "1");
    checkSource.dictCode = "status";
    expect(
      configDictionaryOptions(checkSource, [
        { name: "禁用", value: "0", status: 1 },
        { name: "启用", value: "1", status: 1 },
        { name: "隐藏", value: "2", status: 0 }
      ])
    ).toEqual([
      { label: "禁用", value: "0" },
      { label: "启用", value: "1" }
    ]);
    expect(source).toContain("dictCode");
    expect(source.replace(/\s+/g, " ")).toContain('<a-select :model-value="dirty[record.key] ?? record.value"');
    expect(configDictionaryOptions({ ...checkSource, dictCode: "status" }, [])).toEqual([
      { label: "禁用", value: "0" },
      { label: "启用", value: "1" }
    ]);
  });
  it("makes only hot_reload items editable and constructible in a request", () => {
    const items = [
      item("hot.key", "hot_reload"),
      item("managed.key", "platform_managed"),
      item("restart.key", "restart_required_unsupported"),
      item("readonly.key", "read_only")
    ];
    expect(items.map(isConfigEditable)).toEqual([true, false, false, false]);
    expect(
      buildConfigChanges(items, {
        "hot.key": "new",
        "managed.key": "bypass",
        "restart.key": "bypass",
        "readonly.key": "bypass"
      })
    ).toEqual({ "hot.key": "new" });
    expect(configModePresentation("platform_managed").reason).toContain("平台管理");
    expect(configModePresentation("restart_required_unsupported").reason).toContain("不支持重启后应用");
    expect(configModePresentation("read_only").reason).toContain("只读");
    expect(source).toContain('v-else-if="isConfigEditable(record)"');
  });

  it("preserves old/new/command/actual readback as four separate facts", () => {
    const response: UpdateConfigResp = {
      applied: ["matched.key"],
      requiresRestart: [],
      unknown: [],
      actual: { "matched.key": "new", "mismatch.key": "upstream" },
      mismatched: ["mismatch.key"]
    };
    expect(
      updateResultRows(
        [item("matched.key", "hot_reload"), item("mismatch.key", "hot_reload")],
        {
          "matched.key": "new",
          "mismatch.key": "wanted"
        },
        response
      )
    ).toEqual([
      { key: "matched.key", oldValue: "old", newValue: "new", commandResult: "已回读生效", actualValue: "new", tone: "success" },
      {
        key: "mismatch.key",
        oldValue: "old",
        newValue: "wanted",
        commandResult: "回读不一致",
        actualValue: "upstream",
        tone: "danger"
      }
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

  it("offers the restart entry right where the restart-required result is reported", () => {
    // 语义：保存返回的 requiresRestart 才是「要重启」的真源。行里的 tone 不能反推，
    // 它同时承载「回读不一致」—— 那种情况该做的是重查，不是重启。
    const port = item("rtc.port", "restart_required", "51008");
    expect(
      updateResultRows([port], { "rtc.port": "51070" }, { applied: [], requiresRestart: ["rtc.port"], unknown: [] })[0]?.tone
    ).toBe("warning");
    expect(source).toContain("pendingRestartKeys.value = result.requiresRestart ?? []");
    expect(source).toContain("@click=\"emit('restartRequested')\"");
    // 待办不能挂在 saveResults 上：15 秒轮询会清空回读表，按钮会自己消失。
    expect(source).toContain("if (options.force) pendingRestartKeys.value = []");
    const restartCta = source.indexOf("restart-cta");
    const configBody = source.indexOf('class="config-spin"');
    expect(restartCta).toBeGreaterThan(0);
    expect(restartCta).toBeLessThan(configBody);
    // 点不动时必须说明原因，不能"点了没反应"
    expect(source).toContain("项改动未保存，先保存或放弃草稿再重启");
    expect(source).toContain("重启已受理，正在等待节点离线、心跳恢复和配置收敛");
    expect(source).toContain(':disabled="!!restartBlockedReason"');
    // 面板自己没有重启权限状态：canRestart / restartBusy 都由外层（NodeConfigView）传入，
    // 面板只负责根据它们决定按钮可不可点。
    expect(source).toContain("canRestart?: boolean");
    expect(source).toContain("restartBusy?: boolean");
  });

  it("orders categories by common GB28181 operations", () => {
    const group = (name: string): ConfigGroup => ({ name, items: [] });
    expect(
      orderConfigGroups([
        group("网络端口"),
        group("Hook 回调"),
        group("协议开关"),
        group("运行时策略"),
        group("WebRTC"),
        group("GB28181 国标"),
        group("录制与截图"),
        group("性能调优"),
        group("安全")
      ]).map(item => item.name)
    ).toEqual(["GB28181 国标", "运行时策略", "WebRTC", "协议开关", "录制与截图", "Hook 回调", "性能调优", "网络端口", "安全"]);
  });

  it("keeps vertical scrolling inside the configuration table", () => {
    expect(source).toContain(':scroll="configTableScroll"');
    expect(hasRuleBlock(source, ".node-config-panel", "flex: 1", "min-height: 0", "overflow: hidden")).toBe(true);
    expect(hasRuleBlock(source, ".config-body", "height: 100%", "min-height: 0", "overflow: hidden")).toBe(true);
    expect(hasRuleBlock(source, ".category-detail", "display: flex", "min-height: 0", "overflow: hidden")).toBe(true);
    expect(hasRuleBlock(source, ".config-table", "flex: 1", "min-height: 0", "overflow: hidden")).toBe(true);
  });
});
