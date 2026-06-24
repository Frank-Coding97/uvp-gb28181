<script setup lang="ts">
import { ref, watch, onBeforeUnmount, shallowRef, nextTick } from "vue";

interface Props {
    /** 流地址(http-flv / ws-flv / hls 等),传空字符串关闭播放器 */
    url: string;
}

const props = defineProps<Props>();
const emit = defineEmits<{ (e: "error", msg: string): void }>();

// EasyPlayerPro 由 index.html 静态引入(public/easyplayer/EasyPlayer-pro.js),挂在 window
declare const EasyPlayerPro: any;

const containerRef = ref<HTMLDivElement | null>(null);
const player = shallowRef<any>(null);
const errorMsg = ref("");

function destroy() {
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
    destroy();
    errorMsg.value = "";
    if (!u) return;

    if (typeof EasyPlayerPro === "undefined") {
        errorMsg.value = "EasyPlayer 未加载,请检查 /easyplayer/EasyPlayer-pro.js";
        emit("error", errorMsg.value);
        return;
    }

    await nextTick();
    if (!containerRef.value) return;

    try {
        const p = new EasyPlayerPro(containerRef.value, {
            isLive: true,
            bufferTime: 0.2,
            // 国标 IPC 默认 PCMA(G.711),EasyPlayer wasm 路径支持解码 G711
            hasAudio: true,
            isMute: true,         // 默认静音(浏览器自动播放策略友好)
            stretch: true,
            // 解码模式优先级:MSE > WCS > WASM。打开 WASM 兜底,确保 G711/H265 也能放
            MSE: true,
            WCS: true,
            WASM: true,
            WASMSIMD: true,
            debug: false,
            isBand: true,
            btns: {
                fullscreen: true,
                screenshot: true,
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

        p.play(u);
        player.value = p;
    } catch (e) {
        errorMsg.value = `初始化失败: ${(e as Error).message || e}`;
        emit("error", errorMsg.value);
    }
}

watch(() => props.url, (u) => play(u), { immediate: true });
onBeforeUnmount(destroy);

defineExpose({ stop: destroy });
</script>

<template>
    <div class="play-window">
        <div ref="containerRef" class="player" />
        <div v-if="errorMsg" class="err">{{ errorMsg }}</div>
        <div v-else-if="!url" class="placeholder">点击左侧通道开始播放</div>
    </div>
</template>

<style scoped>
.play-window {
    position: relative;
    width: 100%;
    min-height: 540px;
    background: #000;
    border-radius: 4px;
    overflow: hidden;
}

.player {
    width: 100%;
    height: 100%;
    min-height: 540px;
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
    z-index: 5;
}

.placeholder {
    position: absolute;
    color: #888;
    font-size: 14px;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
}
</style>
