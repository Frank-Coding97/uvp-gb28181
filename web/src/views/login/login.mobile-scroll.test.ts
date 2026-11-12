import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const loginSource = readFileSync(resolve(process.cwd(), "src/views/login/login.vue"), "utf8");
const globalSource = readFileSync(resolve(process.cwd(), "src/style/index.scss"), "utf8");

/** 切出 `.login-page { ... }` 主块（`.login-page.dark-mode` 之前的部分） */
const loginPageBlock = loginSource.slice(loginSource.indexOf(".login-page {"), loginSource.indexOf(".login-page.dark-mode"));
/** 切出 `.topology-bg { ... }` 块 */
const topologyBlock = loginSource.slice(loginSource.indexOf(".topology-bg {"), loginSource.indexOf("@keyframes star-pulse"));
/** 切出手机竖屏那一档媒体查询 */
const mobileBlock = loginSource.slice(
  loginSource.indexOf("@media (width <= 880px)"),
  loginSource.indexOf("@media (width <= 360px)")
);

describe("login page scrolls on short viewports", () => {
  // 回归背景：全局把 html/body/#app 都设成 overflow:hidden（工作台各页自己内部滚动依赖它），
  // 登录页原来只有 min-height:100vh + overflow:hidden ⇒ 手机竖屏时整页比视口高 200~300px，
  // 超出的部分被直接裁掉，登录按钮在视口外且滚不到（375x667 实测按钮 top≈768、scrollTop 恒 0）。
  it("keeps the global shell scroll-locked so the login page has to scroll itself", () => {
    expect(globalSource).toMatch(/html,\s*body,\s*#app\s*\{[^}]*overflow:\s*hidden/s);
  });

  it("turns the login page into its own scroll container with a definite viewport height", () => {
    expect(loginPageBlock).toContain("height: 100vh");
    expect(loginPageBlock).toContain("height: 100dvh");
    // ⚠️ stylelint --fix 会把 overflow-x/overflow-y 合并成 `overflow: hidden auto`（语义等价），
    // 所以这里只认「x 裁掉 + y 自动」这个语义，longhand 与合并两种写法都算过。
    expect(loginPageBlock).toMatch(/overflow:\s*hidden\s+auto|overflow-y:\s*auto/);
    expect(loginPageBlock).toMatch(/overflow:\s*hidden\s+auto|overflow-x:\s*hidden/);
    // ⛔ 两个方向都 hidden 就等于没有滚动能力（min-height 既不限高也不产生滚动条），这条必须挡住
    expect(loginPageBlock).not.toMatch(/^\s*overflow:\s*hidden\s*;/m);
  });

  it("keeps the decorative background pinned while the page scrolls", () => {
    // absolute 会随内容滚走 ⇒ 滚过首屏后网格/光晕整块消失，只剩纯色。
    expect(topologyBlock).toMatch(/position:\s*fixed/);
    expect(topologyBlock).not.toMatch(/position:\s*absolute/);
  });
});

describe("login page mobile layout stays compact", () => {
  // 手机端把"营销长文案"收成标题条：改前整页 976px 高，按钮落在 667px 视口之外；
  // 光靠能滚只是兜底，真正好用的是让它在常见手机上根本不用滚。
  it("collapses the marketing block into a title bar and frees the version strip", () => {
    expect(mobileBlock).toMatch(/\.brand-lede\s*\{\s*display:\s*none/);
    expect(mobileBlock).toMatch(/\.page-meta\s*\{\s*display:\s*none/);
    expect(mobileBlock).toMatch(/\.brand-title\s*\{[^}]*font-size:\s*19px/s);
  });

  it("stacks the footer instead of wrapping it mid-sentence", () => {
    expect(mobileBlock).toMatch(/\.page-foot\s*\{[^}]*flex-direction:\s*column/s);
    expect(mobileBlock).toMatch(/\.foot-powered,\s*\.foot-sep\s*\{\s*display:\s*none/s);
  });
});
