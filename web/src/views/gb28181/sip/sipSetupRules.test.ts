import { describe, expect, it } from "vitest";
import type { SipNetworkAddress } from "@/api/gb28181";
import { deriveNetworkSelection, networkCanContinue, networkOptions } from "./sipSetupRules";

const items: SipNetworkAddress[] = [
    { ip: "0.0.0.0", cidr: "0.0.0.0/0", loopback: false, virtual: false, recommended: false, more: false, listenOnly: true },
    { ip: "192.168.1.10", interfaceName: "en0", cidr: "192.168.1.10/24", loopback: false, virtual: false, recommended: true, more: false, listenOnly: false }
];

describe("SIP network rules", () => {
    it("uses the concrete LAN listener as advertise address", () => {
        expect(deriveNetworkSelection("lan", "192.168.1.10", "", items).advertiseIp).toBe("192.168.1.10");
    });

    it("chooses the recommended address for wildcard LAN listener", () => {
        const result = deriveNetworkSelection("lan", "0.0.0.0", "", items);
        expect(result.advertiseIp).toBe("192.168.1.10");
        expect(result.advertiseIpInferred).toBe(true);
    });

    it("keeps public listen and advertise addresses independent", () => {
        expect(deriveNetworkSelection("public", "192.168.1.10", "203.0.113.10", items).advertiseIp).toBe("203.0.113.10");
    });

    it("rejects wildcard and loopback advertise addresses", () => {
        expect(networkCanContinue("lan", "0.0.0.0", "0.0.0.0")).toBe(false);
        expect(networkCanContinue("lan", "0.0.0.0", "127.0.0.1")).toBe(false);
        expect(networkCanContinue("lan", "0.0.0.0", "192.168.1.10")).toBe(true);
    });

    it("retains a saved address that disappeared from interfaces", () => {
        const options = networkOptions(items, "10.10.10.10");
        expect(options[0]).toMatchObject({ ip: "10.10.10.10", unavailable: true });
    });
});
