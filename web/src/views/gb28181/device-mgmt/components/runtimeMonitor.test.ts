import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const trafficApi = vi.hoisted(() => ({
    getTrafficSummary: vi.fn(),
    getTrafficTrend: vi.fn(),
    getTrafficRealtime: vi.fn(),
    getTrafficCoverage: vi.fn(),
    getTrafficSessions: vi.fn(),
    getCurrentViewers: vi.fn(),
    kickCurrentViewer: vi.fn()
}));
const modal = vi.hoisted(() => ({ confirm: vi.fn() }));
const vchart = vi.hoisted(() => ({
    configs: [] as Array<Record<string, any>>,
    events: [] as Array<(params: Record<string, any>) => void>
}));

vi.mock("../trafficApi", () => trafficApi);
vi.mock("@arco-design/web-vue", () => ({
    Message: { success: vi.fn(), error: vi.fn() },
    Modal: modal
}));
vi.mock("@visactor/vchart", () => ({
    default: class {
        constructor(config: Record<string, any>) { vchart.configs.push(config); }
        renderSync() { return undefined; }
        release() { return undefined; }
        resize() { return undefined; }
        on(_type: string, _query: Record<string, any>, handler: (params: Record<string, any>) => void) { vchart.events.push(handler); }
    }
}));

import TrafficTrend from "./TrafficTrend.vue";
import ViewerTable from "./ViewerTable.vue";

const passthrough = { template: "<div><slot name='icon' /><slot /></div>" };
const buttonStub = {
    emits: ["click"],
    template: "<button :aria-label='$attrs[`aria-label`]' @click='$emit(`click`)'><slot name='icon' /><slot /></button>"
};
const viewerTableStub = {
    props: ["data", "expandedKeys", "loading"],
    provide() { return { viewerTable: this }; },
    template: "<div class='viewer-table-stub' :data-loading='String(loading)'><slot name='columns' /><template v-for='record in data'><slot v-if='expandedKeys?.includes(record.channelId)' name='expand-row' :record='record' /></template><slot v-if='!data.length' name='empty' /></div>"
};
const viewerColumnStub = {
    props: ["title", "dataIndex"],
    inject: ["viewerTable"],
    template: "<div>{{ title }}<template v-for='record in viewerTable.data'><span v-if='dataIndex'>{{ record[dataIndex] }}</span><slot v-else name='cell' :record='record' /></template></div>"
};

function ok<T>(data: T) {
    return { code: 0, message: "", data };
}

function deferred<T>() {
    let resolvePromise!: (value: T) => void;
    const promise = new Promise<T>(resolve => { resolvePromise = resolve; });
    return { promise, resolve: resolvePromise };
}

describe("runtime monitor components", () => {
    beforeEach(() => {
        Object.values(trafficApi).forEach(mock => mock.mockReset());
        modal.confirm.mockReset();
        vchart.configs.length = 0;
        vchart.events.length = 0;
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    it("renders daily traffic as stacked columns with a total line", async () => {
        trafficApi.getTrafficSummary.mockResolvedValue(ok({
            upstreamBytes: 2048,
            downstreamBytes: 1024,
            totalBytes: 3072,
            upstreamDurationSeconds: 0,
            downstreamDurationSeconds: 0,
            upstreamSessions: 0,
            downstreamSessions: 0,
            from: "2026-08-15",
            to: "2026-08-21",
            timezone: "UTC"
        }));
        trafficApi.getTrafficTrend.mockResolvedValue(ok({
            list: [{ date: "2026-08-21", upstreamBytes: 2048, downstreamBytes: 1024, totalBytes: 3072 }],
            timezone: "UTC"
        }));
        trafficApi.getTrafficRealtime.mockResolvedValue(ok({ list: [], estimated: true }));
        trafficApi.getTrafficCoverage.mockResolvedValue(ok({ coverage: "complete", gaps: [] }));

        const wrapper = mount(TrafficTrend, {
            props: { deviceId: "device-1" },
            global: { stubs: {
                "a-spin": passthrough,
                "a-button": buttonStub,
                "a-tooltip": { props: ["content"], template: "<span class='tooltip-stub' :data-content='content'><slot /></span>" },
                "a-empty": { props: ["description"], template: "<div>{{ description }}</div>" }
            } }
        });
        await flushPromises();

        expect(wrapper.text()).toContain("7天");
        expect(wrapper.text()).toContain("24小时");
        expect(wrapper.find(".trend-table-wrap").exists()).toBe(false);
        expect(wrapper.findAll(".metric-card")).toHaveLength(3);
        expect(wrapper.text()).not.toContain("进行中上行");
        expect(wrapper.text()).not.toContain("当前观看");
        expect(wrapper.findAll(".metric-help")).toHaveLength(3);
        expect(wrapper.find("[aria-label='说明：已结算上行']").exists()).toBe(true);
        expect(wrapper.find("[aria-label='说明：已结算下行']").exists()).toBe(true);
        expect(wrapper.find("[aria-label='说明：已结算总量']").exists()).toBe(true);
        expect(wrapper.findAll(".tooltip-stub").map(item => item.attributes("data-content"))).toEqual([
            "设备或摄像机发送到平台流媒体服务器的数据量",
            "平台流媒体服务器发送给浏览器、客户端等观看端的数据量",
            "上行流量与下行流量之和"
        ]);
        expect(vchart.configs).toHaveLength(1);
        expect(vchart.configs[0].type).toBe("common");
        expect(vchart.configs[0].series.map((series: { type: string }) => series.type)).toEqual(["bar", "line"]);
        expect(vchart.configs[0].series[0].stack).toBe(true);
        expect(vchart.configs[0].tooltip.activeType).toBe("dimension");
        expect(vchart.configs[0].background).toBe("transparent");
        const params = trafficApi.getTrafficSummary.mock.calls[0][0];
        expect(Date.parse(`${params.to}T00:00:00Z`) - Date.parse(`${params.from}T00:00:00Z`)).toBe(6 * 86400000);
    });

    it("does not override the metric icon centering display", () => {
        const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/device-mgmt/components/TrafficTrend.vue"), "utf8");
        expect(source).not.toContain(".metric-card span, .metric-card small");
        expect(source).toContain(".metric-card > div > span, .metric-card small");
    });

    it("switches to real hourly buckets and opens the paged ledger from a bar", async () => {
        trafficApi.getTrafficSummary.mockResolvedValue(ok({
            upstreamBytes: 2048, downstreamBytes: 1024, totalBytes: 3072,
            upstreamDurationSeconds: 0, downstreamDurationSeconds: 0,
            upstreamSessions: 0, downstreamSessions: 0,
            from: "2026-08-20T16:00:00+08:00", to: "2026-08-21T16:00:00+08:00",
            timezone: "Asia/Shanghai", granularity: "hour"
        }));
        trafficApi.getTrafficTrend.mockResolvedValue(ok({
            list: [{ bucket: "2026-08-21T15:00:00+08:00", date: "2026-08-21T15:00:00+08:00", upstreamBytes: 2048, downstreamBytes: 1024, totalBytes: 3072 }],
            granularity: "hour", timezone: "Asia/Shanghai"
        }));
        trafficApi.getTrafficRealtime.mockResolvedValue(ok({ list: [], estimated: true }));
        trafficApi.getTrafficCoverage.mockResolvedValue(ok({ coverage: "complete", gaps: [] }));
        trafficApi.getTrafficSessions.mockResolvedValue(ok({ list: [], total: 0, page: 1, pageSize: 10 }));

        const wrapper = mount(TrafficTrend, {
            props: { deviceId: "device-1" },
            global: { stubs: {
                "a-spin": passthrough,
                "a-button": buttonStub,
                "a-empty": { props: ["description"], template: "<div>{{ description }}</div>" },
                "a-modal": { props: ["visible", "title"], template: "<div v-if='visible'><strong>{{ title }}</strong><slot /></div>" },
                "a-table": passthrough,
                "a-table-column": passthrough,
                "a-pagination": passthrough
            } }
        });
        await flushPromises();

        await wrapper.get("button[data-range='24h']").trigger("click");
        await flushPromises();
        expect(trafficApi.getTrafficTrend.mock.calls.at(-1)?.[0]).toMatchObject({ deviceId: "device-1", granularity: "hour" });
        expect(trafficApi.getTrafficTrend.mock.calls.at(-1)?.[0]).not.toHaveProperty("from");

        expect(vchart.events.length).toBeGreaterThan(0);
        vchart.events.at(-1)?.({ datum: { bucket: "2026-08-21T15:00:00+08:00", kind: "上行" } });
        await flushPromises();
        expect(trafficApi.getTrafficSessions).toHaveBeenCalledWith(expect.objectContaining({
            deviceId: "device-1",
            from: "2026-08-21T15:00:00+08:00",
            to: "2026-08-21T16:00:00+08:00",
            page: 1,
            pageSize: 10
        }), expect.any(AbortSignal));
        expect(wrapper.text()).toContain("流量明细");
    });

    it("does not render kick actions when the backend denies super-admin capability", async () => {
        trafficApi.getCurrentViewers.mockResolvedValue(ok({
            list: [{
                channelId: "channel-1", channelName: "前门摄像机", startedAt: "2026-08-21T08:00:00Z",
                aliveSecond: 125, bitrateKbps: 2048, totalBytes: 1024 * 1024, viewerCount: 1, status: "streaming",
                viewers: [{ channelId: "channel-1", schema: "ws", remote: "10.0.0.8:4567", localPort: 80, id: "viewer-1", type: "tcp", kickable: true }]
            }],
            total: 1,
            totalViewers: 1,
            canKick: false
        }));
        const wrapper = mount(ViewerTable, {
            props: { deviceId: "device-1" },
            global: { stubs: {
                "a-table": viewerTableStub,
                "a-table-column": viewerColumnStub,
                "a-button": buttonStub,
                "a-tooltip": passthrough,
                "a-empty": { props: ["description"], template: "<div>{{ description }}</div>" }
            } }
        });
        await flushPromises();

        expect(trafficApi.getCurrentViewers).toHaveBeenCalledWith({ deviceId: "device-1", channelId: undefined }, expect.any(AbortSignal));
        expect(wrapper.text()).toContain("前门摄像机");
        expect(wrapper.text()).toContain("开始时间");
        expect(wrapper.text()).toContain("持续时长");
        expect(wrapper.text()).toContain("实时码率");
        expect(wrapper.text()).toContain("累计收流");
        expect(wrapper.text()).not.toContain("协议");
        expect(wrapper.text()).not.toContain("连接类型");
        const refreshButton = wrapper.get("button[aria-label='刷新当前观看']");
        expect(refreshButton.text()).toBe("刷新");
        expect(refreshButton.attributes("type")).toBe("primary");
        await wrapper.get("button[aria-label='查看 channel-1 的观看连接']").trigger("click");
        expect(wrapper.text()).toContain("10.0.0.8:4567");
        expect(wrapper.find("button[aria-label^='强退']").exists()).toBe(false);
        expect(trafficApi.kickCurrentViewer).not.toHaveBeenCalled();
    });

    it("uses the row channel when kicking from the all-channel viewer list", async () => {
        trafficApi.getCurrentViewers.mockResolvedValue(ok({
            list: [{
                channelId: "channel-2", channelName: "后门摄像机", startedAt: "2026-08-21T08:00:00Z",
                aliveSecond: 60, bitrateKbps: 1024, totalBytes: 2048, viewerCount: 1, status: "streaming",
                viewers: [{ channelId: "channel-2", schema: "ws", remote: "10.0.0.9:4567", localPort: 80, id: "viewer-2", type: "tcp", kickable: true }]
            }],
            total: 1,
            totalViewers: 1,
            canKick: true
        }));
        trafficApi.kickCurrentViewer.mockResolvedValue(ok({ kicked: true }));
        const wrapper = mount(ViewerTable, {
            props: { deviceId: "device-1" },
            global: { stubs: {
                "a-table": viewerTableStub,
                "a-table-column": viewerColumnStub,
                "a-button": buttonStub,
                "a-tooltip": passthrough,
                "a-empty": { props: ["description"], template: "<div>{{ description }}</div>" }
            } }
        });
        await flushPromises();

        await wrapper.get("button[aria-label='查看 channel-2 的观看连接']").trigger("click");
        const kickButton = wrapper.get("button[aria-label='强退 10.0.0.9:4567 的观看连接']");
        expect(kickButton.attributes("type")).toBe("primary");
        expect(kickButton.attributes("status")).toBe("danger");
        await kickButton.trigger("click");
        const confirmation = modal.confirm.mock.calls[0][0];
        expect(confirmation.content).toBe("确认断开 10.0.0.9:4567 的观看连接？同通道其他观看连接不会受影响。");
        await confirmation.onOk();

        expect(trafficApi.kickCurrentViewer).toHaveBeenCalledWith(
            { deviceId: "device-1", channelId: "channel-2" },
            { id: "viewer-2", schema: "ws" }
        );
    });

    it("refreshes every second without showing the table or button loading state", async () => {
        vi.useFakeTimers();
        const autoRefresh = deferred<ReturnType<typeof ok>>();
        trafficApi.getCurrentViewers
            .mockResolvedValueOnce(ok({ list: [], total: 0, totalViewers: 0, canKick: false }))
            .mockReturnValueOnce(autoRefresh.promise);
        const wrapper = mount(ViewerTable, {
            props: { deviceId: "device-1" },
            global: { stubs: {
                "a-table": viewerTableStub,
                "a-table-column": viewerColumnStub,
                "a-button": buttonStub,
                "a-tooltip": passthrough,
                "a-empty": { props: ["description"], template: "<div>{{ description }}</div>" }
            } }
        });
        await flushPromises();
        expect(trafficApi.getCurrentViewers).toHaveBeenCalledTimes(1);

        await vi.advanceTimersByTimeAsync(1000);
        await flushPromises();
        expect(trafficApi.getCurrentViewers).toHaveBeenCalledTimes(2);
        expect(wrapper.get(".viewer-table-stub").attributes("data-loading")).toBe("false");
        expect(wrapper.get("button[aria-label='刷新当前观看']").attributes("loading")).toBe("false");

        autoRefresh.resolve(ok({ list: [], total: 0, totalViewers: 0, canKick: false }));
        await flushPromises();

        wrapper.unmount();
        await vi.advanceTimersByTimeAsync(1000);
        expect(trafficApi.getCurrentViewers).toHaveBeenCalledTimes(2);
    });

    it("aborts the old channel request and ignores its late response", async () => {
        const firstSummary = deferred<ReturnType<typeof ok>>();
        trafficApi.getTrafficSummary.mockImplementation((params: { channelId?: string }) => {
            if (params.channelId === "channel-a") return firstSummary.promise;
            return Promise.resolve(ok({ upstreamBytes: 2048, downstreamBytes: 0, totalBytes: 2048, upstreamDurationSeconds: 0, downstreamDurationSeconds: 0, upstreamSessions: 0, downstreamSessions: 0, from: "", to: "", timezone: "UTC" }));
        });
        trafficApi.getTrafficTrend.mockResolvedValue(ok({ list: [], timezone: "UTC" }));
        trafficApi.getTrafficRealtime.mockResolvedValue(ok({ list: [], estimated: true }));
        trafficApi.getTrafficCoverage.mockResolvedValue(ok({ coverage: "complete", gaps: [] }));

        const wrapper = mount(TrafficTrend, {
            props: { deviceId: "device-1", channelId: "channel-a" },
            global: { stubs: {
                "a-spin": passthrough,
                "a-button": buttonStub,
                "a-empty": { props: ["description"], template: "<div>{{ description }}</div>" }
            } }
        });
        await flushPromises();
        const firstSignal = trafficApi.getTrafficSummary.mock.calls[0][1] as AbortSignal;

        await wrapper.setProps({ channelId: "channel-b" });
        await flushPromises();
        expect(firstSignal.aborted).toBe(true);
        expect(wrapper.text()).toContain("2.0 KB");

        firstSummary.resolve(ok({ upstreamBytes: 1024, downstreamBytes: 0, totalBytes: 1024, upstreamDurationSeconds: 0, downstreamDurationSeconds: 0, upstreamSessions: 0, downstreamSessions: 0, from: "", to: "", timezone: "UTC" }));
        await flushPromises();
        expect(wrapper.text()).toContain("2.0 KB");
        expect(wrapper.text()).not.toContain("1.0 KB");
    });
});

describe("runtime monitor entry contract", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/device-mgmt/index.vue"), "utf8");

    it("exposes runtime monitoring only from devices", () => {
        expect(source).not.toContain("openChannelRuntime");
        expect(source).not.toContain("打开通道运行监控");
        expect(source).not.toContain("uvp-table-action--monitor");
        expect(source).not.toContain("runtimeChannelLocked");
        expect(source).toContain("查看设备${record.deviceId}运行监控");
        expect(source).toContain("查看设备${item.deviceId}运行监控");
    });

    it("keeps device monitoring lazy, switchable and explicit about its state", () => {
        expect(source).toContain("loadRuntimeChannels(statusEventDevice.value)");
        expect(source).toContain("runtimeChannelsError");
        expect(source).toContain("设备运行概览");
        expect(source).toContain("全部通道（设备汇总）");
        expect(source).toContain("v-if=\"runtimeActiveTab === 'traffic'\"");
        expect(source).not.toContain("runtimeSelectedChannel");
        expect(source).toContain("placeholder=\"全部通道（默认）\"");
        expect(source).toContain("<ViewerTable v-if=\"runtimeActiveTab === 'viewers'\"");
        expect(source).toContain(":channel-id=\"runtimeChannelCode || undefined\"");
        expect(source).toContain("<BarChart3");
        expect(source).toContain("<History");
        expect(source).toContain("<Eye");
    });
});
