import { shallowMount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";
import SessionDetail from "./SessionDetail.vue";

vi.mock("@arco-design/web-vue", () => ({ Message: { success: vi.fn(), warning: vi.fn() } }));

const session = {
    day: "2026-08-24T00:00:00.000Z",
    deviceId: "37010301021320000111",
    callId: "ZNNLscRBnYRxuONS21zyq6zIRkJQNiDV",
    firstAt: "2026-08-24T09:08:02.658Z",
    lastAt: "2026-08-24T09:08:02.666Z",
    messageCount: 2,
    inboundCount: 1,
    outboundCount: 1,
    methods: ["MESSAGE"],
    finalStatus: 200,
    firstMethod: "MESSAGE",
    fromUri: "sip:37010301021320000111@3402000000",
    toUri: "sip:34020000002000000002@3402000000",
    businessCode: "keepalive" as const,
    businessType: "心跳保持",
    requestCount: 1,
    finalResponseCount: 1,
    originalAvailable: true,
    originalExpiresAt: "2026-08-31T09:08:02.666Z",
    missingResponse: false,
    anomaly: false
};

const message = {
    eventId: "event-1",
    occurredAt: session.firstAt,
    direction: "inbound" as const,
    transport: "UDP",
    localAddr: "192.168.10.106:5062",
    remoteAddr: "192.168.10.205:5060",
    deviceId: session.deviceId,
    method: "MESSAGE",
    statusCode: 0,
    callId: session.callId,
    cseq: 16683,
    cseqMethod: "MESSAGE",
    businessCode: "keepalive" as const,
    businessType: "心跳保持",
    malformed: false
};

describe("SessionDetail metadata layout", () => {
    it("separates identity, metrics and route into stable rows", () => {
        const wrapper = shallowMount(SessionDetail, {
            props: { session, messages: [], selectedEventId: "" }
        });

        expect(wrapper.findAll(".meta-identity-row .meta-item")).toHaveLength(2);
        expect(wrapper.findAll(".meta-metrics .meta-item")).toHaveLength(4);
        expect(wrapper.get(".meta-time-range").text()).toContain("开始");
        expect(wrapper.get(".meta-time-range").text()).toContain("结束");
        expect(wrapper.get(".meta-route-row").text()).toContain(session.fromUri);
        expect(wrapper.get(".meta-route-row").text()).toContain(session.toUri);
    });

    it("keeps navigation and copy actions visually distinct", () => {
        const wrapper = shallowMount(SessionDetail, {
            props: { session, messages: [], selectedEventId: "" }
        });

        expect(wrapper.get(".back-btn").classes()).toContain("action-neutral");
        expect(wrapper.get(".copy-btn").classes()).toContain("action-brand");
    });

    it("labels remote and local signaling lanes as device and platform", () => {
        const wrapper = shallowMount(SessionDetail, {
            props: { session, messages: [message], selectedEventId: "" }
        });

        expect(wrapper.findAll(".lane-role").map(item => item.text())).toEqual(["设备端", "平台端"]);
        expect(wrapper.findAll(".lane-role-icon")).toHaveLength(2);
        expect(wrapper.findAll(".lane-header-label").map(item => item.text())).toEqual([
            "192.168.10.205:5060",
            "192.168.10.106:5062"
        ]);
    });
});
