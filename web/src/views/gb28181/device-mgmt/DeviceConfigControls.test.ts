import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { defineComponent, h, ref } from "vue";
import { Input } from "@arco-design/web-vue";
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
  it.each([false, true])("真实 Arco 数值输入允许逐字输入，失焦时才限制范围（父组件转数字：%s）", async numericParent => {
    const value = ref<string | number>("60");
    const wrapper = mount(
      defineComponent({
        setup: () => () =>
          h(DeviceConfigSlider, {
            modelValue: value.value,
            min: 10,
            max: 120,
            "onUpdate:modelValue": (v: string) => {
              value.value = numericParent ? Number(v) : v;
            }
          })
      }),
      {
        global: { stubs: { "a-input": { ...Input } } }
      }
    );
    const input = wrapper.get(".cfg-slider-input input");
    (input.element as HTMLInputElement).value = "1";
    await input.trigger("input");
    expect((input.element as HTMLInputElement).value).toBe("1");
    (input.element as HTMLInputElement).value = "108";
    await input.trigger("input");
    await input.trigger("blur");
    expect(String(value.value)).toBe("108");
    (input.element as HTMLInputElement).value = "999";
    await input.trigger("input");
    await input.trigger("blur");
    expect(String(value.value)).toBe("120");
    wrapper.unmount();
  });

  it("真实 Arco 周计划时间输入保留编辑值与秒数", async () => {
    const value = ref([{ weekDayNum: 1, segments: [{ start: "08:00:00", stop: "18:00:00" }] }]);
    const wrapper = mount(
      defineComponent({
        setup: () => () =>
          h(DeviceConfigWeekPlan, {
            modelValue: value.value,
            "onUpdate:modelValue": v => {
              value.value = v;
            }
          })
      }),
      {
        global: { stubs: { "a-input": { ...Input } } }
      }
    );
    const input = wrapper.get("[data-testid='dcw-start-1-0'] input");
    (input.element as HTMLInputElement).value = "09:15:30";
    await input.trigger("input");
    expect((input.element as HTMLInputElement).value).toBe("09:15:30");
    await input.trigger("blur");
    expect(value.value[0].segments[0].start).toBe("09:15:30");
    wrapper.unmount();
  });

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
    expect(drawer).toContain(".dcg-actions .dcg-btn:disabled");
    expect(drawer).toContain("--uvp-text-disabled: var(--uvp-text-secondary)");
    expect(drawer).toContain(".dcg-actions .dcg-btn.is-primary:disabled");
    expect(drawer).toContain("--uvp-dialog-control-bg: var(--uvp-secondary-action-bg)");
    expect(drawer).toContain("--uvp-panel-border: var(--uvp-secondary-action-border)");

    const slider = source("DeviceConfigSlider.vue");
    expect(slider).toContain(".arco-slider-btn:focus-visible::after");
    expect(slider).toContain(".arco-slider-track-disabled .arco-slider-btn:hover::after");
    expect(slider).toContain("transform: none");

    const osd = source("DeviceConfigOsdBlocks.vue");
    expect(osd).toContain("background: var(--uvp-warning-soft)");
    expect(osd).toContain("border: 1px dashed var(--uvp-warning-border)");
  });

  /**
   * 底部操作条（2026-11-03 按钮专题）。
   *
   * ⛔⛔ 主按钮原来写 `color:#ffffff; background: var(--uvp-brand, #2563eb)`。
   *    带**兜底值**的写法绕过了 `style/model/solid-surface-token.test.ts` 的全仓扫描
   *    （那条正则要求 `var(--uvp-brand)` 紧跟右括号），于是暗色下「下发到设备」
   *    一直是**白字压浅蓝 #60a5fa**（对比度 ~2.5:1），整块发白糊掉。
   *    亮色下两种写法都是 #2563eb，看不出来 —— 只有暗色才现原形。
   * ⇒ 断言"实心面必须走 solid token"，且**不许再出现带兜底值的 brand 底色**。
   *    注意不能对整份文件 `not.toContain("var(--uvp-brand, #2563eb)")`：标题栏那根
   *    3px 竖条、重读状态点、分段控件用的是**装饰/文字色**，用 brand 是对的。
   */
  it("底栏主按钮走实心面 token，尺寸对齐对话框房规", () => {
    const drawer = source("DeviceConfigDrawer.vue");

    const primary = drawer.match(/&\.is-primary:not\(:disabled\)\s*\{([^}]*)\}/);
    expect(primary, "主按钮的静态态规则不见了").not.toBeNull();
    expect(primary?.[1]).toContain("color: var(--uvp-solid-text)");
    expect(primary?.[1]).toContain("background: var(--uvp-solid-bg)");

    const primaryHover = drawer.match(/&\.is-primary:hover:not\(:disabled\)\s*\{([^}]*)\}/);
    expect(primaryHover, "主按钮的 hover 规则不见了").not.toBeNull();
    expect(primaryHover?.[1]).toContain("background: var(--uvp-solid-hover-bg)");

    // 选中态的分组导航也是"白字压实心面"，同一个坑。
    const navActive = drawer.match(/&\.is-active\s*\{([^}]*)\}/);
    expect(navActive?.[1]).toContain("background: var(--uvp-solid-bg)");

    // 尺寸：底栏三个按钮按 `.uvp-system-dialog` 那套（32 / 圆角 8 / min-width 88），
    // 不再是 26px 的迷你工具条 —— 否则同一颗对话框里两套按钮尺寸。
    const sizing = drawer.match(/\.dcg-actions \.dcg-btn\.arco-btn\[type="button"\]\s*\{([^}]*)\}/);
    expect(sizing, "底栏按钮尺寸规则不见了").not.toBeNull();
    expect(sizing?.[1]).toContain("height: 32px");
    expect(sizing?.[1]).toContain("min-width: 88px");
    expect(sizing?.[1]).toContain("border-radius: 8px");
  });

  /**
   * 底栏按钮的"进行中"反馈。
   *
   * ⛔⛔ 转圈必须由 Arco 的 `:loading` 给，而且**图标要放 `#icon` 槽**。
   *    Arco 的渲染是 `loading ? <IconLoading/> : <slot name="icon"/>`，默认槽照常渲染
   *    —— 图标留在默认槽里的话，加载中会变成"**两个图标** + 文字"。
   *    （测试环境把 `a-button` 换成桩，类名断言不到 Arco 内部，所以在源码层面钉死。）
   */
  it("底栏读取/下发用 Arco loading，并把图标放进 #icon 槽", () => {
    const drawer = source("DeviceConfigDrawer.vue");
    const footer = drawer.match(/<footer v-if="navSide"[\s\S]*?<\/footer>/)?.[0] ?? "";
    expect(footer, "底栏不见了").not.toBe("");
    expect(footer).toContain(':loading="activeLoading"');
    expect(footer).toContain(':loading="activeApplying"');
    expect(footer.match(/<template #icon>/g)?.length).toBe(3);
    // 点不动要说为什么：三个按钮都得有 title。
    expect(footer.match(/:title="footerHint\('(?:reset|read|apply)'\)"/g)?.length).toBe(3);
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

  /**
   * 2026-11-05 老板以为时间轴能拖选 —— 根源是轴上写着「点这里新增时段」但那行字**没绑点击**，
   * 手型光标 + 看着能点的空白区，点了什么都不发生。这三条钉住"点了到底有没有反应"。
   */
  it("点时间轴空白处真的能新增一段全天时段", async () => {
    const wrapper = mount(DeviceConfigWeekPlan, {
      props: { modelValue: [] },
      global: { stubs: componentStubs }
    });

    await wrapper.get("[data-testid='dcw-track-2']").trigger("click");
    expect(wrapper.emitted("update:modelValue")).toEqual([
      [[{ weekDayNum: 2, segments: [{ start: "00:00:00", stop: "23:59:59" }] }]]
    ]);
  });

  it("点已有色块只定位到输入框，不顺手再新增一段", async () => {
    const wrapper = mount(DeviceConfigWeekPlan, {
      props: { modelValue: [{ weekDayNum: 1, segments: [{ start: "08:00:00", stop: "18:00:00" }] }] },
      global: { stubs: componentStubs }
    });

    await wrapper.get("[data-testid='dcw-block-1-0']").trigger("click");
    expect(wrapper.emitted("update:modelValue")).toBeUndefined();
  });

  it("禁用时点时间轴不会偷偷改计划", async () => {
    const wrapper = mount(DeviceConfigWeekPlan, {
      props: { modelValue: [], disabled: true },
      global: { stubs: componentStubs }
    });

    await wrapper.get("[data-testid='dcw-track-1']").trigger("click");
    expect(wrapper.emitted("update:modelValue")).toBeUndefined();
  });
});
