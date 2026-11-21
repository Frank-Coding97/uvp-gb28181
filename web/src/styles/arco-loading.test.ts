import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { compileString } from "sass-embedded";
import postcss from "postcss";
import { afterEach, describe, expect, it } from "vitest";

const css = postcss.parse(compileString(readFileSync(resolve(process.cwd(), "src/styles/arco-overrides.scss"), "utf8")).css);

function declarationsFor(element: Element) {
  const declarations: Record<string, string> = {};
  css.walkRules(rule => {
    if (!rule.selector.includes(".arco-spin") || !element.matches(rule.selector)) return;
    rule.walkDecls(decl => {
      declarations[decl.prop] = decl.value;
    });
  });
  return declarations;
}

afterEach(() => {
  document.body.removeAttribute("arco-theme");
  document.body.replaceChildren();
});

describe("global Arco loading theme", () => {
  it.each(["light", "dark"])("covers SIP and other content wrappers in %s mode", theme => {
    if (theme === "dark") document.body.setAttribute("arco-theme", "dark");
    document.body.innerHTML =
      '<div class="arco-spin sip-platform-wrapper"><div class="arco-spin-mask"><span class="arco-spin-icon"></span><span class="arco-spin-tip"></span></div></div>';
    const mask = document.querySelector(".arco-spin-mask")!;
    expect(declarationsFor(mask)).toMatchObject({
      "background-color": "color-mix(in srgb, var(--uvp-panel-bg) 78%, transparent)",
      "border-radius": "inherit"
    });
    for (const selector of [".arco-spin-icon", ".arco-spin-tip"]) {
      expect(declarationsFor(document.querySelector(selector)!)).toMatchObject({ color: "var(--uvp-brand-strong)" });
    }
  });

  it("overrides the generated Arco mask without changing animation or content sizing", () => {
    const rules: postcss.Rule[] = [];
    css.walkRules(rule => {
      if (rule.selector === ".arco-spin .arco-spin-mask") rules.push(rule);
    });
    expect(rules).toHaveLength(1);
    expect(rules[0].nodes.some(node => node.type === "decl" && node.prop === "background-color" && node.important)).toBe(true);
    for (const property of ["animation", "height", "min-height", "width", "position"]) {
      expect(rules[0].nodes.some(node => node.type === "decl" && node.prop === property)).toBe(false);
    }
  });
});
