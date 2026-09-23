import { describe, expect, it } from "vitest";
import { buildQrUrl, formatCountdown, mayGenerateQr, normalizeBaseUrl, validateBaseUrl } from "./qrProvisionState";

const TOKEN = "AbCdEfGhIjKlMnOpQrStUv";

describe("baseUrl normalization", () => {
    it("strips a single trailing slash", () => {
        expect(normalizeBaseUrl("http://a:8280/")).toBe("http://a:8280");
    });

    it("strips repeated trailing slashes", () => {
        expect(normalizeBaseUrl("http://a:8280///")).toBe("http://a:8280");
    });
});

describe("baseUrl validation", () => {
    it("rejects empty input", () => {
        expect(validateBaseUrl("")).not.toBe("");
    });

    it("rejects a missing scheme", () => {
        expect(validateBaseUrl("192.168.1.10:8280")).not.toBe("");
    });

    it("rejects non-http schemes", () => {
        expect(validateBaseUrl("ftp://a")).not.toBe("");
    });

    it("accepts http", () => {
        expect(validateBaseUrl("http://a:8280")).toBe("");
    });

    it("accepts https", () => {
        expect(validateBaseUrl("https://a")).toBe("");
    });
});

describe("QR url assembly", () => {
    it("puts the token in the URL fragment", () => {
        expect(buildQrUrl("http://a:8280/", TOKEN)).toBe(`http://a:8280/gb28181/qr#t=${TOKEN}`);
    });

    // D6 回归防线:query 形式会被系统相机一扫就 GET 消费掉 token.
    it("never emits a query parameter", () => {
        expect(buildQrUrl("http://a:8280", TOKEN)).not.toContain("?t=");
    });
});

describe("countdown formatting", () => {
    it("formats minutes and seconds", () => {
        expect(formatCountdown(272)).toBe("4:32");
    });

    it("pads seconds below ten", () => {
        expect(formatCountdown(65)).toBe("1:05");
    });

    it("formats zero", () => {
        expect(formatCountdown(0)).toBe("0:00");
    });

    it("clamps negative values to zero", () => {
        expect(formatCountdown(-5)).toBe("0:00");
    });
});

describe("QR generation permission", () => {
    it("allows the super admin wildcard", () => {
        expect(mayGenerateQr(["*:*:*"])).toBe(true);
    });

    // D10: 复用 config:view —— 能看明文密码的人才能发接入码,不新建权限点.
    it("allows the exact sip config view permission", () => {
        expect(mayGenerateQr(["gb28181:sip:config:view"])).toBe(true);
    });

    it("denies unrelated permissions", () => {
        expect(mayGenerateQr(["gb28181:device:list"])).toBe(false);
    });
});
