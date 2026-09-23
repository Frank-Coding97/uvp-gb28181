import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import { defineComponent, nextTick } from "vue";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ProbeSnapshot } from "@/api/gb28181";

// VChart 会被连带加载的 lottie-web 拖下水,jsdom 下没有 canvas 上下文会整包加载失败。
// 本文件只关心「图表实例何时被创建 / 销毁重建」,用计数替身观察即可。
const vchartStub = vi.hoisted(() => ({ created: 0, updated: 0, released: 0, resized: [] as number[][] }));

vi.mock("@visactor/vchart", () => ({
  default: class {
    constructor() {
      vchartStub.created += 1;
    }
    renderSync() {
      return undefined;
    }
    updateSpecSync() {
      vchartStub.updated += 1;
    }
    resize(width: number, height: number) {
      vchartStub.resized.push([width, height]);
    }
    release() {
      vchartStub.released += 1;
    }
    on() {
      return undefined;
    }
  },
}));

import ProbeTimelineDialog from "./ProbeTimelineDialog.vue";
// 源码原文,用来钉住那条只能靠布局才能暴露的 CSS 契约(见文件末尾的契约用例)。
import dialogSource from "./ProbeTimelineDialog.vue?raw";

const ModalStub = defineComponent({
  name: "ProbeTimelineModalStub",
  inheritAttrs: false,
  props: { visible: Boolean },
  template: `<div v-if="visible"><slot name="title" /><slot /></div>`,
});

/** 快照只填弹窗实际读到的字段:timeline、summary.sampleDurationMs、timestamps、health。 */
function buildSnapshot(): ProbeSnapshot {
  return {
    summary: { sampleDurationMs: 3000, frameCount: 246, totalBytes: 0, averageBitrateKbps: 0 },
    timeline: [
      { sequence: 0, trackType: "video", codec: "H264", keyFrame: true, configFrame: false, relativeTimeMs: 0, frameSize: 20480 },
      { sequence: 1, trackType: "video", codec: "H264", keyFrame: false, configFrame: false, relativeTimeMs: 40, frameSize: 3072 },
      { sequence: 2, trackType: "audio", codec: "PCMA", keyFrame: false, configFrame: false, relativeTimeMs: 60, frameSize: 200 },
      // 40ms 与 1200ms 之间断了 1160ms,超过后端阈值 500ms。
      { sequence: 3, trackType: "video", codec: "H264", keyFrame: true, configFrame: false, relativeTimeMs: 1200, frameSize: 21504 },
      { sequence: 4, trackType: "audio", codec: "PCMA", keyFrame: false, configFrame: false, relativeTimeMs: 1220, frameSize: 200 },
    ],
    timelineTruncated: false,
    timestamps: { videoDtsIntervalMeanMs: 40, arrivalJitterMs: 6, ptsDtsMaxMs: 0, avArrivalSkewMaxMs: 0 },
    // 刻意放两条并存的问题:诊断区必须都列出来(侧栏当年只读 issues[0],第二条会被吞掉)。
    health: {
      status: "warning",
      issues: [
        { code: "large_arrival_gap", message: "帧到达出现连续大间隔", thresholdMs: 500, observedMs: 1160 },
        { code: "missing_keyframe", message: "三秒视频采样窗口内没有关键帧", thresholdMs: 3000, observedMs: 3000 },
      ],
      thresholds: { largeArrivalGapMs: 500, keyFrameWindowMs: 3000 },
    },
  } as unknown as ProbeSnapshot;
}

/** 健康快照:用来核对诊断区在「没问题」时说的是什么。 */
function healthySnapshot(): ProbeSnapshot {
  return {
    ...buildSnapshot(),
    health: { status: "ok", issues: [], thresholds: { largeArrivalGapMs: 500, keyFrameWindowMs: 3000 } },
  } as unknown as ProbeSnapshot;
}

/**
 * 弹窗贴在 props.visible 的 false→true 上做挂载,直接以 visible:true 挂载不会触发,
 * 所以这里先关后开。
 */
async function mountDialog(
  snapshot: ProbeSnapshot = buildSnapshot()
): Promise<VueWrapper<InstanceType<typeof ProbeTimelineDialog>>> {
  const wrapper = mount(ProbeTimelineDialog, {
    props: { visible: false, snapshot },
    global: { stubs: { "a-modal": ModalStub } },
  });
  await wrapper.setProps({ visible: true });
  await nextTick();
  await flushPromises();
  return wrapper;
}

/** jsdom 里 getBoundingClientRect 全是 0,刷选比例要先给它一个真实宽度才算得出来。 */
function stubBrushRect(wrapper: VueWrapper): void {
  const brush = wrapper.get("[data-testid='probe-timeline-brush']").element as HTMLElement;
  brush.getBoundingClientRect = () =>
    ({ left: 0, top: 0, right: 200, bottom: 40, width: 200, height: 40, x: 0, y: 0, toJSON: () => ({}) }) as DOMRect;
}

describe("ProbeTimelineDialog", () => {
  let wrapper: VueWrapper | null = null;
  let savedResizeObserver: typeof globalThis.ResizeObserver | undefined;

  beforeEach(() => {
    vchartStub.created = 0;
    vchartStub.updated = 0;
    vchartStub.released = 0;
    vchartStub.resized = [];
    savedResizeObserver = globalThis.ResizeObserver;
  });

  afterEach(() => {
    wrapper?.unmount();
    wrapper = null;
    if (savedResizeObserver) globalThis.ResizeObserver = savedResizeObserver;
    else delete (globalThis as { ResizeObserver?: unknown }).ResizeObserver;
  });

  it("打开时按视频/音频各建一个图表实例", async () => {
    wrapper = await mountDialog();

    expect(wrapper.find("[data-testid='probe-timeline-dialog']").exists()).toBe(true);
    expect(wrapper.get("[data-testid='probe-timeline-summary']").text()).toContain("1 处到达断档");
    expect(wrapper.get("[data-testid='probe-timeline-range']").text()).toBe("全部");
    expect(vchartStub.created).toBe(2);
  });

  it("诊断区逐条列出后端的问题,并带出实测与阈值对照", async () => {
    wrapper = await mountDialog();

    // 状态徽章直接反映后端三态,不再把 warning 与 error 合并成同一句「需关注」。
    expect(wrapper.get("[data-testid='probe-timeline-status']").text()).toBe("需关注");
    // 首条是 large_arrival_gap 而源端时间戳连续 → 定责指向传输链路。
    expect(wrapper.get("[data-testid='probe-timeline-verdict']").text()).toContain("到达节奏异常");

    const items = wrapper.get("[data-testid='probe-timeline-issues']").findAll("li");
    expect(items).toHaveLength(2);
    expect(items[0].text()).toContain("帧到达出现连续大间隔");
    expect(items[0].text()).toContain("实测 1160 ms ／ 阈值 500 ms");
    expect(items[0].text()).toContain("传输链路");
    // 第二条同样要出得来 —— 侧栏旧实现只读 issues[0],这条会被静默吞掉。
    expect(items[1].text()).toContain("没有关键帧");
    expect(items[1].text()).toContain("GOP");
  });

  it("节奏对照把源端编码节奏与链路到达节奏并排放出来", async () => {
    wrapper = await mountDialog();

    const rhythm = wrapper.get(".ptl-diag-rhythm").text();
    expect(rhythm).toContain("编码节奏");
    expect(rhythm).toContain("40.0 ms");
    expect(rhythm).toContain("到达节奏");
    expect(rhythm).toContain("6.0 ms");
    expect(rhythm).toContain("平稳");
  });

  it("健康快照显示平稳,并明说未发现异常帧间隔", async () => {
    wrapper = await mountDialog(healthySnapshot());

    expect(wrapper.get("[data-testid='probe-timeline-status']").text()).toBe("平稳");
    expect(wrapper.get("[data-testid='probe-timeline-verdict']").text()).toContain("节奏一致");
    expect(wrapper.find("[data-testid='probe-timeline-issues']").exists()).toBe(false);
    expect(wrapper.get(".ptl-diag-clean").text()).toBe("未发现异常帧间隔");
  });

  it("缩放时间范围时销毁重建图表,不走 scatter 不被支持的增量更新", async () => {
    wrapper = await mountDialog();
    stubBrushRect(wrapper);
    const createdBefore = vchartStub.created;

    await wrapper.get("[data-testid='probe-timeline-brush']").trigger("mousedown", { clientX: 40 });
    window.dispatchEvent(new MouseEvent("mousemove", { clientX: 140 }));
    window.dispatchEvent(new MouseEvent("mouseup"));
    await flushPromises();
    await nextTick();
    await flushPromises();

    // 0.2~0.7 的选区内缩到 3000ms 采样窗口 → 600~2100ms。
    expect(wrapper.get("[data-testid='probe-timeline-range']").text()).toBe("600 – 2100 ms");
    // VChart 1.13.10 对 scatter 序列调 updateSpecSync 会在内部 transform 管线抛
    // TypeError(同 spec 换 line/bar 正常),所以横轴范围只能靠重建实例来更新。
    expect(vchartStub.updated).toBe(0);
    expect(vchartStub.created).toBe(createdBefore + 2);
    expect(vchartStub.released).toBe(createdBefore);
  });

  it("重置后回到全量范围,同样重建而不是增量更新", async () => {
    wrapper = await mountDialog();
    stubBrushRect(wrapper);

    await wrapper.get("[data-testid='probe-timeline-brush']").trigger("mousedown", { clientX: 40 });
    window.dispatchEvent(new MouseEvent("mousemove", { clientX: 140 }));
    window.dispatchEvent(new MouseEvent("mouseup"));
    await flushPromises();
    await nextTick();
    await flushPromises();

    const createdBefore = vchartStub.created;
    await wrapper.get("[data-testid='probe-timeline-reset']").trigger("click");
    await flushPromises();
    await nextTick();
    await flushPromises();

    expect(wrapper.get("[data-testid='probe-timeline-range']").text()).toBe("全部");
    expect(vchartStub.updated).toBe(0);
    expect(vchartStub.created).toBe(createdBefore + 2);
  });

  it("宿主尺寸没有真正变化时不重复 resize", async () => {
    // jsdom 没有 ResizeObserver,这里换成一个能手动触发回调的替身。
    const callbacks: Array<(entries: unknown[]) => void> = [];
    globalThis.ResizeObserver = class {
      constructor(callback: (entries: unknown[]) => void) {
        callbacks.push(callback);
      }
      observe() {
        return undefined;
      }
      unobserve() {
        return undefined;
      }
      disconnect() {
        return undefined;
      }
    } as unknown as typeof ResizeObserver;

    wrapper = await mountDialog();
    expect(callbacks.length).toBe(2); // 视频 + 音频各一个观察器

    const fire = (width: number, height: number) => callbacks[0]([{ contentRect: { width, height } }]);

    fire(928, 208);
    expect(vchartStub.resized).toEqual([[928, 208]]);

    // 同一尺寸重复回调(亚像素抖动取整后相同)——这正是「无限变宽」反馈环的空转形态,
    // 不做去重的话每轮都会重绘一次,canvas 尺寸又被改回去再触发观察器。
    fire(928, 208);
    fire(928.4, 208.4);
    expect(vchartStub.resized).toEqual([[928, 208]]);

    fire(760, 208);
    expect(vchartStub.resized).toEqual([
      [928, 208],
      [760, 208],
    ]);
  });

  it("图表宿主的网格轨道与宽度约束不会被改回 auto", () => {
    // ⛔ 这一组只能靠真实布局才能暴露:grid 的 auto 轨道按内容定宽,而宿主的内容就是
    // VChart 那张带 px 宽度的 canvas,轨道跟着 canvas 长、canvas 又被按宿主宽度重设,
    // 于是每轮 +2px(那圈 1px 边框)无限变宽。实机验证过,改成 minmax(0,1fr) 后 7 秒恒定。
    // jsdom 没有布局引擎,所以只能把契约钉在源码上。
    for (const rule of [".ptl {", ".ptl-lane {", ".ptl-brush-section {"]) {
      const start = dialogSource.indexOf(rule);
      expect(start, `样式里找不到 ${rule}`).toBeGreaterThan(-1);
      const body = dialogSource.slice(start, dialogSource.indexOf("}", start));
      expect(body, `${rule} 必须显式写 grid-template-columns: minmax(0, 1fr)`).toContain(
        "grid-template-columns: minmax(0, 1fr)"
      );
    }

    const canvasStart = dialogSource.indexOf(".ptl-canvas {");
    expect(canvasStart).toBeGreaterThan(-1);
    const canvasBody = dialogSource.slice(canvasStart, dialogSource.indexOf("}", canvasStart));
    expect(canvasBody).toContain("min-width: 0");
    expect(canvasBody).toContain("box-sizing: border-box");
    expect(canvasBody).toContain("overflow: hidden");
  });
});
