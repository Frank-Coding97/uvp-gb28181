<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import {
    AlertTriangle,
    ArrowDown,
    ArrowDownLeft,
    ArrowDownRight,
    ArrowLeft,
    ArrowRight,
    ArrowUp,
    ArrowUpLeft,
    ArrowUpRight,
    Check,
    Copy,
    Gauge,
    Info,
    Loader2,
    RadioTower,
    RefreshCcw,
    Signal,
    Square,
    Video,
    Wifi,
    ZoomIn,
    ZoomOut
} from "@lucide/vue";
import { startPlay, stopPlay, type PlayResult } from "@/api/gb28181";
import PlayWindow from "./PlayWindow.vue";

interface PlaybackChannel {
    id: number;
    channelId: string;
    deviceId: string;
    name?: string;
    alias?: string;
    manufacturer?: string;
    model?: string;
    ptzType?: number;
    status: number;
    streamTransport?: string;
}

type SessionPhase = "idle" | "requesting" | "playing" | "error" | "stopping";
type StreamProtocol = "http-flv" | "ws-flv" | "hls";

const props = defineProps<{
    visible: boolean;
    channel: PlaybackChannel | null;
}>();
const emit = defineEmits<{ (event: "update:visible", value: boolean): void }>();

const phase = ref<SessionPhase>("idle");
const result = ref<PlayResult | null>(null);
const errorMessage = ref("");
const playerError = ref("");
const errorStep = ref(1);
const startedAt = ref<number | null>(null);
const now = ref(Date.now());
const speed = ref(5);
const sessionToken = ref(0);
const diagnosticsOpen = ref(false);
const protocol = ref<StreamProtocol>("http-flv");
let timer: number | null = null;

const title = computed(() => props.channel?.alias?.trim() || props.channel?.name?.trim() || props.channel?.channelId || "未选择通道");
const isPtzCapable = computed(() => [1, 2, 4].includes(Number(props.channel?.ptzType || 0)));
const sessionStatus = computed(() => {
    if (phase.value === "requesting") return "建立会话";
    if (phase.value === "playing") return "播放中";
    if (phase.value === "stopping") return "正在停播";
    if (phase.value === "error") return "异常";
    return "待播放";
});
const sessionStatusClass = computed(() => ({
    active: phase.value === "playing",
    loading: phase.value === "requesting" || phase.value === "stopping",
    error: phase.value === "error"
}));
const activeUrl = computed(() => {
    if (!result.value) return "";
    if (protocol.value === "ws-flv") return result.value.wsflvUrl || result.value.httpFlvUrl || result.value.hlsUrl;
    if (protocol.value === "hls") return result.value.hlsUrl || result.value.httpFlvUrl || result.value.wsflvUrl;
    return result.value.httpFlvUrl || result.value.wsflvUrl || result.value.hlsUrl;
});
const phaseIndex = computed(() => {
    if (phase.value === "playing") return 4;
    if (phase.value === "error") return errorStep.value;
    if (phase.value === "requesting") return 0;
    return 0;
});
const phaseTitle = computed(() => {
    if (phase.value === "requesting") return "正在建立国标媒体会话";
    if (phase.value === "playing") return "媒体链路已就绪";
    if (phase.value === "error") return "点播未完成";
    if (phase.value === "stopping") return "正在释放媒体会话";
    return "准备点播";
});
const phaseHint = computed(() => {
    if (phase.value === "requesting") return "平台正在发送 SIP INVITE,等待设备应答并接收 RTP 媒体。";
    if (phase.value === "playing") return "设备已完成会话协商,ZLM 已注册媒体流。";
    if (phase.value === "error") return errorMessage.value || playerError.value || "请检查设备在线状态和媒体节点。";
    return "点播成功后,这里会显示从信令到媒体的完整链路。";
});
const elapsedText = computed(() => formatDuration(startedAt.value ? Math.max(0, now.value - startedAt.value) : 0));
const expiryText = computed(() => {
    if (!result.value?.expireAt) return "未提供";
    const seconds = Math.max(0, Math.ceil(result.value.expireAt - now.value / 1000));
    return seconds ? `${seconds} 秒后空闲释放` : "即将释放";
});

const steps = [
    { label: "SIP INVITE", hint: "发起信令" },
    { label: "设备应答", hint: "200 OK / ACK" },
    { label: "RTP 收流", hint: "等待媒体" },
    { label: "ZLM 就绪", hint: "注册流" },
    { label: "浏览器播放", hint: "已呈现" }
];

function formatDuration(ms: number) {
    const seconds = Math.floor(ms / 1000);
    return `${String(Math.floor(seconds / 60)).padStart(2, "0")}:${String(seconds % 60).padStart(2, "0")}`;
}

function formatTime(value?: number | null) {
    if (!value) return "-";
    return new Date(value * 1000).toLocaleTimeString("zh-CN", { hour12: false });
}

function copyValue(value: string | undefined, label: string) {
    if (!value) return;
    const copyTask = navigator.clipboard?.writeText(value);
    if (!copyTask) {
        Message.warning("当前浏览器不支持复制,请手动选择文本");
        return;
    }
    copyTask.then(
        () => Message.success(`${label}已复制`),
        () => Message.warning("复制失败,请手动选择文本")
    );
}

function beginTimer() {
    if (timer) return;
    timer = window.setInterval(() => { now.value = Date.now(); }, 1000);
}

function clearTimer() {
    if (timer) window.clearInterval(timer);
    timer = null;
}

async function cleanupSession(expectedToken: number) {
    const current = result.value;
    if (current?.streamId) {
        phase.value = "stopping";
        try {
            await stopPlay(current.streamId);
        } catch (error: any) {
            console.warn("停播请求异常", error);
        }
    }
    if (expectedToken !== sessionToken.value) return;
    result.value = null;
    startedAt.value = null;
    phase.value = "idle";
    playerError.value = "";
    clearTimer();
}

async function stopSession() {
    const token = ++sessionToken.value;
    await cleanupSession(token);
}

async function startSession() {
    const channel = props.channel;
    if (!channel || !props.visible) return;
    const token = ++sessionToken.value;
    await cleanupSession(token);
    if (!props.visible || !props.channel) return;
    if (token !== sessionToken.value) return;

    phase.value = "requesting";
    errorMessage.value = "";
    playerError.value = "";
    errorStep.value = 0;
    startedAt.value = Date.now();
    now.value = Date.now();
    beginTimer();
    try {
        const response = await startPlay(channel.deviceId, channel.channelId);
        if (token !== sessionToken.value || !props.visible) {
            if (response.data?.streamId) await stopPlay(response.data.streamId);
            return;
        }
        if (response.code !== 0 || !response.data) {
            throw new Error(response.message || "点播失败");
        }
        result.value = response.data;
        phase.value = "playing";
        protocol.value = response.data.httpFlvUrl ? "http-flv" : response.data.wsflvUrl ? "ws-flv" : "hls";
    } catch (error: any) {
        if (token !== sessionToken.value) return;
        phase.value = "error";
        errorStep.value = 0;
        errorMessage.value = error?.message || "点播失败,请检查设备在线状态";
    }
}

function handleClose() {
    emit("update:visible", false);
}

async function reconnect() {
    await startSession();
}

function showPendingControl(label: string) {
    Message.info(`${label}控制接口待接入,当前仅展示控制台操作面板`);
}

function sendPtz(action: string) {
    if (!isPtzCapable.value) return;
    showPendingControl(action);
}

function selectProtocol(value: string) {
    if (value === "http-flv" || value === "ws-flv" || value === "hls") protocol.value = value;
}

watch(
    [() => props.visible, () => props.channel?.id],
    ([visible]) => {
        if (visible && props.channel) void startSession();
        else void stopSession();
    },
    { immediate: true }
);
onBeforeUnmount(() => {
    clearTimer();
    void stopSession();
});
</script>

<template>
    <a-modal
        :visible="visible"
        :width="1180"
        :footer="false"
        :mask-closable="false"
        unmount-on-close
        modal-class="control-console-modal"
        @cancel="handleClose"
    >
        <template #title>
            <div class="console-title">
                <span class="console-title-icon"><RadioTower :size="17" /></span>
                <div class="console-title-copy">
                    <strong>设备控制台</strong>
                    <span>{{ title }} · {{ channel?.channelId || "未选择通道" }}</span>
                </div>
                <span class="console-status" :class="sessionStatusClass">
                    <span class="status-light"></span>{{ sessionStatus }}
                </span>
            </div>
        </template>

        <div class="console-layout">
            <section class="console-main">
                <div class="console-player-frame">
                    <PlayWindow v-if="result" :url="activeUrl" @error="playerError = $event; errorStep = 4; phase = 'error'" />
                    <div v-else class="console-player-empty">
                        <Loader2 v-if="phase === 'requesting'" :size="30" class="spin" />
                        <Video v-else :size="32" />
                        <strong>{{ phase === 'requesting' ? '正在等待设备媒体' : phase === 'error' ? '无法建立播放链路' : '准备播放' }}</strong>
                        <span>{{ phaseHint }}</span>
                    </div>
                    <div v-if="phase === 'error' && result" class="player-error-banner">
                        <AlertTriangle :size="15" /> {{ playerError || errorMessage }}
                    </div>
                </div>

                <div class="console-player-bar">
                    <div class="bar-context">
                        <span class="live-dot" :class="{ active: phase === 'playing' }"></span>
                        <span>{{ phaseTitle }}</span>
                        <span v-if="result" class="bar-divider"></span>
                        <span v-if="result" class="bar-muted">{{ elapsedText }}</span>
                    </div>
                    <div class="bar-actions">
                        <button class="console-btn" type="button" :disabled="phase === 'requesting' || phase === 'stopping'" @click="reconnect">
                            <RefreshCcw :size="14" />重连
                        </button>
                        <button class="console-btn danger" type="button" :disabled="!result && phase !== 'requesting'" @click="stopSession">
                            <Square :size="13" />停播
                        </button>
                    </div>
                </div>

                <div class="session-track">
                    <div class="session-track-head">
                        <div>
                            <span class="section-kicker">国标会话链路</span>
                            <strong>{{ phaseTitle }}</strong>
                        </div>
                        <span class="track-hint">{{ phaseHint }}</span>
                    </div>
                    <ol class="session-steps">
                        <li v-for="(step, index) in steps" :key="step.label" :class="{ done: index < phaseIndex, current: index === phaseIndex && phase !== 'error', failed: phase === 'error' && index === phaseIndex }">
                            <span class="step-icon"><Check v-if="index < phaseIndex" :size="12" /><AlertTriangle v-else-if="phase === 'error' && index === phaseIndex" :size="12" /><span v-else>{{ index + 1 }}</span></span>
                            <span class="step-copy"><strong>{{ step.label }}</strong><small>{{ step.hint }}</small></span>
                        </li>
                    </ol>
                </div>
            </section>

            <aside class="console-side">
                <section class="console-panel target-panel">
                    <div class="panel-heading"><span><Signal :size="14" />控制目标</span><span class="target-online" :class="{ offline: channel?.status !== 1 }">{{ channel?.status === 1 ? '通道在线' : '通道离线' }}</span></div>
                    <strong class="target-name">{{ title }}</strong>
                    <div class="target-id mono">{{ channel?.channelId || '-' }}</div>
                    <div class="target-grid">
                        <span>所属设备</span><strong class="mono">{{ channel?.deviceId || '-' }}</strong>
                        <span>厂商 / 型号</span><strong>{{ [channel?.manufacturer, channel?.model].filter(Boolean).join(' / ') || '未上报' }}</strong>
                        <span>媒体传输</span><strong>{{ channel?.streamTransport || '未上报' }}</strong>
                    </div>
                </section>

                <section class="console-panel ptz-panel">
                    <div class="panel-heading"><span><Gauge :size="14" />云台控制</span><span class="panel-capability">{{ isPtzCapable ? '已上报能力' : '固定镜头' }}</span></div>
                    <div class="ptz-pad" :class="{ disabled: !isPtzCapable }">
                        <button type="button" title="左上" :disabled="!isPtzCapable" @click="sendPtz('左上')"><ArrowUpLeft :size="17" /></button>
                        <button type="button" title="上" :disabled="!isPtzCapable" @click="sendPtz('上')"><ArrowUp :size="17" /></button>
                        <button type="button" title="右上" :disabled="!isPtzCapable" @click="sendPtz('右上')"><ArrowUpRight :size="17" /></button>
                        <button type="button" title="左" :disabled="!isPtzCapable" @click="sendPtz('左')"><ArrowLeft :size="17" /></button>
                        <button class="ptz-stop" type="button" title="停止" :disabled="!isPtzCapable" @click="sendPtz('停止')"><span></span></button>
                        <button type="button" title="右" :disabled="!isPtzCapable" @click="sendPtz('右')"><ArrowRight :size="17" /></button>
                        <button type="button" title="左下" :disabled="!isPtzCapable" @click="sendPtz('左下')"><ArrowDownLeft :size="17" /></button>
                        <button type="button" title="下" :disabled="!isPtzCapable" @click="sendPtz('下')"><ArrowDown :size="17" /></button>
                        <button type="button" title="右下" :disabled="!isPtzCapable" @click="sendPtz('右下')"><ArrowDownRight :size="17" /></button>
                    </div>
                    <div class="zoom-row" :class="{ disabled: !isPtzCapable }">
                        <button type="button" :disabled="!isPtzCapable" title="缩小" @click="sendPtz('缩小')"><ZoomOut :size="15" /></button>
                        <label><span>移动速度</span><input v-model.number="speed" type="range" min="1" max="10" :disabled="!isPtzCapable" /></label>
                        <button type="button" :disabled="!isPtzCapable" title="放大" @click="sendPtz('放大')"><ZoomIn :size="15" /></button>
                    </div>
                    <div v-if="!isPtzCapable" class="panel-hint"><Info :size="13" /> 当前通道未上报可动云台能力,控制区仅作占位。</div>
                </section>

                <section class="console-panel capability-panel">
                    <div class="panel-heading"><span><Wifi :size="14" />设备动作</span><span class="panel-capability">国标 DeviceControl</span></div>
                    <div class="capability-actions">
                        <button type="button" title="控制接口待接入" @click="showPendingControl('设备信息')"><RadioTower :size="14" /><span>设备信息</span><small>待接入</small></button>
                        <button type="button" title="控制接口待接入" @click="showPendingControl('强制关键帧')"><Video :size="14" /><span>请求关键帧</span><small>待接入</small></button>
                        <button type="button" title="控制接口待接入" @click="showPendingControl('远程重启')"><RefreshCcw :size="14" /><span>远程重启</span><small>待接入</small></button>
                    </div>
                </section>

                <details class="console-panel diagnostic-panel" :open="diagnosticsOpen" @toggle="diagnosticsOpen = !diagnosticsOpen">
                    <summary><span><Info :size="14" />流诊断</span><span>{{ result ? '已建立' : '等待会话' }}</span></summary>
                    <div class="diagnostic-grid">
                        <span>播放协议</span><strong>{{ protocol }}</strong>
                        <span>SSRC</span><strong class="mono">{{ result?.ssrc || '-' }} <button v-if="result?.ssrc" type="button" title="复制 SSRC" @click.stop="copyValue(result?.ssrc, 'SSRC')"><Copy :size="11" /></button></strong>
                        <span>流标识</span><strong class="mono">{{ result?.streamId || '-' }}</strong>
                        <span>ZLM App</span><strong>{{ result?.app || '-' }}</strong>
                        <span>预计释放</span><strong>{{ expiryText }}</strong>
                        <span>建立时间</span><strong>{{ formatTime(startedAt ? Math.floor(startedAt / 1000) : null) }}</strong>
                    </div>
                    <div class="protocol-switch" v-if="result">
                        <button v-for="item in [{ key: 'http-flv', label: 'HTTP-FLV', url: result.httpFlvUrl }, { key: 'ws-flv', label: 'WS-FLV', url: result.wsflvUrl }, { key: 'hls', label: 'HLS', url: result.hlsUrl }]" :key="item.key" type="button" :disabled="!item.url" :class="{ active: protocol === item.key }" @click="selectProtocol(item.key)">{{ item.label }}</button>
                    </div>
                    <div v-if="result" class="url-row"><span class="mono">{{ activeUrl }}</span><button type="button" title="复制播放地址" @click="copyValue(activeUrl, '播放地址')"><Copy :size="12" /></button></div>
                </details>
            </aside>
        </div>
    </a-modal>
</template>

<style scoped lang="scss">
.console-title { display: flex; align-items: center; gap: 9px; min-width: 0; }
.console-title-icon { display: inline-grid; place-items: center; width: 30px; height: 30px; color: var(--uvp-brand); background: var(--uvp-brand-soft); border-radius: 8px; }
.console-title-copy { display: grid; gap: 2px; min-width: 0; }
.console-title-copy strong { color: var(--uvp-text-primary); font-size: 14px; }
.console-title-copy span { overflow: hidden; color: var(--uvp-text-tertiary); font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }
.console-status { display: inline-flex; align-items: center; gap: 5px; margin-left: auto; padding: 4px 9px; color: var(--uvp-text-tertiary); background: var(--uvp-list-toolbar-bg); border: 1px solid var(--uvp-panel-border); border-radius: 999px; font-size: 11px; white-space: nowrap; }
.console-status.active { color: var(--uvp-brand-cyan); background: color-mix(in srgb, var(--uvp-brand-cyan) 10%, transparent); border-color: color-mix(in srgb, var(--uvp-brand-cyan) 26%, transparent); }
.console-status.error { color: #ef4444; background: color-mix(in srgb, #ef4444 9%, transparent); border-color: color-mix(in srgb, #ef4444 24%, transparent); }
.status-light, .live-dot { width: 6px; height: 6px; background: var(--uvp-text-tertiary); border-radius: 50%; }
.console-status.active .status-light, .live-dot.active { background: var(--uvp-brand-cyan); box-shadow: 0 0 0 3px color-mix(in srgb, var(--uvp-brand-cyan) 18%, transparent); }
.console-status.loading .status-light { background: var(--uvp-warning); }
.console-layout { display: grid; grid-template-columns: minmax(0, 1fr) 316px; gap: 14px; min-height: 0; }
.console-main { display: grid; gap: 10px; min-width: 0; align-content: start; }
.console-player-frame { position: relative; overflow: hidden; min-width: 0; background: #08111f; border: 1px solid var(--uvp-panel-border); border-radius: 12px; }
.console-player-frame :deep(.play-window) { border: 0; border-radius: 0; }
.console-player-empty { display: grid; place-items: center; align-content: center; gap: 8px; min-height: 438px; padding: 30px; color: var(--uvp-text-tertiary); text-align: center; }
.console-player-empty strong { color: #dbeafe; font-size: 14px; }
.console-player-empty span { max-width: 420px; color: #94a3b8; font-size: 12px; line-height: 1.5; }
.player-error-banner { position: absolute; right: 12px; bottom: 12px; left: 12px; display: flex; align-items: center; gap: 6px; padding: 9px 11px; color: #fecaca; background: rgb(127 29 29 / 86%); border: 1px solid rgb(248 113 113 / 34%); border-radius: 8px; font-size: 12px; }
.console-player-bar { display: flex; align-items: center; justify-content: space-between; gap: 10px; min-height: 38px; padding: 0 4px; }
.bar-context, .bar-actions { display: inline-flex; align-items: center; gap: 8px; color: var(--uvp-text-secondary); font-size: 12px; }
.bar-muted, .track-hint { color: var(--uvp-text-tertiary); }
.bar-divider { width: 1px; height: 14px; background: var(--uvp-panel-border); }
.console-btn { display: inline-flex; align-items: center; gap: 5px; height: 28px; padding: 0 9px; color: var(--uvp-text-secondary); background: var(--uvp-search-secondary-btn-bg); border: 1px solid var(--uvp-search-secondary-btn-border); border-radius: 7px; cursor: pointer; font-size: 12px; }
.console-btn:hover:not(:disabled) { color: var(--uvp-brand); border-color: var(--uvp-brand); }
.console-btn.danger { color: #ef4444; }
.console-btn:disabled { cursor: not-allowed; opacity: 0.45; }
.session-track, .console-panel { background: var(--uvp-panel-bg); border: 1px solid var(--uvp-panel-border); border-radius: 10px; }
.session-track { padding: 12px; }
.session-track-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; margin-bottom: 13px; }
.session-track-head > div { display: grid; gap: 3px; }
.section-kicker { color: var(--uvp-text-tertiary); font-size: 10px; letter-spacing: 0.04em; }
.session-track-head strong { color: var(--uvp-text-primary); font-size: 13px; }
.track-hint { max-width: 52%; text-align: right; font-size: 11px; line-height: 1.5; }
.session-steps { display: grid; grid-template-columns: repeat(5, 1fr); gap: 5px; padding: 0; margin: 0; list-style: none; }
.session-steps li { position: relative; display: grid; gap: 6px; min-width: 0; }
.session-steps li:not(:last-child)::after { position: absolute; top: 12px; right: -5px; left: 28px; height: 1px; content: ""; background: var(--uvp-panel-border); }
.session-steps li.done:not(:last-child)::after { background: color-mix(in srgb, var(--uvp-brand-cyan) 60%, var(--uvp-panel-border)); }
.step-icon { position: relative; z-index: 1; display: grid; place-items: center; width: 24px; height: 24px; color: var(--uvp-text-tertiary); background: var(--uvp-list-toolbar-bg); border: 1px solid var(--uvp-panel-border); border-radius: 50%; font-size: 10px; }
.session-steps li.current .step-icon { color: var(--uvp-brand); background: var(--uvp-brand-soft); border-color: color-mix(in srgb, var(--uvp-brand) 34%, var(--uvp-panel-border)); }
.session-steps li.done .step-icon { color: var(--uvp-brand-cyan); background: color-mix(in srgb, var(--uvp-brand-cyan) 12%, transparent); border-color: color-mix(in srgb, var(--uvp-brand-cyan) 30%, transparent); }
.session-steps li.failed .step-icon { color: #ef4444; background: color-mix(in srgb, #ef4444 10%, transparent); border-color: color-mix(in srgb, #ef4444 26%, transparent); }
.step-copy { display: grid; gap: 2px; min-width: 0; }
.step-copy strong { overflow: hidden; color: var(--uvp-text-secondary); font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }
.step-copy small { color: var(--uvp-text-tertiary); font-size: 10px; }
.console-side { display: grid; align-content: start; gap: 10px; max-height: 694px; overflow-y: auto; padding-right: 2px; }
.console-panel { padding: 12px; }
.panel-heading { display: flex; align-items: center; justify-content: space-between; gap: 8px; margin-bottom: 10px; color: var(--uvp-text-tertiary); font-size: 11px; }
.panel-heading > span:first-child, .diagnostic-panel summary > span:first-child { display: inline-flex; align-items: center; gap: 5px; color: var(--uvp-text-secondary); font-weight: 600; }
.target-online, .panel-capability { color: var(--uvp-brand-cyan); font-size: 10px; }
.target-online.offline { color: var(--uvp-text-tertiary); }
.target-name { display: block; overflow: hidden; color: var(--uvp-text-primary); font-size: 15px; text-overflow: ellipsis; white-space: nowrap; }
.target-id { margin-top: 3px; color: var(--uvp-text-tertiary); font-size: 11px; }
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
.target-grid, .diagnostic-grid { display: grid; grid-template-columns: 72px minmax(0, 1fr); gap: 6px 8px; margin-top: 12px; font-size: 11px; }
.target-grid span, .diagnostic-grid span { color: var(--uvp-text-tertiary); }
.target-grid strong, .diagnostic-grid strong { min-width: 0; overflow: hidden; color: var(--uvp-text-secondary); font-weight: 500; text-overflow: ellipsis; white-space: nowrap; }
.ptz-pad { display: grid; grid-template-columns: repeat(3, 1fr); gap: 5px; max-width: 180px; margin: 0 auto 10px; }
.ptz-pad button, .zoom-row > button { display: grid; place-items: center; width: 42px; height: 32px; padding: 0; margin: auto; color: var(--uvp-text-secondary); background: var(--uvp-list-toolbar-bg); border: 1px solid var(--uvp-panel-border); border-radius: 7px; cursor: pointer; }
.ptz-pad button:hover:not(:disabled), .zoom-row > button:hover:not(:disabled) { color: var(--uvp-brand); border-color: var(--uvp-brand); }
.ptz-pad button:disabled, .zoom-row > button:disabled { cursor: not-allowed; opacity: 0.38; }
.ptz-stop span { width: 9px; height: 9px; background: #ef4444; border-radius: 2px; }
.zoom-row { display: grid; grid-template-columns: 42px minmax(0, 1fr) 42px; align-items: center; gap: 8px; }
.zoom-row label { display: grid; gap: 3px; color: var(--uvp-text-tertiary); font-size: 10px; text-align: center; }
.zoom-row input { width: 100%; accent-color: var(--uvp-brand); }
.panel-hint { display: flex; align-items: flex-start; gap: 5px; margin-top: 10px; color: var(--uvp-text-tertiary); font-size: 10px; line-height: 1.45; }
.capability-actions { display: grid; gap: 5px; }
.capability-actions button { display: grid; grid-template-columns: 18px minmax(0, 1fr) auto; align-items: center; gap: 7px; min-height: 30px; padding: 0 7px; color: var(--uvp-text-tertiary); background: var(--uvp-list-toolbar-bg); border: 1px solid var(--uvp-panel-border); border-radius: 7px; cursor: pointer; text-align: left; }
.capability-actions button:hover { color: var(--uvp-brand); border-color: color-mix(in srgb, var(--uvp-brand) 30%, var(--uvp-panel-border)); }
.capability-actions span { color: var(--uvp-text-secondary); font-size: 11px; }
.capability-actions small { color: var(--uvp-text-tertiary); font-size: 10px; }
.diagnostic-panel { padding: 0; overflow: hidden; }
.diagnostic-panel summary { display: flex; align-items: center; justify-content: space-between; min-height: 38px; padding: 0 12px; color: var(--uvp-text-tertiary); cursor: pointer; list-style: none; font-size: 10px; }
.diagnostic-panel summary::-webkit-details-marker { display: none; }
.diagnostic-panel[open] summary { border-bottom: 1px solid var(--uvp-panel-border); }
.diagnostic-grid { padding: 0 12px; }
.diagnostic-grid button, .url-row button { display: inline-grid; place-items: center; width: 18px; height: 18px; padding: 0; color: var(--uvp-text-tertiary); background: transparent; border: 0; cursor: pointer; vertical-align: middle; }
.diagnostic-grid button:hover, .url-row button:hover { color: var(--uvp-brand); }
.protocol-switch { display: flex; gap: 4px; padding: 10px 12px 0; }
.protocol-switch button { height: 24px; padding: 0 7px; color: var(--uvp-text-tertiary); background: transparent; border: 1px solid var(--uvp-panel-border); border-radius: 5px; cursor: pointer; font-size: 10px; }
.protocol-switch button.active { color: var(--uvp-brand); background: var(--uvp-brand-soft); border-color: color-mix(in srgb, var(--uvp-brand) 30%, var(--uvp-panel-border)); }
.protocol-switch button:disabled { cursor: not-allowed; opacity: 0.4; }
.url-row { display: flex; align-items: center; gap: 4px; padding: 9px 12px 12px; color: var(--uvp-text-tertiary); font-size: 10px; }
.url-row span { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.url-row button { flex: 0 0 auto; }
@media (max-width: 900px) {
    .console-layout { grid-template-columns: 1fr; }
    .console-side { max-height: none; overflow: visible; }
    .console-player-empty { min-height: 280px; }
}
@media (max-width: 600px) {
    .session-steps { grid-template-columns: repeat(2, 1fr); gap: 10px; }
    .session-steps li:not(:last-child)::after { display: none; }
    .track-hint { display: none; }
    .console-player-bar { align-items: flex-start; flex-direction: column; }
}
</style>
