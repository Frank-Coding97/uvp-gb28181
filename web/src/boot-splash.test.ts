import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const read = (p: string) => readFileSync(resolve(process.cwd(), p), "utf8");

const indexHtml = read("index.html");
const loadingCss = read("public/css/loading.css");
const themeStore = read("src/store/modules/theme-config.ts");

/**
 * 2026-10-05 暗色模式 F5 刷新闪蓝/闪白。
 *
 * 根因：index.html 里的 .init-page 启动占位是浏览器拿到 HTML 就立刻绘制的，
 * 而暗色要等 useThemeMethods 在运行时给 body 加 arco-theme="dark"（实测约 100ms 后）。
 * 那段时间占位页只有写死的 background:#fff + 品牌蓝 rgb(22,93,255)，
 * 暗色用户于是看到「白底 + 蓝条」一闪而过（dev 5177 与生产 dist 实测一致）。
 *
 * 修法：index.html 在 <link> 之前同步读一次 localStorage，命中暗色就打
 * html.uvp-boot-dark；loading.css 据此给占位页上暗色底 + 浅色蓝条。
 * ⛔ 这些断言守的是「时序」而不是样式好不好看：删掉内联脚本或 class 名，
 *    立刻会退化成闪白，用例必须红。
 */
describe("boot splash theme pre-set", () => {
  it("presets the dark theme before the app bundle runs", () => {
    // 内联脚本必须排在 loading.css 之前：先打 class，后者的规则才能命中
    const scriptAt = indexHtml.indexOf("uvp-boot-dark");
    const cssAt = indexHtml.indexOf("/css/loading.css");

    expect(scriptAt).toBeGreaterThan(-1);
    expect(cssAt).toBeGreaterThan(-1);
    expect(scriptAt).toBeLessThan(cssAt);

    // 必须是同步内联脚本，不能等 main.ts（那就又变成"太晚"）
    expect(indexHtml).toMatch(/<script>[\s\S]*?uvp-boot-dark[\s\S]*?<\/script>/);
    // 读的是 pinia 持久化用的那个 key
    expect(indexHtml).toContain('localStorage.getItem("theme-config")');
    expect(indexHtml).toContain('classList.add("uvp-boot-dark")');
  });

  it("keeps the boot splash dark so it cannot flash white on F5", () => {
    expect(loadingCss).toMatch(/\.uvp-boot-dark\s+\.init-page\s*\{[^}]*background:\s*#0b1118;/s);
    // 蓝条也必须换色，否则暗底上仍是亮蓝，仍然刺眼
    expect(loadingCss).toMatch(/\.uvp-boot-dark\s+\.snow-loader\s*\{[^}]*--c:[^}]*rgb\(96 165 250\)/s);
    // 亮色默认态保持原样，别把亮色用户也弄成深底
    expect(loadingCss).toMatch(/\.init-page\s*\{[^}]*background:\s*#fff;/s);
  });

  it("uses the same localStorage key as the theme store", () => {
    // 键名漂移 ⇒ 内联脚本永远读不到暗色 ⇒ 静默退化成闪白，所以钉住
    expect(themeStore).toContain('persistedstateConfig("theme-config")');
    expect(indexHtml).toContain('"theme-config"');
  });

  it("keeps the in-app loader on the theme-aware token instead of a hardcoded brand blue", () => {
    // src/style/model/loading-page.scss 的 .dc-loader 用 var(--primary-6)，
    // 主题一变就跟着变；s-internal-link-page 组件仍在用这个类，不能删。
    const scss = read("src/style/model/loading-page.scss");
    expect(scss).toMatch(/--c:\s*no-repeat linear-gradient\(rgb\(var\(--primary-6\)\)\s*0\s*0\)/);
    // 被删掉的两份重复工具不应复活（它们零引用，且会让人误以为该用它们）
    expect(() => read("src/utils/loading-page.ts")).toThrow();
    expect(() => read("src/utils/progress/index.ts")).toThrow();
  });
});
