import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import TableView from "./TableView.vue";
import type { TraceSessionSummary } from "@/api/gb28181-trace";

const session: TraceSessionSummary = {
    day: "2026-08-24T00:00:00Z",
    deviceId: "34020000001320000001",
    callId: "call-1",
    firstAt: "2026-08-24T06:00:00Z",
    lastAt: "2026-08-24T06:00:01Z",
    messageCount: 2,
    inboundCount: 1,
    outboundCount: 1,
    methods: ["MESSAGE"],
    finalStatus: 200,
    firstMethod: "MESSAGE",
    fromUri: "sip:34020000001320000001@3402000000",
    toUri: "sip:34020000002000000001@3402000000",
    fromId: "34020000001320000001",
    toId: "34020000002000000001",
    sourceAddr: "192.0.2.20:5060",
    destinationAddr: "192.0.2.10:5062",
    businessCode: "keepalive",
    businessType: "心跳保持",
    businessConfidence: "high",
    requestCount: 1,
    finalResponseCount: 1,
    originalAvailable: true,
    originalExpiresAt: "2026-08-31T06:00:01Z",
    missingResponse: false,
    anomaly: false
};

describe("SIP trace table business semantics", () => {
    it("keeps the SIP method and renders business type plus national IDs", () => {
        const wrapper = mount(TableView, {
            props: { sessions: [session], selectedCallId: "" },
            global: {
                stubs: {
                    "a-table": {
                        props: ["data", "columns"],
                        template: `<div>
                            <span v-for="column in columns" :key="column.title">{{ column.title }}</span>
                            <slot name="method" :record="data[0]" />
                            <slot name="business" :record="data[0]" />
                            <slot name="from" :record="data[0]" />
                            <slot name="to" :record="data[0]" />
                        </div>`
                    }
                }
            }
        });

        expect(wrapper.text()).toContain("起始方法");
        expect(wrapper.text()).toContain("业务类型");
        expect(wrapper.text()).toContain("MESSAGE");
        expect(wrapper.text()).toContain("心跳保持");
        expect(wrapper.text()).toContain("34020000001320000001");
        expect(wrapper.text()).toContain("34020000002000000001");
        expect(wrapper.text()).not.toContain("sip:");
    });
});
