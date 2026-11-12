import { onUnmounted, ref } from "vue";
import { driver, type DriveStep, type Driver } from "driver.js";
import "driver.js/dist/driver.css";
import { buildGuideSteps, markGuideSeen, MIN_AUTO_START_STEPS, shouldAutoStartGuide, type GuideStepInput } from "./guide";

/**
 * 新手引导（产品导览）composable。
 *
 * 为什么不是 `s-guide` 组件：引导**没有自己的 DOM** —— 浮层由 driver.js 直接挂到
 * body 上，页面里没有任何可视产物。做成组件会出现一个「模板里放个空标签」的空壳，
 * 反而要多解释一层。composable 只暴露 start/stop，触发按钮由页面自己按现有规范画。
 *
 * 视觉：皮肤在 `src/style/model/uvp-guide.scss`，靠 `popoverClass` 挂上去。
 * ⛔⛔ 改下面这个类名必须同步改那个 scss 的选择器，否则样式整体失效、退化成
 *     driver.js 默认的 13px 黑边小方框（那种一看就不是我们的系统）。
 */

/** 挂在 driver 浮层上的类名，皮肤的选择器前缀。 */
const POPOVER_CLASS = "uvp-guide";

export interface UseGuideOptions {
  /** 步骤声明。传函数可以每次启动时重新取值（步骤随权限/数据变化的场景）。 */
  steps: readonly GuideStepInput[] | (() => readonly GuideStepInput[]);
  /**
   * 「已经看过」的存储键。不传 = 不写存储、也不自动弹（只剩手动入口）。
   * 建议带上版本号，改动引导内容时改版本号即可让老用户重新看一遍。
   */
  storageKey?: string;
}

export interface GuideController {
  /** 手动开始。返回 false 表示当前页面没有可讲的步骤（不弹空的浮层）。 */
  start: (stepIndex?: number) => boolean;
  /** 首次访问自动开始。返回 false 表示不满足自动弹出的条件。 */
  autoStart: () => boolean;
  stop: () => void;
  /** 浮层是否正在显示（给触发按钮做状态联动用）。 */
  active: () => boolean;
}

/** 遮罩色。亮色下压一层冷灰，暗色下压更重 —— 两套主题用同一个色会一边偏白一边糊。 */
function overlayColor(): string {
  const dark = typeof document !== "undefined" && document.body.getAttribute("arco-theme") === "dark";
  return dark ? "rgb(2 6 12 / 78%)" : "rgb(15 23 42 / 52%)";
}

function safeStorage(): Storage | null {
  if (typeof window === "undefined") return null;
  try {
    return window.localStorage;
  } catch {
    // Safari 隐私模式 / 被策略禁用时，连访问 localStorage 这个属性本身都会抛错。
    return null;
  }
}

export function useGuide(options: UseGuideOptions): GuideController {
  const showing = ref(false);
  let instance: Driver | null = null;

  function resolveInputs(): readonly GuideStepInput[] {
    return typeof options.steps === "function" ? options.steps() : options.steps;
  }

  /** 把声明编译成 driver 的步骤；当前视图里没有的锚点已被 `buildGuideSteps` 丢掉。 */
  function compileSteps(): DriveStep[] {
    return buildGuideSteps(resolveInputs()).map(step => ({
      element: step.element,
      popover: {
        title: step.title,
        description: step.description,
        side: step.side,
        align: step.align
      }
    }));
  }

  function start(stepIndex = 0): boolean {
    if (instance?.isActive()) return false;
    const steps = compileSteps();
    if (!steps.length) return false;

    instance = driver({
      steps,
      // ⛔ 每一项都显式给：driver.js 的默认值（13px 字号 / 5px 圆角 / 白色箭头）
      //    和我们的语言差得远，靠 CSS 兜不如一开始就传对。
      showProgress: true,
      progressText: "{{current}} / {{total}}",
      nextBtnText: "下一步",
      prevBtnText: "上一步",
      doneBtnText: "知道了",
      allowClose: true,
      allowKeyboardControl: true,
      overlayColor: overlayColor(),
      // 颜色本身已经带 alpha，再乘一次会淡到看不清高亮区。
      overlayOpacity: 1,
      overlayClickBehavior: "close",
      stagePadding: 6,
      stageRadius: 12,
      popoverOffset: 12,
      smoothScroll: true,
      animate: true,
      // 讲解期间不允许点高亮元素本身：边看边点很容易把后续步骤的锚点弄没了
      // （比如把抽屉点开，遮罩下再想关就关不掉）。
      disableActiveInteraction: true,
      popoverClass: POPOVER_CLASS,
      onDestroyed: () => {
        showing.value = false;
        instance = null;
        // 只要浮层真的显示过就记「看过」。中途 Esc 也算 —— 手动入口一直在工具栏上，
        // 想再看一次随时能点；反过来「每次进来都自动弹」才是真的烦人。
        markGuideSeen(options.storageKey, safeStorage());
      }
    });

    showing.value = true;
    instance.drive(stepIndex);
    return true;
  }

  function autoStart(): boolean {
    if (!shouldAutoStartGuide(options.storageKey, safeStorage())) return false;
    // 先试编译：页面还没把列表渲染出来时步骤会被大量丢掉，此时自动弹等于弹个空壳。
    if (compileSteps().length < MIN_AUTO_START_STEPS) return false;
    return start();
  }

  function stop(): void {
    instance?.destroy();
    instance = null;
    showing.value = false;
  }

  onUnmounted(() => {
    // ⛔ 不销毁就是脏浮层：路由跳走以后遮罩还挂在 body 上，整个系统看着像卡死。
    //    这里直接清引用再 destroy，避免 onDestroyed 里对已卸载组件写状态。
    const active = instance;
    instance = null;
    showing.value = false;
    active?.destroy();
  });

  return {
    start,
    autoStart,
    stop,
    active: () => showing.value
  };
}
