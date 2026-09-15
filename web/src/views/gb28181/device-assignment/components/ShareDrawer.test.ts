import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = () => readFileSync(resolve(process.cwd(), "src/views/gb28181/device-assignment/components/ShareDrawer.vue"), "utf-8");

describe("ShareDrawer contract", () => {
    it("keeps add/remove changes in an explicit draft until save", () => {
        const content = source();
        expect(content).toContain("addDraft");
        expect(content).toContain("removeDraft");
        expect(content).toContain("保存变更");
        expect(content).toContain("applyPermissionWorkbenchGrants");
        expect(content).toContain("放弃未保存变更");
    });

    it("supports paged targets and all/partial/none aggregation", () => {
        const content = source();
        expect(content).toContain("searchPermissionWorkbenchGrantTargets");
        expect(content).toContain('optionState(option) === "partial"');
        expect(content).toContain("targetPageSize");
        expect(content).toContain("invalidCount");
    });
});
