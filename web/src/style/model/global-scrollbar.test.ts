import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { parse } from "postcss";
import { compile } from "sass-embedded";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/style/model/global-scrollbar.scss"), "utf8");

const css = parse(
  compile(resolve(process.cwd(), "src/style/model/global-scrollbar.scss"), {
    importers: [
      { findFileUrl: url => (url.startsWith("@/") ? pathToFileURL(resolve(process.cwd(), "src", url.slice(2))) : null) }
    ]
  }).css
);

function declarations(selector: string) {
  const values: Record<string, string> = {};
  css.walkRules(rule => {
    if (!rule.selectors.includes(selector)) return;
    rule.walkDecls(decl => {
      values[decl.prop] = decl.value;
    });
  });
  return values;
}

describe("global scrollbar theme colors", () => {
  it("provides separate light and dark thumb and hover colors", () => {
    const light = declarations(":root");
    const dark = declarations("body[arco-theme=dark]");
    for (const token of ["--uvp-scrollbar-thumb", "--uvp-scrollbar-thumb-hover"]) {
      expect(light[token]).toMatch(/^#[0-9a-f]{6}$/i);
      expect(dark[token]).toMatch(/^#[0-9a-f]{6}$/i);
      expect(dark[token]).not.toBe(light[token]);
    }
    expect(light["--uvp-scrollbar-thumb-hover"]).not.toBe(light["--uvp-scrollbar-thumb"]);
    expect(dark["--uvp-scrollbar-thumb-hover"]).not.toBe(dark["--uvp-scrollbar-thumb"]);
  });

  it("uses the same theme tokens for native and Arco thumbs, including dragging", () => {
    expect(declarations("::-webkit-scrollbar-thumb")["background-color"]).toBe("var(--uvp-scrollbar-thumb)");
    expect(declarations("::-webkit-scrollbar-thumb:hover")["background-color"]).toBe("var(--uvp-scrollbar-thumb-hover)");
    expect(declarations(".arco-scrollbar .arco-scrollbar-thumb-bar")["background-color"]).toBe("var(--uvp-scrollbar-thumb)");
    for (const state of [":hover", "-dragging"]) {
      expect(declarations(`.arco-scrollbar .arco-scrollbar-thumb${state} .arco-scrollbar-thumb-bar`)["background-color"]).toBe(
        "var(--uvp-scrollbar-thumb-hover)"
      );
    }
  });
});

describe("global Arco scrollbar alignment", () => {
  it("centers the slim horizontal and vertical bars in the 15px Arco tracks", () => {
    expect(source).toMatch(
      /\.arco-scrollbar-thumb-direction-horizontal \.arco-scrollbar-thumb-bar\s*{[^}]*height:\s*4px !important;[^}]*margin:\s*5\.5px 0 !important;/s
    );
    expect(source).toMatch(
      /\.arco-scrollbar-thumb-direction-vertical \.arco-scrollbar-thumb-bar\s*{[^}]*width:\s*4px !important;[^}]*margin:\s*0 5\.5px !important;/s
    );
  });
});
