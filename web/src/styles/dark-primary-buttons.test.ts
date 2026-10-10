import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { compileString } from "sass-embedded";
import postcss from "postcss";
import { compileStyle, parse } from "vue/compiler-sfc";
import { afterEach, describe, expect, it } from "vitest";

const readSource = (path: string) => readFileSync(resolve(process.cwd(), "src", path), "utf8");
const css = postcss.parse(compileString(readSource("styles/arco-overrides.scss")).css);
const primaryRules: { selector: string; declarations: Record<string, string> }[] = [];
css.walkRules(rule => {
  if (!rule.selector.startsWith("body[arco-theme=dark]") || !rule.selector.includes(".arco-btn-primary")) return;
  const declarations: Record<string, string> = {};
  rule.walkDecls(decl => {
    declarations[decl.prop] = decl.value;
  });
  primaryRules.push({ selector: rule.selector, declarations });
});

function rulesFor(classes: string, disabled = false) {
  const button = document.createElement("button");
  button.className = classes;
  button.disabled = disabled;
  document.body.append(button);
  return primaryRules.filter(
    rule => !/:(hover|active|focus-visible)\b/.test(rule.selector) && button.matches(rule.selector.replace(/\s+/g, " "))
  );
}

afterEach(() => {
  document.body.removeAttribute("arco-theme");
  document.body.replaceChildren();
});

describe("dark primary button contract", () => {
  it("keeps the document background on the UVP shell color", () => {
    const bodyRule = css.nodes.find(node => node.type === "rule" && (node as postcss.Rule).selector === "body") as
      | postcss.Rule
      | undefined;
    expect(bodyRule).toBeDefined();
    expect(
      bodyRule?.nodes.some(
        node => node.type === "decl" && node.prop === "background" && node.value === "var(--uvp-navigation-bg)" && node.important
      )
    ).toBe(true);
  });

  it.each(["arco-btn arco-btn-primary", "btn-primary", "arco-btn arco-btn-primary btn-primary"])(
    "gives %s an opaque blue fill and white text without changing geometry",
    classes => {
      document.body.setAttribute("arco-theme", "dark");
      const rules = rulesFor(classes);
      expect(rules).toHaveLength(1);
      // ⛔ 这里断言的是 **token 名** 而不是字面色值：实心面的色由 `--uvp-solid-*` 统一承载
      //   （见 style/model/solid-surface-token.test.ts），暗色下解析为深蓝 #2563eb。
      expect(rules[0].declarations).toMatchObject({
        color: "var(--uvp-solid-text)",
        background: "var(--uvp-solid-bg)",
        "box-shadow": "none"
      });
      for (const property of ["height", "padding", "border-radius"]) {
        expect(rules[0].declarations).not.toHaveProperty(property);
      }
    }
  );

  it("distinguishes native disabled buttons and Arco disabled links from enabled actions", () => {
    document.body.setAttribute("arco-theme", "dark");
    for (const rules of [rulesFor("btn-primary", true), rulesFor("arco-btn arco-btn-primary arco-btn-disabled")]) {
      expect(rules).toHaveLength(1);
      expect(rules[0].declarations).toMatchObject({
        color: "var(--uvp-text-disabled)",
        background: "var(--uvp-dialog-control-bg)",
        opacity: "1"
      });
    }
  });

  it.each([
    "arco-btn-status-danger",
    "arco-btn-status-warning",
    "arco-btn-status-success",
    "uvp-create-btn",
    "uvp-refresh-btn",
    "btn-danger"
  ])("preserves the independent semantic color of %s", status => {
    document.body.setAttribute("arco-theme", "dark");
    expect(rulesFor(`arco-btn arco-btn-primary btn-primary ${status}`)).toHaveLength(0);
  });

  it("leaves light-mode styles untouched", () => {
    expect(rulesFor("arco-btn arco-btn-primary btn-primary")).toHaveLength(0);
  });

  it("keeps loading buttons out of hover and pressed rules and preserves keyboard focus", () => {
    const hover = primaryRules.find(rule => rule.selector.includes(":hover"));
    const pressed = primaryRules.find(rule => rule.selector.includes(":active"));
    const focus = primaryRules.find(rule => rule.selector.includes(":focus-visible"));
    expect(hover?.selector).toContain(":not(.arco-btn-loading)");
    expect(pressed?.selector).toContain(":not(.arco-btn-loading)");
    expect(focus?.declarations).toMatchObject({ outline: "2px solid var(--uvp-brand)", "outline-offset": "2px" });
  });

  it("compiles OSD dark switch styles onto the control without recoloring the whole page", () => {
    const path = "views/gb28181/device-mgmt/DeviceConfigOsdBlocks.vue";
    const { descriptor } = parse(readSource(path));
    const result = compileStyle({
      source: compileString(descriptor.styles[0].content).css,
      scoped: true,
      id: "data-v-osd",
      filename: path
    });
    expect(result.errors).toEqual([]);
    const rules = postcss
      .parse(result.code)
      .nodes.filter(node => node.type === "rule" && /^body\[arco-theme/.test(node.selector));
    expect(rules).toHaveLength(2);
    for (const rule of rules as postcss.Rule[]) {
      expect(rule.selector).toContain(".osd-switch");
      expect(rule.selector).toContain("[data-v-osd]");
    }
  });

  it.each([
    ["views/gb28181/components/play-console/PictureVideoParamCard.vue", "vpc-segment"],
    ["views/gb28181/device-mgmt/DeviceConfigDrawer.vue", "dcg-segment"]
  ])("compiles %s dark bitrate styles onto the scoped button, never onto the body", (path, segment) => {
    const { descriptor } = parse(readSource(path));
    const style = descriptor.styles[0];
    const result = compileStyle({
      source: style.lang === "scss" ? compileString(style.content).css : style.content,
      scoped: true,
      id: "data-v-bitrate",
      filename: path
    });
    expect(result.errors).toEqual([]);
    const rules = postcss
      .parse(result.code)
      .nodes.filter(node => node.type === "rule" && /^body\[arco-theme="?dark"?\]/.test(node.selector));
    expect(rules.length).toBeGreaterThan(0);
    for (const rule of rules as postcss.Rule[]) {
      expect(rule.selector.replaceAll('"', "")).toContain(`.${segment} button.arco-btn[type=button]`);
      expect(rule.selector).toContain("[data-v-bitrate]");
    }
    const rule = rules.find(node => (node as postcss.Rule).selector.includes("].is-on")) as postcss.Rule;
    expect(rule).toBeDefined();
    expect(rule.selector.replaceAll('"', "")).toContain(`.${segment} button.arco-btn[type=button].is-on`);
    expect(rule.selector).toContain("[data-v-bitrate]");
    expect(rule.selector).toContain(":not(:disabled)");
    expect(rule.nodes).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ prop: "color", value: "#ffffff" }),
        expect.objectContaining({ prop: "background", value: "#2563eb" })
      ])
    );
  });
});
