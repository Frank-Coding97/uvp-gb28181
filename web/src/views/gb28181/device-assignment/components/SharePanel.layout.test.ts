import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(
    resolve(process.cwd(), "src/views/gb28181/device-assignment/components/SharePanel.vue"),
    "utf-8"
);

describe("SharePanel layout", () => {
    it("expands both transfer views to use the modal width", () => {
        expect(source).toMatch(
            /:deep\(\.arco-transfer-view\)\s*\{[\s\S]*?flex:\s*1 1 0;[\s\S]*?min-width:\s*0;/
        );
    });

    it("uses business labels instead of the default Source and Target titles", () => {
        expect(source).toContain(':title="[\'可选部门\', \'已授权部门\']"');
        expect(source).toContain(':title="[\'可选用户\', \'已授权用户\']"');
        expect(source).not.toContain('source-title=');
        expect(source).not.toContain('target-title=');
    });
});
