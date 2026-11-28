import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/router/index.ts"), "utf8");

describe("initial-password route gate", () => {
  it("checks the server profile before allowing business navigation", () => {
    expect(source).toContain("mustChangePassword");
    expect(source).toContain('next("/login")');
    expect(source).toContain("getUserInfo()");
  });
});
