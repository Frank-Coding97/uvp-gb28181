import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/sip/PlatformInfo.vue"), "utf8");

describe("SIP platform deployment card", () => {
    it("uses the themed panel surface as the gradient endpoint", () => {
        const introRule = source.match(/\.sip-card--intro\s*\{([^}]*)\}/)?.[1] || "";

        expect(introRule).toContain("var(--uvp-panel-bg");
        expect(introRule).not.toMatch(/,\s*#fff(?:fff)?\s+62%/);
    });

    it("keeps the simulator download hover surface theme-aware", () => {
        const hoverRule = source.match(/\.guide-download:hover\s*\{([^}]*)\}/)?.[1] || "";

        expect(hoverRule).toContain("var(--uvp-panel-bg");
        expect(hoverRule).not.toContain("var(--uvp-brand-soft, #e8f2ff) 70%, #ffffff");
    });
});
