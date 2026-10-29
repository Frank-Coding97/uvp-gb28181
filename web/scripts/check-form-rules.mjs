import { readdirSync, readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const localRequire = createRequire(import.meta.url);
const compilerSfc = localRequire("vue/compiler-sfc");
const compilerRequire = createRequire(localRequire.resolve("vue/compiler-sfc"));
const { parse, NodeTypes } = compilerRequire("@vue/compiler-dom");
const { parse: parseSfc } = compilerSfc;

const textInputs = new Set(["a-input", "a-textarea", "a-input-password"]);

export function findFormRuleViolations(source) {
  const ast = parse(source);
  const violations = [];

  const walk = node => {
    if (node.type === NodeTypes.ELEMENT) {
      const attrs = node.props.filter(prop => prop.type === NodeTypes.ATTRIBUTE);
      const directives = node.props.filter(prop => prop.type === NodeTypes.DIRECTIVE);
      const hasAllowClear =
        attrs.some(prop => prop.name === "allow-clear") ||
        directives.some(prop => prop.name === "bind" && ["allow-clear", "allowClear"].includes(prop.arg?.content));
      if (textInputs.has(node.tag) && !hasAllowClear) {
        violations.push({ line: node.loc.start.line, tag: node.tag, rule: "allow-clear" });
      }

      const clamps = node.props.some(prop =>
        prop.type === NodeTypes.ATTRIBUTE
          ? prop.name === "min" || prop.name === "max"
          : prop.type === NodeTypes.DIRECTIVE &&
            prop.name === "bind" &&
            (prop.arg?.content === "min" || prop.arg?.content === "max")
      );
      if (node.tag === "a-input-number" && clamps) {
        violations.push({ line: node.loc.start.line, tag: node.tag, rule: "min/max" });
      }
    }
    for (const child of node.children ?? []) walk(child);
  };
  walk(ast);
  return violations;
}

function collectVueFiles(directory) {
  return readdirSync(directory, { withFileTypes: true }).flatMap(entry => {
    const path = resolve(directory, entry.name);
    if (entry.isDirectory()) return collectVueFiles(path);
    return entry.isFile() && entry.name.endsWith(".vue") ? [path] : [];
  });
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
  const sourceRoot = resolve(webRoot, "src");
  const violations = [];
  for (const path of collectVueFiles(sourceRoot).sort()) {
    const source = readFileSync(path, "utf8");
    const { descriptor, errors } = parseSfc(source, { filename: path });
    if (errors.length) {
      violations.push(`${relative(webRoot, path)}: Vue template parse failed`);
      continue;
    }
    if (!descriptor.template) continue;
    for (const violation of findFormRuleViolations(descriptor.template.content)) {
      violations.push(`${relative(webRoot, path)}:${violation.line} <${violation.tag}> violates ${violation.rule}`);
    }
  }

  if (violations.length) {
    console.error(violations.join("\n"));
    process.exitCode = 1;
  } else {
    console.log("Vue form rules passed.");
  }
}
