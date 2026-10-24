import { ObjectDirective } from "vue";
import { hasAnyPermission } from "@/hooks/usePermission";

/**
 * 按钮级权限指令：无权限时**置灰禁用**，而不是把节点从页面上删掉。
 *
 * ⭐ 为什么改成禁用：节点被删除后用户看不到这些操作，也就无从知道平台有哪些功能。
 *    置灰保留了「功能可见性」，同时用 `pointer-events: none` + `[disabled]` 保证点不动。
 *
 * ⛔ 与旧实现的区别：旧实现是 `el.parentNode.removeChild(el)`（直接删 DOM）。
 *    那种写法**没法**改成禁用 —— 指令只拿得到渲染后的 DOM，而 Arco 组件的禁用
 *    既靠 `disabled` 属性（语义 / 键盘）又靠 `.arco-btn-disabled` 类（视觉），
 *    删节点等于把两者的载体一起丢了。
 *
 * ⚠️ 真正拦住越权的是后端 casbin 中间件；这里只是「前端收敛入口」，
 *    让用户看得到但点不动，**不能**当作唯一门禁。
 */

/** 指令自己打的标记，用于「只回收自己加的东西」以及幂等判断。 */
const PERM_FLAG = "uvpPermDisabled";
/** 指令介入**之前**元素是否已因业务原因禁用 —— 恢复时不能越权解除它。 */
const PREV_FLAG = "uvpPermPrevDisabled";

/** 兜底类：不依赖 Arco 组件类型，保证任何元素都能被置灰且不可点。 */
const FALLBACK_CLASS = "uvp-perm-disabled";
/** Arco 链接禁用态（Arco 原生与 `uvp-ui-language.scss` 均按此类名匹配）。 */
const LINK_CLASS = "arco-link-disabled";
/** Arco 按钮禁用态（`arco-overrides.scss` 按 `[disabled], .arco-btn-disabled` 匹配）。 */
const BUTTON_CLASS = "arco-btn-disabled";

/**
 * 归一化指令绑定值。
 * @param value 指令绑定值，字符串或字符串数组
 * @returns 权限标识数组
 */
const normalizePermissions = (value: unknown): string[] => {
  if (!value) throw new Error("v-hasPerm 指令需要配置权限标识");
  return Array.isArray(value) ? (value as string[]) : [String(value)];
};

/**
 * 宿主内部真正可交互的控件。
 * ⛔ 指令可能挂在**包了一层**的宿主上（典型：`a-upload`，里面才是那个触发按钮）：
 *    这时只给宿主加类名没有意义 —— 宿主是个 `div`，既没有按钮的视觉态也没有
 *    `disabled` 语义，用户看到的仍是「一个蓝色、看起来能点」的上传按钮。
 *    ⇒ 必须把禁用态**下沉**到内部控件上（全仓仅 `system/affix` 的 `a-upload` 沿用这一形态）。
 */
const INTERACTIVE_SELECTOR = "button, a, .arco-btn, .arco-link";

/**
 * 收集需要置灰的目标：**宿主自身 + 内部可交互控件**。
 * ⛔ 必须带上宿主自己：包裹层虽然只是"容器"，但阻断点击靠的正是它身上的
 *    `pointer-events: none`；漏掉它，还原时标记就清不掉了。
 * @param el 指令绑定的元素
 */
const collectTargets = (el: HTMLElement): HTMLElement[] => [
  el,
  ...Array.from(el.querySelectorAll<HTMLElement>(INTERACTIVE_SELECTOR))
];

/**
 * 置灰单个节点并阻止交互。
 * ⛔ 必须先记下节点原本的 `disabled` 状态：有些按钮是「没选中记录」而禁用的，
 *    权限恢复时不能把它一并解除。
 */
const markDisabled = (node: HTMLElement) => {
  if (node.dataset[PERM_FLAG] !== "true") {
    node.dataset[PREV_FLAG] = node.hasAttribute("disabled") ? "1" : "0";
    node.dataset[PERM_FLAG] = "true";
  }
  node.setAttribute("aria-disabled", "true");
  node.classList.add(FALLBACK_CLASS);
  if (node.tagName === "BUTTON" || node.classList.contains("arco-btn")) {
    node.classList.add(BUTTON_CLASS);
    // 用属性而不是仅靠 CSS：保证键盘 Tab + 回车也点不动。
    if (node.tagName === "BUTTON") node.setAttribute("disabled", "");
    return;
  }
  // ⛔ 只给链接加链接禁用色：`arco-link-disabled` 带链接色，压在普通 `div` 宿主上会串色。
  if (node.tagName === "A" || node.classList.contains("arco-link")) node.classList.add(LINK_CLASS);
};

/**
 * 还原单个节点的可用状态；只回收指令自己加过的东西。
 * @param node 目标节点
 */
const releaseDisabled = (node: HTMLElement) => {
  if (node.dataset[PERM_FLAG] !== "true") return;
  const wasDisabled = node.dataset[PREV_FLAG] === "1";
  node.classList.remove(FALLBACK_CLASS, BUTTON_CLASS, LINK_CLASS);
  if (!wasDisabled) {
    node.removeAttribute("disabled");
    node.removeAttribute("aria-disabled");
  }
  delete node.dataset[PERM_FLAG];
  delete node.dataset[PREV_FLAG];
};

/** 置灰（宿主自己或下沉到内部控件）。 */
const applyDisabled = (el: HTMLElement) => collectTargets(el).forEach(markDisabled);

/** 还原可用状态（宿主自己或下沉到内部控件）。 */
const applyEnabled = (el: HTMLElement) => collectTargets(el).forEach(releaseDisabled);

/**
 * 依据当前权限同步元素状态。
 * @param el 指令绑定的元素
 * @param bindingValue 指令绑定值
 */
const syncPermissionState = (el: HTMLElement, bindingValue: unknown) => {
  try {
    if (hasAnyPermission(normalizePermissions(bindingValue))) applyEnabled(el);
    else applyDisabled(el);
  } catch (error) {
    // ⛔ 异常时按「无权限」兜底，但**不再删节点**：配置写错不该让功能从页面上消失。
    console.error(`权限指令错误: ${error}`);
    applyDisabled(el);
  }
};

const hasPerm: ObjectDirective = {
  mounted: (el, binding) => syncPermissionState(el as HTMLElement, binding.value),
  updated: (el, binding) => syncPermissionState(el as HTMLElement, binding.value)
};

export default hasPerm;
