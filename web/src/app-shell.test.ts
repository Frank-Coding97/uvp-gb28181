import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

describe("app shell", () => {
  it("uses a neutral default browser icon", () => {
    const indexHtml = readFileSync(resolve(process.cwd(), "index.html"), "utf8");
    const appSource = readFileSync(resolve(process.cwd(), "src/App.vue"), "utf8");

    expect(indexHtml).toContain('href="/src/assets/sys/default.svg"');
    expect(appSource).toContain('import defaultFavicon from "@/assets/sys/default.svg"');
    expect(appSource).toContain("const defaultIconUrl = defaultFavicon;");
  });
});
