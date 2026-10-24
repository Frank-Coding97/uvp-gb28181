/**
 * 回放会话「拆除」(停止)的失败归因,以及「等通道释放」的等待策略。
 *
 * ## 背景(实测大华 DH-3H3405-ADG,192.168.10.204 / 通道 ...0006)
 *
 * 该设备对 MANSRTSP TEARDOWN 会回 2xx,但**恒不回 BYE**;平台的拆除是
 * fail-closed 的 —— 没拿到设备确认就保留通道绑定,交后台清扫器按退避重试,
 * 直到超过重试上限才止损释放。在这段窗口里:
 *
 * - `DELETE .../playback-sessions/:id` 固定 504 且 `errorStage=teardown`,
 *   不是媒体侧的「收流超时」;
 * - 再建会话固定 429 `playback_busy`「当前通道已有回放会话」。
 *
 * 所以「点一次没成功就报错收场」会让页面直接卡死:用户再拖动只会不断撞 429。
 * 正确做法是等通道真正释放,再继续用户原本的操作。
 *
 * ## 为什么不能用 DELETE 轮询来观察释放
 *
 * 通道绑定被保留时,第二次 DELETE 不会重发那条 TEARDOWN INFO
 * (`record.closing` 已置位),只会再白等一个 SIP 超时后返回同样失败,
 * 推不动任何状态。唯一能反映进展的信号是「会话是否已从平台消失」。
 *
 * ## 前端提示语从哪来
 *
 * 这一页的错误文案不是模板渲染的,而是 `utils/http` 封装层对**每一个**失败
 * 请求自动弹的全局 `Message.error(response.data.message)`。重试循环里的中间
 * 失败必须用 `showErrorMessage: false` 抑制,只在真正失败时弹一条。
 */

/** 观察通道释放的退避间隔(毫秒),合计约 11s,覆盖后台清扫器的首轮退避窗口。 */
export const channelReleaseWatchDelays = [500, 750, 1000, 1500, 2000, 2500, 3000] as const;

/** 释放观察的探测预算(= 探测次数)。 */
export const channelReleaseWatchBudget = channelReleaseWatchDelays.length;

interface PlaybackFailureShape {
  errorCode?: string;
  response?: { status?: number; data?: { message?: string; data?: { errorCode?: string; errorStage?: string } } };
}

function failureShape(error: unknown): PlaybackFailureShape {
  return (error ?? {}) as PlaybackFailureShape;
}

export function playbackFailureStatus(error: unknown): number | undefined {
  return failureShape(error).response?.status;
}

/** 后端业务错误码。优先取响应体里的 `errorCode`,兼容直接挂在错误对象上的写法。 */
export function playbackFailureCode(error: unknown): string {
  const candidate = failureShape(error);
  return candidate.response?.data?.data?.errorCode || candidate.errorCode || "";
}

export function playbackFailureStage(error: unknown): string {
  return failureShape(error).response?.data?.data?.errorStage || "";
}

/**
 * 设备没确认拆除:通道绑定被保留,后台会继续重试。
 * 属于「等一会儿」而不是「坏了」,因此调用方应转去观察释放,而不是报错收场。
 */
export function isTeardownStall(error: unknown): boolean {
  if (playbackFailureStage(error) === "teardown") return true;
  const status = playbackFailureStatus(error);
  // 网关类失败同样表示"平台侧还没收敛",等待释放比立刻失败更接近真相。
  return status === 502 || status === 503 || status === 504;
}

/**
 * 会话已从平台消失 = 通道已经释放(可能是后台清扫器收的尾)。
 * 对调用方来说与「停止成功」等价。
 */
export function isSessionReleased(error: unknown): boolean {
  return playbackFailureStatus(error) === 404 || playbackFailureCode(error) === "playback_not_found";
}

/** 通道仍被上一路回放的占位占着,平台不放行第二路。 */
export function isChannelBusy(error: unknown): boolean {
  return playbackFailureStatus(error) === 429 || playbackFailureCode(error) === "playback_busy";
}

/** 等到第 `attempt` 次探测该发出去的时机(attempt 从 0 起,超出预算按最后一个间隔计)。 */
export function waitForNextProbe(attempt: number): Promise<void> {
  const delay = channelReleaseWatchDelays[Math.min(Math.max(attempt, 0), channelReleaseWatchDelays.length - 1)];
  return new Promise(resolve => {
    setTimeout(resolve, delay);
  });
}

/**
 * 按退避间隔反复探测通道是否已释放,预算内探测到即返回 true。
 * 探测函数返回 true 表示「已释放」,由调用方决定用什么请求(会话查询即可)。
 */
export async function waitForChannelRelease(probe: () => Promise<boolean>, budget = channelReleaseWatchBudget): Promise<boolean> {
  for (let attempt = 0; attempt < budget; attempt += 1) {
    await waitForNextProbe(attempt);
    if (await probe()) return true;
  }
  return false;
}
