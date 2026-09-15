<script setup lang="ts">
import { computed, ref, watch, onBeforeUnmount, shallowRef, nextTick } from "vue";
import { useUserStoreHook } from "@/store/modules/user";
import { PlayerSmoothness, type SmoothnessSnapshot } from "./playerSmoothness";

interface Props {
    /** 流地址(http-flv / ws-flv / hls 等),传空字符串关闭播放器 */
    url: string;
    /** 录像回放页嵌入模式：填满父容器，由外层提供业务控制栏。 */
    playback?: boolean;
    hasAudio?: boolean;
    /** 启用 EasyPlayer 的 ZLMediaKit WebRTC 信令适配。 */
    zlmWebrtc?: boolean;
}

const props = defineProps<Props>();
const emit = defineEmits<{
    (e: "error", msg: string): void;
    (e: "timeupdate", timestamp: number): void;
    (e: "loading", value: boolean): void;
    (e: "smoothness", snapshot: SmoothnessSnapshot): void;
}>();

// EasyPlayerPro 由 index.html 静态引入(public/easyplayer/EasyPlayer-pro.js),挂在 window
declare const EasyPlayerPro: any;

const containerRef = ref<HTMLDivElement | null>(null);
const player = shallowRef<any>(null);
const smoothness = shallowRef<PlayerSmoothness | null>(null);
const errorMsg = ref("");
const userStore = useUserStoreHook();
const canScreenshot = computed(() => {
    const permissions = userStore.account.permissions ?? [];
    return permissions.includes("*:*:*") || permissions.includes("gb28181:play:snapshot");
});

function destroy() {
    smoothness.value?.dispose();
    smoothness.value = null;
    if (player.value) {
        try {
            player.value.destroy();
        } catch (e) {
            console.warn("EasyPlayerPro destroy error", e);
        }
        player.value = null;
    }
}

async function play(u: string) {
    errorMsg.value = "";
    if (!u) {
        destroy();
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
        } catch (e) {
            errorMsg.value = `切换播放地址失败: ${(e as Error).message || e}`;
            emit("error", errorMsg.value);
        }
        return;
    }

    await nextTick();
    if (!containerRef.value) return;

    try {
        const p = new EasyPlayerPro(containerRef.value, {
            isLive: true,
            bufferTime: 0.2,
            // 国标 IPC 默认 PCMA(G.711),EasyPlayer wasm 路径支持解码 G711
            hasAudio: props.hasAudio ?? true,
            isMute: true,         // 默认静音(浏览器自动播放策略友好)
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
        });
        p.on("timeout", () => {
            errorMsg.value = "拉流超时";
            emit("error", errorMsg.value);
        });
        p.on("timeUpdate", (timestamp: unknown) => {
            if (typeof timestamp === "number" && Number.isFinite(timestamp)) emit("timeupdate", timestamp);
        });
        p.on("loading", (value: unknown) => emit("loading", Boolean(value)));

        player.value = p;

        // 原生控制栏流畅度徽标:只消费播放器每秒统计,不干预播放链路
        if (containerRef.value) {
            const badge = new PlayerSmoothness(containerRef.value);
            badge.bind(p, (snapshot) => emit("smoothness", snapshot));
            smoothness.value = badge;
        }

        await p.play(u);
    } catch (e) {
        errorMsg.value = `初始化失败: ${(e as Error).message || e}`;
        emit("error", errorMsg.value);
    }
}

watch(() => props.url, (u) => play(u), { immediate: true });
watch(canScreenshot, () => {
    if (!player.value || !props.url) return;
    const currentUrl = props.url;
    destroy();
    void play(currentUrl);
});
onBeforeUnmount(destroy);

/** 供父组件读取当前流畅度(卡片展示等),未播放时返回 null。 */
function getSmoothness(): SmoothnessSnapshot | null {
    return smoothness.value?.snapshot() ?? null;
}

defineExpose({ stop: destroy, getSmoothness });
</script>

<template>
    <div :class="['play-window', { playback }]">
        <div ref="containerRef" class="player" />
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
    background:
        radial-gradient(circle at 50% 50%, rgb(30 41 59 / 32%) 0%, rgb(2 6 23 / 94%) 72%),
        #020617;
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
    background:
        radial-gradient(circle at 50% 50%, rgb(59 130 246 / 80%) 0%, rgb(59 130 246 / 12%) 64%, transparent 66%);
    border-radius: 999px;
    box-shadow: 0 0 0 1px rgb(148 163 184 / 18%);
}

@media (width <= 768px) {
    .play-window {
        aspect-ratio: 4 / 3;
    }
}
</style>
