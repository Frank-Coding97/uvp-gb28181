import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/styles/arco-overrides.scss"), "utf8");

describe("collapsed sidebar menu popup styling", () => {
  it("uses the shared UVP popup surface instead of Arco's neutral gray", () => {
    expect(source).toMatch(
      /\.arco-trigger-menu\s*\{[^}]*background:\s*var\(--uvp-popconfirm-bg\)\s*!important;[^}]*border:\s*1px solid var\(--uvp-popconfirm-border\)\s*!important;[^}]*box-shadow:\s*var\(--uvp-popconfirm-shadow\)\s*!important;/s
    );
  });

  it("keeps popup items readable and gives hover a brand-tinted surface", () => {
    expect(source).toMatch(
      /\.arco-trigger-menu \.arco-trigger-menu-item,\s*\.arco-trigger-menu \.arco-trigger-menu-pop-header\s*\{[^}]*color:\s*var\(--uvp-text-secondary\)\s*!important;[^}]*background:\s*transparent\s*!important;/s
    );
    expect(source).toMatch(
      /\.arco-trigger-menu \.arco-trigger-menu-item:hover,[\s\S]*?background:\s*var\(--uvp-brand-soft\)\s*!important;/s
    );
  });
});
