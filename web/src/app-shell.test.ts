import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

describe("app shell", () => {
  it("uses the menu logo in the browser tab and share page", () => {
    const indexHtml = readFileSync(resolve(process.cwd(), "index.html"), "utf8");
    const appSource = readFileSync(resolve(process.cwd(), "src/App.vue"), "utf8");

    const shareHtml = readFileSync(resolve(process.cwd(), "share.html"), "utf8");

    expect(indexHtml).toContain('href="/src/assets/logo/uvp-mark.png?v=uvp-20261118-menu"');
    expect(shareHtml).toContain('href="/src/assets/logo/uvp-mark.png?v=uvp-20261118-menu"');
    expect(appSource).toContain('import uvpMark from "@/assets/logo/uvp-mark.png"');
    expect(appSource).toContain("const defaultIconUrl = uvpMark;");
  });

  it("uses the approved server logo across brand surfaces", () => {
    const mark = readFileSync(resolve(process.cwd(), "src/assets/logo/uvp-mark.png"));
    const logoComponent = readFileSync(resolve(process.cwd(), "src/components/s-logo/index.vue"), "utf8");
    const loginPage = readFileSync(resolve(process.cwd(), "src/views/login/login.vue"), "utf8");

    const sharePage = readFileSync(resolve(process.cwd(), "src/share/ShareApp.vue"), "utf8");

    expect(mark.subarray(0, 8).toString("hex")).toBe("89504e470d0a1a0a");
    expect(mark.readUInt32BE(16)).toBe(256);
    expect(mark.readUInt32BE(20)).toBe(256);
    expect(logoComponent).toContain('import uvpMark from "@/assets/logo/uvp-mark.png"');
    expect(logoComponent).toContain("defaultImageUrl: uvpMark");
    expect(loginPage).toContain('import uvpMark from "@/assets/logo/uvp-mark.png"');
    expect(sharePage).toContain('import uvpLogo from "@/assets/logo/uvp-mark.png"');
    expect(loginPage).toContain('class="brand-mark"');
  });
});
