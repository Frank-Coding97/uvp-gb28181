import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

describe("app shell", () => {
  it("uses the UVP logo in the browser tab", () => {
    const indexHtml = readFileSync(resolve(process.cwd(), "index.html"), "utf8");
    const appSource = readFileSync(resolve(process.cwd(), "src/App.vue"), "utf8");

    expect(indexHtml).toContain('href="/src/assets/logo/uvp-favicon.svg?v=uvp-20260918"');
    expect(appSource).toContain('import uvpFavicon from "@/assets/logo/uvp-favicon.svg"');
    expect(appSource).toContain("const defaultIconUrl = uvpFavicon;");
  });

  it("uses a font-independent unified-video gateway mark across brand surfaces", () => {
    const favicon = readFileSync(resolve(process.cwd(), "src/assets/logo/uvp-favicon.svg"), "utf8");
    const mark = readFileSync(resolve(process.cwd(), "src/assets/logo/uvp-mark.svg"), "utf8");
    const logoComponent = readFileSync(resolve(process.cwd(), "src/components/s-logo/index.vue"), "utf8");
    const loginPage = readFileSync(resolve(process.cwd(), "src/views/login/login.vue"), "utf8");

    expect(favicon).toContain('data-uvp-logo="unified-video-gateway"');
    expect(mark).toContain('data-uvp-logo="unified-video-gateway"');
    expect(favicon).not.toContain("<text");
    expect(mark).not.toContain("<text");
    expect(logoComponent).toContain('import uvpMark from "@/assets/logo/uvp-mark.svg"');
    expect(logoComponent).toContain("defaultImageUrl: uvpMark");
    expect(loginPage).toContain('class="brand-mark"');
  });
});
