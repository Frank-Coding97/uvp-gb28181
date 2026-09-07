import { describe, expect, it } from "vitest";
import type { SipNetworkAddress } from "@/api/gb28181";
import {
    activeSipAddresses, deriveDomain, deriveNetworkSelection, evaluatePasswordStrength, formatRegisterUri,
    identityCanContinue, networkCanContinue, networkOptions, passwordAcceptable, sipAddressAvailability
} from "./sipSetupRules";

const items: SipNetworkAddress[] = [
    { ip: "0.0.0.0", cidr: "0.0.0.0/0", loopback: false, virtual: false, recommended: false, more: false, listenOnly: true },
    { ip: "192.168.1.10", interfaceName: "en0", cidr: "192.168.1.10/24", loopback: false, virtual: false, recommended: true, more: false, listenOnly: false }
];

describe("SIP network rules", () => {
    it("uses the concrete LAN listener as advertise address", () => {
        expect(deriveNetworkSelection("lan", "192.168.1.10", "", items).advertiseIp).toBe("192.168.1.10");
    });

    it("does not persist a recommended address for wildcard LAN listener", () => {
        const result = deriveNetworkSelection("lan", "0.0.0.0", "10.10.10.10", items);
        expect(result.advertiseIp).toBe("");
        expect(result.advertiseIpInferred).toBe(false);
    });

    it("keeps public listen and advertise addresses independent", () => {
        expect(deriveNetworkSelection("public", "192.168.1.10", "203.0.113.10", items).advertiseIp).toBe("203.0.113.10");
    });

    it("allows wildcard LAN without a single advertise address", () => {
        expect(networkCanContinue("lan", "0.0.0.0", "")).toBe(true);
        expect(networkCanContinue("public", "0.0.0.0", "")).toBe(false);
        expect(networkCanContinue("public", "0.0.0.0", "203.0.113.10")).toBe(true);
    });

    it("keeps a disappeared saved address visible without treating it as current", () => {
        const options = networkOptions(items, "10.10.10.10");
        expect(options.map(item => item.ip)).toContain("10.10.10.10");
        expect(options.find(item => item.ip === "10.10.10.10")?.unavailable).toBe(true);
    });

    it("does not report a LAN address change while the saved IPs are present", () => {
        expect(sipAddressAvailability("lan", "192.168.1.10", "192.168.1.10", {
            items,
            scanStatus: "ok"
        })).toBe("ok");
    });

    it("reports a missing concrete LAN address but ignores dynamic and public NAT addresses", () => {
        expect(sipAddressAvailability("lan", "192.168.1.10", "192.168.1.10", {
            items: [items[0]],
            scanStatus: "ok"
        })).toBe("missing");
        expect(sipAddressAvailability("lan", "192.168.1.10", "192.168.1.20", {
            items,
            scanStatus: "ok"
        })).toBe("missing");
        expect(sipAddressAvailability("lan", "0.0.0.0", "", {
            items: [items[0]],
            scanStatus: "ok"
        })).toBe("ok");
        expect(sipAddressAvailability("public", "192.168.1.10", "203.0.113.10", {
            items,
            scanStatus: "ok"
        })).toBe("ok");
        expect(sipAddressAvailability("public", "192.168.1.10", "203.0.113.10", {
            items: [items[0]],
            scanStatus: "ok"
        })).toBe("missing");
        expect(sipAddressAvailability("public", "0.0.0.0", "203.0.113.10", {
            items: [items[0]],
            scanStatus: "ok"
        })).toBe("ok");
    });

    it("does not report an address change when the interface scan fails", () => {
        expect(sipAddressAvailability("lan", "192.168.1.10", "192.168.1.10", {
            items: [],
            scanStatus: "failed"
        })).toBe("unavailable");
    });

    it("lists only currently scanned addresses for wildcard LAN", () => {
        const currentItems: SipNetworkAddress[] = [
            ...items,
            { ip: "10.8.0.3", interfaceName: "utun4", cidr: "10.8.0.3/32", loopback: false, virtual: true, recommended: false, more: true, listenOnly: false },
            { ip: "127.0.0.1", interfaceName: "lo0", cidr: "127.0.0.1/8", loopback: true, virtual: false, recommended: false, more: true, listenOnly: false }
        ];
        expect(activeSipAddresses("lan", "0.0.0.0", "192.168.10.106", currentItems)).toEqual([
            "192.168.1.10",
            "10.8.0.3"
        ]);
        expect(activeSipAddresses("lan", "192.168.1.10", "192.168.10.106", currentItems)).toEqual(["192.168.1.10"]);
        expect(activeSipAddresses("public", "0.0.0.0", "203.0.113.10", currentItems)).toEqual(["203.0.113.10"]);
    });
});

describe("SIP identity rules", () => {
    it("derives the domain from the first ten ID digits", () => {
        expect(deriveDomain("34020000002000000001")).toBe("3402000000");
        expect(deriveDomain("340200000")).toBe("");
    });

    it("validates port, identity and password retention", () => {
        expect(identityCanContinue(5061, "34020000002000000001", "3402000000", "Sec12345Aa!!", false)).toBe(true);
        expect(identityCanContinue(0, "34020000002000000001", "3402000000", "Sec12345Aa!!", false)).toBe(false);
        expect(identityCanContinue(5061, "34020000002000000001", "3402000000", "", true)).toBe(true);
        expect(identityCanContinue(5061, "34020000002000000001", "3402000000", "", false)).toBe(false);
    });

    it("formats the device registration URI from advertise IP only", () => {
        expect(formatRegisterUri("34020000002000000001", "192.168.1.10", 5061)).toBe("sip:34020000002000000001@192.168.1.10:5061");
        expect(formatRegisterUri("34020000002000000001", "0.0.0.0", 5061)).toBe("");
    });
});

describe("SIP password strength", () => {
    it("rejects short / weak / sequential passwords", () => {
        expect(evaluatePasswordStrength("").level).toBe(0);
        expect(evaluatePasswordStrength("Aa1!Aa1").level).toBe(1);      // too short
        expect(evaluatePasswordStrength("abcdefghijkl").level).toBe(1); // only lower
        expect(evaluatePasswordStrength("password").level).toBe(1);    // common weak
        expect(evaluatePasswordStrength("admin123").level).toBe(1);
        expect(evaluatePasswordStrength("0123456789ab").level).toBe(1); // sequential
    });

    it("accepts strong passwords", () => {
        expect(evaluatePasswordStrength("Sec12345Aa!!").level).toBeGreaterThanOrEqual(2);
        expect(evaluatePasswordStrength("MyP@ssw0rdX1").level).toBeGreaterThanOrEqual(2);
    });

    it("gates save with passwordAcceptable", () => {
        expect(passwordAcceptable("Sec12345Aa!!", false)).toBe(true);
        expect(passwordAcceptable("weak123", false)).toBe(false);
        expect(passwordAcceptable("", true)).toBe(true); // 编辑态保留原密码
        expect(passwordAcceptable("", false)).toBe(false); // 首次配置必填
    });
});
