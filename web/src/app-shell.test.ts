import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

describe("app shell", () => {
  it("uses the UVP logo in the browser tab", () => {
    const indexHtml = readFileSync(resolve(process.cwd(), "index.html"), "utf8");
    const appSource = readFileSync(resolve(process.cwd(), "src/App.vue"), "utf8");

    expect(indexHtml).toContain('href="/src/assets/logo/uvp-favicon.svg?v=uvp-20260815"');
    expect(appSource).toContain('import uvpFavicon from "@/assets/logo/uvp-favicon.svg"');
    expect(appSource).toContain("const defaultIconUrl = uvpFavicon;");
  });
});
