<script setup lang="ts">
import { ref, watch, onBeforeUnmount, shallowRef } from "vue";
import mpegts from "mpegts.js";

interface Props {
    /** 流地址(http-flv 或 ws-flv),传空字符串关闭播放器 */
    url: string;
    /** 流类型,默认 flv;hls 走 video 原生(浏览器 hls 兼容性差时再说) */
    type?: "flv" | "mse-h265" | "hls";
}

const props = withDefaults(defineProps<Props>(), { type: "flv" });
const emit = defineEmits<{ (e: "error", msg: string): void }>();

const videoRef = ref<HTMLVideoElement | null>(null);
const player = shallowRef<mpegts.Player | null>(null);
const errorMsg = ref("");

function destroy() {
    if (player.value) {
        try {
            player.value.pause();
            player.value.unload();
            player.value.detachMediaElement();
            player.value.destroy();
        } catch (e) {
            console.warn("destroy player error", e);
        }
        player.value = null;
    }
}

function play(u: string) {
    destroy();
    errorMsg.value = "";
    if (!u || !videoRef.value) return;

    if (!mpegts.getFeatureList().mseLivePlayback) {
        errorMsg.value = "浏览器不支持 MSE 直播";
        emit("error", errorMsg.value);
        return;
    }

    const isWs = u.startsWith("ws://") || u.startsWith("wss://");
    const p = mpegts.createPlayer(
        {
            type: "flv",
            url: u,
            isLive: true,
            cors: true,
            hasAudio: true,
            hasVideo: true
        },
        {
            enableStashBuffer: false,
            stashInitialSize: 128,
            liveBufferLatencyChasing: true,
            liveBufferLatencyMaxLatency: 1.5,
            liveBufferLatencyMinRemain: 0.3
        }
    );
    p.attachMediaElement(videoRef.value);
    p.load();
    const playPromise = p.play();
    if (playPromise && typeof (playPromise as Promise<void>).catch === "function") {
        (playPromise as Promise<void>).catch((err: Error) => {
            errorMsg.value = `播放失败: ${err.message || err}`;
            emit("error", errorMsg.value);
        });
    }
    p.on(mpegts.Events.ERROR, (errType, errDetail) => {
        errorMsg.value = `${errType} / ${errDetail}`;
        emit("error", errorMsg.value);
    });
    player.value = p;
    // 防止 ws/wss 协议下 mpegts 默认走 fetch 失败:它实际通过自身 wsLoader 走 WebSocket
    void isWs;
}

watch(() => props.url, (u) => play(u), { immediate: true });
onBeforeUnmount(destroy);

defineExpose({ stop: destroy });
</script>

<template>
    <div class="play-window">
        <video ref="videoRef" controls muted autoplay class="player" />
        <div v-if="errorMsg" class="err">{{ errorMsg }}</div>
        <div v-else-if="!url" class="placeholder">点击左侧通道开始播放</div>
    </div>
</template>

<style scoped>
.play-window {
    position: relative;
    width: 100%;
    height: 100%;
    min-height: 360px;
    background: #000;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: 4px;
    overflow: hidden;
}

.player {
    width: 100%;
    height: 100%;
    object-fit: contain;
}

.err {
    position: absolute;
    bottom: 8px;
    left: 8px;
    right: 8px;
    color: #ff7d7d;
    background: rgba(0, 0, 0, 0.6);
    padding: 6px 8px;
    border-radius: 4px;
    font-size: 12px;
}

.placeholder {
    position: absolute;
    color: #888;
    font-size: 14px;
}
</style>
