import { nextTick } from "vue";
import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import type { SipNetworkInterfaces } from "@/api/gb28181";
import type { SipSetupForm } from "../useSipSetup";
import NetworkStep from "./NetworkStep.vue";

const network: SipNetworkInterfaces = {
    items: [
        { ip: "192.168.1.10", interfaceName: "LAN", cidr: "192.168.1.10/24", loopback: false, virtual: false, recommended: true, more: false, listenOnly: false },
        { ip: "0.0.0.0", interfaceName: "all", cidr: "0.0.0.0/0", loopback: false, virtual: false, recommended: false, more: false, listenOnly: true }
    ],
    scanStatus: "ok"
};

function form(mode: "lan" | "public", listenIp = ""): SipSetupForm {
    return {
        deploymentMode: mode,
        listenIp,
        advertiseIp: mode === "lan" ? listenIp : "203.0.113.10",
        advertiseIpInferred: false,
        mediaReceiveHost: "",
        mediaPlaybackHost: "",
        port: 5061,
        domain: "3402000000",
        serverId: "34020000002000000001",
        password: "Sec12345Aa!!"
    };
}

const stubs = {
    "a-alert": { template: "<div><slot /></div>" },
    "a-form": { template: "<form><slot /></form>" },
    "a-form-item": { props: ["label"], template: "<div><span>{{ label }}</span><slot /><slot name='extra' /></div>" },
    "a-option": { template: "<div />" },
    "a-select": {
        props: ["modelValue"],
        emits: ["change"],
        template: "<div data-testid='nic-select' @click=\"$emit('change', '192.168.1.10')\"><slot /></div>"
    },
    "a-input": {
        props: ["modelValue"],
        emits: ["update:modelValue"],
        template: "<input :value='modelValue' @input=\"$emit('update:modelValue', $event.target.value)\" />"
    },
    "a-tag": { template: "<span><slot /></span>" }
};

describe("SIP network step media addresses", () => {
    it("prefills both media hosts from a concrete LAN SIP listener", async () => {
        const values = form("lan");
        const wrapper = mount(NetworkStep, { props: { form: values, network, mediaRequired: true }, global: { stubs } });
        await nextTick();

        const patches = (wrapper.emitted("update") || []).map(([patch]) => patch);
        expect(patches).toContainEqual(expect.objectContaining({
            listenIp: "192.168.1.10",
            advertiseIp: "192.168.1.10",
            mediaReceiveHost: "192.168.1.10",
            mediaPlaybackHost: "192.168.1.10"
        }));
        expect(wrapper.text()).toContain("媒体接收地址");
    });

    it("does not copy the SIP advertise address for public deployment", async () => {
        const values = form("public", "192.168.1.10");
        const wrapper = mount(NetworkStep, { props: { form: values, network, mediaRequired: true }, global: { stubs } });
        await nextTick();

        expect(values.mediaReceiveHost).toBe("");
        expect(values.mediaPlaybackHost).toBe("");
        expect(wrapper.text()).toContain("媒体播放地址");
    });
});
