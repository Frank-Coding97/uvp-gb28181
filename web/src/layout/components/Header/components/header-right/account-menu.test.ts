import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const readHeaderSource = () => readFileSync(resolve(process.cwd(), "src/layout/components/Header/components/header-right/index.vue"), "utf8");
const readSource = (path: string) => readFileSync(resolve(process.cwd(), path), "utf8");

describe("header account menu", () => {
  it("renders the reference-style account summary", () => {
    const source = readHeaderSource();

    expect(source).toContain('class="uvp-user-menu-profile"');
    expect(source).toContain('class="uvp-user-menu-profile__name"');
    expect(source).toContain('class="uvp-user-menu-profile__account"');
    expect(source).toContain("account.nickname || account.username");
  });

  it("does not open the image preview when an account avatar is clicked", () => {
    const source = readHeaderSource();

    expect(source.match(/:preview="false"/g)).toHaveLength(2);
  });

  it("matches the reference menu dimensions and neutral interaction states", () => {
    const source = readHeaderSource();

    expect(source).toContain(":global(.arco-dropdown:has(.uvp-user-menu-profile))");
    expect(source).toMatch(/\.uvp-user-menu-option\)\s*\{[^}]*height:\s*36px/s);
    expect(source).toMatch(/\.uvp-user-menu-option:hover\)\s*\{[^}]*color-mix\(in srgb, var\(--uvp-text-primary\) 7%, transparent\)/s);
    expect(source).not.toContain("uvp-user-menu-option--danger");
  });

  it("provides localized text for every account action", () => {
    expect(readSource("src/lang/modules/zhCN.ts")).toContain('["project-address"]: "项目地址"');
    expect(readSource("src/lang/modules/enUS.ts")).toContain('["project-address"]: "project address"');
  });
});
