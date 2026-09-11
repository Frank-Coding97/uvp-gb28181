import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import type { SipNetworkInterfaces } from "@/api/gb28181";
import type { SipSetupForm } from "../useSipSetup";
import ConfirmStep from "./ConfirmStep.vue";

const network: SipNetworkInterfaces = { items: [], scanStatus: "ok" };
const form: SipSetupForm = {
    deploymentMode: "lan",
    listenIp: "192.168.1.10",
    advertiseIp: "192.168.1.10",
    advertiseIpInferred: false,
    mediaReceiveHost: "192.168.1.10",
    mediaPlaybackHost: "127.0.0.1",
    port: 5061,
    domain: "3402000000",
    serverId: "34020000002000000001",
    password: "Sec12345Aa!!"
};

describe("SIP confirmation media addresses", () => {
    it("shows both confirmed media hosts for required onboarding", () => {
        const wrapper = mount(ConfirmStep, { props: { form, hasPassword: false, network, mediaRequired: true } });

        expect(wrapper.text()).toContain("媒体接收地址");
        expect(wrapper.text()).toContain("192.168.1.10");
        expect(wrapper.text()).toContain("媒体播放地址");
        expect(wrapper.text()).toContain("127.0.0.1");
    });

    it("does not add onboarding media fields to legacy confirmation", () => {
        const wrapper = mount(ConfirmStep, { props: { form, hasPassword: false, network, mediaRequired: false } });

        expect(wrapper.text()).not.toContain("媒体接收地址");
        expect(wrapper.text()).not.toContain("媒体播放地址");
    });
});
