<script setup lang="ts">
import { computed, ref, watch, onBeforeUnmount, shallowRef, nextTick } from "vue";
import { useUserStoreHook } from "@/store/modules/user";
import { PlayerSmoothness, type SmoothnessSnapshot } from "./playerSmoothness";

interface Props {
  /** 流地址(http-flv / ws-flv / hls 等),传空字符串关闭播放器 */
  url: string;
  /** 录像回放页嵌入模式：填满父容器，由外层提供业务控制栏。 */
  playback?: boolean;
  /**
   * 通道音频开关（即 `gb_channel.audio_enabled`）。
   * - `true`：播放器出声，原生控制栏出现音频控制图标
   * - `false` / 不传：不建音频链路，控制栏也不出现音频图标
   *
   * 各调用点需显式传通道的 `audioEnabled`；不传一律按「无音频」处理。
   */
  hasAudio?: boolean;
  /** 启用 EasyPlayer 的 ZLMediaKit WebRTC 信令适配。 */
  zlmWebrtc?: boolean;
}

const props = defineProps<Props>();

/** 通道音频开关：不传按「无音频」处理。 */
const audioEnabled = computed(() => props.hasAudio === true);
const emit = defineEmits<{
  (e: "error", msg: string): void;
  (e: "timeupdate", timestamp: number): void;
  (e: "loading", value: boolean): void;
  (e: "smoothness", snapshot: SmoothnessSnapshot): void;
  /** 画面真实解码尺寸变化（首帧、或设备换了分辨率）。null = 还没拿到。 */
  (e: "videosize", size: VideoSize | null): void;
  (e: "firstFrame", event: PlaybackClientFact): void;
  (e: "playerError", event: PlaybackClientFact): void;
}>();

interface PlaybackClientFact {
  event: "first_frame" | "player_error";
  code?: "player_error" | "player_timeout";
  clientElapsedMs: number;
}

/** 画面真实解码尺寸（像素）。 */
interface VideoSize {
  width: number;
  height: number;
}

// EasyPlayerPro 由 index.html 静态引入(public/easyplayer/EasyPlayer-pro.js),挂在 window
declare const EasyPlayerPro: any;

const containerRef = ref<HTMLDivElement | null>(null);
const player = shallowRef<any>(null);
const smoothness = shallowRef<PlayerSmoothness | null>(null);
const errorMsg = ref("");
const firstFrameReady = ref(false);
const userStore = useUserStoreHook();
const canScreenshot = computed(() => {
  const permissions = userStore.account.permissions ?? [];
  return permissions.includes("*:*:*") || permissions.includes("gb28181:play:snapshot");
});

/**
 * 画面真实解码尺寸。遮挡框选这类「画面坐标系」的操作必须用它做基准 ——
 * 通道目录里声明的码流分辨率、`video-params` 回读的码值都可能与画面实际不符。
 */
const videoSize = ref<VideoSize | null>(null);
let sizeTimer: number | undefined;
let playbackSession = 0;
let playbackStartedAt = 0;
let firstFrameReported = false;
let playerErrorReported = false;

/** 轮询间隔。取 1s：分辨率是低频变化量，但首帧后要尽快拿到。 */
const VIDEO_SIZE_POLL_MS = 1000;

/**
 * 读一次 `getVideoInfo()`，有效且变化时更新并广播。
 *
 * ⛔ **不能订阅库的 `videoInfo` 事件**：库内 emit 被 `!this.init` 守卫，触发一次后
 *    立刻置 `init = true`，此后分辨率再变也不会通知（只有 `resetInit()` 才复位）。
 *    事件订阅写法看起来完全合理，实际只在第一帧收到一次 —— 所以这里自己按时读。
 */
function readVideoSize() {
  const session = playbackSession;
  let raw: { width?: unknown; height?: unknown } | null = null;
  try {
    raw = player.value?.getVideoInfo?.() ?? null;
  } catch (e) {
    console.warn("EasyPlayerPro getVideoInfo error", e);
  }
  const width = Number(raw?.width);
  const height = Number(raw?.height);
  const next: VideoSize | null =
    Number.isFinite(width) && Number.isFinite(height) && width > 0 && height > 0 ? { width, height } : null;
  const prev = videoSize.value;
  if (next && !firstFrameReported && session === playbackSession) {
    firstFrameReported = true;
    firstFrameReady.value = true;
    emit("firstFrame", {
      event: "first_frame",
      clientElapsedMs: Math.max(0, Math.round(performance.now() - playbackStartedAt))
    });
  }
  if (next?.width === prev?.width && next?.height === prev?.height) return;
  videoSize.value = next;
  emit("videosize", next);
}

function startSizePolling() {
  stopSizePolling();
  readVideoSize();
  sizeTimer = window.setInterval(readVideoSize, VIDEO_SIZE_POLL_MS);
}

function stopSizePolling() {
  if (sizeTimer !== undefined) {
    window.clearInterval(sizeTimer);
    sizeTimer = undefined;
  }
}

/**
 * 销毁当前播放器，并**等到库把容器交还**才返回。
 *
 * ⛔ 必须 await：`EasyPlayerPro.destroy()` 返回 Promise，而库靠容器上的
 *    `data-EasyProv` 标记判定「容器已被占用」（构造函数里 `_checkHasCreated()`），
 *    该标记是在 destroy 的 then 链里删的。实测（EasyPlayer-pro 2025-12-5 + 无头
 *    Chrome）：标记要跨过 **1 个宏任务**才消失，调用后同步读仍是 SET。
 *    不等就 `new`，构造函数会直接抛 `EasyPlayerPro err container`
 *    —— 只 `await nextTick()`（微任务）差一个量级，钉不住。
 */
async function destroy(): Promise<void> {
  stopSizePolling();
  videoSize.value = null;
  smoothness.value?.dispose();
  smoothness.value = null;
  const instance = player.value;
  player.value = null;
  if (!instance) return;
  try {
    await instance.destroy();
  } catch (e) {
    console.warn("EasyPlayerPro destroy error", e);
  }
}

function reportPlayerError(code: "player_error" | "player_timeout") {
  if (playerErrorReported || !props.url) return;
  playerErrorReported = true;
  emit("playerError", {
    event: "player_error",
    code,
    clientElapsedMs: Math.max(0, Math.round(performance.now() - playbackStartedAt))
  });
}

async function play(u: string, token: number) {
  errorMsg.value = "";
  if (!u) {
    await destroy();
    return;
  }

  if (typeof EasyPlayerPro === "undefined") {
    errorMsg.value = "EasyPlayer 未加载,请检查 /easyplayer/EasyPlayer-pro.js";
    emit("error", errorMsg.value);
    return;
  }

  if (player.value) {
    try {
      await player.value.play(u);
      if (token !== playToken) return;
      applyAudioState(player.value);
      startSizePolling();
    } catch (e) {
      errorMsg.value = `切换播放地址失败: ${(e as Error).message || e}`;
      emit("error", errorMsg.value);
    }
    return;
  }

  await nextTick();
  // 期间又发生了新的切换(含跨协议重建):这次构造作废,让位给更新的那次,
  // 否则两个 await 之间插进来的切换会让旧实例回写到 player 上。
  if (token !== playToken) return;
  if (!containerRef.value) return;

  try {
    const p = new EasyPlayerPro(containerRef.value, {
      isLive: true,
      bufferTime: 0.2,
      // 国标 IPC 默认 PCMA(G.711),EasyPlayer wasm 路径支持解码 G711
      // 通道关音频时库自己会摘掉音频按钮(内部 `operateBtns.audio = false`)
      hasAudio: audioEnabled.value,
      // ⚠️ 库里 isMute 的语义与命名相反:归一化时 `void 0!==e.isMute&&(t.isNotMute=e.isMute)`,
      // 传 true 等于「不静音」——play() 里 `_opt.isNotMute && this.mute(false)` 会主动取消静音。
      // 所以这里传的是「通道是否开音频」,才能做到「通道开音频 → 播放器出声」。
      isMute: audioEnabled.value,
      stretch: true,
      isRtcZLM: props.zlmWebrtc ?? false,
      // 解码模式优先级:MSE > WCS > WASM。打开 WASM 兜底,确保 G711/H265 也能放
      MSE: true,
      WCS: true,
      WASM: true,
      WASMSIMD: true,
      debug: false,
      isBand: true,
      btns: {
        fullscreen: true,
        screenshot: canScreenshot.value,
        play: true,
        audio: true,
        record: false,
        stretch: true
      }
    });

    // 事件订阅
    p.on("error", (err: any) => {
      errorMsg.value = `EasyPlayer 错误: ${typeof err === "string" ? err : JSON.stringify(err)}`;
      emit("error", errorMsg.value);
      reportPlayerError("player_error");
    });
    p.on("timeout", () => {
      errorMsg.value = "拉流超时";
      emit("error", errorMsg.value);
      reportPlayerError("player_timeout");
    });
    p.on("timeUpdate", (timestamp: unknown) => {
      if (typeof timestamp === "number" && Number.isFinite(timestamp)) emit("timeupdate", timestamp);
    });
    p.on("loading", (value: unknown) => emit("loading", Boolean(value)));

    player.value = p;

    // 原生控制栏流畅度徽标:只消费播放器每秒统计,不干预播放链路
    if (containerRef.value) {
      const badge = new PlayerSmoothness(containerRef.value);
      badge.bind(p, snapshot => emit("smoothness", snapshot));
      smoothness.value = badge;
    }

    await p.play(u);
    applyAudioState(p);
    startSizePolling();
  } catch (e) {
    errorMsg.value = `初始化失败: ${(e as Error).message || e}`;
    emit("error", errorMsg.value);
  }
}

/**
 * 解复用族。
 *
 * EasyPlayer 在 `_play()` 里按 URL 后缀决定 demuxType，并把结果写进**实例级**的
 * `_opt.isFlv` / `isHls` / `isTs` / `isWebrtc` … 一组互斥标志
 * （`_resetDemuxType(x)` = 全部清 `false` 再把 `x` 置 `true`）。
 *
 * ⛔ 这些标志**不在顶层默认配置里**，而 `_replay()` 只做
 *    `Object.assign(this._opt, _getConfig())` ⇒ **永远清不掉上一次的值**。
 *    库里给 `.flv` / `.fmp4` / `.mpeg4` / `.h264` / `.mp4` 都写了复位分支，
 *    **唯独 `.m3u8` 没有**；而 demuxType 的取值链是
 *    `… isFlv ? flv : isTs ? ts : (… : 后缀 .m3u8 ? hls : …)` ——
 *    `isFlv` **排在** `.m3u8` 后缀判定**之前**。
 *    于是「先播过 FLV，再切 HLS」会得到 `play protocol is hls, demuxType is flv`
 *    这种错配（实测），画面出不来；只有换实例才能让这组标志整体归零。
 *
 * ⭐ 判据是**族**而不是单个标志：同族（ws-flv ↔ http-flv、hls ↔ https-hls）复用安全，
 *    跨族一律重建。取值口径刻意对齐库里的后缀判定（先 `split("?")[0]`，故 `play_token`
 *    之类的查询串不影响判定）。
 */
type DemuxFamily = "webrtc" | "hls" | "flv" | "ts" | "fmp4" | "mpeg4" | "naked" | "wsrtc" | "other";

function demuxFamily(u: string, zlmWebrtc: boolean | undefined): DemuxFamily {
  if (zlmWebrtc === true || /^(webrtc|artc|wt):\/\//i.test(u)) return "webrtc";
  const path = u.split("?")[0].toLowerCase();
  const httpish = /^https?:\/\//.test(path);
  const wsish = /^wss?:\/\//.test(path);
  const streamish = httpish || wsish;
  if (httpish && path.endsWith(".m3u8")) return "hls";
  if (streamish && path.endsWith(".flv")) return "flv";
  if (streamish && path.endsWith(".ts")) return "ts";
  if (streamish && (path.endsWith(".fmp4") || path.includes(".mp4"))) return "fmp4";
  if (streamish && path.endsWith(".mpeg4")) return "mpeg4";
  if (streamish && (path.endsWith(".h264") || path.endsWith(".h265"))) return "naked";
  if (wsish && path.includes("6688")) return "wsrtc";
  return "other";
}

/** 当前实例上一次按哪个解复用族播过。跨族必须换实例。 */
let playerFamily: DemuxFamily | null = null;

/**
 * 播放代次。每次切换自增；跨 `await` 的旧任务据此让位给更新的那次，
 * 避免「先发起的重建后落地」把播放器状态写回旧值。
 */
let playToken = 0;

/**
 * 统一切换入口：按需换实例，再交给 `play()`。
 *
 * `force` 用于「族没变、但实例必须重建」的场景（截图权限影响控制栏按钮）。
 */
async function switchTo(u: string, force = false): Promise<void> {
  const token = ++playToken;
  const nextFamily = demuxFamily(u, props.zlmWebrtc);
  if (player.value && (force || nextFamily !== playerFamily)) {
    // ⛔ 必须等 destroy 落定再重建（库的容器占用标记是异步清掉的），
    //    否则构造函数会抛 `EasyPlayerPro err container`。
    await destroy();
    if (token !== playToken) return;
  }
  playerFamily = nextFamily;
  await play(u, token);
}

watch(
  [() => props.url, () => props.zlmWebrtc],
  ([u]) => {
    playbackSession += 1;
    playbackStartedAt = performance.now();
    firstFrameReported = false;
    firstFrameReady.value = false;
    playerErrorReported = false;
    // ⛔ 跨解复用族必须销毁重建，不能复用实例（同族之间切换仍复用）。
    //   两类已知的实例级残留（都由「顶层默认配置里没这些字段 ⇒ _replay 清不掉」造成）：
    //   ① 进过 WebRTC 后切回任何 FLV 地址：`_opt.isWebrtc` 仍为 true，
    //      `<video>` 拿不到 MediaSource（src 恒空串）⇒ 永久黑屏；
    //   ② 先播过 FLV 再切 HLS：`_opt.isFlv` 仍为 true，demuxType 被判成 flv 而
    //      协议是 hls ⇒ 错配 ⇒ 画面出不来（库唯独没有 .m3u8 的复位分支）。
    //    重建实例是唯一能让这组构造期状态整体归零的做法。
    void switchTo(u);
  },
  { immediate: true }
);
watch(canScreenshot, () => {
  if (!player.value || !props.url) return;
  void switchTo(props.url, true);
});
onBeforeUnmount(() => {
  void destroy();
});

/**
 * 播放后对齐一次音频状态。
 *
 * 原生控制栏的两个音频图标是同级的 DOM:`.easyplayer-icon-audio` 默认 `display:none`、
 * `.easyplayer-icon-mute` 默认可见,只有收到 `volumechange` 才会互换——复位过一次,
 * 就不会出现「通道开了音频,图标却停在静音」的状态。通道关音频时不调用(控件本就不渲染)。
 */
function applyAudioState(instance: any) {
  if (!audioEnabled.value) return;
  try {
    instance.setMute?.(0);
  } catch (e) {
    console.warn("EasyPlayerPro setMute error", e);
  }
}

/** 供父组件读取当前流畅度(卡片展示等),未播放时返回 null。 */
function getSmoothness(): SmoothnessSnapshot | null {
  return smoothness.value?.snapshot() ?? null;
}

/** 供父组件读取当前画面真实解码尺寸（遮挡框选等画面坐标系的唯一真源）。未就绪返回 null。 */
function getVideoSize(): VideoSize | null {
  return videoSize.value;
}

defineExpose({ stop: destroy, getSmoothness, getVideoSize, refreshVideoSize: readVideoSize });
</script>

<template>
  <div :class="['play-window', { playback }]">
    <div ref="containerRef" class="player" />
    <div v-if="url && !firstFrameReady && !errorMsg" class="loading" data-testid="player-loading" role="status">
      <span class="loading-spinner" aria-hidden="true" />
      <span>正在连接视频流，等待首帧…</span>
    </div>
    <div v-if="errorMsg" class="err">{{ errorMsg }}</div>
    <div v-else-if="!url" class="placeholder">点击左侧通道开始播放</div>
  </div>
</template>

<style scoped>
.play-window {
  position: relative;
  box-sizing: border-box;
  width: 100%;
  min-height: 0;
  aspect-ratio: 16 / 9;
  overflow: hidden;
  background: radial-gradient(circle at 50% 50%, rgb(30 41 59 / 32%) 0%, rgb(2 6 23 / 94%) 72%), #020617;
  border: 1px solid rgb(148 163 184 / 18%);
  border-radius: 14px;
}

.play-window.playback {
  height: 100%;
  aspect-ratio: auto;
  background: #000000;
  border: 0;
  border-radius: 0;
}

.play-window.playback .placeholder {
  display: none;
}

.player {
  width: 100%;
  height: 100%;
  min-height: 100%;
}

.loading {
  position: absolute;
  inset: 0;
  z-index: 4;
  display: flex;
  flex-direction: column;
  gap: 12px;
  align-items: center;
  justify-content: center;
  font-size: 13px;
  color: rgb(226 232 240 / 92%);
  pointer-events: none;
  background: radial-gradient(circle at 50% 45%, rgb(30 41 59 / 46%) 0%, rgb(2 6 23 / 72%) 72%);
}

.loading-spinner {
  width: 30px;
  height: 30px;
  border: 3px solid rgb(148 163 184 / 28%);
  border-top-color: #60a5fa;
  border-radius: 50%;
  animation: player-loading-spin 0.8s linear infinite;
}

@keyframes player-loading-spin {
  to {
    transform: rotate(360deg);
  }
}

/* EasyPlayer 原生控制栏上的流畅度徽标:运行时插入,需穿透 scoped 才能命中 */
.play-window :deep(.uvp-smooth-badge) {
  display: inline-flex;
  flex: none;
  gap: 5px;
  align-items: center;
  height: 20px;
  padding: 0 8px;
  margin-right: 8px;
  font-size: 12px;
  line-height: 1;
  color: rgb(255 255 255 / 62%);
  white-space: nowrap;
  user-select: none;
  background: rgb(255 255 255 / 10%);
  border-radius: 10px;
  transition:
    color 0.2s ease,
    background-color 0.2s ease;
}

.play-window :deep(.uvp-smooth-dot) {
  flex: none;
  width: 6px;
  height: 6px;
  background: currentcolor;
  border-radius: 999px;
}

.play-window :deep(.uvp-smooth-badge.is-smooth) {
  color: #4ade80;
  background: rgb(74 222 128 / 16%);
}

.play-window :deep(.uvp-smooth-badge.is-fair) {
  color: #fbbf24;
  background: rgb(251 191 36 / 16%);
}

.play-window :deep(.uvp-smooth-badge.is-laggy) {
  color: #f87171;
  background: rgb(248 113 113 / 18%);
}

.play-window :deep(.uvp-smooth-badge.is-stalled) {
  color: #fca5a5;
  background: rgb(239 68 68 / 26%);
}

.play-window :deep(.uvp-smooth-badge.is-stalled .uvp-smooth-dot) {
  animation: uvp-smooth-pulse 1.2s ease-in-out infinite;
}

.play-window :deep(.uvp-smooth-badge.is-compact) {
  padding: 0 6px;
}

.play-window :deep(.uvp-smooth-badge.is-compact .uvp-smooth-text) {
  display: none;
}

.play-window :deep(.uvp-smooth-badge.is-hidden) {
  display: none;
}

@keyframes uvp-smooth-pulse {
  0%,
  100% {
    opacity: 1;
  }

  50% {
    opacity: 0.25;
  }
}

.err {
  position: absolute;
  right: 12px;
  bottom: 12px;
  left: 12px;
  z-index: 5;
  padding: 10px 12px;
  font-size: 12px;
  color: #fecaca;
  background: rgb(127 29 29 / 78%);
  border: 1px solid rgb(248 113 113 / 34%);
  border-radius: 10px;
  backdrop-filter: blur(10px);
}

.placeholder {
  position: absolute;
  top: 50%;
  left: 50%;
  display: flex;
  flex-direction: column;
  gap: 8px;
  align-items: center;
  justify-content: center;
  width: min(72%, 320px);
  font-size: 14px;
  line-height: 1.5;
  color: rgb(203 213 225 / 88%);
  text-align: center;
  transform: translate(-50%, -50%);
}

.placeholder::before {
  width: 44px;
  height: 44px;
  content: "";
  background: radial-gradient(circle at 50% 50%, rgb(59 130 246 / 80%) 0%, rgb(59 130 246 / 12%) 64%, transparent 66%);
  border-radius: 999px;
  box-shadow: 0 0 0 1px rgb(148 163 184 / 18%);
}

@media (width <= 768px) {
  .play-window {
    aspect-ratio: 4 / 3;
  }
}
</style>
