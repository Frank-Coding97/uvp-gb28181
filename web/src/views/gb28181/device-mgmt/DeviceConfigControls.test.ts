import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { defineComponent, h } from "vue";
import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import DeviceConfigOsdBlocks from "./DeviceConfigOsdBlocks.vue";
import DeviceConfigSlider from "./DeviceConfigSlider.vue";
import DeviceConfigWeekPlan from "./DeviceConfigWeekPlan.vue";

const CONTROL_COMPONENTS = [
  "DeviceConfigDrawer.vue",
  "DeviceConfigOsdBlocks.vue",
  "DeviceConfigSlider.vue",
  "DeviceConfigTextItems.vue",
  "DeviceConfigWeekPlan.vue"
];

const ArcoButtonStub = defineComponent({
  inheritAttrs: false,
  props: {
    disabled: Boolean,
    type: { type: String, default: "" },
    htmlType: { type: String, default: "" }
  },
  emits: ["click"],
  setup(props, { attrs, emit, slots }) {
    return () =>
      h(
        "button",
        {
          ...attrs,
          type: props.htmlType || "button",
          disabled: props.disabled,
          "data-arco-type": props.type,
          onClick: (event: MouseEvent) => emit("click", event)
        },
        [slots.icon?.(), slots.default?.()]
      );
  }
});

const ArcoSliderStub = defineComponent({
  inheritAttrs: false,
  props: { modelValue: { type: Number, default: 0 }, disabled: Boolean },
  emits: ["update:modelValue"],
  setup(props, { attrs }) {
    return () => h("input", { ...attrs, type: "range", value: props.modelValue, disabled: props.disabled });
  }
});

const ArcoInputStub = defineComponent({
  inheritAttrs: false,
  props: { modelValue: { type: [String, Number], default: "" }, disabled: Boolean },
  emits: ["change", "pressEnter"],
  setup(props, { attrs }) {
    return () => h("input", { ...attrs, value: props.modelValue, disabled: props.disabled });
  }
});

const ArcoSelectStub = { template: "<select><slot /></select>" };
const ArcoOptionStub = { template: "<option><slot /></option>" };

function source(name: string): string {
  return readFileSync(resolve(process.cwd(), "src/views/gb28181/device-mgmt", name), "utf8");
}

const componentStubs = {
  "a-button": ArcoButtonStub,
  "a-input": ArcoInputStub,
  "a-slider": ArcoSliderStub,
  "a-select": ArcoSelectStub,
  "a-option": ArcoOptionStub
};

describe("播放控制台共享设备配置控件", () => {
  it("标准操作按钮使用 Arco，并显式声明 button 语义", () => {
    const nativeButtons = CONTROL_COMPONENTS.flatMap(name => [...source(name).matchAll(/<button\b/g)].map(() => name));
    expect(nativeButtons).toEqual([]);

    for (const name of CONTROL_COMPONENTS) {
      const content = source(name);
      const buttonCount = content.match(/<a-button\b/g)?.length ?? 0;
      expect(content.match(/html-type="button"/g)?.length ?? 0, `${name} 的 Arco 按钮缺少 html-type`).toBe(buttonCount);
      expect(content.match(/\s(?:[:]?type)=/g)?.length ?? 0, `${name} 的 Arco 按钮缺少语义 type`).toBe(buttonCount);
    }
  });

  it("共享配置控件保留禁用态、暗色态与键盘焦点反馈", () => {
    const drawer = source("DeviceConfigDrawer.vue");
    expect(drawer).toContain("&:disabled");
    expect(drawer).toContain(":not(.is-on, :disabled)");
    expect(drawer).toContain(".is-on:not(:disabled)");

    const slider = source("DeviceConfigSlider.vue");
    expect(slider).toContain(".arco-slider-btn:focus-visible::after");
    expect(slider).toContain(".arco-slider-track-disabled .arco-slider-btn:hover::after");
    expect(slider).toContain("transform: none");

    const osd = source("DeviceConfigOsdBlocks.vue");
    expect(osd).toContain("background: var(--uvp-warning-soft)");
    expect(osd).toContain("border: 1px dashed var(--uvp-warning-border)");
  });

  it("数字滑块的加减仍按 step 工作并在边界 clamp", async () => {
    const wrapper = mount(DeviceConfigSlider, {
      props: { modelValue: "99", min: 0, max: 100, step: 2, label: "帧率" },
      global: { stubs: componentStubs }
    });

    await wrapper.get("[aria-label='增大帧率']").trigger("click");
    await wrapper.get("[aria-label='减小帧率']").trigger("click");

    expect(wrapper.emitted("update:modelValue")).toEqual([["100"], ["97"]]);
  });

  it("OSD 开关仍发出布尔更新事件，并保留 Arco 按钮的状态语义", async () => {
    const wrapper = mount(DeviceConfigOsdBlocks, {
      props: {
        timeEnable: true,
        timeType: "1",
        timeX: "10",
        timeY: "20",
        textEnable: false,
        items: [],
        canvas: { width: 1920, height: 1080 },
        maxItems: 8,
        canvasLinked: true
      },
      global: { stubs: componentStubs }
    });

    const toggle = wrapper.get("[data-testid='osd-time-switch']");
    expect(toggle.attributes("aria-pressed")).toBe("true");
    await toggle.trigger("click");
    expect(wrapper.emitted("update:timeEnable")).toEqual([[false]]);
  });

  it("OSD 开关圆点被限制在固定轨道内，不受 Arco 行高影响而溢出", () => {
    const osd = source("DeviceConfigOsdBlocks.vue");
    expect(osd).toContain("box-sizing: border-box");
    expect(osd).toContain("overflow: hidden");
    expect(osd).toContain("line-height: 0");
    expect(osd).toContain("flex: 0 0 12px");
  });

  it("周计划开关仍生成全天时段，而不是零长度时段", async () => {
    const wrapper = mount(DeviceConfigWeekPlan, {
      props: { modelValue: [] },
      global: { stubs: componentStubs }
    });

    await wrapper.get("[data-testid='dcw-toggle-1']").trigger("click");
    expect(wrapper.emitted("update:modelValue")).toEqual([
      [[{ weekDayNum: 1, segments: [{ start: "00:00:00", stop: "23:59:59" }] }]]
    ]);
  });
});
