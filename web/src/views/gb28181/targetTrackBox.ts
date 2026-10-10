import type { TargetTrackArea } from "@/api/gb28181";

/**
 * 目标跟踪「画面拉框 → A.2.3.1.14 TargetArea 六项」的换算。
 *
 * ## 为什么抽成纯函数
 *
 * 这段换算有**三项独立的约定**（归一化坐标系、渲染像素基准、四舍五入与钳位的次序），
 * 任何一项写歪都会发出一条**格式合法但落点错误**的报文 —— 设备照着换算，
 * 跟踪框落在离目标半个屏幕的地方，而平台侧一切正常（无应答命令，连错都看不见）。
 * 所以它不进组件的 `finishDragZoom` 那样的内联代码，而是放在这里配单测钉住。
 *
 * ## 坐标口径（标准原文，A.2.3.1.14 注释）
 *
 * > 由于平台与设备画面比例大小不同，需要进行比例关系转化。因此，平台应提供画面大小：
 * > 播放窗口长度像素值和播放窗口宽度像素值。
 *
 * ⇒ `length` / `width` 传的是**视频画面在页面上实际渲染的像素尺寸**
 * （`getBoundingClientRect()` 量到的那块，不是视频原始分辨率、不是播放器元素外框），
 * 而 `midPointX/midPointY/lengthX/lengthY` 必须是**同一坐标系**里相对画面左上角的像素。
 * 设备负责按自己的画幅做比例换算。
 *
 * ⛔ 这与**遮挡**（`PictureMask`）的基准刻意不同：那边是"设备 OSD 声明/上报的图像尺寸"
 * （真机 704×576，与页面渲染无关，所以镜像/缩放都不影响落点）。两套基准不能互相套用 ——
 * 目标跟踪的框是"操作员在**这块画面**上圈出来的目标"，它天然长在渲染坐标里。
 */

/** 归一化坐标点：`x`/`y` ∈ [0, 1]，原点 = 画面左上角。 */
export interface TargetTrackPoint {
  x: number;
  y: number;
}

export interface TargetTrackBoxInput {
  /** 按下点。 */
  start: TargetTrackPoint;
  /** 抬起点。 */
  end: TargetTrackPoint;
  /** 画面**渲染**宽度（px），通常来自 `.play-window` 的 `getBoundingClientRect().width`。 */
  renderedWidth: number;
  /** 画面**渲染**高度（px）。 */
  renderedHeight: number;
}

/**
 * 框太小就不发：一次误触（点一下没拖）会送出"一个 1×1 像素的目标"。
 *
 * ⛔ 门禁在**这边**、不在服务端：服务端只校验"尺寸为正数、中心落在窗口内"，
 *    一个 1px 的框完全合法（见 `ValidateTargetTrackCommand`）。也就是说
 *    没有这道闸门时，手抖点一下会静默地让设备去跟踪一个像素点。
 *    阈值与拉框变焦保持一致（0.02 ≈ 画面短边的 2%），用户不必记两套。
 */
export const TARGET_TRACK_MIN_BOX_RATIO = 0.02;

/**
 * 比例比较的容差。
 *
 * ⛔ 必须有：归一化坐标是 `pointer / 元素尺寸` 两次除法减出来的浮点数，
 *    `0.2 + 0.02 - 0.2` 在 IEEE754 下是 `0.01999999999999999` —— 比阈值小 1e-17。
 *    不加容差的表现是"框选范围刚好卡在阈值上时**随机**被拒"，而且用户完全无从理解
 *    （他画的框看着和上一次一模一样）。1e-9 远大于指针坐标的浮点噪声（~1e-7 px 量级），
 *    又远小于任何有意义的框尺寸，不会把真正的小框放过去。
 */
const RATIO_EPSILON = 1e-9;

export type TargetTrackAreaResult = { ok: true; area: TargetTrackArea } | { ok: false; reason: string };

function clampUnit(value: number) {
  if (!Number.isFinite(value)) return 0;
  return Math.max(0, Math.min(1, value));
}

/**
 * 把一次框选换算成 `TargetArea`。
 *
 * ⛔ 与后端 `ValidateTargetTrackCommand` **刻意不共用**的一条规则：这里**允许框超出画面**，
 *    先按 [0,1] 钳位再算，不因为越界就拒发。理由是操作员拖着拖到画面边缘、
 *    目标本身贴着边被 UI 裁掉半个框，都是正常操作；设备侧本来就要做比例换算，
 *    越界的钳位是它的事（与"拉框放大"不同 —— 那个框是平台自己算的，越界一定是平台错了）。
 */
export function buildTargetTrackArea(input: TargetTrackBoxInput): TargetTrackAreaResult {
  const renderedWidth = Math.round(input.renderedWidth);
  const renderedHeight = Math.round(input.renderedHeight);
  // ⛔ 尺寸为 0 必须在这里被拦住，别指望它"到设备那边会变成 0 也还行"：
  //    0 会让比例换算除零，而且它会顺带掩盖"前端根本没量到画面尺寸"这个 bug。
  if (!(renderedWidth > 0) || !(renderedHeight > 0)) {
    return { ok: false, reason: "未测到画面的渲染尺寸，无法换算跟踪框（请等画面起播后再框选）" };
  }

  const startX = clampUnit(input.start.x);
  const startY = clampUnit(input.start.y);
  const endX = clampUnit(input.end.x);
  const endY = clampUnit(input.end.y);

  const left = Math.min(startX, endX);
  const top = Math.min(startY, endY);
  const ratioX = Math.abs(endX - startX);
  const ratioY = Math.abs(endY - startY);
  if (ratioX < TARGET_TRACK_MIN_BOX_RATIO - RATIO_EPSILON || ratioY < TARGET_TRACK_MIN_BOX_RATIO - RATIO_EPSILON) {
    return { ok: false, reason: "框选范围太小，请重新框选要跟踪的目标" };
  }

  // `length` / `width`：标准的取值就是播放窗口的**像素**尺寸，原样送，不做任何换算。
  const length = Math.max(1, renderedWidth);
  const width = Math.max(1, renderedHeight);

  // ⛔ 先算浮点再统一取整，**不要**分别对各分量先取整：四项独立四舍五入会出现
  //    "中心点 + 半宽 比 右边界多出 1px"这种自相矛盾的一帧，真机侧按哪个解释都不算错。
  const midPointX = clampRounded((left + ratioX / 2) * length, length);
  const midPointY = clampRounded((top + ratioY / 2) * width, width);
  // 尺寸至少 1px：钳位后可能出现 ratioX 极小但 ≥ 阈值的情况（宽画面），取整会掉到 0，
  // 而 0 是不合法值（服务端会拒）。
  const lengthX = Math.max(1, Math.round(ratioX * length));
  const lengthY = Math.max(1, Math.round(ratioY * width));

  return { ok: true, area: { length, width, midPointX, midPointY, lengthX, lengthY } };
}

/** 取整后夹在 `[0, limit]`（含端点）—— 中心点允许贴边，但绝不能落到窗口外。 */
function clampRounded(value: number, limit: number) {
  return Math.max(0, Math.min(limit, Math.round(value)));
}
