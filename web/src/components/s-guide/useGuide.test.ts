import { mount } from "@vue/test-utils";
import { afterEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h, nextTick } from "vue";
import type { GuideStepInput } from "./guide";
import { useGuide, type GuideController } from "./useGuide";

/**
 * 组件级测试只锁一件事：**浮层真的被创建出来、并且带上了我们皮肤要求的类名**。
 *
 * 为什么这条必须锁：皮肤（`src/style/model/uvp-guide.scss`）的全部选择器都挂在
 * `.driver-popover.uvp-guide` 上，而类名是靠 `useGuide` 里的 `popoverClass` 传下去的。
 * 这个字符串一旦拼错或漏传，**不会报任何错**，只是浮层静默退化成 driver.js 的原生
 * 白底小方框 —— 只有人工点开才看得出来。
 */

const VISIBLE_RECT = {
  width: 120,
  height: 44,
  top: 10,
  left: 10,
  right: 130,
  bottom: 54,
  x: 10,
  y: 10,
  toJSON: () => ({})
} as DOMRect;

/** 造一个「看得见」的锚点，并把 driver 真正需要的高亮目标挂进文档。 */
function makeVisibleTarget(selector: string): HTMLElement {
  const element = document.createElement("div");
  element.setAttribute("data-test-target", selector);
  document.body.appendChild(element);
  vi.spyOn(element, "getBoundingClientRect").mockReturnValue(VISIBLE_RECT);
  return element;
}

function mountHarness(steps: readonly GuideStepInput[], storageKey?: string) {
  let controller!: GuideController;
  const wrapper = mount(
    defineComponent({
      setup() {
        controller = useGuide({ steps, storageKey });
        return () => h("div");
      }
    })
  );
  return { wrapper, controller };
}

afterEach(() => {
  document.body.innerHTML = "";
  document.body.removeAttribute("arco-theme");
});

describe("useGuide", () => {
  it("一个锚点都取不到时不开浮层（避免弹一堆居中的空壳）", () => {
    const steps: GuideStepInput[] = [{ target: '[data-test-target="nope"]', title: "缺失", description: "-" }];
    const { wrapper, controller } = mountHarness(steps);
    // 只有锚点取不到的步骤 ⇒ 编译后为空 ⇒ 直接拒绝启动。
    expect(controller.start()).toBe(false);
    expect(document.body.querySelector(".driver-popover")).toBeNull();
    wrapper.unmount();
  });

  it("锚点可见时创建浮层，且带上了皮肤类名 uvp-guide", async () => {
    makeVisibleTarget("anchor");
    const steps: GuideStepInput[] = [{ target: '[data-test-target="anchor"]', title: "标题", description: "说明" }];
    const { wrapper, controller } = mountHarness(steps);

    expect(controller.start()).toBe(true);
    expect(controller.active()).toBe(true);
    await nextTick();

    const popover = document.body.querySelector(".driver-popover");
    expect(popover).not.toBeNull();
    // ⛔ 这条是皮肤生效的唯一保障：类名丢了样式就整体失效。
    expect(popover!.classList.contains("uvp-guide")).toBe(true);
    expect(popover!.querySelector(".driver-popover-title")?.innerHTML).toBe("标题");

    controller.stop();
    wrapper.unmount();
  });

  it("重复调用 start 不会叠加出第二个浮层", async () => {
    makeVisibleTarget("anchor");
    const steps: GuideStepInput[] = [{ target: '[data-test-target="anchor"]', title: "标题", description: "说明" }];
    const { wrapper, controller } = mountHarness(steps);

    expect(controller.start()).toBe(true);
    expect(controller.start()).toBe(false);
    await nextTick();
    expect(document.body.querySelectorAll(".driver-popover").length).toBe(1);

    controller.stop();
    wrapper.unmount();
  });

  it("stop 之后浮层从 body 上摘掉", async () => {
    makeVisibleTarget("anchor");
    const steps: GuideStepInput[] = [{ target: '[data-test-target="anchor"]', title: "标题", description: "说明" }];
    const { wrapper, controller } = mountHarness(steps);

    controller.start();
    await nextTick();
    controller.stop();
    await nextTick();
    expect(document.body.querySelector(".driver-popover")).toBeNull();
    expect(controller.active()).toBe(false);

    wrapper.unmount();
  });

  it("autoStart：步骤不足门槛时不动（页面还没渲染完）", () => {
    // 只有一个可见锚点 ⇒ 编译后 1 步 < MIN_AUTO_START_STEPS，说明列表还没出来。
    makeVisibleTarget("anchor");
    const steps: GuideStepInput[] = [{ target: '[data-test-target="anchor"]', title: "标题", description: "说明" }];
    const { wrapper, controller } = mountHarness(steps, "uvp.guide.test.auto");
    expect(controller.autoStart()).toBe(false);
    expect(document.body.querySelector(".driver-popover")).toBeNull();
    wrapper.unmount();
  });

  it("autoStart：步骤够多且是首次访问时才弹", async () => {
    for (const key of ["a", "b", "c"]) makeVisibleTarget(key);
    const steps: GuideStepInput[] = ["a", "b", "c"].map(key => ({
      target: `[data-test-target="${key}"]`,
      title: key,
      description: "-"
    }));
    window.localStorage.removeItem("uvp.guide.test.first");
    const { wrapper, controller } = mountHarness(steps, "uvp.guide.test.first");

    expect(controller.autoStart()).toBe(true);
    await nextTick();
    expect(document.body.querySelector(".driver-popover")).not.toBeNull();

    controller.stop();
    wrapper.unmount();
  });

  it("组件卸载时把浮层一起销毁（路由跳走后不留脏遮罩）", async () => {
    makeVisibleTarget("anchor");
    const steps: GuideStepInput[] = [{ target: '[data-test-target="anchor"]', title: "标题", description: "说明" }];
    const { wrapper, controller } = mountHarness(steps);

    controller.start();
    await nextTick();
    expect(document.body.querySelector(".driver-popover")).not.toBeNull();

    wrapper.unmount();
    await nextTick();
    expect(document.body.querySelector(".driver-popover")).toBeNull();
  });
});
