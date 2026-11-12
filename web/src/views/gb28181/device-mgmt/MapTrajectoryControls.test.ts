import { mount, type Stubs } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import MapTrajectoryControls from "./MapTrajectoryControls.vue";
import type { TrackPoint } from "./mapTrajectory";

function point(longitude: number, latitude: number, seconds: number): TrackPoint {
  return {
    eventTime: new Date(Date.UTC(2026, 9, 21, 2, 0, seconds)).toISOString(),
    longitude,
    latitude
  };
}

const twoPoints = [point(116.4, 39.9, 0), point(116.41, 39.9, 30)];

const stubs: Stubs = {
  "a-range-picker": {
    props: ["modelValue"],
    template: "<div data-testid='range' />"
  },
  "a-button": {
    props: ["disabled", "loading"],
    // ⛔ 不要写 `@click='$emit("click")'`：stub 的根元素会**同时**收到父组件透传的
    //    原生 onClick 和这次 $emit，一次真实点击会触发两遍 handler。
    template: "<button :disabled='disabled'><slot name='icon' /><slot /></button>"
  },
  "a-slider": {
    props: ["modelValue"],
    template: "<div data-testid='slider' :data-value='modelValue' />"
  },
  "a-select": {
    props: ["modelValue", "options"],
    template: "<div data-testid='speed' :data-value='modelValue' />"
  }
};

function mountPanel(props: Record<string, unknown> = {}) {
  return mount(MapTrajectoryControls, {
    props: {
      visible: true,
      channelName: "执法仪-01",
      points: twoPoints,
      distanceMeters: 1234,
      progress: 0,
      speed: 1,
      ...props
    },
    global: { stubs }
  });
}

describe("MapTrajectoryControls", () => {
  it("不可见时不渲染任何内容", () => {
    const wrapper = mountPanel({ visible: false });
    expect(wrapper.find(".trajectory-panel").exists()).toBe(false);
  });

  it("展示通道名", () => {
    const wrapper = mountPanel();
    expect(wrapper.text()).toContain("执法仪-01");
  });

  it("有点时展示点数与里程", () => {
    const wrapper = mountPanel();
    expect(wrapper.text()).toContain("2");
    expect(wrapper.text()).toContain("1.2 公里");
  });

  // 空轨迹必须给出**具体原因**（开关没开 / 设备不上报 / 换个时间段），
  // 统一成"暂无轨迹"会让用户去三个不相干的地方找。
  it("空态展示传入的原因文案", () => {
    const wrapper = mountPanel({
      points: [],
      distanceMeters: 0,
      emptyText: "位置历史记录未开启，平台没有保存轨迹。"
    });
    expect(wrapper.text()).toContain("位置历史记录未开启");
  });

  it("没有空态文案时给兜底提示", () => {
    const wrapper = mountPanel({ points: [], distanceMeters: 0 });
    expect(wrapper.text()).toContain("该时间段内没有轨迹");
  });

  it("抽稀时显示标记，避免被当成设备漏报", () => {
    const wrapper = mountPanel({ truncated: true });
    expect(wrapper.text()).toContain("已抽稀");
  });

  it("未抽稀时不显示标记", () => {
    const wrapper = mountPanel();
    expect(wrapper.text()).not.toContain("已抽稀");
  });

  it("少于两个点时禁用回放（一条线才走得起来）", () => {
    const wrapper = mountPanel({ points: [twoPoints[0]] });
    const play = wrapper.find(".trajectory-panel__play");
    expect(play.attributes("disabled")).toBeDefined();
  });

  it("点击播放派发 toggle 事件", async () => {
    const wrapper = mountPanel();
    await wrapper.find(".trajectory-panel__play").trigger("click");
    expect(wrapper.emitted("toggle")).toHaveLength(1);
  });

  it("无播放权限时回放按钮禁用", () => {
    const wrapper = mountPanel({ canPlay: false });
    expect(wrapper.find(".trajectory-panel__play").attributes("disabled")).toBeDefined();
  });

  it("点击关闭派发 close 事件", async () => {
    const wrapper = mountPanel();
    await wrapper.find(".trajectory-panel__close").trigger("click");
    expect(wrapper.emitted("close")).toHaveLength(1);
  });

  it("点查询派发 search，并带上默认的最近一小时窗口", async () => {
    const wrapper = mountPanel();
    const buttons = wrapper.findAll("button").filter(button => button.text().includes("查询"));
    expect(buttons).toHaveLength(1);
    await buttons[0].trigger("click");
    const emitted = wrapper.emitted("search");
    expect(emitted).toHaveLength(1);
    const range = emitted![0][0] as { startTime: string; endTime: string };
    expect(range.startTime).toMatch(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/);
    expect(range.endTime > range.startTime).toBe(true);
  });

  it("回放中把当前时刻显示出来", () => {
    const wrapper = mountPanel({ playing: true, currentTime: "10:20:30" });
    expect(wrapper.text()).toContain("回放到 10:20:30");
  });

  it("进度以百分比传给滑块", () => {
    const wrapper = mountPanel({ progress: 0.42 });
    expect(wrapper.find("[data-testid='slider']").attributes("data-value")).toBe("42");
  });
});
