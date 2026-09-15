import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/layout/layout-defaults/index.vue"), "utf8");

describe("default workspace frame", () => {
    it("keeps the padded shell inside the viewport and renders all frame edges", () => {
        const layoutRule = source.match(/\.layout\s*\{([^}]*)\}/)?.[1] || "";
        const frameRule = source.match(/\.layout-right\s*\{([^}]*)\}/)?.[1] || "";

        expect(layoutRule).toContain("box-sizing: border-box;");
        expect(frameRule).toContain("box-sizing: border-box;");
        expect(frameRule).not.toContain("border-left-color: transparent;");
    });
});
