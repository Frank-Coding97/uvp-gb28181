import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/device-mgmt/components/DirectoryPanel.vue"), "utf8");

describe("device directory panel layout", () => {
    it("keeps tree padding inside its scroll viewport", () => {
        const treeRule = source.match(/\.directory-tree\s*\{([^}]*)\}/)?.[1] || "";

        expect(treeRule).toContain("box-sizing: border-box;");
        expect(treeRule).toContain("height: 100%;");
    });
});
