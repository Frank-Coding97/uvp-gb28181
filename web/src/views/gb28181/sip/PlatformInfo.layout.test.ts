import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/sip/PlatformInfo.vue"), "utf8");

describe("SIP platform deployment card", () => {
    it("keeps only copy and edit actions in the top toolbar", () => {
        expect(source).toContain('<a-button v-if="config" class="uvp-page-action-btn sip-copy-btn" @click="copyAll">');
        expect(source).not.toContain('class="uvp-page-action-btn uvp-refresh-btn"');
        expect(source).not.toContain("刷新状态");
        expect(source).not.toContain("RefreshCw");
        expect(source).not.toContain('shape="circle"');
        expect(source).toContain('class="uvp-page-action-btn" type="primary"');
    });

    it("gives the copy action a distinct teal treatment", () => {
        const copyRule = source.match(/\.sip-copy-btn\s*\{([^}]*)\}/)?.[1] || "";

        expect(copyRule).toContain("background: #0f766e !important;");
        expect(copyRule).toContain("border-color: #0f766e !important;");
        expect(copyRule).toContain("color: #ffffff !important;");
    });

    it("uses the themed panel surface as the gradient endpoint", () => {
        const introRule = source.match(/\.sip-card--intro\s*\{([^}]*)\}/)?.[1] || "";

        expect(introRule).toContain("var(--uvp-panel-bg");
        expect(introRule).not.toMatch(/,\s*#fff(?:fff)?\s+62%/);
    });

    it("uses a solid brand blue for the deployment icon", () => {
        const iconRule = source.match(/\.sip-card__icon\s*\{([^}]*)\}/)?.[1] || "";

        expect(iconRule).toContain("background: #2563eb;");
        expect(iconRule).not.toContain("background: var(--uvp-brand");
    });

    it("keeps the simulator download entry out of the guide card", () => {
        // 下载入口已搬到扫码接入卡(见 QrProvisionCard.vue),这里只防回归
        expect(source).not.toContain("guide-download");
        expect(source).not.toContain("download.uvplatform.cn");
    });
});
