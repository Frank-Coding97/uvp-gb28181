import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/system/userinfo/userinfo.vue"), "utf8");

describe("userinfo page layout", () => {
  it("keeps profile and settings in one panel", () => {
    expect(source.match(/<a-card\b/g)?.length).toBe(1);
    expect(source).not.toContain("uvp-system-panel__stack");
    expect(source).toContain("uvp-page-shell-flat");
    expect(source).toContain("userinfo-content");
    expect(source).toContain("userinfo-profile");
  });

  it("keeps the editable tabs and profile data intact", () => {
    expect(source).toContain('title="用户资料"');
    expect(source).toContain('key="1" title="基本信息"');
    expect(source).toContain('key="2" title="安全设置"');
    expect(source).toContain("<BasicInfo");
    expect(source).toContain("<SecuritySettings");
  });

  it("uses neutral labels and a rounded-square profile avatar", () => {
    expect(source).toContain('class="userinfo-avatar"');
    expect(source).toContain('shape="square"');
    expect(source).toMatch(/\.userinfo-profile[\s\S]*?\.arco-descriptions-item-label[\s\S]*?background:\s*transparent/);
    expect(source).not.toContain("border-radius: 50%");
  });
});
