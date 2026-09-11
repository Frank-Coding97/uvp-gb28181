import { shallowMount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";
import DetailPanel from "./DetailPanel.vue";

vi.mock("@arco-design/web-vue", () => ({ Message: { success: vi.fn(), warning: vi.fn() } }));

const payload = "MESSAGE sip:34020000002000000002@3402000000 SIP/2.0\r\n" +
    "Via: SIP/2.0/UDP 192.168.10.205:5060;rport\r\n" +
    "From: <sip:37010301021320000111@3402000000>;tag=1\r\n" +
    "To: <sip:34020000002000000002@3402000000>\r\n" +
    "Call-ID: detail-call-id\r\n" +
    "CSeq: 16683 MESSAGE\r\n" +
    "Content-Type: Application/MANSCDP+xml\r\n\r\n" +
    "<Notify><CmdType>Keepalive</CmdType></Notify>";

const message = {
    eventId: "event-1",
    occurredAt: "2026-08-24T09:08:02.658Z",
    direction: "inbound" as const,
    transport: "UDP",
    localAddr: "192.168.10.106:5062",
    remoteAddr: "192.168.10.205:5060",
    deviceId: "37010301021320000111",
    method: "MESSAGE",
    statusCode: 0,
    callId: "detail-call-id",
    cseq: 16683,
    cseqMethod: "MESSAGE",
    businessCode: "keepalive" as const,
    businessType: "心跳保持",
    malformed: false,
    payload,
    sensitive: false
};

function mountPanel() {
    return shallowMount(DetailPanel, {
        props: { message },
        global: {
            stubs: {
                "a-spin": { template: "<div><slot /></div>" },
                "a-tooltip": { template: "<div><slot /></div>" },
                "a-empty": true
            }
        }
    });
}

describe("DetailPanel structured view", () => {
    it("separates start line, headers and body into clear sections", () => {
        const wrapper = mountPanel();

        expect(wrapper.findAll(".structured-section")).toHaveLength(3);
        expect(wrapper.findAll(".header-row")).toHaveLength(6);
        expect(wrapper.get(".section-start-line").text()).toContain("MESSAGE");
        expect(wrapper.get(".section-body").text()).toContain("Keepalive");
    });

    it("renders header values without repeating their names", () => {
        const wrapper = mountPanel();
        const firstRow = wrapper.get(".header-row");

        expect(firstRow.get("dt").text()).toBe("Via");
        expect(firstRow.get("dd").text()).toBe("SIP/2.0/UDP 192.168.10.205:5060;rport");
        expect(firstRow.get("dd").text()).not.toContain("Via:");
    });
});
