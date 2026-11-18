<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref } from "vue";
import { Copy, ExternalLink, Pause, Play, RefreshCw } from "@lucide/vue";
import QRCode from "qrcode";
import uvpLogo from "@/assets/logo/uvp-mark.svg";
import { preparePlayerDecoder } from "@/views/gb28181/components/playerDecoderResources";
import {
  closeViewer,
  createShareURL,
  createViewer,
  escapeHTMLAttribute,
  getViewerStatus,
  parseShareFragment,
  resolveShare,
  type PublicShare,
  type ShareCredentials,
  type ViewerSession
} from "./shareSession";

declare const EasyPlayerPro: any;

type ViewState =
  | "loading"
  | "playing"
  | "offline"
  | "paused"
  | "network-error"
  | "playback-error"
  | "manual-play"
  | "unsupported"
  | "expired";

// EasyPlayer reports these errors before trying its software decoder. Only its
// playFailedAndPaused event means that fallback has also failed.
const unsupportedErrors = new Set([
  "mediaSourceH265NotSupport",
  "webcodecsH265NotSupport",
  "webcodecsUnsupportedConfigurationError",
  "simdH264DecodeVideoWidthIsTooLarge",
  "videoCodecIdError"
]);
const manualPlayErrors = new Set(["videoElementPlayingFailed", "videoElementPlayingFailedForWebrtc"]);

const credentials = ref<ShareCredentials | null>(null);
const share = ref<PublicShare | null>(null);
const viewer = ref<ViewerSession | null>(null);
const playbackStarted = ref(false);
const state = ref<ViewState>("loading");
const errorText = ref("");
const playerRoot = ref<HTMLDivElement | null>(null);
const qrDataURL = ref("");
const currentTime = ref(Date.now());
const serverTimeOffset = ref(0);
const shareURL = ref("");
const embedMode = ref(false);
let player: any;
let statusTimer: number | undefined;
let clockTimer: number | undefined;
let requestGeneration = 0;
let disposed = false;

const remainingSeconds = computed(() => {
  if (!share.value) return 0;
  const serverNow = currentTime.value + serverTimeOffset.value;
  return Math.max(0, Math.ceil((Date.parse(share.value.expiresAt) - serverNow) / 1000));
});
const expiryLabel = computed(() => {
  if (!share.value) return "";
  return new Date(share.value.expiresAt).toLocaleString();
});
const iframeCode = computed(() => {
  const src = createShareURL(window.location, credentials.value ?? { publicId: "", signature: "", embed: true }, true);
  const title = escapeHTMLAttribute(share.value?.name || "实时视频分享");
  return `<iframe src="${escapeHTMLAttribute(src)}" title="${title}" allow="autoplay; fullscreen" allowfullscreen style="width:100%;aspect-ratio:16/9;border:0"></iframe>`;
});

async function initialize() {
  const parsed = parseShareFragment(window.location.hash);
  if (!parsed) {
    state.value = "expired";
    errorText.value = "分享链接无效，请联系分享人";
    return;
  }
  credentials.value = parsed;
  embedMode.value = parsed.embed;
  shareURL.value = createShareURL(window.location, parsed, false);
  history.replaceState(null, "", window.location.pathname);
  const generation = requestGeneration;
  try {
    const resolved = await resolveShare(parsed);
    if (disposed || generation !== requestGeneration) return;
    updateShare(resolved);
    if (!resolved.online) {
      state.value = "offline";
      errorText.value = "通道当前离线";
      return;
    }
    await createPlaybackSession();
  } catch (error) {
    if (!disposed && generation === requestGeneration) showRequestError(error);
  }
}

function updateShare(resolved: PublicShare) {
  share.value = resolved;
  currentTime.value = Date.now();
  serverTimeOffset.value = Date.parse(resolved.serverTime) - currentTime.value;
}

async function createPlaybackSession() {
  if (disposed) return;
  if (share.value && remainingSeconds.value <= 0) {
    await expire();
    return;
  }
  if (!credentials.value || !share.value || !share.value.online || remainingSeconds.value <= 0) {
    state.value = remainingSeconds.value <= 0 ? "expired" : "offline";
    return;
  }
  const generation = ++requestGeneration;
  state.value = "loading";
  errorText.value = "";
  try {
    const nextViewer = await createViewer(credentials.value);
    if (disposed || generation !== requestGeneration) {
      void closeViewer(nextViewer.viewerToken).catch(() => undefined);
      return;
    }
    viewer.value = nextViewer;
    await nextTick();
    if (!(await startPlayer(nextViewer, generation)) || disposed || generation !== requestGeneration) return;
    if (state.value === "loading") {
      playbackStarted.value = true;
      state.value = "playing";
    }
  } catch (error) {
    if (!disposed && generation === requestGeneration) showRequestError(error);
  }
}

async function startPlayer(session: ViewerSession, generation: number): Promise<boolean> {
  if (disposed || generation !== requestGeneration) return false;
  if (!playerRoot.value) throw new Error("播放器未就绪");
  if (typeof EasyPlayerPro === "undefined") throw new Error("播放器资源未加载");
  const mediaPath = chooseMediaPath(session);
  if (!mediaPath) {
    showPlaybackError(new DOMException("No supported video format", "NotSupportedError"));
    return false;
  }
  const decoderPath = await preparePlayerDecoder();
  if (disposed || generation !== requestGeneration || !playerRoot.value) return false;
  const nextPlayer = new EasyPlayerPro(playerRoot.value, {
    isLive: true,
    ...(decoderPath ? { decoderPath } : {}),
    bufferTime: 0.2,
    hasAudio: share.value?.hasAudio === true,
    // This library maps isMute to isNotMute: false intentionally starts muted.
    isMute: false,
    stretch: false,
    MSE: true,
    WCS: true,
    WASM: true,
    WASMSIMD: true,
    debug: false,
    btns: { fullscreen: true, play: true, audio: share.value?.hasAudio === true, screenshot: false, record: false }
  });
  player = nextPlayer;
  const isCurrent = () => !disposed && generation === requestGeneration && player === nextPlayer;
  nextPlayer.on("error", (code: unknown, detail: unknown) => {
    if (!isCurrent()) return;
    if (typeof code === "string" && (unsupportedErrors.has(code) || manualPlayErrors.has(code))) return;
    showPlaybackError(code, detail);
  });
  nextPlayer.on("playFailedAndPaused", (code: unknown, detail: unknown) => {
    if (isCurrent()) showPlaybackError(code, detail);
  });
  nextPlayer.on("play", () => {
    if (isCurrent()) {
      playbackStarted.value = true;
      state.value = "playing";
      errorText.value = "";
    }
  });
  nextPlayer.on("timeout", () => {
    if (!isCurrent()) return;
    state.value = "playback-error";
    errorText.value = "连接视频超时，请重试";
  });
  startStatusPolling();
  try {
    await nextPlayer.play(mediaPath);
    return isCurrent();
  } catch (error) {
    if (isCurrent()) showPlaybackError(error);
    return false;
  }
}

function showPlaybackError(error: unknown, detail?: unknown) {
  const code = typeof error === "string" ? error : (error as { name?: string } | null)?.name;
  const detailName = (detail as { name?: string } | null)?.name;
  if (code === "NotAllowedError" || detailName === "NotAllowedError" || manualPlayErrors.has(code || "")) {
    state.value = "manual-play";
    errorText.value = "视频未能自动播放，请点击播放";
  } else if (code === "NotSupportedError" || detailName === "NotSupportedError" || unsupportedErrors.has(code || "")) {
    state.value = "unsupported";
    errorText.value = "当前浏览器对该视频编码不支持，请更换浏览器或联系分享人调整编码";
  } else {
    state.value = "playback-error";
    errorText.value = "视频暂时无法播放，请检查网络或稍后重试";
  }
}

async function playManually() {
  const current = player;
  const active = viewer.value;
  if (disposed || !current || !active || state.value !== "manual-play") return;
  if (remainingSeconds.value <= 0) {
    await expire();
    return;
  }
  const generation = requestGeneration;
  state.value = "loading";
  errorText.value = "";
  try {
    // Call native play while the click still carries browser user activation.
    // Reuse the authorized viewer; no exchange, decoder preparation or nextTick precedes it.
    const nativePlayback = Array.from(playerRoot.value?.querySelectorAll("video") || []).map(video => video.play());
    await Promise.all([...nativePlayback, current.play(chooseMediaPath(active))]);
    if (!disposed && generation === requestGeneration && player === current && state.value === "loading") {
      playbackStarted.value = true;
      state.value = "playing";
    }
  } catch (error) {
    if (!disposed && generation === requestGeneration && player === current) showPlaybackError(error);
  }
}

function chooseMediaPath(session: ViewerSession): string {
  const ua = navigator.userAgent;
  const prefersHLS = /iPhone|iPad|iPod/i.test(ua) || (navigator.maxTouchPoints > 1 && /Macintosh/i.test(ua));
  const path = prefersHLS ? session.hlsPath || session.httpFlvPath : session.httpFlvPath || session.hlsPath;
  return path ? new URL(path, window.location.origin).toString() : "";
}

async function pause() {
  await releaseViewer();
  if (!disposed && remainingSeconds.value <= 0) {
    await expire();
    return;
  }
  if (!disposed && state.value !== "expired") {
    state.value = "paused";
    errorText.value = "";
  }
}

async function retry() {
  if (disposed || state.value === "expired") return;
  await releaseViewer();
  if (disposed) return;
  const generation = requestGeneration;
  try {
    if (!credentials.value) return;
    if (share.value && remainingSeconds.value <= 0) {
      await expire();
      return;
    }
    if (!share.value || !share.value.online) {
      const resolved = await resolveShare(credentials.value);
      if (disposed || generation !== requestGeneration) return;
      updateShare(resolved);
    }
    if (!share.value?.online) {
      state.value = "offline";
      errorText.value = "通道当前离线";
      return;
    }
    await createPlaybackSession();
  } catch (error) {
    if (!disposed && generation === requestGeneration) showRequestError(error);
  }
}

async function releaseViewer() {
  requestGeneration += 1;
  stopStatusPolling();
  const active = viewer.value;
  viewer.value = null;
  playbackStarted.value = false;
  await destroyPlayer();
  if (active) await closeViewer(active.viewerToken).catch(() => undefined);
}

async function expire(message = "分享已失效，请联系分享人") {
  state.value = "expired";
  errorText.value = message;
  await releaseViewer();
}

async function destroyPlayer() {
  const current = player;
  player = undefined;
  if (!current) return;
  try {
    await current.destroy();
  } catch {
    // The session is released even if the browser player fails to destroy.
  }
}

function startStatusPolling() {
  stopStatusPolling();
  statusTimer = window.setInterval(async () => {
    const active = viewer.value;
    if (!active) return;
    const generation = requestGeneration;
    try {
      const status = await getViewerStatus(active.viewerToken);
      if (disposed || generation !== requestGeneration || viewer.value !== active) return;
      if (share.value) {
        updateShare({ ...share.value, expiresAt: status.expiresAt, serverTime: status.serverTime });
      }
      if (remainingSeconds.value <= 0 && !playbackStarted.value) await expire();
    } catch (error) {
      if (disposed || generation !== requestGeneration || viewer.value !== active) return;
      showRequestError(error);
      if (state.value === "expired") await expire(errorText.value);
    }
  }, 5000);
}

function stopStatusPolling() {
  if (statusTimer !== undefined) {
    window.clearInterval(statusTimer);
    statusTimer = undefined;
  }
}

function showRequestError(error: unknown) {
  const message = error instanceof Error ? error.message : "分享暂时不可用，请重试";
  state.value = message.includes("失效") || message.includes("无效") ? "expired" : "network-error";
  errorText.value = message;
}

function copy(text: string) {
  navigator.clipboard.writeText(text).catch(() => {
    errorText.value = "复制失败，请检查浏览器剪贴板权限";
  });
}

async function createQRCode() {
  if (disposed || !shareURL.value || embedMode.value) return;
  try {
    qrDataURL.value = await QRCode.toDataURL(shareURL.value, { width: 168, margin: 1, errorCorrectionLevel: "M" });
  } catch {
    qrDataURL.value = "";
  }
}

onMounted(() => {
  clockTimer = window.setInterval(() => {
    currentTime.value = Date.now();
    if (remainingSeconds.value <= 0 && state.value !== "expired" && share.value && !playbackStarted.value) {
      void expire();
    }
  }, 1000);
  void initialize().then(createQRCode);
});

onUnmounted(() => {
  disposed = true;
  if (clockTimer !== undefined) window.clearInterval(clockTimer);
  void releaseViewer();
});
</script>

<template>
  <main :class="['share-page', { 'share-page--embed': embedMode }]">
    <header v-if="!embedMode" class="share-header">
      <div class="share-brand">
        <img :src="uvpLogo" alt="UVP-GB28181" width="28" height="28" />UVP-GB28181 <span>实时视频分享</span>
      </div>
      <div v-if="share" class="share-expiry">链接{{ remainingSeconds <= 0 ? "已到期" : "有效期至" }} {{ expiryLabel }}</div>
    </header>

    <section class="share-content">
      <div class="share-title" v-if="!embedMode && share">
        <h1>{{ share.name || "实时通道" }}</h1>
        <span :class="['online-state', { 'online-state--off': !share.online }]">
          <i aria-hidden="true" />{{ share.online ? "在线" : "离线" }}
        </span>
      </div>

      <div class="share-player" :aria-label="share?.name || '实时视频画面'">
        <div ref="playerRoot" class="share-player__screen" />
        <div v-if="state === 'loading'" class="player-message" role="status">正在连接视频流…</div>
        <div
          v-else-if="
            state === 'offline' ||
            state === 'network-error' ||
            state === 'playback-error' ||
            state === 'unsupported' ||
            state === 'expired'
          "
          class="player-message player-message--error"
          role="status"
        >
          <strong>{{ errorText }}</strong>
          <button v-if="state !== 'expired'" type="button" class="icon-text-button" @click="retry">
            <RefreshCw :size="15" />重试
          </button>
        </div>
        <div v-else-if="state === 'manual-play'" class="player-message" role="status">
          <span>{{ errorText }}</span>
          <button type="button" class="icon-text-button" @click="playManually"><Play :size="15" />点击播放</button>
        </div>
        <div v-else-if="state === 'paused'" class="player-message">
          <span>已暂停</span>
          <button type="button" class="icon-text-button" @click="retry"><Play :size="15" />继续播放</button>
        </div>
      </div>

      <div v-if="!embedMode && share && state !== 'expired'" class="share-actions">
        <button v-if="state === 'playing'" type="button" class="icon-text-button" @click="pause"><Pause :size="15" />暂停</button>
        <span v-if="remainingSeconds <= 0" class="share-remaining">链接已到期，当前观看不受影响</span>
        <span v-else class="share-remaining"
          >链接剩余 {{ Math.floor(remainingSeconds / 60) }} 分 {{ remainingSeconds % 60 }} 秒</span
        >
      </div>

      <section v-if="!embedMode && state === 'playing' && remainingSeconds > 0" class="share-tools" aria-label="分享方式">
        <div class="share-tool share-tool--link">
          <label for="share-link">分享链接</label>
          <div class="share-link-row">
            <input id="share-link" :value="shareURL" readonly />
            <button type="button" class="icon-button" aria-label="复制分享链接" title="复制分享链接" @click="copy(shareURL)">
              <Copy :size="16" />
            </button>
            <a
              class="icon-button"
              :href="shareURL"
              target="_blank"
              rel="noopener noreferrer"
              aria-label="在新窗口打开"
              title="在新窗口打开"
              ><ExternalLink :size="16"
            /></a>
          </div>
        </div>
        <div class="share-tool share-tool--embed">
          <label for="share-iframe">iframe 嵌入代码</label>
          <div class="share-code-row">
            <textarea id="share-iframe" :value="iframeCode" readonly rows="2" />
            <button
              type="button"
              class="icon-button"
              aria-label="复制 iframe 代码"
              title="复制 iframe 代码"
              @click="copy(iframeCode)"
            >
              <Copy :size="16" />
            </button>
          </div>
        </div>
        <div class="share-tool share-tool--qr">
          <img v-if="qrDataURL" :src="qrDataURL" alt="视频分享二维码" width="136" height="136" />
          <span>扫码观看</span>
        </div>
      </section>
    </section>
    <footer v-if="!embedMode" class="share-footer">链接到期时间 {{ expiryLabel }}；到期后无法新访问，已开始的观看可继续。</footer>
  </main>
</template>
