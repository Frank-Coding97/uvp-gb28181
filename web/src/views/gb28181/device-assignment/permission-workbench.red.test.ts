import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const read = (path: string) => readFileSync(resolve(process.cwd(), path), "utf-8");

describe("device permission workbench baseline contracts", () => {
    it("uses only real permission APIs", () => {
        const source = read("src/views/gb28181/device-assignment/api.ts");
        expect(source).not.toContain("./permissionMock");
        expect(source).not.toContain("shouldUsePermissionMock");
    });

    it("keeps complete device records for cross-page selection", () => {
        const source = read("src/views/gb28181/device-assignment/index.vue");
        expect(source).toContain("useCrossPageSelection");
        expect(source).toContain("const selectedRowKeys = ref<number[]>([])");
        expect(source).toContain('@select-all="onTableSelectAll"');
    });

    it("does not write grants from transfer watchers", () => {
        const source = read("src/views/gb28181/device-assignment/components/SharePanel.vue");
        expect(source).not.toMatch(/watch\((deptTargetKeys|userTargetKeys)/);
        expect(source).toContain("保存变更");
    });
});
