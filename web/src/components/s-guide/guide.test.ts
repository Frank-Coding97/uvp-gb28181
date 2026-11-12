import { afterEach, describe, expect, it, vi } from "vitest";
import {
  buildGuideSteps,
  isTargetVisible,
  markGuideSeen,
  MIN_AUTO_START_STEPS,
  resolveGuideTarget,
  shouldAutoStartGuide,
  type GuideStepInput
} from "./guide";

/**
 * 引导的两个关键判断必须被锁死，否则坏起来是「静默」的：
 *   · 可见性判错 → 引导里出现「高亮一片空白」的步骤，不报错、只有人肉点才发现；
 *   · 是否首访判错 → 要么每次都弹（烦），要么永远不弹（新用户看不到）。
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

const EMPTY_RECT = {
  width: 0,
  height: 0,
  top: 0,
  left: 0,
  right: 0,
  bottom: 0,
  x: 0,
  y: 0,
  toJSON: () => ({})
} as DOMRect;

function makeElement(rect: DOMRect = VISIBLE_RECT): HTMLElement {
  const element = document.createElement("div");
  document.body.appendChild(element);
  vi.spyOn(element, "getBoundingClientRect").mockReturnValue(rect);
  return element;
}

afterEach(() => {
  document.body.innerHTML = "";
});

describe("isTargetVisible", () => {
  it("空值、已脱离文档的节点都不算可见", () => {
    expect(isTargetVisible(null)).toBe(false);
    expect(isTargetVisible(undefined)).toBe(false);

    const detached = document.createElement("div");
    vi.spyOn(detached, "getBoundingClientRect").mockReturnValue(VISIBLE_RECT);
    expect(isTargetVisible(detached)).toBe(false);
  });

  it("尺寸为 0 判为不可见 —— 这是 v-show / display:none 父子级吞掉时的实际表现", () => {
    // ⛔ 这条是核心：querySelector 查得到、但 rect 全 0 的元素，driver.js 会照常
    //    在左上角画一个 0×0 的高亮框，看起来就像「引导坏了」。
    expect(isTargetVisible(makeElement(EMPTY_RECT))).toBe(false);
  });

  it("有尺寸且参与布局才算可见", () => {
    expect(isTargetVisible(makeElement())).toBe(true);
  });

  it("display:none 与 visibility:hidden 无论尺寸如何都不可见", () => {
    const hidden = makeElement();
    hidden.style.display = "none";
    expect(isTargetVisible(hidden)).toBe(false);

    const invisible = makeElement();
    invisible.style.visibility = "hidden";
    expect(isTargetVisible(invisible)).toBe(false);
  });
});

describe("resolveGuideTarget", () => {
  it("没写锚点返回 null", () => {
    expect(resolveGuideTarget(undefined)).toBeNull();
  });

  it("按选择器取第一个命中的元素", () => {
    const element = makeElement();
    element.setAttribute("data-guide", "stats");
    expect(resolveGuideTarget('[data-guide="stats"]')).toBe(element);
  });

  it("选择器非法 / 取值函数抛错时返回 null，不把异常冒到驱动层", () => {
    expect(resolveGuideTarget('[data-guide="unclosed')).toBeNull();
    expect(
      resolveGuideTarget(() => {
        throw new Error("boom");
      })
    ).toBeNull();
    expect(resolveGuideTarget(() => undefined)).toBeNull();
  });
});

describe("buildGuideSteps", () => {
  const inputs: GuideStepInput[] = [
    { title: "欢迎", description: "居中说明" },
    { target: '[data-guide="stats"]', title: "统计环", description: "在线率" },
    { target: '[data-guide="missing"]', title: "不存在的锚点", description: "应当被丢掉" }
  ];

  it("没有锚点的居中步骤始终保留（首尾说明靠它）", () => {
    const steps = buildGuideSteps(inputs, () => null);
    expect(steps.map(step => step.title)).toContain("欢迎");
    expect(steps[0].element).toBeUndefined();
  });

  it("当前视图取不到的锚点整步丢弃，而不是退化成居中气泡", () => {
    // 典型场景：默认卡片视图里没有表格操作列 ⇒ 那一步不该出现。
    const visible = makeElement();
    const steps = buildGuideSteps(inputs, target => (target === '[data-guide="stats"]' ? visible : null));
    expect(steps.map(step => step.title)).toEqual(["欢迎", "统计环"]);
    expect(steps[1].element).toBe(visible);
  });

  it("锚点存在但不可见时同样丢弃", () => {
    const hidden = makeElement(EMPTY_RECT);
    const steps = buildGuideSteps(inputs, target => (target === '[data-guide="stats"]' ? hidden : null));
    expect(steps.map(step => step.title)).toEqual(["欢迎"]);
  });

  it("原样透传 side / align，让 driver 的朝向配置不被吞掉", () => {
    const visible = makeElement();
    const [step] = buildGuideSteps([{ target: () => visible, title: "t", description: "d", side: "left", align: "end" }]);
    expect(step.side).toBe("left");
    expect(step.align).toBe("end");
  });

  it("门槛数值本身被锁住：少于 3 步时不值得自动弹", () => {
    expect(MIN_AUTO_START_STEPS).toBe(3);
  });
});

describe("shouldAutoStartGuide / markGuideSeen", () => {
  function memoryStorage(initial: Record<string, string> = {}) {
    const store = new Map(Object.entries(initial));
    return {
      getItem: (key: string) => store.get(key) ?? null,
      setItem: (key: string, value: string) => void store.set(key, value),
      dump: () => Object.fromEntries(store)
    };
  }

  it("没配 storageKey 就不自动弹（只剩手动入口）", () => {
    expect(shouldAutoStartGuide(undefined, memoryStorage())).toBe(false);
    expect(shouldAutoStartGuide("k", null)).toBe(false);
  });

  it("首次访问要弹，标记过之后不再弹", () => {
    const storage = memoryStorage();
    expect(shouldAutoStartGuide("uvp.guide", storage)).toBe(true);
    markGuideSeen("uvp.guide", storage);
    expect(shouldAutoStartGuide("uvp.guide", storage)).toBe(false);
    expect(storage.dump()["uvp.guide"]).toBe("1");
  });

  it("读取抛错（无痕模式 / 被策略禁用）时照弹，不能让引导整个消失", () => {
    const broken = {
      getItem: () => {
        throw new Error("SecurityError");
      },
      setItem: () => {
        throw new Error("SecurityError");
      }
    };
    expect(shouldAutoStartGuide("uvp.guide", broken)).toBe(true);
    // 写不进去也不能把异常抛给调用方（否则 onDestroyed 里会炸掉整个浮层销毁流程）。
    expect(() => markGuideSeen("uvp.guide", broken)).not.toThrow();
  });
});
