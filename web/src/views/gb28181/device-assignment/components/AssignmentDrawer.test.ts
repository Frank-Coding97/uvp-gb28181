import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = () => readFileSync(resolve(process.cwd(), "src/views/gb28181/device-assignment/components/AssignmentDrawer.vue"), "utf-8");

describe("AssignmentDrawer contract", () => {
    it("shows current identity, target scope and stale-safe payload fields", () => {
        const content = source();
        expect(content).toContain("expectedOwnerDeptId");
        expect(content).toContain("expectedCount");
        expect(content).toContain("includeChildren");
        expect(content).toContain("resolvePermissionWorkbenchDevices");
    });

    it("prevents duplicate saves and emits operation results", () => {
        const content = source();
        expect(content).toContain("submitting");
        expect(content).toContain("emit(\"submitted\"");
        expect(content).toContain("部分失败");
    });
});
