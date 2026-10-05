<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import {
  ArrowLeft,
  CalendarRange,
  CircleAlert,
  Clock3,
  Download,
  Fullscreen,
  LoaderCircle,
  Pause,
  Play,
  RotateCcw,
  Search,
  Square,
  Video,
  Videotape
} from "@lucide/vue";
import {
  getRecordQueryOptions,
  queryDeviceRecords,
  type RecordQueryItem,
  type RecordQueryOptions,
  type RecordQueryResult
} from "../device-mgmt/api";
import {
  createDefaultRecordQueryForm,
  mapRecordQueryError,
  recordQueryRequestTypes,
  recordQueryTypeText,
  serializeRecordQueryForm,
  validateRecordQueryForm,
  type RecordQueryForm,
  type RecordQueryUiState
} from "../device-mgmt/recordQueryState";
import { createPlaybackState, reducePlaybackState } from "./playbackState";
import {
  channelReleaseWatchBudget,
  isChannelBusy,
  isSessionReleased,
  isTeardownStall,
  waitForChannelRelease,
  waitForNextProbe
} from "./playbackTeardown";
import { positionToTime } from "./timeline";
import RecordTimeline, { type TimelineLocateEvent } from "./components/RecordTimeline.vue";
import PlayWindow from "../components/PlayWindow.vue";
import { resolvePlaybackSource, type PlaybackSource } from "../playbackProtocol";
import { useUserStoreHook } from "@/store/modules/user";
import { Message } from "@arco-design/web-vue";
import { createRecordCacheTask } from "@/api/recordCache";
import RecordCacheProgressDialog from "./RecordCacheProgressDialog.vue";
import {
  actionPlaybackSession,
  createPlaybackSession,
  deletePlaybackSession,
  getPlaybackSession,
  type PlaybackActionRequest,
  type PlaybackSession
} from "./api";

const route = useRoute();
const router = useRouter();
const userStore = useUserStoreHook();
const channelId = computed(() => Number(route.params.channelId || 31));
const playbackChannelId = channelId.value;
const options = ref<RecordQueryOptions | null>(null);
const form = ref<RecordQueryForm | null>(null);
const queryState = ref<RecordQueryUiState>("idle");
const queryMessage = ref("");
const result = ref<RecordQueryResult | null>(null);
const records = ref<RecordQueryItem[]>([]);
const playback = ref(createPlaybackState());
const playbackMediaUrl = ref("");
const playbackSource = ref<PlaybackSource | null>(null);
const playbackHasAudio = ref(false);
const playbackBuffering = ref(false);
const controlPending = ref(false);
// 通道释放观察中:上一路回放的占位还没被后台清扫器释放,正在退避等待。
// 它是唯一能解释「为什么点了半天还没动静」的状态文案。
const channelReleasePending = ref(false);
const downloadPending = ref(false);
const downloadNotice = ref("");
// 提交成功后弹出的实时进度弹窗。缓存跑在服务端，弹窗只是"看得见"，
// 关掉它（转后台）任务照常推进。
const cacheDialogVisible = ref(false);
const cacheTaskId = ref("");
const cacheTaskName = ref("");
const queryToken = ref(0);
const viewport = ref<HTMLElement | null>(null);
let queryAbort: AbortController | null = null;
let sessionToken = 0;
let sessionPollTimer: number | null = null;
let downloadTimer: number | null = null;
let playerClock: { sessionId: string | null; sourceTimestamp: number | null; recordTimestamp: number | null } = {
  sessionId: null,
  sourceTimestamp: null,
  recordTimestamp: null
};

const selectedRecord = computed(() => records.value.find(item => item.recordKey === playback.value.recordKey) || null);
const queryRange = computed(() => ({
  startTime: form.value?.startTime || options.value?.serverNow || "",
  endTime: form.value?.endTime || options.value?.serverNow || ""
}));
// a-range-picker 的 v-model 是一个二元组,而表单/序列化仍然按 startTime、endTime
// 两个字段走(与设备管理里的录像查询抽屉同一套桥接)。:allow-clear="false" 保证
// 清空按钮不存在,所以 setter 不会收到 null。
// ⛔ get/set 的返回与参数类型必须写出来:否则数组字面量被推成 string[] 而不是元组,
//    报「Target requires 2 element(s) but source may have fewer」。
const timeRange = computed<[string, string]>({
  get: (): [string, string] => [form.value?.startTime || "", form.value?.endTime || ""],
  set: (value: [string, string]) => {
    if (!form.value) return;
    form.value.startTime = value?.[0] || "";
    form.value.endTime = value?.[1] || "";
  }
});
const isPlaying = computed(() => playback.value.status === "playing");
const isPlaybackLoading = computed(() => ["creating", "buffering", "stopping"].includes(playback.value.status));
const hasPermission = (permission: string) =>
  userStore.account.permissions.includes("*:*:*") || userStore.account.permissions.includes(permission);
const canQuery = computed(() => hasPermission("gb28181:device-record:query"));
const canPlayPermission = computed(() => hasPermission("gb28181:device-record:play"));
const canDownload = computed(() => hasPermission("gb28181:device-record:download"));
const canStartPlayback = computed(
  () =>
    canPlayPermission.value &&
    Boolean(selectedRecord.value) &&
    !controlPending.value &&
    !["creating", "buffering", "stopping"].includes(playback.value.status)
);
const playbackScaleOptions = [0.25, 0.5, 1, 2, 4];
// 倍速下拉的 v-model 桥。a-select 的 :value 绑的是数字,但组件内部回传的值
// 在单测桩里是字符串(setValue 走原生 select),统一 Number() 归一后再交给 setScale,
// setScale 只接受数字。
const playbackScale = computed<number>({
  get: () => playback.value.scale,
  set: value => {
    void setScale(Number(value));
  }
});
const statusText = computed(() => {
  // 等待通道释放是跨「停止」与「创建」两个阶段的同一件事,单独一句文案说清楚,
  // 否则用户只会看到「正在停止」卡住不动。
  if (channelReleasePending.value) return "正在等待通道释放";
  return (
    {
      unselected: "请选择录像段",
      selected: "已选择，等待播放",
      creating: "正在创建回放会话",
      buffering: "设备响应，正在缓冲",
      playing: "正在回放",
      paused: "已暂停",
      ended: "回放结束",
      failed: "回放失败",
      stopping: "正在停止",
      stopped: "已停止"
    } as Record<string, string>
  )[playback.value.status];
});
const querySummary = computed(() => {
  if (queryState.value === "querying") return "正在向设备查询录像目录";
  if (queryState.value === "complete") return `查询完成，共 ${records.value.length} 段`;
  if (queryState.value === "partial") return `已收到 ${records.value.length} 段，结果可能不完整`;
  if (queryState.value === "empty") return "所选时段没有设备录像";
  return queryMessage.value || "设置条件后查询设备录像";
});

function localDateTime(value: string | null | undefined) {
  if (!value) return "--";
  return value.replace("T", " ").replace(/([+-]\d{2}:\d{2}|Z)$/, "");
}

function shortTime(value: string | null | undefined) {
  return value ? localDateTime(value).slice(11, 19) : "--:--:--";
}

function durationText(record: RecordQueryItem) {
  const seconds = Math.max(0, Math.floor((Date.parse(record.endTime || "") - Date.parse(record.startTime || "")) / 1000));
  if (!Number.isFinite(seconds)) return "--";
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  const remain = seconds % 60;
  return [hours, minutes, remain].map(value => String(value).padStart(2, "0")).join(":");
}

function fileSizeText(value: number) {
  if (value >= 1024 ** 3) return `${(value / 1024 ** 3).toFixed(2)} GB`;
  if (value >= 1024 ** 2) return `${(value / 1024 ** 2).toFixed(1)} MB`;
  if (value >= 1024) return `${Math.round(value / 1024)} KB`;
  return `${value} B`;
}

function defaultRecord(records: RecordQueryItem[]) {
  return (
    records.find(record => {
      const duration = Date.parse(record.endTime || "") - Date.parse(record.startTime || "");
      return Number.isFinite(duration) && duration > 1000;
    }) || records[0]
  );
}

function recordIdentity(record: Pick<RecordQueryItem, "startTime" | "endTime">) {
  return `${record.startTime || ""}|${record.endTime || ""}`;
}

function readLastRecordIdentity() {
  if (typeof window === "undefined") return "";
  return window.sessionStorage.getItem(`uvp:gb28181:playback:${playbackChannelId}`) || "";
}

function saveLastRecordIdentity(record: RecordQueryItem) {
  if (typeof window === "undefined") return;
  window.sessionStorage.setItem(`uvp:gb28181:playback:${playbackChannelId}`, recordIdentity(record));
}

async function loadOptions() {
  if (!canQuery.value) return;
  try {
    const response = await getRecordQueryOptions(playbackChannelId);
    options.value = response.data;
    form.value = createDefaultRecordQueryForm(response.data);
    await nextTick();
    await runQuery();
  } catch (error) {
    queryState.value = "error";
    queryMessage.value = (error as Error)?.message || "无法加载通道录像查询配置";
  }
}

async function runQuery() {
  if (!canQuery.value || !form.value || !options.value) return;
  const errors = validateRecordQueryForm(form.value, options.value);
  const firstError = Object.values(errors)[0];
  if (firstError) {
    queryState.value = "error";
    queryMessage.value = firstError;
    return;
  }
  await stopPlayback(false, false);
  queryAbort?.abort();
  const token = ++queryToken.value;
  queryAbort = new AbortController();
  queryState.value = "querying";
  queryMessage.value = "";
  try {
    const response = await queryDeviceRecords(playbackChannelId, serializeRecordQueryForm(form.value), queryAbort.signal);
    if (token !== queryToken.value) return;
    result.value = response.data;
    records.value = response.data.list;
    queryState.value = response.data.status;
    const lastIdentity = readLastRecordIdentity();
    const record = records.value.find(item => recordIdentity(item) === lastIdentity) || defaultRecord(records.value);
    if (record && canPlayPermission.value) await playRecord(record);
  } catch (error) {
    if ((error as Error)?.name === "AbortError" || token !== queryToken.value) return;
    const mapped = mapRecordQueryError(error);
    queryState.value = mapped.state;
    queryMessage.value = mapped.message;
    records.value = [];
    result.value = null;
    playback.value = createPlaybackState();
  }
}

async function selectRecord(record: RecordQueryItem) {
  if (!canPlayPermission.value) return false;
  if (playback.value.recordKey === record.recordKey) return true;
  // ⛔ 判据不能只看 status:停止失败时会停在 "failed",但会话 id 还留着 ——
  // 那种情况下必须先重新把上一路拆干净,否则新建会话只会拿到 429
  // 「当前通道已有回放会话」,用户看到的是"拖动之后一直失败"。
  if (playback.value.sessionId || ["creating", "buffering", "playing", "paused"].includes(playback.value.status)) {
    if (!(await stopPlayback(false, false))) return false;
  }
  playback.value = reducePlaybackState(playback.value, { type: "select", recordKey: record.recordKey });
  return true;
}

async function playRecord(record: RecordQueryItem) {
  if (!canPlayPermission.value) return;
  if (playback.value.recordKey === record.recordKey && ["creating", "buffering", "playing"].includes(playback.value.status))
    return;
  if (!(await selectRecord(record))) return;
  await startPlayback();
}

function clearPlaybackTimers() {
  if (sessionPollTimer !== null) window.clearTimeout(sessionPollTimer);
  sessionPollTimer = null;
}

/**
 * 把选中的录像段交给**服务端后台缓存**。
 *
 * 这里不再把设备推上来的直播流 fetch 成 blob：那条路要等整段流播完才拿到内容，
 * 没有进度也不能取消，而且下载模式单会话 30 分钟，长录像必然被掐断。
 * 现在只提交一个任务，拉流/落盘都在服务端跑，用户去「录像缓存」看进度、
 * 完成后从服务器下载静态文件。
 */
async function queueDownload(record: RecordQueryItem | null = selectedRecord.value) {
  if (!canDownload.value || !record || !record.startTime || downloadPending.value) return;
  if (downloadTimer !== null) window.clearTimeout(downloadTimer);
  downloadPending.value = true;
  downloadNotice.value = "";
  try {
    const response = await createRecordCacheTask({
      channelId: playbackChannelId,
      recordKey: record.recordKey,
      playFrom: record.startTime
    });
    const created = response?.data;
    if (created?.taskId) {
      // 受理成功就直接弹实时进度：用户能立刻确认"真的在跑"（有速率），
      // 而不是只收到一句看不见进度的提示。弹窗可随时转后台。
      cacheTaskId.value = created.taskId;
      cacheTaskName.value = record.name || "未命名录像";
      cacheDialogVisible.value = true;
    } else {
      downloadNotice.value = `已加入缓存队列 · ${record.name || "未命名录像"}，可在「录像缓存」查看进度`;
    }
  } catch (error) {
    downloadNotice.value = playbackErrorMessage(error).replace("设备录像回放", "录像缓存");
  } finally {
    downloadPending.value = false;
    if (downloadNotice.value) {
      downloadTimer = window.setTimeout(() => {
        downloadNotice.value = "";
        downloadTimer = null;
      }, 4000);
    }
  }
}

/** 弹窗的「查看缓存任务」出口：缓存任务在服务端继续跑，这里把用户送到任务列表。 */
function goRecordCachePage() {
  void router.push("/gb28181/record-cache");
}

function createIdempotencyKey(record: RecordQueryItem) {
  return `playback-${encodeURIComponent(`${playbackChannelId}|${recordIdentity(record)}`)}`;
}

function playbackErrorMessage(error: unknown) {
  const value = error as { message?: string; response?: { data?: { message?: string } } };
  return value?.response?.data?.message || value?.message || "设备录像回放失败";
}

function sessionTime(session: PlaybackSession) {
  const durationSeconds = Math.max(0, (Date.parse(session.segmentEnd) - Date.parse(session.segmentStart)) / 1000);
  return positionToTime(
    { startTime: session.segmentStart, endTime: session.segmentEnd },
    session.positionSeconds,
    durationSeconds
  );
}

function resetPlayerClock() {
  playerClock = { sessionId: null, sourceTimestamp: null, recordTimestamp: null };
}

function handlePlayerLoading(value: boolean) {
  playbackBuffering.value = value;
}

function handlePlayerTimeUpdate(timestamp: number) {
  if (playbackBuffering.value) return;
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
    playerClock = {
      sessionId: playback.value.sessionId,
      sourceTimestamp: timestamp,
      recordTimestamp: current
    };
    return;
  }

  const duration = end - start;
  const elapsed = Math.min(duration, Math.max(0, playerClock.recordTimestamp - start + timestamp - playerClock.sourceTimestamp));
  playback.value = {
    ...playback.value,
    currentTime: positionToTime({ startTime: record.startTime!, endTime: record.endTime! }, elapsed, duration)
  };
}

function scheduleSessionPoll(sessionId: string, token: number) {
  if (sessionPollTimer !== null) window.clearTimeout(sessionPollTimer);
  sessionPollTimer = window.setTimeout(async () => {
    try {
      // 会话被后台收尾后这里会拿到 404「回放会话不存在」——那是正常收流,
      // 不该弹全局错误提示,状态由下面的 catch 落到「回放失败」即可。
      const response = await getPlaybackSession(playbackChannelId, sessionId, { showErrorMessage: false });
      if (token !== sessionToken) return;
      applySession(response.data, token);
    } catch (error) {
      if (token !== sessionToken) return;
      playback.value = { ...playback.value, status: "failed", error: playbackErrorMessage(error) };
      playbackMediaUrl.value = "";
      playbackSource.value = null;
      clearPlaybackTimers();
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
  playbackHasAudio.value = session.hasAudio;
  if (previousSessionId !== session.sessionId || !playbackSource.value) {
    playbackSource.value = resolvePlaybackSource(
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
  playbackMediaUrl.value = playbackSource.value?.url || "";
  playbackBuffering.value = false;

  if (["ended", "failed", "stopped"].includes(session.state)) {
    if (sessionPollTimer !== null) window.clearTimeout(sessionPollTimer);
    sessionPollTimer = null;
    playbackMediaUrl.value = "";
    playbackSource.value = null;
    resetPlayerClock();
    return;
  }
  scheduleSessionPoll(session.sessionId, token);
}

// 建会话时通道可能还被上一路的占位占着(上一路刚结束、后台清扫器还没释放),
// 平台此时返回 429 playback_busy。等通道释放后重试,而不是把「当前通道已有
// 回放会话」直接摔给用户 —— 后者正是"拖动时间轴后一直报错"的表象。
//
// 重试沿用同一个幂等键:平台按幂等键去重,不会因为重试生成第二路会话。
async function createSessionAfterChannelRelease(record: RecordQueryItem, token: number) {
  for (let attempt = 0; ; attempt += 1) {
    try {
      return await createPlaybackSession(
        playbackChannelId,
        {
          recordKey: record.recordKey,
          playFrom: playback.value.currentTime || record.startTime || ""
        },
        createIdempotencyKey(record),
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

async function startPlayback() {
  if (!selectedRecord.value || !canStartPlayback.value) return;
  if (playback.value.status === "paused" && playback.value.sessionId) {
    await sendPlaybackAction({ action: "resume" });
    return;
  }
  clearPlaybackTimers();
  playbackMediaUrl.value = "";
  playbackSource.value = null;
  playback.value = reducePlaybackState(playback.value, { type: "creating" });
  const token = ++sessionToken;
  const record = selectedRecord.value;
  saveLastRecordIdentity(record);
  try {
    const response = await createSessionAfterChannelRelease(record, token);
    if (token !== sessionToken) return;
    applySession(response.data, token);
  } catch (error) {
    if (token !== sessionToken) return;
    const message = playbackErrorMessage(error);
    playback.value = { ...playback.value, status: "failed", error: message };
    playbackMediaUrl.value = "";
    playbackSource.value = null;
    // 重试期间的中间失败已被抑制,真正失败时统一提示一次。
    Message.error(isChannelBusy(error) ? "当前通道已有回放会话，请稍后重试" : message);
  } finally {
    if (token === sessionToken) channelReleasePending.value = false;
  }
}

async function sendPlaybackAction(action: PlaybackActionRequest, syncPosition = false) {
  const sessionId = playback.value.sessionId;
  if (!canPlayPermission.value || !sessionId || controlPending.value) return;
  const token = sessionToken;
  controlPending.value = true;
  try {
    const response = await actionPlaybackSession(playbackChannelId, sessionId, action);
    applySession(response.data, token, syncPosition);
  } catch (error) {
    if (token === sessionToken) playback.value = { ...playback.value, error: playbackErrorMessage(error) };
  } finally {
    controlPending.value = false;
  }
}

async function pausePlayback() {
  await sendPlaybackAction({ action: "pause" });
}

type TeardownOutcome = { state: "released" } | { state: "superseded" } | { state: "failed"; message: string };

// 收尾一路回放会话,并明确回答「通道到底释放了没有」。
//
// 只打一次 DELETE:平台在「设备没确认拆除」时会保留通道绑定、交给后台清扫器
// 按退避重试,此时重复 DELETE 推不动任何状态(那条 TEARDOWN 已经发过,不会
// 重发),只会再白等一个 SIP 超时 —— 详见 playbackTeardown.ts 的说明。
// 所以失败后转为「观察会话是否已从平台消失」,释放了就继续用户原本的操作。
async function teardownSession(sessionId: string, token: number): Promise<TeardownOutcome> {
  try {
    await deletePlaybackSession(playbackChannelId, sessionId, { showErrorMessage: false });
    return { state: "released" };
  } catch (error) {
    if (token !== sessionToken) return { state: "superseded" };
    // 会话已经不在平台上 = 通道本来就已经释放(多半是后台清扫器收的尾)。
    if (isSessionReleased(error)) return { state: "released" };
    if (!isTeardownStall(error)) return { state: "failed", message: playbackErrorMessage(error) };
  }
  channelReleasePending.value = true;
  try {
    const released = await waitForChannelRelease(async () => {
      try {
        await getPlaybackSession(playbackChannelId, sessionId, { showErrorMessage: false });
        return false; // 还查得到 = 占位仍在,通道没释放
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

async function stopPlayback(keepSelection = true, enforcePermission = true) {
  if (enforcePermission && !canPlayPermission.value) return false;
  const sessionId = playback.value.sessionId;
  const selectedKey = playback.value.recordKey;
  const token = ++sessionToken;
  clearPlaybackTimers();
  resetPlayerClock();
  playbackBuffering.value = false;
  playbackMediaUrl.value = "";
  playbackSource.value = null;
  if (sessionId) {
    playback.value = reducePlaybackState(playback.value, { type: "stopping" });
    const outcome = await teardownSession(sessionId, token);
    if (outcome.state === "superseded") return false;
    if (outcome.state === "failed") {
      // 通道没释放:保留会话 id 与所选录像段,让用户可以直接重试 ——
      // 下一次选段会先重新走拆除,而不是带着旧占位去建会话撞 429。
      playback.value = { ...playback.value, status: "failed", error: outcome.message };
      Message.error(outcome.message);
      return false;
    }
  }
  if (token !== sessionToken) return false;
  if (keepSelection && selectedKey) {
    playback.value = reducePlaybackState(playback.value, { type: "stopped" });
  } else {
    playback.value = createPlaybackState();
  }
  return true;
}

async function setScale(scale: number) {
  if (!canPlayPermission.value) return;
  if (playback.value.sessionId && ["playing", "paused"].includes(playback.value.status)) {
    await sendPlaybackAction({ action: "scale", scale });
    return;
  }
  playback.value = reducePlaybackState(playback.value, { type: "scale", scale });
}

async function handleTimelineLocate(event: TimelineLocateEvent) {
  if (!canPlayPermission.value) return;
  if (!event.recordKey) return;
  const record = records.value.find(item => item.recordKey === event.recordKey);
  if (!record) return;
  if (playback.value.recordKey !== event.recordKey && !(await selectRecord(record))) return;
  playback.value = { ...playback.value, currentTime: event.time };
  if (!playback.value.sessionId) {
    await startPlayback();
    return;
  }
  if (!record.startTime || !record.endTime) return;
  const duration = Math.max(0, (Date.parse(record.endTime) - Date.parse(record.startTime)) / 1000);
  const positionSeconds = Math.min(
    Math.max(0, duration - 0.001),
    Math.max(0, (Date.parse(event.time) - Date.parse(record.startTime)) / 1000)
  );
  await sendPlaybackAction({ action: "seek", positionSeconds }, true);
}

function handlePlayerError(message: string) {
  playback.value = { ...playback.value, status: "failed", error: message };
}

async function toggleFullscreen() {
  if (!viewport.value) return;
  if (document.fullscreenElement) await document.exitFullscreen();
  else await viewport.value.requestFullscreen?.();
}

function goBack() {
  const registeredRoutes = router.getRoutes();
  const target =
    registeredRoutes.find((item: { name?: string | symbol }) => item.name === "device-mgmt-list") ??
    registeredRoutes.find((item: { name?: string | symbol }) => item.name === "device-mgmt");
  if (!target?.name) {
    router.back();
    return;
  }
  router.push({ name: target.name, query: route.query.returnKey ? { returnKey: String(route.query.returnKey) } : undefined });
}

onMounted(() => {
  loadOptions();
});
onUnmounted(() => {
  queryAbort?.abort();
  const sessionId = playback.value.sessionId;
  ++sessionToken;
  clearPlaybackTimers();
  // 离开页面时的收尾属于后台行为,失败不该再弹提示。
  if (sessionId) void deletePlaybackSession(playbackChannelId, sessionId, { showErrorMessage: false }).catch(() => undefined);
  if (downloadTimer !== null) window.clearTimeout(downloadTimer);
});
</script>

<template>
  <div class="snow-fill record-playback-page">
    <div class="snow-fill-inner uvp-page-shell-flat playback-workspace">
      <div v-if="!canQuery" class="record-playback-permission-state" data-testid="record-playback-permission-denied">
        无权查询设备录像，请联系管理员分配设备录像查询权限。
      </div>
      <template v-else>
        <header class="query-bar" data-testid="playback-query-bar">
          <a-button class="icon-command back-command" aria-label="返回设备管理" title="返回设备管理" @click="goBack">
            <template #icon><ArrowLeft :size="18" /></template>
          </a-button>
          <div class="channel-context">
            <span class="channel-icon"><Video :size="17" /></span>
            <div>
              <strong>{{ options?.channel.name || `通道 ${channelId}` }}</strong>
              <span>{{ options?.device.name || "正在加载设备" }} · {{ options?.channel.code || "--" }}</span>
            </div>
            <i :class="['online-indicator', { offline: options && !options.device.online }]">{{
              options?.device.online === false ? "离线" : "在线"
            }}</i>
          </div>

          <!-- ⛔ 查询栏里不要用 <label> 包 Arco 控件。label 没写 for 时浏览器会把里面
               第一个可标注控件当成它的关联控件,于是把点击**再转发一次**;而 Arco 的下拉
               点击目标是 view 那个 span、关联控件却是另一个隐藏的 readonly input,一次点击
               变成两次开合 ⇒ 面板弹出来立刻就收回去。原生 select 不看重复 click,所以这个
               坑只在换成 Arco 之后才出现。控件自身带 aria-label,语义不依赖 label。 -->
          <div v-if="form" class="query-fields">
            <div class="query-field time-field">
              <span><CalendarRange :size="13" />录像时间</span>
              <a-range-picker
                v-model="timeRange"
                class="time-picker"
                aria-label="录像时间范围"
                show-time
                value-format="YYYY-MM-DDTHH:mm:ss"
                format="YYYY-MM-DD HH:mm:ss"
                :allow-clear="false"
                :disabled="queryState === 'querying'"
              />
            </div>
            <div class="query-field type-field">
              <span>录像类型</span>
              <a-select v-model="form.type" class="type-select" aria-label="录像类型" :disabled="queryState === 'querying'">
                <a-option v-for="item in recordQueryRequestTypes" :key="item.value" :value="item.value">{{
                  item.label
                }}</a-option>
              </a-select>
            </div>
            <a-button
              data-testid="record-query-submit"
              class="query-submit"
              type="primary"
              :loading="queryState === 'querying'"
              :disabled="queryState === 'querying'"
              @click="runQuery"
            >
              <template #icon><Search :size="15" /></template>
              查询录像
            </a-button>
          </div>
          <div v-else class="query-loading"><LoaderCircle :size="16" class="spin" />正在加载查询条件</div>
        </header>

        <main class="playback-main">
          <section class="player-column">
            <div ref="viewport" class="playback-viewport" data-testid="playback-viewport">
              <PlayWindow
                v-if="playbackMediaUrl"
                data-testid="playback-player"
                :data-media-url="playbackMediaUrl"
                :url="playbackMediaUrl"
                :zlm-webrtc="playbackSource?.zlmWebrtc || false"
                :has-audio="playbackHasAudio"
                playback
                @error="handlePlayerError"
                @timeupdate="handlePlayerTimeUpdate"
                @loading="handlePlayerLoading"
              />
              <div v-if="playbackMediaUrl && playbackBuffering" class="playback-buffering" data-testid="playback-buffering">
                <LoaderCircle :size="22" class="spin" aria-hidden="true" />
                <span>画面缓冲中</span>
              </div>
              <div v-if="playbackMediaUrl" class="video-overlay top-overlay">
                <span>CH-01 {{ options?.channel.name || "东门出入口" }}</span>
              </div>
              <div
                v-else
                class="playback-idle-cover"
                data-testid="playback-idle-cover"
                role="img"
                :aria-label="isPlaybackLoading ? statusText : '录像未播放'"
              >
                <div v-if="isPlaybackLoading" class="playback-loading" data-testid="playback-loading">
                  <LoaderCircle :size="42" class="spin" aria-hidden="true" />
                  <span>{{ statusText }}</span>
                </div>
                <CircleAlert v-else-if="playback.status === 'failed'" :size="48" aria-hidden="true" />
                <Videotape v-else :size="64" :stroke-width="1.35" aria-hidden="true" />
              </div>
              <span class="playback-status-sr" data-testid="playback-status" aria-live="polite">{{ statusText }}</span>
              <div v-if="downloadNotice" class="download-notice" data-testid="download-notice">
                <Download :size="14" />{{ downloadNotice }}
              </div>
            </div>

            <div class="playback-controls" data-testid="playback-controls">
              <a-button
                data-testid="playback-primary-action"
                class="control-primary"
                type="primary"
                shape="circle"
                :disabled="!canStartPlayback"
                :aria-label="isPlaying ? '暂停' : '播放'"
                @click="isPlaying ? pausePlayback() : startPlayback()"
              >
                <template #icon>
                  <Pause v-if="isPlaying" :size="17" fill="currentColor" />
                  <Play v-else :size="17" fill="currentColor" />
                </template>
              </a-button>
              <a-button
                class="control-icon"
                aria-label="停止"
                title="停止"
                :disabled="!selectedRecord || !canPlayPermission"
                @click="stopPlayback()"
              >
                <template #icon><Square :size="15" fill="currentColor" /></template>
              </a-button>
              <div class="control-spacer"></div>
              <a-select
                v-model="playbackScale"
                class="scale-select"
                data-testid="playback-scale"
                title="播放倍速"
                :disabled="!canPlayPermission"
              >
                <a-option v-for="scale in playbackScaleOptions" :key="scale" :value="scale">{{ scale }}x</a-option>
              </a-select>
              <a-button
                v-if="canDownload"
                data-testid="playback-download"
                class="control-icon"
                aria-label="缓存到服务器"
                title="缓存到服务器"
                :disabled="!selectedRecord || downloadPending"
                @click="queueDownload()"
              >
                <template #icon>
                  <LoaderCircle v-if="downloadPending" :size="16" class="spin" />
                  <Download v-else :size="16" />
                </template>
              </a-button>
              <a-button class="control-icon" aria-label="全屏" title="全屏" @click="toggleFullscreen">
                <template #icon><Fullscreen :size="16" /></template>
              </a-button>
            </div>
          </section>

          <aside class="segment-panel" data-testid="record-segment-list">
            <div class="segment-header">
              <div>
                <strong>录像段</strong><span>{{ querySummary }}</span>
              </div>
              <a-button
                class="icon-command"
                aria-label="重新查询"
                title="重新查询"
                :disabled="queryState === 'querying'"
                @click="runQuery"
              >
                <template #icon><RotateCcw :size="14" /></template>
              </a-button>
            </div>
            <div v-if="records.length" class="segment-list">
              <div
                v-for="(record, index) in records"
                :key="record.recordKey"
                :class="['segment-row', { selected: playback.recordKey === record.recordKey }]"
              >
                <button
                  :data-testid="`record-segment-${index}`"
                  :class="['segment-item', { selected: playback.recordKey === record.recordKey }]"
                  :aria-current="playback.recordKey === record.recordKey ? 'true' : undefined"
                  :aria-disabled="!canPlayPermission ? 'true' : undefined"
                  :disabled="!canPlayPermission"
                  type="button"
                  @click="playRecord(record)"
                >
                  <span class="segment-content">
                    <strong>{{ record.name || `录像段 ${index + 1}` }}</strong>
                    <span class="segment-time"
                      ><Clock3 :size="12" />{{ shortTime(record.startTime) }} - {{ shortTime(record.endTime) }}</span
                    >
                    <span class="segment-meta">
                      <i class="segment-type"
                        ><span class="segment-marker" :class="record.type || 'unknown'"></span
                        >{{ recordQueryTypeText(record.type) }}</i
                      >
                      <i>{{ durationText(record) }}</i>
                      <i v-if="record.fileSize != null && record.fileSize >= 0" :data-testid="`record-segment-size-${index}`">{{
                        fileSizeText(record.fileSize)
                      }}</i>
                    </span>
                  </span>
                </button>
                <a-button
                  v-if="canDownload"
                  :data-testid="`record-segment-download-${index}`"
                  class="icon-command segment-download"
                  type="text"
                  :aria-label="`缓存 ${record.name || `录像段 ${index + 1}`} 到服务器`"
                  :title="`缓存 ${record.name || `录像段 ${index + 1}`} 到服务器`"
                  :disabled="downloadPending"
                  @click="queueDownload(record)"
                >
                  <template #icon><Download :size="15" /></template>
                </a-button>
              </div>
            </div>
            <div v-else class="segment-empty">
              <LoaderCircle v-if="queryState === 'querying'" :size="26" class="spin" />
              <Video v-else :size="28" />
              <strong>{{ querySummary }}</strong>
              <span v-if="['timeout', 'offline', 'error'].includes(queryState)">{{ queryMessage }}</span>
            </div>
            <div v-if="result" class="segment-footer">
              <span>{{ result.receivedCount }} / {{ result.declaredTotal }} 段</span>
              <span>耗时 {{ result.elapsedMs }} ms</span>
            </div>
          </aside>
        </main>

        <RecordTimeline
          class="timeline-panel"
          :range="queryRange"
          :records="records"
          :selected-record-key="playback.recordKey"
          :current-time="playback.currentTime"
          @locate="handleTimelineLocate"
        />
      </template>
    </div>

    <RecordCacheProgressDialog
      v-model:visible="cacheDialogVisible"
      :task-id="cacheTaskId"
      :task-name="cacheTaskName"
      @navigate="goRecordCachePage"
    />
  </div>
</template>

<style scoped>
.record-playback-page {
  height: 100%;
  min-height: 0;
  overflow: hidden;
  color: var(--uvp-text-primary);
}
.playback-workspace {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  padding: 0;
  overflow: hidden;
}
.record-playback-permission-state {
  padding: 18px;
  margin: 12px;
  color: var(--uvp-text-secondary);
  background: var(--uvp-search-panel-bg);
  border: 1px solid var(--uvp-list-panel-border);
  border-radius: var(--uvp-panel-radius);
}
.query-bar {
  display: flex;
  flex: none;
  gap: 14px;
  align-items: center;
  min-height: 72px;
  padding: 10px 16px;
  margin: 0 8px 10px;
  background: var(--uvp-search-panel-bg);
  border: 1px solid var(--uvp-list-panel-border);
  border-radius: var(--uvp-panel-radius);
  box-shadow: var(--uvp-search-panel-shadow);
}

/* 图标按钮统一 32×32 的方格尺寸。Arco 默认按钮是靠 padding 撑开的,固定成方格
   时必须同时清掉 padding,否则 16px 的图标会被挤出按钮。底色/描边/hover 归 Arco。 */
.icon-command,
.control-icon {
  width: 32px;
  height: 32px;
  padding: 0;
}
.channel-context {
  display: flex;
  gap: 9px;
  align-items: center;
  min-width: 260px;
}
.channel-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 34px;
  height: 34px;
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
  border-radius: 6px;
}
.channel-context div {
  display: flex;
  flex-direction: column;
  min-width: 0;
}
.channel-context strong {
  font-size: 14px;
  line-height: 20px;
}
.channel-context span:not(.channel-icon) {
  font-size: 11px;
  line-height: 17px;
  color: var(--uvp-text-tertiary);
  white-space: nowrap;
}
.online-indicator {
  padding: 2px 7px;
  margin-left: auto;
  font-size: 11px;
  font-style: normal;
  color: #168456;
  background: rgb(22 132 86 / 10%);
  border-radius: 4px;
}
.online-indicator.offline {
  color: var(--uvp-danger);
  background: var(--uvp-danger-soft);
}
.query-fields {
  display: flex;
  gap: 8px;
  align-items: flex-end;
  min-width: 0;
  margin-left: auto;
}
.query-field {
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}
.query-field > span:first-child {
  display: flex;
  gap: 4px;
  align-items: center;
}

/* 时间选择器与录像类型下拉的外观全部交给 Arco,页面只声明它们占多宽。 */

/* ⛔ 作用在 a-select / a-date-picker 上的规则必须用 :deep():scoped 样式不会给
   这两个组件的根元素加父 scopeId,直接写类名编译得过、运行时永远匹配不到。 */
.query-field :deep(.time-picker) {
  width: 360px;
}
.query-field :deep(.type-select) {
  width: 108px;
}
.playback-main {
  display: grid;
  flex: 1;
  grid-template-columns: minmax(0, 1fr) 312px;
  min-height: 0;
  padding: 12px;
  margin: 0 8px;
  overflow: hidden;
  background: var(--uvp-shell-muted);
  border: 1px solid var(--uvp-panel-border);
  border-radius: var(--uvp-panel-radius);
}
.player-column {
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
}
.playback-viewport {
  position: relative;
  flex: 1;
  min-height: clamp(220px, 30vh, 340px);
  overflow: hidden;
  color: #e8eef5;
  background: #000000;
}
.playback-idle-cover {
  position: absolute;
  inset: 0;
  display: grid;
  place-items: center;
  color: #89939d;
  background: #000000;
}
.playback-loading {
  display: flex;
  flex-direction: column;
  gap: 12px;
  align-items: center;
  font-size: 12px;
  color: #c9d1d9;
}
.playback-buffering {
  position: absolute;
  top: 12px;
  left: 50%;
  z-index: 4;
  display: flex;
  gap: 7px;
  align-items: center;
  padding: 6px 9px;
  font-size: 11px;
  color: #ffffff;
  background: rgb(10 15 20 / 76%);
  border: 1px solid rgb(255 255 255 / 18%);
  border-radius: 5px;
  transform: translateX(-50%);
}
.playback-idle-cover .lucide-circle-alert {
  color: #e05d5d;
}
.playback-status-sr {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  overflow: hidden;
  white-space: nowrap;
  border: 0;
  clip: rect(0, 0, 0, 0);
  clip-path: inset(50%);
}
.video-overlay {
  position: absolute;
  right: 14px;
  left: 14px;
  z-index: 2;
  display: flex;
  gap: 12px;
  justify-content: space-between;
  font:
    11px ui-monospace,
    SFMono-Regular,
    Menlo,
    monospace;
  color: rgb(239 246 255 / 86%);
  text-shadow: 0 1px 3px #000000;
}
.top-overlay {
  top: 12px;
}
.download-notice {
  position: absolute;
  top: 42px;
  right: 14px;
  z-index: 5;
  display: flex;
  gap: 6px;
  align-items: center;
  max-width: calc(100% - 28px);
  padding: 7px 10px;
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 11px;
  color: #ffffff;
  white-space: nowrap;
  background: rgb(10 15 20 / 82%);
  border: 1px solid rgb(255 255 255 / 18%);
  border-radius: 4px;
  box-shadow: 0 4px 14px rgb(0 0 0 / 24%);
}
.playback-controls {
  display: flex;
  gap: 7px;
  align-items: center;
  height: 48px;
  padding: 0 10px;
  color: var(--uvp-text-secondary);
  background: var(--uvp-panel-bg);
  border: 1px solid var(--uvp-panel-border);
  border-top: 0;
}
.control-spacer {
  flex: 1;
}

/* 倍速下拉:外观归 Arco,页面只管占宽。a-select 必须走 :deep()。 */
.playback-controls :deep(.scale-select) {
  width: 78px;
}
.segment-panel {
  display: flex;
  flex-direction: column;
  min-height: 0;
  margin-left: 10px;
  background: var(--uvp-list-panel-bg);
  border: 1px solid var(--uvp-list-panel-border);
}
.segment-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  min-height: 53px;
  padding: 8px 10px 8px 12px;
  background: var(--uvp-list-toolbar-bg);
  border-bottom: 1px solid var(--uvp-list-panel-border);
}
.segment-header div {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}
.segment-header strong {
  font-size: 13px;
}
.segment-header span {
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 10px;
  color: var(--uvp-text-tertiary);
  white-space: nowrap;
}
.segment-list {
  flex: 1;
  min-height: 0;
  padding: 4px;
  overflow-y: auto;
}
.segment-row {
  position: relative;
}
.segment-item {
  position: relative;
  display: flex;
  align-items: center;
  width: 100%;
  min-height: 74px;
  padding: 9px 40px 9px 11px;
  color: var(--uvp-text-primary);
  text-align: left;
  cursor: pointer;
  background: var(--uvp-table-row-bg);
  border: 1px solid transparent;
  border-bottom-color: var(--uvp-list-panel-border);
  border-radius: 5px;
  transition:
    background-color 170ms ease,
    border-color 170ms ease,
    box-shadow 170ms ease;
}
.segment-item:hover {
  background: var(--uvp-table-row-hover-bg);
}
.segment-item:focus-visible {
  outline: 2px solid var(--uvp-brand);
  outline-offset: -2px;
  border-color: var(--uvp-brand);
}
.segment-item.selected {
  z-index: 1;
  background: var(--uvp-table-row-checked-bg);
  border-color: var(--uvp-brand);
  box-shadow: 0 2px 8px rgb(29 100 235 / 12%);
}

/* ⛔ 段卡片刻意保留原生 button:它是 74px 高、内嵌三行内容的卡片式列表项,
   不是按钮控件。换成 a-button 得把 Arco 的 height/padding/display 再覆盖回去,
   收益为零而维护成本更高。禁用态因此也要自己兜(全局的 button:disabled 已随
   原生控件一起删除)。 */
.segment-item:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}
.segment-marker {
  flex: none;
  width: 7px;
  height: 7px;
  background: #708090;
  border-radius: 50%;
}
.segment-marker.time {
  background: var(--uvp-brand);
}
.segment-marker.alarm {
  background: var(--uvp-danger);
}
.segment-marker.manual {
  background: var(--uvp-warning);
}
.segment-content {
  display: flex;
  flex: 1;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}
.segment-content strong {
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 12px;
  line-height: 17px;
  white-space: nowrap;
}
.segment-time {
  display: flex;
  gap: 4px;
  align-items: center;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 10px;
  color: var(--uvp-text-secondary);
}
.segment-meta {
  display: flex;
  gap: 10px;
}
.segment-meta i {
  font-size: 10px;
  font-style: normal;
  color: var(--uvp-text-tertiary);
}
.segment-type {
  display: inline-flex;
  gap: 5px;
  align-items: center;
}

/* 段内下载按钮换成 Arco 的 text 按钮后只剩定位(尺寸共用 .icon-command)。
   选中行高亮是本页业务语义,保留;hover/focus 的底色交给 Arco。 */
.segment-download {
  position: absolute;
  top: 21px;
  right: 8px;
  z-index: 2;
}
.segment-row.selected .segment-download {
  color: var(--uvp-brand);
}
.segment-empty {
  display: flex;
  flex: 1;
  flex-direction: column;
  gap: 8px;
  align-items: center;
  justify-content: center;
  padding: 24px;
  color: var(--uvp-text-tertiary);
  text-align: center;
}
.segment-empty strong {
  font-size: 12px;
}
.segment-empty span {
  font-size: 11px;
}
.segment-footer {
  display: flex;
  justify-content: space-between;
  padding: 8px 11px;
  font-size: 10px;
  color: var(--uvp-text-tertiary);
  background: var(--uvp-list-toolbar-bg);
  border-top: 1px solid var(--uvp-list-panel-border);
}
.timeline-panel {
  flex: none;
  margin: 10px 8px 12px;
  overflow: hidden;
  border-radius: var(--uvp-panel-radius);
}
.spin {
  animation: spin 900ms linear infinite;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

@media (width <= 1180px) {
  .query-bar {
    flex-wrap: wrap;
  }
  .channel-context {
    flex: 1;
  }
  .query-fields {
    flex-basis: 100%;
    justify-content: flex-end;
  }
  .playback-main {
    grid-template-columns: minmax(0, 1fr) 280px;
  }
}

@media (width <= 768px) {
  .record-playback-page {
    height: auto;
    min-height: 100%;
    overflow: auto;
  }
  .playback-workspace {
    height: auto;
    min-height: 100%;
    border-radius: 0;
  }
  .query-bar {
    align-items: flex-start;
    padding: 10px 12px;
    margin: 0 8px 8px;
  }
  .channel-context {
    min-width: 0;
  }
  .online-indicator {
    display: none;
  }
  .query-fields {
    display: grid;
    grid-template-columns: 1fr 1fr;
    width: 100%;
  }

  /* 时间范围选择器在窄屏独占一行:两个日期输入挤在半列里会把时间文本截断。 */
  .time-field {
    grid-column: 1 / -1;
  }
  .query-field :deep(.time-picker),
  .query-field :deep(.type-select),
  .query-submit {
    width: 100%;
  }
  .playback-main {
    display: flex;
    flex: none;
    flex-direction: column;
    padding: 8px;
    margin: 0 8px;
  }
  .playback-viewport {
    flex: none;
    min-height: 0;
    aspect-ratio: 16 / 9;
  }
  .segment-panel {
    height: 280px;
    margin: 8px 0 0;
  }
  .timeline-panel {
    margin: 8px;
  }
}

@media (width <= 430px) {
  .back-command {
    width: 36px;
    height: 36px;
  }
  .channel-context span:not(.channel-icon) {
    max-width: 220px;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .query-fields {
    grid-template-columns: 1fr;
  }
  .query-field :deep(.time-picker),
  .query-field :deep(.type-select),
  .query-submit {
    height: 44px;
  }
  .playback-controls {
    height: 52px;
  }
  .control-icon,
  .control-primary {
    width: 36px;
    height: 36px;
  }
  .control-spacer {
    display: none;
  }
  .playback-controls :deep(.scale-select) {
    display: none;
  }
  .segment-panel {
    height: 306px;
  }
}

@media (prefers-reduced-motion: reduce) {
  *,
  *::before,
  *::after {
    scroll-behavior: auto !important;
    transition-duration: 0.01ms !important;
    animation-duration: 0.01ms !important;
    animation-iteration-count: 1 !important;
  }
}
</style>
