import { describe, it, expect } from "vitest";
import dayjs from "dayjs";
import { parseQuery, toQuery, defaultRange, isView, isDirection } from "./traceFilterTypes";

describe("traceFilterTypes helpers", () => {
    it("isView / isDirection guards", () => {
        expect(isView("text")).toBe(true);
        expect(isView("session")).toBe(true);
        expect(isView("matrix")).toBe(true);
        expect(isView("foo")).toBe(false);
        expect(isDirection("inbound")).toBe(true);
        expect(isDirection("outbound")).toBe(true);
        expect(isDirection("")).toBe(true);
        expect(isDirection("upstream")).toBe(false);
    });

    it("defaultRange returns last 15 minutes based on given now", () => {
        const now = dayjs("2026-07-20 12:00:00");
        const [from, to] = defaultRange(now);
        expect(from).toBe("2026-07-20 11:45:00");
        expect(to).toBe("2026-07-20 12:00:00");
    });
});

describe("parseQuery", () => {
    const now = dayjs("2026-07-20 12:00:00");

    it("uses defaults when query is empty", () => {
        const s = parseQuery({}, now);
        expect(s.view).toBe("text");
        expect(s.range).toEqual(["2026-07-20 11:45:00", "2026-07-20 12:00:00"]);
        expect(s.deviceId).toBe("");
        expect(s.direction).toBe("");
        expect(s.method).toBe("");
        expect(s.statusCode).toBe("");
        expect(s.callId).toBe("");
        expect(s.captureId).toBe("");
        expect(s.captureEndsAt).toBe("");
    });

    it("parses view + deviceId from valid query", () => {
        const s = parseQuery({ view: "session", deviceId: "D1", callId: "abc" }, now);
        expect(s.view).toBe("session");
        expect(s.deviceId).toBe("D1");
        expect(s.callId).toBe("abc");
    });

    it("rejects invalid view (falls back to text)", () => {
        const s = parseQuery({ view: "unknown" }, now);
        expect(s.view).toBe("text");
    });

    it("rejects invalid direction (falls back to empty)", () => {
        const s = parseQuery({ direction: "banana" }, now);
        expect(s.direction).toBe("");
    });

    it("takes explicit from/to when both present", () => {
        const s = parseQuery({ from: "2026-07-20 08:00:00", to: "2026-07-20 09:00:00" }, now);
        expect(s.range).toEqual(["2026-07-20 08:00:00", "2026-07-20 09:00:00"]);
    });

    it("falls back to defaults when only from present", () => {
        const s = parseQuery({ from: "2026-07-20 08:00:00" }, now);
        expect(s.range).toEqual(["2026-07-20 11:45:00", "2026-07-20 12:00:00"]);
    });
});

describe("toQuery", () => {
    const now = dayjs("2026-07-20 12:00:00");

    it("emits only non-empty fields", () => {
        const state = parseQuery({}, now);
        const q = toQuery(state);
        expect(q.view).toBe("text");
        expect(q.from).toBe("2026-07-20 11:45:00");
        expect(q.to).toBe("2026-07-20 12:00:00");
        expect(q.deviceId).toBeUndefined();
        expect(q.direction).toBeUndefined();
    });

    it("emits all provided fields", () => {
        const state = parseQuery(
            {
                view: "matrix",
                deviceId: "D1",
                direction: "outbound",
                method: "INVITE",
                statusCode: "200",
                callId: "abc",
                captureId: "cap1",
                captureEndsAt: "2026-07-20 13:00:00"
            },
            now
        );
        const q = toQuery(state);
        expect(q).toMatchObject({
            view: "matrix",
            deviceId: "D1",
            direction: "outbound",
            method: "INVITE",
            statusCode: "200",
            callId: "abc",
            captureId: "cap1",
            captureEndsAt: "2026-07-20 13:00:00"
        });
    });
});
