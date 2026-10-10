/**
 * 「跟随最新日志」的滚动判定规则。
 *
 * 终端区只有一个滚动条，但它承载两个相反的意图：
 *   · 用户滚离底部 ⇒ 暂停跟随，否则新日志不停把视线拽回底部，历史没法看；
 *   · 用户滚回底部 ⇒ 恢复跟随，不必再专门去点一次「继续跟随」。
 *
 * 两侧阈值刻意不相等，中间留一段滞回区：离底 > 80px 才暂停、≤ 24px 才恢复，
 * 落在中间时保持原状态。等阈值会让「恰好停在边界」的滚动反复切换跟随状态。
 */

/** 距底 ≤ 该值视为「已在底部」，恢复跟随。 */
export const FOLLOW_RESUME_DISTANCE = 24;
/** 距底 > 该值视为「已离开底部」，暂停跟随。 */
export const FOLLOW_PAUSE_DISTANCE = 80;

export type FollowAction = "keep" | "resume" | "pause";

/**
 * 依据「当前距底距离」和「当前是否跟随」决定要不要改变跟随状态。
 *
 * @param distanceFromBottom scrollHeight - scrollTop - clientHeight；负值（scrollTop 溢出）按已在底部处理
 * @param following          当前是否处于跟随状态
 */
export function resolveFollowAction(distanceFromBottom: number, following: boolean): FollowAction {
  if (distanceFromBottom <= FOLLOW_RESUME_DISTANCE) return following ? "keep" : "resume";
  if (distanceFromBottom > FOLLOW_PAUSE_DISTANCE) return following ? "pause" : "keep";
  return "keep";
}
