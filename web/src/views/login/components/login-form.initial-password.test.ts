import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/login/components/login-form.vue"), "utf8");

describe("login initial-password flow", () => {
  it("keeps defaults as placeholders instead of filling credentials into the form", () => {
    expect(source).toContain(':placeholder="usernamePlaceholder"');
    expect(source).toContain(':placeholder="passwordPlaceholder"');
    expect(source).not.toContain("form.value.username = newConfig.defaultusername");
    expect(source).not.toContain("form.value.password = newConfig.defaultpassword");
  });

  it("enters the system shell before the forced password dialog is shown", () => {
    expect(source).not.toContain("InitialPasswordDialog");
    expect(source).toContain("await routeStore.initSetRouter()");
    expect(source).toContain('router.replace("/home")');
  });
});
