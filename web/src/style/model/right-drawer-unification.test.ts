import { readFileSync, readdirSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { hasRuleBlock } from "@/test/source-assert";

const root = resolve(process.cwd(), "src");
function vueFiles(directory: string): string[] {
  return readdirSync(directory, { withFileTypes: true }).flatMap(entry => {
    const path = resolve(directory, entry.name);
    return entry.isDirectory() ? vueFiles(path) : entry.name.endsWith(".vue") ? [path] : [];
  });
}
function attribute(tag: string, name: string): string | undefined {
  return tag.match(new RegExp("(?:^|\\s)" + name + '="([^"]*)"'))?.[1];
}
const rightDrawers = vueFiles(root).flatMap(path =>
  [...readFileSync(path, "utf8").matchAll(/<a-drawer\b[^>]*>/g)]
    .map(match => match[0])
    .filter(tag => attribute(tag, "placement") !== "left")
    .map(node => ({ path: path.slice(root.length + 1), node }))
);

describe("right drawer theme coverage", () => {
  it("finds business and settings drawers", () => {
    expect(rightDrawers.length).toBeGreaterThan(0);
    expect(rightDrawers.some(({ path }) => path.endsWith("AlarmDetailDrawer.vue"))).toBe(true);
  });

  it.each(rightDrawers)("uses the shared panel and body skin: $path", ({ node }) => {
    expect(attribute(node, "class")?.split(/\s+/)).toContain("uvp-system-drawer");
    expect(attribute(node, "body-class")?.split(/\s+/)).toContain("uvp-system-dialog__body");
  });

  it("themes description labels, values and loading masks inside drawers", () => {
    const styles = readFileSync(resolve(root, "style/model/uvp-ui-language.scss"), "utf8");
    expect(
      hasRuleBlock(
        styles,
        ".uvp-system-drawer .arco-drawer-body .arco-descriptions-item-label",
        "background: var(--uvp-table-header-bg)"
      )
    ).toBe(true);
    expect(
      hasRuleBlock(
        styles,
        ".uvp-system-drawer .arco-drawer-body .arco-descriptions-item-value",
        "background: var(--uvp-dialog-bg)"
      )
    ).toBe(true);
    expect(
      hasRuleBlock(
        styles,
        ".uvp-system-drawer .arco-drawer-body .arco-spin-mask",
        "background: color-mix(in srgb, var(--uvp-dialog-bg) 78%, transparent)"
      )
    ).toBe(true);
  });
});
