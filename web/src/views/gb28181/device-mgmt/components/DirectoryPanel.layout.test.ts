import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/device-mgmt/components/DirectoryPanel.vue"), "utf8");

describe("device directory panel layout", () => {
    it("matches the multi-screen playback directory tab treatment", () => {
        expect(source).toContain('role="tab"');
        expect(source).toContain(':aria-selected="modelValue.view === \'national\'"');
        expect(source).toContain("gap: 4px;");
        expect(source).toContain("padding: 0 8px;");
        expect(source).toContain("border: 1px solid transparent;");
        expect(source).toContain("transition: color 0.15s ease, background 0.15s ease, border-color 0.15s ease, box-shadow 0.15s ease;");
        expect(source).toContain(".directory-switch button:hover:not(.active)");
        expect(source).toContain(".directory-switch button:focus-visible");
        expect(source).toContain("color: var(--uvp-brand-strong, var(--uvp-brand));");
        expect(source).toContain("font-weight: 650;");
        expect(source).not.toContain("box-shadow: 0 0 0 1px var(--uvp-panel-border);");
    });

    it("keeps tree padding inside its scroll viewport", () => {
        const treeRule = source.match(/\.directory-tree\s*\{([^}]*)\}/)?.[1] || "";

        expect(treeRule).toContain("box-sizing: border-box;");
        expect(treeRule).toContain("height: 100%;");
    });

    it("uses the theme-aware accessible success color for online counts", () => {
        expect(source).toContain(".node-online-count { color: #158052;");
        expect(source).toContain(':global(body[arco-theme="dark"]) .node-online-count { color: #86efac; }');
    });
});
