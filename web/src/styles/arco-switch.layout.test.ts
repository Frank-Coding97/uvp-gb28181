import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/styles/arco-overrides.scss"), "utf8");

describe("global switch styling", () => {
    it("distinguishes usable off, on and disabled states in dark mode", () => {
        expect(source).toMatch(/\.arco-switch\s*\{[^}]*background-color:\s*var\(--uvp-panel-border\)/s);
        expect(source).toMatch(/&\.arco-switch-checked\s*\{[^}]*background-color:\s*var\(--uvp-brand\)/s);
        expect(source).toMatch(/&\[disabled\]\s*\{[^}]*opacity:\s*0\.5/s);
    });

    it("provides a distinct adaptive color for refresh actions", () => {
        expect(source).toMatch(/\.uvp-refresh-btn\s*\{[^}]*background:\s*var\(--uvp-refresh-btn-bg\)/s);
        expect(source).toMatch(/\.uvp-refresh-btn\s*\{[^}]*border-color:\s*var\(--uvp-refresh-btn-border\)/s);
        expect(source).toMatch(/\.uvp-refresh-btn[\s\S]*&:hover[^}]*background:\s*var\(--uvp-refresh-btn-hover-bg\)/s);
    });

    it("defines shared page-action sizing and create semantics", () => {
        expect(source).toMatch(/\.uvp-page-action-btn\s*\{[^}]*min-width:\s*88px;[^}]*height:\s*40px;[^}]*border-radius:\s*10px;/s);
        expect(source).toMatch(/\.uvp-create-btn\s*\{[^}]*background:\s*#16845f[^}]*border-color:\s*#16845f/s);
        expect(source).toMatch(/\.uvp-create-btn[\s\S]*&:hover[^}]*background:\s*#0f6f4f[^}]*border-color:\s*#0f6f4f/s);
    });
});
