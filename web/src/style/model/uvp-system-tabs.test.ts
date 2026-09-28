import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/style/model/uvp-ui-language.scss"), "utf8");

describe("system tabs surface", () => {
  it("removes Arco's default full-width navigation divider", () => {
    expect(source).toMatch(/\.uvp-system-tabs \.arco-tabs-nav::before\s*\{[^}]*display:\s*none;/s);
  });
});
