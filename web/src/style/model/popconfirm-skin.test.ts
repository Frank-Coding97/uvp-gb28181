import { Popconfirm } from "@arco-design/web-vue";
import { mount } from "@vue/test-utils";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import { defineComponent, h, nextTick } from "vue";

/**
 * 确认气泡（`a-popconfirm`）皮肤门禁。
 *
 * ⛔ 为什么必须真渲染一遍：Arco 的 popconfirm 有两层类名 ——
 *   触发器根叫 `.arco-popconfirm`（**自身没有底色**），真正带背景的面板是 Trigger 的
 *   `content-class` = `.arco-popconfirm-popup-content`。
 *   2026-11-04 之前本仓的皮肤规则全写在 `.arco-trigger-popup .arco-popconfirm { background: … }`
 *   上，看着像"已统一"，实际面板一直是 Arco 默认的中性灰（暗色 #373739）——
 *   在深蓝卡片上就是"灰盒子"。这类"按我们以为的类名写规则"的错误，桩测/读源码都发现不了，
 *   只有真挂一个组件看 DOM 才露馅（同 `dictionary.readonlyGate.trigger.test.ts` 的思路）。
 */

const SRC = resolve(process.cwd(), "src");
const skinSource = readFileSync(resolve(SRC, "style/model/uvp-ui-language.scss"), "utf8");

/** 只取 popconfirm 相关那段，避免被文件里其它 `.arco-trigger-popup` 规则误命中。 */
function popconfirmBlock(source: string) {
  const start = source.indexOf(".arco-trigger-popup .arco-popconfirm-popup-content {");
  const end = source.indexOf("// ==========", start === -1 ? 0 : start);
  return start === -1 ? "" : source.slice(start, end === -1 ? undefined : end);
}

const Harness = defineComponent({
  setup() {
    return () =>
      h(
        Popconfirm,
        { content: "确定删除该账号吗?", okText: "确定", cancelText: "取消" },
        { default: () => h("a", { class: "trigger" }, "删除") }
      );
  }
});

async function openPopconfirm() {
  const wrapper = mount(Harness, { attachTo: document.body });
  await wrapper.find(".trigger").trigger("click");
  await nextTick();
  await new Promise(resolve => setTimeout(resolve, 0));
  return wrapper;
}

describe("确认气泡皮肤", () => {
  afterEach(() => {
    document.body.innerHTML = "";
  });

  it("真渲染出来的面板类名就是 .arco-popconfirm-popup-content", async () => {
    const wrapper = await openPopconfirm();
    const panel = document.body.querySelector(".arco-popconfirm-popup-content");
    expect(panel, "面板类名变了：皮肤规则会再次静默失效").not.toBeNull();
    // 裸 `.arco-popconfirm` 只是触发器根节点，它没有底色 —— 皮肤绝不能只写在这一层
    expect(panel).not.toBe(document.body.querySelector(".arco-popconfirm"));
    wrapper.unmount();
  });

  it("面板确实挂在 .arco-trigger-popup 之下（祖先选择器才生效）", async () => {
    const wrapper = await openPopconfirm();
    // ⛔ 皮肤规则写的是 `.arco-trigger-popup .arco-popconfirm-popup-content`。
    //   一旦 Arco 换了 popup 容器类名，规则会**静默**退化成 Arco 默认灰底 ——
    //   和 2026-11-04 之前那个 bug 一模一样。所以这里必须验祖先，而不只是验面板存在。
    expect(document.body.querySelector(".arco-trigger-popup .arco-popconfirm-popup-content")).not.toBeNull();
    wrapper.unmount();
  });

  it("皮肤规则打在面板与箭头上，且用平台浮层 token", () => {
    const block = popconfirmBlock(skinSource);
    expect(block, "找不到确认气泡皮肤段落").not.toBe("");

    expect(block).toMatch(/\.arco-trigger-popup \.arco-popconfirm-popup-content \{/);
    expect(block).toMatch(/background: var\(--uvp-popconfirm-bg\) !important;/);
    expect(block).toMatch(/border: 1px solid var\(--uvp-popconfirm-border\) !important;/);
    expect(block).toMatch(/box-shadow: var\(--uvp-popconfirm-shadow\) !important;/);
    expect(block).toMatch(/\.arco-popconfirm-popup-arrow \{/);
    expect(block).toMatch(/background-color: var\(--uvp-popconfirm-bg\) !important;/);
  });

  it("不再把底色写在无底色的触发器根上（旧错写法的反向断言）", () => {
    // 允许 `.arco-trigger-popup .arco-popconfirm` 出现在注释里，但不允许它后面直接跟一条
    // 带 background 的声明块 —— 那正是"看着改了、实际没生效"的写法。
    expect(skinSource).not.toMatch(/\.arco-trigger-popup \.arco-popconfirm \{[^}]*background:/s);
  });

  it("内边距只由面板出，body / footer 不再各加一层", () => {
    const block = popconfirmBlock(skinSource);
    expect(block).toMatch(/\.arco-popconfirm-body \{[^}]*padding: 0 !important;/s);
    expect(block).toMatch(/\.arco-popconfirm-footer \{[^}]*padding: 0 !important;/s);
  });
});
