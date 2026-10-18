// 设计稿（design-mockups）是一次性产物，内联样式会触发 stylelint 大量误报。
// lint-staged v15 已废弃 ignore 配置，且它以显式路径传参会绕过 .stylelintignore，
// 所以用「函数任务」在传参前把 design-mockups 过滤掉，真实 html 仍照常走 stylelint。
const isDesignMockup = (file) => file.includes("design-mockups");

module.exports = {
  "*.{js,jsx,ts,tsx}": ["eslint --fix", "prettier --write"],
  "{!(package)*.json,*.code-snippets,.!(browserslist)*rc}": [
    "prettier --write --parser json",
  ],
  "package.json": ["prettier --write"],
  "*.vue": ["eslint --fix", "prettier --write", "stylelint --fix"],
  "*.{scss,less,styl,html}": (filenames) => {
    const targets = filenames.filter((f) => !isDesignMockup(f));
    if (targets.length === 0) return [];
    return [
      `stylelint --fix -- ${targets.join(" ")}`,
      `prettier --write -- ${targets.join(" ")}`,
    ];
  },
  "*.md": ["prettier --write"],
};
