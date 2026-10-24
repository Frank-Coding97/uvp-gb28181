import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import { hasRuleBlock, ruleBlocks } from "@/test/source-assert";
import PtzCruiseCard from "./PtzCruiseCard.vue";
import PtzPresetCard from "./PtzPresetCard.vue";
import PtzScanCard from "./PtzScanCard.vue";
import PtzWiperCard from "./PtzWiperCard.vue";

const CONTROL_COMPONENTS = [
  "PlayConsoleDialogs.vue",
  "PlayConsolePtzSidebar.vue",
  "PtzPresetCard.vue",
  "PtzCruiseCard.vue",
  "PtzScanCard.vue",
  "PtzHomeCard.vue",
  "PtzWiperCard.vue"
];

function readComponent(name: string) {
  return readFileSync(resolve(process.cwd(), "src/views/gb28181/components/play-console", name), "utf8");
}

describe("播放控制台 PTZ 控件", () => {
  it("标准操作按钮使用 Arco Button，并声明 button 语义和 mini 尺寸", () => {
    const nativeButtons = CONTROL_COMPONENTS.flatMap(name => [...readComponent(name).matchAll(/<button\b/g)].map(() => name));
    expect(nativeButtons).toEqual([]);

    for (const name of CONTROL_COMPONENTS) {
      const source = readComponent(name);
      if (source.includes("<a-button")) {
        expect(source).toContain('html-type="button"');
        expect(source).toContain('size="mini"');
      }
    }
  });

  it("预置位卡保留同步、添加、调用和删除事件", async () => {
    const wrapper = mount(PtzPresetCard, {
      props: {
        presets: [{ id: 1, name: "入口" }],
        visiblePresets: [{ id: 1, name: "入口" }],
        hasMore: false,
        activeId: null,
        moreVisible: false,
        syncing: false,
        syncLabel: "同步",
        syncTitle: "从设备同步",
        tooltipDelay: 0
      }
    });

    await wrapper.get('[data-testid="preset-sync-btn"]').trigger("click");
    await wrapper.get('[data-testid="preset-save-btn"]').trigger("click");
    await wrapper.find(".preset-tile-hit").trigger("click");
    await wrapper.find(".preset-tile-del").trigger("click");

    expect(wrapper.emitted("sync")).toHaveLength(1);
    expect(wrapper.emitted("add")).toHaveLength(1);
    expect(wrapper.emitted("call")).toEqual([[1]]);
    expect(wrapper.emitted("delete")).toEqual([[1]]);
    wrapper.unmount();
  });

  it("扫描卡保留开始、边界和速度命令", async () => {
    const wrapper = mount(PtzScanCard, {
      props: {
        group: 1,
        speed: 2,
        groupMin: 1,
        groupMax: 255,
        speedMin: 1,
        speedMax: 255,
        state: "stopped",
        activeGroup: null,
        canSend: true,
        speedInvalid: false,
        error: ""
      }
    });

    await wrapper.get('[data-testid="scan-toggle"]').trigger("click");
    await wrapper.get('[data-testid="scan-set-left"]').trigger("click");
    await wrapper.get('[data-testid="scan-set-right"]').trigger("click");
    await wrapper.get('[data-testid="scan-set-speed"]').trigger("click");

    expect(wrapper.emitted("command")).toEqual([["scan_start"], ["scan_set_left"], ["scan_set_right"], ["scan_set_speed"]]);
    wrapper.unmount();
  });

  it("雨刷卡点击一次并保留禁用属性", async () => {
    const wrapper = mount(PtzWiperCard, {
      props: { canSend: true, state: "off", error: "", title: "开启雨刷" }
    });

    await wrapper.get('[data-testid="wiper-toggle"]').trigger("click");
    expect(wrapper.emitted("toggle")).toHaveLength(1);
    expect(wrapper.get('[data-testid="wiper-toggle"]').attributes("disabled")).toBeUndefined();
    wrapper.unmount();
  });

  it("巡航卡禁用轨迹不会下发切换事件", async () => {
    const wrapper = mount(PtzCruiseCard, {
      props: {
        tracks: [{ id: 1, name: "默认", enabled: false, pending: false, points: [], source: "device" }],
        visibleTracks: [{ id: 1, name: "默认", enabled: false, pending: false, points: [], source: "device" }],
        hasMore: false,
        activeId: null,
        state: "stopped",
        moreVisible: false,
        syncing: false,
        syncLabel: "同步",
        syncTitle: "从设备同步",
        loadError: "",
        canAdd: false,
        tooltipDelay: 0,
        tileTitle: () => "默认",
        tileState: () => "disabled"
      }
    });

    const tile = wrapper.get(".cruise-item");
    expect(tile.attributes("disabled")).toBeDefined();
    await tile.trigger("click");
    expect(wrapper.emitted("toggle")).toBeUndefined();
    wrapper.unmount();
  });

  it("镜头连续控制同时保留指针生命周期和键盘按下/释放语义", () => {
    const source = readComponent("PlayConsolePtzSidebar.vue");
    expect(source.match(/@pointerdown\.prevent="sendPtz/g)).toHaveLength(6);
    expect(source.match(/@pointerup\.prevent="sendPtz/g)).toHaveLength(6);
    expect(source.match(/@pointerleave="sendPtz/g)).toHaveLength(6);
    expect(source.match(/@pointercancel="sendPtz/g)).toHaveLength(6);
    expect(source.match(/@keydown\.enter\.prevent="sendPtz/g)).toHaveLength(6);
    expect(source.match(/@keyup\.enter\.prevent="sendPtz/g)).toHaveLength(6);
    expect(source).toContain(":aria-pressed=\"ptzMode === 'speed'\"");
    expect(source).toContain(":aria-pressed=\"ptzMode === 'precise'\"");
  });

  it("PTZ 禁用动作保留文字层级，不用整体透明度压暗控件", () => {
    const source = readComponent("PlayConsolePtzSidebar.vue");
    for (const selector of [
      '.talk-mode-switch button.arco-btn[type="button"]:disabled',
      '.talk-button.arco-btn[type="button"]:disabled',
      '.drag-zoom-switch button.arco-btn[type="button"]:disabled',
      '.ptz-iframe button.arco-btn[type="button"]:disabled',
      '.target-track-switch button.arco-btn[type="button"]:disabled'
    ]) {
      expect(
        hasRuleBlock(
          source,
          selector,
          "color: var(--uvp-text-disabled)",
          "background: var(--uvp-dialog-control-bg)",
          "opacity: 1"
        )
      ).toBe(true);
      expect(ruleBlocks(source, selector).join(" ")).not.toMatch(/opacity:\s*0\./);
    }
    for (const selector of [
      '.talk-mode-switch button.arco-btn[type="button"].active:disabled',
      '.drag-zoom-switch button.arco-btn[type="button"].active:disabled'
    ]) {
      expect(hasRuleBlock(source, selector, "background: var(--uvp-brand-soft)")).toBe(true);
    }
    expect(
      hasRuleBlock(
        readComponent("PtzWiperCard.vue"),
        '.wiper-toggle.arco-btn[type="button"]:disabled',
        "opacity: 1",
        "color: var(--uvp-text-disabled)"
      )
    ).toBe(true);
  });
});
