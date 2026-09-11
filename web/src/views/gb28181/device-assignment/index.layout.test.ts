import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/device-assignment/index.vue"), "utf-8");

describe("device assignment department tree", () => {
    it("expands the fully loaded department tree by default", () => {
        const sidebarTree = source.match(/<a-tree[\s\S]*?<\/a-tree>/)?.[0] ?? "";

        expect(sidebarTree).toContain('v-if="deptTree.length"');
        expect(sidebarTree).toContain("default-expand-all");
    });

    it("keeps vertical scrolling inside the device table", () => {
        expect(source).toMatch(/\.right-box\s*\{[\s\S]*?min-height:\s*0;[\s\S]*?overflow:\s*hidden;/);
    });
});
