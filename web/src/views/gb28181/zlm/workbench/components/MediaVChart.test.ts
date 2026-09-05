import { flushPromises, mount } from "@vue/test-utils";
import { defineComponent, nextTick } from "vue";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const chart = vi.hoisted(() => ({
  created: 0,
  rendered: 0,
  updated: 0,
  resized: 0,
  released: 0,
  crosshairActive: false,
  layoutStart: null as (() => void) | null,
  specs: [] as Array<Record<string, unknown>>,
  instances: [] as Array<{ release: () => void }>
}));

vi.mock("@visactor/vchart", () => ({
  default: class {
    constructor(spec: Record<string, unknown>) {
      chart.created += 1;
      chart.specs.push(spec);
      chart.instances.push(this);
    }

    renderSync() {
      chart.rendered += 1;
    }

    on(event: string, callback: () => void) {
      if (event === "layoutStart") chart.layoutStart = callback;
    }

    updateSpecSync(spec: Record<string, unknown>) {
      if (chart.crosshairActive) throw new Error("stale crosshair references previous series data");
      chart.updated += 1;
      chart.specs.push(spec);
    }

    resize() {
      chart.resized += 1;
    }

    getComponents() {
      return [{ hideCrosshair: () => { chart.crosshairActive = false; } }];
    }

    hideTooltip() { return true; }

    release() {
      chart.released += 1;
    }
  }
}));

import MediaVChart from "./MediaVChart.vue";

class ResizeObserverStub {
  static latest: ResizeObserverStub | undefined;
  readonly callback: ResizeObserverCallback;
  disconnected = false;

  constructor(callback: ResizeObserverCallback) {
    this.callback = callback;
    ResizeObserverStub.latest = this;
  }

  observe() { return undefined; }

  disconnect() { this.disconnected = true; }

  emit(width = 640, height = 300) {
    this.callback([{ contentRect: { width, height } } as ResizeObserverEntry], this as unknown as ResizeObserver);
  }
}

function matchMedia(matches = false) {
  return { matches, media: "(prefers-reduced-motion: reduce)", onchange: null, addListener: vi.fn(), removeListener: vi.fn(), addEventListener: vi.fn(), removeEventListener: vi.fn(), dispatchEvent: vi.fn() };
}

describe("MediaVChart", () => {
  beforeEach(() => {
    chart.created = 0;
    chart.rendered = 0;
    chart.updated = 0;
    chart.resized = 0;
    chart.released = 0;
    chart.crosshairActive = false;
    chart.layoutStart = null;
    chart.specs.length = 0;
    chart.instances.length = 0;
    ResizeObserverStub.latest = undefined;
    vi.stubGlobal("ResizeObserver", ResizeObserverStub);
    vi.stubGlobal("matchMedia", () => matchMedia(false));
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("creates once, updates synchronously, resizes, and releases on unmount", async () => {
    const wrapper = mount(MediaVChart, {
      props: { title: "节点负载", spec: { type: "bar" }, summary: "节点 2 当前负载 42%" }
    });
    await flushPromises();

    expect(chart.created).toBe(1);
    expect(chart.rendered).toBe(1);
    await wrapper.setProps({ spec: { type: "line" } });
    expect(chart.created).toBe(1);
    expect(chart.updated).toBe(1);
    ResizeObserverStub.latest?.emit();
    expect(chart.resized).toBe(1);
    expect(wrapper.get("[role='img']").attributes("aria-label")).toContain("节点负载");
    expect(wrapper.get("[role='img']").attributes("aria-describedby")).toBeTruthy();
    expect(wrapper.text()).toContain("节点 2 当前负载 42%");
    wrapper.unmount();
    expect(chart.released).toBe(1);
    expect(ResizeObserverStub.latest?.disconnected).toBe(true);
  });

  it("clears hovered crosshair state before replacing series data or theme colors", async () => {
    const wrapper = mount(MediaVChart, { props: { title: "实时曲线", spec: { type: "area", color: ["#2563eb"] } } });
    chart.crosshairActive = true;
    await wrapper.setProps({ spec: { type: "area", color: ["#60a5fa"] } });
    expect(chart.crosshairActive).toBe(false);
    expect(chart.updated).toBe(1);
    chart.crosshairActive = true;
    chart.layoutStart?.();
    expect(chart.crosshairActive).toBe(false);
    wrapper.unmount();
  });

  it("honors chart-specific rolling motion and turns it off for reduced-motion", () => {
    const rollingSpec = {
      type: "line",
      animationAppear: { duration: 300 },
      animationUpdate: { duration: 450 },
      animationExit: { duration: 300 }
    };
    const wrapper = mount(MediaVChart, { props: { title: "趋势", spec: rollingSpec } });
    expect(chart.specs[0]).toMatchObject({ animationAppear: { duration: 300 }, animationUpdate: { duration: 450 }, animationExit: { duration: 300 } });
    wrapper.unmount();

    chart.specs.length = 0;
    vi.stubGlobal("matchMedia", () => matchMedia(true));
    const reduced = mount(MediaVChart, { props: { title: "趋势", spec: rollingSpec } });
    expect(chart.specs[0]).toMatchObject({ animationAppear: false, animationUpdate: false, animationExit: false });
    reduced.unmount();
  });

  it("renders explicit empty, unknown, and unavailable states without mounting a chart", () => {
    for (const status of ["empty", "unknown", "unavailable"] as const) {
      const wrapper = mount(MediaVChart, {
        props: { title: "节点健康", status, statusText: `${status} 状态`, spec: { type: "bar" } }
      });
      expect(wrapper.find("[role='img']").exists()).toBe(false);
      expect(wrapper.get("[role='status']").text()).toContain(`${status} 状态`);
      wrapper.unmount();
    }
    expect(chart.created).toBe(0);
    const partial = mount(MediaVChart, {
      props: { title: "节点健康", status: "partial", statusText: "partial 状态", spec: { type: "bar" } }
    });
    expect(partial.find("[role='img']").exists()).toBe(true);
    partial.unmount();
  });

  it("renders an optional compact legend and can visually hide the technical summary", () => {
    const wrapper = mount(MediaVChart, {
      props: {
        title: "媒体速率趋势",
        spec: { type: "line" },
        legendLabel: "媒体速率（KB/s）",
        summary: "当前节点已记录 27 个采样点",
        showSummary: false
      }
    });

    expect(wrapper.get(".media-vchart__legend").text()).toBe("媒体速率（KB/s）");
    expect(wrapper.get(".media-vchart__summary").classes()).toContain("media-vchart__summary--sr-only");
    expect(wrapper.get("[role='img']").attributes("aria-describedby")).toBeTruthy();
    wrapper.unmount();
  });

  it("releases an inactive chart and can recreate it when explicitly activated", async () => {
    const wrapper = mount(MediaVChart, { props: { title: "节点", spec: { type: "bar" }, active: true } });
    await wrapper.setProps({ active: false });
    expect(chart.released).toBe(1);
    expect(ResizeObserverStub.latest?.disconnected).toBe(true);
    await wrapper.setProps({ active: true });
    expect(chart.created).toBe(2);
    wrapper.unmount();
  });

  it("releases the chart through Vue KeepAlive deactivation", async () => {
    const harness = defineComponent({
      components: { MediaVChart },
      data: () => ({ visible: true }),
      template: `<KeepAlive><MediaVChart v-if="visible" title="节点" :spec="{ type: 'bar' }" /></KeepAlive>`
    });
    const wrapper = mount(harness);
    await flushPromises();
    expect(chart.created).toBe(1);
    wrapper.vm.visible = false;
    await nextTick();
    expect(chart.released).toBe(1);
    wrapper.vm.visible = true;
    await nextTick();
    expect(chart.created).toBe(2);
    wrapper.unmount();
  });
});
