import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = () => readFileSync(resolve(process.cwd(), "src/views/gb28181/device-assignment/index.vue"), "utf-8");

describe("permission workbench page shell", () => {
    it("uses summary counts and has both assignment and sharing views", () => {
        const content = source();
        expect(content).toContain("getPermissionWorkbenchSummary");
        expect(content).toContain("设备归属");
        expect(content).toContain("共享授权");
        expect(content).toContain("summary");
    });

    it("keeps a debounced search path and a stable operation column", () => {
        const content = source();
        expect(content).toContain("useDebounceFn");
        expect(content).toContain("operation-column");
        expect(content).not.toContain("scroll.x=1200");
    });

    it("normalizes Arco selection events before updating cross-page state", () => {
        const content = source();
        expect(content).toContain("normalizeSelectedIds");
        expect(content).toContain("@select=\"onTableSelect\"");
        expect(content).toContain("@select-all=\"onTableSelectAll\"");
    });
});
