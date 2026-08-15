import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

const trafficApi = vi.hoisted(() => ({
    getTrafficSummary: vi.fn(),
    getTrafficTrend: vi.fn(),
    getTrafficRealtime: vi.fn(),
    getTrafficCoverage: vi.fn(),
    getCurrentViewers: vi.fn(),
    kickCurrentViewer: vi.fn()
}));
const modal = vi.hoisted(() => ({ confirm: vi.fn() }));

vi.mock("../trafficApi", () => trafficApi);
vi.mock("@arco-design/web-vue", () => ({
    Message: { success: vi.fn(), error: vi.fn() },
    Modal: modal
}));
vi.mock("@visactor/vchart", () => ({
    default: class {
        renderSync() {}
        release() {}
        resize() {}
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
    props: ["data"],
    provide() { return { viewerTable: this }; },
    template: "<div><slot name='columns' /><slot v-if='!data.length' name='empty' /></div>"
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
    });

    it("does not render kick actions when the backend denies super-admin capability", async () => {
        trafficApi.getCurrentViewers.mockResolvedValue(ok({
            list: [{ channelId: "channel-1", schema: "ws", remote: "10.0.0.8:4567", localPort: 80, id: "viewer-1", type: "tcp", kickable: true }],
            total: 1,
            canKick: false
        }));
        const wrapper = mount(ViewerTable, {
            props: { deviceId: "device-1", channelId: "channel-1" },
            global: { stubs: {
                "a-table": viewerTableStub,
                "a-table-column": viewerColumnStub,
                "a-button": buttonStub,
                "a-tooltip": passthrough,
                "a-empty": { props: ["description"], template: "<div>{{ description }}</div>" }
            } }
        });
        await flushPromises();

        expect(wrapper.text()).toContain("10.0.0.8:4567");
        expect(wrapper.find("button[aria-label^='强退']").exists()).toBe(false);
        expect(trafficApi.kickCurrentViewer).not.toHaveBeenCalled();
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

    it("keeps device entry lazy and channel entry locked", () => {
        expect(source).toContain("runtimeChannelLocked.value = Boolean(channel)");
        expect(source).toContain("if (!channel && initialTab !== \"status\") loadRuntimeChannels(record)");
        expect(source).toContain("v-if=\"runtimeActiveTab === 'traffic'\"");
        expect(source).toContain("runtimeActiveTab === 'viewers' && runtimeChannelCode && runtimeSelectedChannel");
    });
});
