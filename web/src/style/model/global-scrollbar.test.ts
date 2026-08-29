import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/style/model/global-scrollbar.scss"), "utf8");

describe("global Arco scrollbar alignment", () => {
  it("centers the slim horizontal and vertical bars in the 15px Arco tracks", () => {
    expect(source).toMatch(/\.arco-scrollbar-thumb-direction-horizontal \.arco-scrollbar-thumb-bar\s*{[^}]*height:\s*4px !important;[^}]*margin:\s*5\.5px 0 !important;/s);
    expect(source).toMatch(/\.arco-scrollbar-thumb-direction-vertical \.arco-scrollbar-thumb-bar\s*{[^}]*width:\s*4px !important;[^}]*margin:\s*0 5\.5px !important;/s);
  });
});
