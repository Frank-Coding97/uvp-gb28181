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
});
