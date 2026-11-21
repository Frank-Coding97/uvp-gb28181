/**
 * 新手引导（driver.js）的纯逻辑层。
 *
 * 为什么单独抽出来：这套东西只有两个判断值得上测试 ——
 *   ① 「这个锚点此刻看不看得见」：设备列表默认是**卡片视图**，而表格操作列只在
 *      列表视图里存在；不判断就会把「高亮一片空白」的步骤也念给用户听。
 *   ② 「这台机器是不是第一次来」：localStorage 在无痕模式 / 被策略禁用时会抛错，
 *      抛错不能把引导整条链路带崩。
 * 两边都是纯函数（元素/存储都从参数进来），所以能直接在 happy-dom 里锁死。
 */

export type GuideSide = "top" | "right" | "bottom" | "left";
export type GuideAlign = "start" | "center" | "end";

/** 锚点：CSS 选择器，或就地取元素的函数（同一类按钮分散在两种视图里时用函数） */
export type GuideTarget = string | (() => Element | null | undefined);

export interface GuideStepInput {
  /** 留空表示「屏幕居中」的欢迎 / 收尾步骤，不做高亮 */
  target?: GuideTarget;
  title: string;
  /** 允许 HTML —— driver.js 用 innerHTML 注入，按钮图例靠它排版 */
  description: string;
  side?: GuideSide;
  align?: GuideAlign;
}

export interface ResolvedGuideStep {
  element?: Element;
  title: string;
  description: string;
  side?: GuideSide;
  align?: GuideAlign;
}

/**
 * 自动弹出的最低步数门槛。
 *
 * 过滤掉「当前视图里不存在的锚点」之后如果只剩一两步，说明页面还没把列表渲染出来
 * （请求还在飞 / 空数据），这时候自动弹出来只会是几个孤零零的气泡，不如不弹。
 */
export const MIN_AUTO_START_STEPS = 3;

/** 写进 localStorage 的标记值。改动它等于让所有老用户重新看一遍引导。 */
const SEEN_VALUE = "1";

export interface GuideStorage {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
}

/**
 * 元素此刻是否真的「看得见」。
 *
 * ⛔ 不能只用 `document.querySelector` 判空：`v-show` 隐藏、以及被 `display:none`
 *    父级吞掉的元素照样查得到，但 getBoundingClientRect 是全 0。驱动浮层挂上去就是
 *    在屏幕左上角画一个 0×0 的高亮框，看起来像「引导坏了」。
 */
export function isTargetVisible(element: Element | null | undefined): element is Element {
  if (!element || !element.isConnected) return false;
  const rect = element.getBoundingClientRect();
  if (rect.width <= 0 || rect.height <= 0) return false;
  if (typeof window === "undefined" || !window.getComputedStyle) return true;
  const style = window.getComputedStyle(element);
  return style.display !== "none" && style.visibility !== "hidden";
}

/** 解析锚点。选择器非法、函数抛错都当成「没找到」，不让异常冒到驱动层。 */
export function resolveGuideTarget(target: GuideTarget | undefined): Element | null {
  if (!target) return null;
  if (typeof target === "function") {
    try {
      return target() ?? null;
    } catch {
      return null;
    }
  }
  try {
    return document.querySelector(target);
  } catch {
    return null;
  }
}

/**
 * 把「步骤声明」编译成「本轮真正能跑的步骤」。
 *
 * 两种步骤命运不同，别一起砍：
 *   · 没写 target → 居中步骤，**始终保留**（首尾说明就靠它）；
 *   · 写了 target 但当前视图取不到元素 → **整步丢弃**（卡片视图里没有表格操作列）。
 */
export function buildGuideSteps(
  inputs: readonly GuideStepInput[],
  lookup: (target: GuideTarget | undefined) => Element | null = resolveGuideTarget
): ResolvedGuideStep[] {
  const steps: ResolvedGuideStep[] = [];
  for (const input of inputs) {
    const base = { title: input.title, description: input.description, side: input.side, align: input.align };
    if (!input.target) {
      steps.push(base);
      continue;
    }
    const element = lookup(input.target);
    if (!isTargetVisible(element)) continue;
    steps.push({ ...base, element });
  }
  return steps;
}

/**
 * 是否该自动弹。
 *
 * ⚠️ 存储读取失败时返回 `true`（照弹）。取舍：读不到就没法记「看过了」，宁可每次
 * 多弹一次，也不要让无痕模式下的人一次都看不到 —— 而且手动入口一直在工具栏上，
 * 真嫌烦的人一次就能学会怎么关。
 */
export function shouldAutoStartGuide(storageKey: string | undefined, storage: GuideStorage | null | undefined): boolean {
  if (!storageKey || !storage) return false;
  try {
    return storage.getItem(storageKey) !== SEEN_VALUE;
  } catch {
    return true;
  }
}

/** 记「已经看过」。写不进去不算错误 —— 顶多下次再弹一遍。 */
export function markGuideSeen(storageKey: string | undefined, storage: GuideStorage | null | undefined): void {
  if (!storageKey || !storage) return;
  try {
    storage.setItem(storageKey, SEEN_VALUE);
  } catch {
    /* 无痕模式 / 存储配额满：忽略。 */
  }
}
