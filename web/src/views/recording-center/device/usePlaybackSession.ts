import { computed, onUnmounted, ref, watch, type ComputedRef, type Ref } from "vue";
import { Message } from "@arco-design/web-vue";
import type { RecordQueryItem } from "../../gb28181/device-mgmt/api";
import { readableHttpError } from "../../gb28181/device-mgmt/recordQueryState";
import { createPlaybackState, reducePlaybackState, type PlaybackState } from "../../gb28181/device-record-playback/playbackState";
import {
  channelReleaseWatchBudget,
  isChannelBusy,
  isSessionReleased,
  isTeardownStall,
  waitForChannelRelease,
  waitForNextProbe
} from "../../gb28181/device-record-playback/playbackTeardown";
import { positionToTime } from "../../gb28181/device-record-playback/timeline";
import { resolvePlaybackSource, type PlaybackSource } from "../../gb28181/playbackProtocol";
import {
  actionPlaybackSession,
  createPlaybackSession,
  deletePlaybackSession,
  getPlaybackSession,
  type PlaybackActionRequest,
  type PlaybackSession
} from "../../gb28181/device-record-playback/api";

export interface UsePlaybackSessionOptions {
  channelId: Ref<number>;
  /** 当前列表：切段、上一段/下一段、以及「播放头所指位置属于哪一段」都靠它。 */
  records: Ref<RecordQueryItem[]>;
  canPlay: ComputedRef<boolean>;
}

type TeardownOutcome = { state: "released" } | { state: "superseded" } | { state: "failed"; message: string };

/**
 * 设备录像回放会话的生命周期。
 *
 * 这套逻辑是从旧回放页原样搬过来的，每一处"看起来多余"的判断都对应一次线上故障：
 * - 幂等键：连点「播放」不能生成第二个会话；
 * - 429 重试：上一路刚结束、后台清扫器还没释放通道时，平台会回 `playback_busy`，
 *   直接把这个错误摔给用户就是"拖了下时间轴就一直报错"；
 * - 只 DELETE 一次 + 观察会话是否消失：重复 DELETE 推不动任何状态，只会白等一个 SIP 超时；
 * - 播放时钟校准：`timeupdate` 只给"播放了多久"，得自己换算成录像时间轴上的位置。
 *
 * 和旧页的差别只有一处：切段走**队列**，连点只执行最后一次，
 * 避免用户快速点时间轴时把会话拆了又建、建了又拆。
 */
export function usePlaybackSession(options: UsePlaybackSessionOptions) {
  const playback = ref<PlaybackState>(createPlaybackState());
  const mediaUrl = ref("");
  const source = ref<PlaybackSource | null>(null);
  const hasAudio = ref(false);
  const buffering = ref(false);
  const controlPending = ref(false);
  /** 正在等上一路的通道占位被释放 —— 唯一能解释「点了半天没动静」的文案。 */
  const channelReleasePending = ref(false);

  let sessionToken = 0;
  let pollTimer: number | null = null;
  let playerClock: { sessionId: string | null; sourceTimestamp: number | null; recordTimestamp: number | null } = {
    sessionId: null,
    sourceTimestamp: null,
    recordTimestamp: null
  };

  const selectedRecord = computed(() => options.records.value.find(item => item.recordKey === playback.value.recordKey) || null);
  const isPlaying = computed(() => playback.value.status === "playing");
  const isLoading = computed(() => ["creating", "buffering", "stopping"].includes(playback.value.status));

  const statusText = computed(() => {
    if (channelReleasePending.value) return "正在等待通道释放";
    const map: Record<string, string> = {
      unselected: "请选择一个时段",
      selected: "已选择，等待播放",
      creating: "正在创建回放会话",
      buffering: "设备响应，正在缓冲",
      playing: "正在播放",
      paused: "已暂停",
      ended: "本段播放结束",
      failed: "播放失败",
      stopping: "正在停止",
      stopped: "已停止"
    };
    return map[playback.value.status] || "";
  });

  function clearTimers() {
    if (pollTimer !== null) window.clearTimeout(pollTimer);
    pollTimer = null;
  }

  function resetPlayerClock() {
    playerClock = { sessionId: null, sourceTimestamp: null, recordTimestamp: null };
  }

  function errorMessage(error: unknown) {
    return readableHttpError(error, "设备录像播放失败");
  }

  function recordIdentity(record: Pick<RecordQueryItem, "startTime" | "endTime">) {
    return `${record.startTime || ""}|${record.endTime || ""}`;
  }

  function idempotencyKey(record: RecordQueryItem) {
    return `playback-${encodeURIComponent(`${options.channelId.value}|${recordIdentity(record)}`)}`;
  }

  function sessionTime(session: PlaybackSession) {
    const durationSeconds = Math.max(0, (Date.parse(session.segmentEnd) - Date.parse(session.segmentStart)) / 1000);
    return positionToTime(
      { startTime: session.segmentStart, endTime: session.segmentEnd },
      session.positionSeconds,
      durationSeconds
    );
  }

  function schedulePoll(sessionId: string, token: number) {
    if (pollTimer !== null) window.clearTimeout(pollTimer);
    pollTimer = window.setTimeout(async () => {
      try {
        // 会话被后台收尾后这里会 404「回放会话不存在」—— 那是正常结束，不弹全局错误。
        const response = await getPlaybackSession(options.channelId.value, sessionId, { showErrorMessage: false });
        if (token !== sessionToken) return;
        applySession(response.data, token);
      } catch (error) {
        if (token !== sessionToken) return;
        playback.value = { ...playback.value, status: "failed", error: errorMessage(error) };
        mediaUrl.value = "";
        source.value = null;
        clearTimers();
      }
    }, 800);
  }

  function applySession(session: PlaybackSession, token: number, syncPosition = false) {
    if (token !== sessionToken) return;
    const previousSessionId = playback.value.sessionId;
    const shouldSyncPosition = syncPosition || !playback.value.currentTime;
    playback.value = reducePlaybackState(playback.value, {
      type: "session",
      sessionId: session.sessionId,
      status: session.state,
      currentTime: shouldSyncPosition ? sessionTime(session) : undefined,
      message: session.errorCode || undefined
    });
    playback.value = reducePlaybackState(playback.value, { type: "scale", scale: session.scale || 1 });
    if (previousSessionId !== session.sessionId || shouldSyncPosition) resetPlayerClock();
    hasAudio.value = session.hasAudio;
    if (previousSessionId !== session.sessionId || !source.value) {
      source.value = resolvePlaybackSource(
        {
          defaultProtocol: session.media?.defaultProtocol,
          protocol: session.media?.protocol,
          url: session.media?.url,
          zlmWebrtc: session.media?.zlmWebrtc,
          urls: session.media?.urls
        },
        window.location.protocol === "https:"
      );
    }
    mediaUrl.value = source.value?.url || "";
    buffering.value = false;

    if (["ended", "failed", "stopped"].includes(session.state)) {
      clearTimers();
      mediaUrl.value = "";
      source.value = null;
      resetPlayerClock();
      return;
    }
    schedulePoll(session.sessionId, token);
  }

  async function createAfterChannelRelease(record: RecordQueryItem, token: number) {
    for (let attempt = 0; ; attempt += 1) {
      try {
        return await createPlaybackSession(
          options.channelId.value,
          { recordKey: record.recordKey, playFrom: playback.value.currentTime || record.startTime || "" },
          idempotencyKey(record),
          { showErrorMessage: false }
        );
      } catch (error) {
        if (token !== sessionToken || !isChannelBusy(error) || attempt >= channelReleaseWatchBudget) throw error;
        channelReleasePending.value = true;
        await waitForNextProbe(attempt);
        if (token !== sessionToken) throw error;
      }
    }
  }

  async function sendAction(action: PlaybackActionRequest, syncPosition = false) {
    const sessionId = playback.value.sessionId;
    if (!options.canPlay.value || !sessionId || controlPending.value) return;
    const token = sessionToken;
    controlPending.value = true;
    try {
      const response = await actionPlaybackSession(options.channelId.value, sessionId, action);
      applySession(response.data, token, syncPosition);
    } catch (error) {
      if (token === sessionToken) playback.value = { ...playback.value, error: errorMessage(error) };
    } finally {
      controlPending.value = false;
    }
  }

  /** 收尾一路会话，并明确回答「通道到底释放了没有」。 */
  async function teardown(sessionId: string, token: number): Promise<TeardownOutcome> {
    try {
      await deletePlaybackSession(options.channelId.value, sessionId, { showErrorMessage: false });
      return { state: "released" };
    } catch (error) {
      if (token !== sessionToken) return { state: "superseded" };
      if (isSessionReleased(error)) return { state: "released" };
      if (!isTeardownStall(error)) return { state: "failed", message: errorMessage(error) };
    }
    channelReleasePending.value = true;
    try {
      const released = await waitForChannelRelease(async () => {
        try {
          await getPlaybackSession(options.channelId.value, sessionId, { showErrorMessage: false });
          return false;
        } catch (error) {
          return isSessionReleased(error);
        }
      });
      if (token !== sessionToken) return { state: "superseded" };
      return released ? { state: "released" } : { state: "failed", message: "设备未确认拆除，通道仍在后台释放中，请稍后重试" };
    } finally {
      if (token === sessionToken) channelReleasePending.value = false;
    }
  }

  async function stop(keepSelection = true, enforcePermission = true) {
    if (enforcePermission && !options.canPlay.value) return false;
    const sessionId = playback.value.sessionId;
    const selectedKey = playback.value.recordKey;
    const token = ++sessionToken;
    clearTimers();
    resetPlayerClock();
    buffering.value = false;
    mediaUrl.value = "";
    source.value = null;
    if (sessionId) {
      playback.value = reducePlaybackState(playback.value, { type: "stopping" });
      const outcome = await teardown(sessionId, token);
      if (outcome.state === "superseded") return false;
      if (outcome.state === "failed") {
        playback.value = { ...playback.value, status: "failed", error: outcome.message };
        Message.error(outcome.message);
        return false;
      }
    }
    if (token !== sessionToken) return false;
    playback.value =
      keepSelection && selectedKey ? reducePlaybackState(playback.value, { type: "stopped" }) : createPlaybackState();
    return true;
  }

  async function start() {
    if (!selectedRecord.value || !options.canPlay.value) return;
    if (playback.value.status === "paused" && playback.value.sessionId) {
      await sendAction({ action: "resume" });
      return;
    }
    if (isLoading.value) return;
    clearTimers();
    mediaUrl.value = "";
    source.value = null;
    playback.value = reducePlaybackState(playback.value, { type: "creating" });
    const token = ++sessionToken;
    const record = selectedRecord.value;
    try {
      const response = await createAfterChannelRelease(record, token);
      if (token !== sessionToken) return;
      applySession(response.data, token);
    } catch (error) {
      if (token !== sessionToken) return;
      const message = errorMessage(error);
      playback.value = { ...playback.value, status: "failed", error: message };
      mediaUrl.value = "";
      source.value = null;
      // 重试期间的中间失败已经被抑制，真正失败时统一提示一次。
      Message.error(isChannelBusy(error) ? "当前通道已有回放会话，请稍后重试" : message);
    } finally {
      if (token === sessionToken) channelReleasePending.value = false;
    }
  }

  async function pause() {
    await sendAction({ action: "pause" });
  }

  async function toggle() {
    if (isPlaying.value) await pause();
    else await start();
  }

  async function setScale(scale: number) {
    if (!options.canPlay.value) return;
    if (playback.value.sessionId && ["playing", "paused"].includes(playback.value.status)) {
      await sendAction({ action: "scale", scale });
      return;
    }
    playback.value = reducePlaybackState(playback.value, { type: "scale", scale });
  }

  /** 选中某一段（不自动开播）：切换段时先保证上一路被拆干净。 */
  async function select(record: RecordQueryItem) {
    if (!options.canPlay.value) return false;
    if (playback.value.recordKey === record.recordKey) return true;
    // ⛔ 判据不能只看 status：停止失败时会停在 failed，但会话 id 还留着 ——
    //    那种情况下必须先重新拆干净，否则新建会话只会拿到 429。
    if (playback.value.sessionId || ["creating", "buffering", "playing", "paused"].includes(playback.value.status)) {
      if (!(await stop(false, false))) return false;
    }
    playback.value = reducePlaybackState(playback.value, { type: "select", recordKey: record.recordKey });
    return true;
  }

  // ── 连点合并 ────────────────────────────────────────────────────────────────
  // 用户快速点时间轴 / 点上一段下一段时，只执行最后一次意图：
  // 前几次请求全部作废，避免「拆了又建、建了又拆」把通道折腾到 429。
  let queue: RecordQueryItem | null = null;
  let draining = false;

  async function play(record: RecordQueryItem) {
    queue = record;
    if (draining) return;
    draining = true;
    await Promise.resolve();
    try {
      while (queue) {
        const next = queue;
        queue = null;
        if (!(await select(next))) continue;
        await start();
      }
    } finally {
      draining = false;
    }
  }

  /**
   * 时间轴定位：找到该时刻所属的录像段，必要时切段后从该位置开播。
   * 返回 false 表示这个时刻没有录像（由页面去提示「该时段无录像」）。
   */
  async function seekTo(time: string) {
    if (!options.canPlay.value) return false;
    const record = options.records.value.find(item => {
      const start = Date.parse(item.startTime || "");
      const end = Date.parse(item.endTime || "");
      const value = Date.parse(time);
      return Number.isFinite(start) && Number.isFinite(end) && Number.isFinite(value) && value >= start && value < end;
    });
    if (!record) return false;
    if (playback.value.recordKey !== record.recordKey) {
      if (!(await select(record))) return false;
    }
    playback.value = { ...playback.value, currentTime: time };
    if (!playback.value.sessionId) {
      await start();
      return true;
    }
    if (!record.startTime || !record.endTime) return true;
    const duration = Math.max(0, (Date.parse(record.endTime) - Date.parse(record.startTime)) / 1000);
    const positionSeconds = Math.min(
      Math.max(0, duration - 0.001),
      Math.max(0, (Date.parse(time) - Date.parse(record.startTime)) / 1000)
    );
    await sendAction({ action: "seek", positionSeconds }, true);
    return true;
  }

  /** 按列表顺序切到相邻段；到头了就停在原地，不循环。 */
  async function step(direction: -1 | 1) {
    const list = options.records.value;
    if (!list.length) return;
    const currentKey = playback.value.recordKey;
    const index = currentKey ? list.findIndex(item => item.recordKey === currentKey) : -1;
    const nextIndex = index < 0 ? (direction > 0 ? 0 : list.length - 1) : index + direction;
    if (nextIndex < 0 || nextIndex >= list.length) return;
    const target = list[nextIndex];
    const startTime = target.startTime;
    if (startTime) {
      await seekTo(startTime);
      return;
    }
    await play(target);
  }

  function handlePlayerLoading(value: boolean) {
    buffering.value = value;
  }

  function handlePlayerError(message: string) {
    playback.value = { ...playback.value, status: "failed", error: message };
  }

  /**
   * 把播放器的 `timeupdate` 换算成录像时间轴上的绝对时间。
   * `timeupdate` 给的是「本次播放已经过了多少秒」，重启播放后会从 0 重新算，
   * 所以要在会话切换或时间戳回退时重新锚定基准。
   */
  function handlePlayerTimeUpdate(timestamp: number) {
    if (buffering.value) return;
    const record = selectedRecord.value;
    const currentTime = playback.value.currentTime;
    const start = Date.parse(record?.startTime || "");
    const end = Date.parse(record?.endTime || "");
    const current = Date.parse(currentTime || "");
    if (!record || !currentTime || !Number.isFinite(start) || !Number.isFinite(end) || !Number.isFinite(current)) return;

    if (
      playerClock.sessionId !== playback.value.sessionId ||
      playerClock.sourceTimestamp === null ||
      playerClock.recordTimestamp === null ||
      timestamp < playerClock.sourceTimestamp
    ) {
      playerClock = { sessionId: playback.value.sessionId, sourceTimestamp: timestamp, recordTimestamp: current };
      return;
    }

    const duration = end - start;
    const elapsed = Math.min(
      duration,
      Math.max(0, playerClock.recordTimestamp - start + timestamp - playerClock.sourceTimestamp)
    );
    playback.value = {
      ...playback.value,
      currentTime: positionToTime({ startTime: record.startTime!, endTime: record.endTime! }, elapsed, duration)
    };
  }

  // 列表换了（翻到另一天）而旧会话还在播：那一段已经不在当前时间轴上，必须停掉。
  watch(
    options.records,
    list => {
      if (!playback.value.sessionId) return;
      if (list.some(item => item.recordKey === playback.value.recordKey)) return;
      void stop(false, false);
    },
    { flush: "post" }
  );

  onUnmounted(() => {
    const sessionId = playback.value.sessionId;
    ++sessionToken;
    clearTimers();
    // 离开页面时的收尾属于后台行为，失败不该再弹提示。
    if (sessionId) {
      void deletePlaybackSession(options.channelId.value, sessionId, { showErrorMessage: false }).catch(() => undefined);
    }
  });

  return {
    playback,
    mediaUrl,
    source,
    hasAudio,
    buffering,
    controlPending,
    channelReleasePending,
    selectedRecord,
    isPlaying,
    isLoading,
    statusText,
    play,
    start,
    pause,
    toggle,
    stop,
    select,
    setScale,
    seekTo,
    step,
    handlePlayerLoading,
    handlePlayerError,
    handlePlayerTimeUpdate
  };
}
