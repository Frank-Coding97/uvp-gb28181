import { describe, expect, it } from "vitest";
import { coverageLabel, formatBytes, latestRequestGuard } from "./trafficState";

describe("device traffic state", () => {
    it("distinguishes zero from missing history", () => {
        expect(formatBytes(0)).toBe("0 B");
        expect(formatBytes(1536)).toBe("1.5 KB");
        expect(coverageLabel("not_started")).toBe("尚未开始统计");
        expect(coverageLabel("partial")).toBe("存在采集缺口");
    });

    it("rejects stale request versions", () => {
        const guard = latestRequestGuard();
        const first = guard.next();
        const second = guard.next();
        expect(guard.current(first)).toBe(false);
        expect(guard.current(second)).toBe(true);
        guard.cancel();
        expect(guard.current(second)).toBe(false);
    });
});
