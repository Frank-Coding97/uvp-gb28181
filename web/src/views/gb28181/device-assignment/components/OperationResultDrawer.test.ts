import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/device-assignment/components/OperationResultDrawer.vue"), "utf-8");

describe("OperationResultDrawer contract", () => {
    it("renders four summary buckets and failed-only retry", () => {
        expect(source).toContain("requested");
        expect(source).toContain("changed");
        expect(source).toContain("skipped");
        expect(source).toContain("failed");
        expect(source).toContain("只重试失败项");
    });

    it("exposes a structured log navigation event", () => {
        expect(source).toContain("viewLog");
        expect(source).toContain("emit('viewLog')");
    });
});
