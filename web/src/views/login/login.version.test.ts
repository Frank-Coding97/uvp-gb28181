import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/login/login.vue"), "utf8");

describe("login product version", () => {
  it("renders the shared product version without a hard-coded fallback", () => {
    expect(source).toContain("APP_VERSION_TEXT");
    expect(source).toContain("{{ APP_VERSION_TEXT }} · GB/T 28181-2022");
    expect(source).not.toContain("v2.3.0");
  });
});

