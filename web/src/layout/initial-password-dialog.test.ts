import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/layout/index.vue"), "utf8");

describe("authenticated initial-password dialog", () => {
  it("mounts inside the authenticated layout and logs out after success", () => {
    expect(source).toContain("InitialPasswordDialog");
    expect(source).toContain('v-if="mustChangePassword"');
    expect(source).toContain("await userStore.logOut()");
    expect(source).toContain('router.replace("/login")');
  });
});
