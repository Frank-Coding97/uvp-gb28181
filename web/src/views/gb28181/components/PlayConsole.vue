<script setup lang="ts">
/**
 * PlayConsole - 专业播放控制台(静态原型 v1)
 *
 * 设计定位:平台核心功能弹窗,承担点播 + 云台 + 探针 + 诊断 + 录制的一体化操作台
 *
 * 静态版说明:所有数据均为 mock,真实接口在下一阶段倒推
 *   - stream/monitor: 每 2s 更新码率/观众/丢包 → 现在用假数据
 *   - ptz/*         : 云台命令 → 现在只弹提示
 *   - probe/*       : 视频探针 → 现在用假数据
 *   - record/*      : 录制 → 现在只切 UI 状态
 *
 * 视觉语言:深色为主,青色作强调,毛玻璃卡片,状态用色带 + 脉冲呼吸
 * 布局:左侧视频区(65%) + 右侧功能标签(35%,可折叠)
 */
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import {
    Activity,
    AlertTriangle,
    CheckCircle2,
    Circle,
    Compass,
    Copy,
    Crosshair,
    Focus as FocusIcon,
    Gauge,
    Hash,
    Home,
    Info,
    Loader2,
    Mic,
    Move3d,
    Navigation,
    Pause,
    Play,
    RadioTower,
    RefreshCcw,
    Route,
    Settings,
    ShieldCheck,
    Signal,
    SlidersHorizontal,
    Square,
    Target,
    Trash2,
    Video,
    ZoomIn,
    ZoomOut,
} from "@lucide/vue";

/* ────────────────────────── Props / Emits ────────────────────────── */

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

const props = defineProps<{
    visible: boolean;
    channel: PlaybackChannel | null;
}>();
const emit = defineEmits<{ (event: "update:visible", value: boolean): void }>();

/* ────────────────────────── 会话状态 ────────────────────────── */

type SessionPhase =
    | "idle"          // 待播放
    | "requesting"    // 建立会话中
    | "playing"       // 播放中
    | "paused"        // 暂停
    | "error"         // 异常
    | "stopping";     // 停播中

const phase = ref<SessionPhase>("idle");
const errorMessage = ref("");
const startedAt = ref<number | null>(null);
const now = ref(Date.now());
let timer: number | null = null;

/* ────────────────────────── Tab 切换 ────────────────────────── */

type TabKey = "ptz" | "probe" | "advanced";
const activeTab = ref<TabKey>("ptz");
const sideCollapsed = ref(false);

const tabs: Array<{ key: TabKey; label: string; icon: any; description: string }> = [
    { key: "ptz", label: "云台控制", icon: Compass, description: "GB28181-2022 全能力" },
    { key: "probe", label: "视频探针", icon: Activity, description: "逐帧采样 · 时间戳" },
    { key: "advanced", label: "高级", icon: Settings, description: "关键帧 · 布防 · 重启" },
];

/* ────────────────────────── 视频区状态 ────────────────────────── */

type StreamProtocol = "ws-flv" | "http-flv" | "hls" | "webrtc" | "rtmp" | "rtsp";
const protocol = ref<StreamProtocol>("ws-flv");
const showOverlay = ref(true); // 显示画面 HUD

// 语音对讲状态
type TalkState = "idle" | "talking";
const talkState = ref<TalkState>("idle");
const isAudioCapable = computed(() => props.channel?.status === 1); // mock: 在线设备支持

// Mock 多协议 URL(后续对接后端 /api/gb28181/devices/play 返回)
const protocolUrls = ref({
    "ws-flv": "ws://192.168.10.220:18080/rtp/0064179006.live.flv",
    "http-flv": "http://192.168.10.220:18080/rtp/0064179006.live.flv",
    "hls": "http://192.168.10.220:18080/rtp/0064179006/hls.m3u8",
    "webrtc": "webrtc://192.168.10.220:18080/rtp/0064179006",
    "rtmp": "rtmp://192.168.10.220:18080/rtp/0064179006",
    "rtsp": "rtsp://192.168.10.220:18080/rtp/0064179006",
});

const protocolOptions: Array<{ value: StreamProtocol; label: string; desc: string }> = [
    { value: "ws-flv", label: "WS-FLV", desc: "延迟最低,适合实时监控" },
    { value: "http-flv", label: "HTTP-FLV", desc: "兼容性好,延迟较低" },
    { value: "hls", label: "HLS", desc: "兼容性最佳,延迟较高" },
    { value: "webrtc", label: "WebRTC", desc: "超低延迟,需 HTTPS" },
    { value: "rtmp", label: "RTMP", desc: "传统直播协议" },
    { value: "rtsp", label: "RTSP", desc: "监控设备标准" },
];

const currentProtocolUrl = computed(() => protocolUrls.value[protocol.value]);

const title = computed(
    () => props.channel?.alias?.trim() || props.channel?.name?.trim() || props.channel?.channelId || "未选择通道",
);

const phaseHint = computed(() => {
    if (phase.value === "requesting") return "正在向设备发送 INVITE,等待 RTP 媒体建立";
    if (phase.value === "playing") return "媒体链路正常,画面已实时呈现";
    if (phase.value === "paused") return "已暂停,可随时恢复";
    if (phase.value === "error") return errorMessage.value || "无法建立播放链路";
    if (phase.value === "stopping") return "正在释放会话资源";
    return "点击左侧通道开始播放";
});

const sessionStatusText = computed(() => {
    if (phase.value === "requesting") return "建立中";
    if (phase.value === "playing") return "播放中";
    if (phase.value === "paused") return "已暂停";
    if (phase.value === "stopping") return "停播中";
    if (phase.value === "error") return "异常";
    return "待播放";
});

const sessionStatusClass = computed(() => ({
    active: phase.value === "playing",
    loading: phase.value === "requesting" || phase.value === "stopping",
    error: phase.value === "error",
    paused: phase.value === "paused",
}));

const elapsedText = computed(() =>
    startedAt.value ? formatDuration(Math.max(0, now.value - startedAt.value)) : "00:00",
);

/* ────────────────────────── 云台状态(mock) ────────────────────────── */

const ptzMode = ref<"speed" | "precise">("speed"); // 速度模式 / 精准模式
const moveSpeed = ref(6); // 1-10 步进,转发时 * 25 得 GB28181 1-255
const focusMode = ref<"auto" | "manual">("auto");
const irisMode = ref<"auto" | "manual">("auto");
type JoystickDirection = "左上" | "上" | "右上" | "左" | "右" | "左下" | "下" | "右下";
const joystickDragging = ref(false);
const joystickPointerId = ref<number | null>(null);
const joystickDirection = ref<JoystickDirection | "">("");
const joystickOffsetX = ref(0);
const joystickOffsetY = ref(0);
const joystickHandleStyle = computed(() => ({
    transform: `translate(calc(-50% + ${joystickOffsetX.value}px), calc(-50% + ${joystickOffsetY.value}px))`,
}));

// 精准 PTZ(2022)
const precisePan = ref(180);   // 0-360
const preciseTilt = ref(0);    // -90 to 90
const preciseZoom = ref(1);    // 1-32x

// 预置位(1-255, mock 8 个)
type Preset = { id: number; name: string; setAt?: string };
const presets = ref<Preset[]>([
    { id: 1, name: "大门朝向", setAt: "2026-07-20 09:12" },
    { id: 2, name: "停车场", setAt: "2026-07-20 09:14" },
    { id: 3, name: "值班室", setAt: "2026-07-21 14:03" },
    { id: 4, name: "监视窗口", setAt: "2026-07-21 14:08" },
]);
const activePresetId = ref<number | null>(null);
const newPresetName = ref("");

// 巡航轨迹(2022 CruiseTrackListQuery)
type CruiseTrack = { id: number; name: string; enabled: boolean; presets: number[]; dwellSec: number };
const cruiseTracks = ref<CruiseTrack[]>([
    { id: 1, name: "白天巡航", enabled: true, presets: [1, 2, 3], dwellSec: 8 },
    { id: 2, name: "夜间安防", enabled: false, presets: [3, 4], dwellSec: 15 },
]);
const activeCruiseId = ref<number | null>(null);
const cruiseState = ref<"stopped" | "running" | "paused">("stopped");

// 看守位(2022 HomePositionQuery)
const homePosition = ref<{ enabled: boolean; presetId?: number; delaySec: number }>({
    enabled: true,
    presetId: 1,
    delaySec: 300,
});

/* ────────────────────────── 流信息(mock) ────────────────────────── */

const streamInfo = ref({
    streamId: "0102030405060708",
    ssrc: "0102030405",
    nodeId: 1,
    nodeName: "ZLM-Node-1",
    nodeHost: "192.168.10.220",
    videoCodec: "H.264",
    audioCodec: "G.711A",
    audioSampleRate: 8000,
    resolution: "1920×1080",
    videoFps: 25,
    urls: {
        wsflv: "ws://192.168.10.220/rtp/0102030405.live.flv",
        httpflv: "http://192.168.10.220/rtp/0102030405.live.flv",
        hls: "http://192.168.10.220/rtp/0102030405/hls.m3u8",
        webrtc: "webrtc://192.168.10.220/rtp/0102030405",
    },
});

/* ────────────────────────── 流概况 + 视频探针(mock) ────────────────────────── */

const liveMetrics = ref({ bitrate: 0, loss: 0 });
const readerCount = ref(3);

type ProbeState = "idle" | "sampling" | "complete";
const probeState = ref<ProbeState>("idle");
const probeRemainingMs = ref(3000);
const probeFinishedAt = ref("");
let probeCountdownTimer: number | null = null;
let probeFinishTimer: number | null = null;

const probeStatusText = computed(() => {
    if (probeState.value === "sampling") return "采样中";
    if (probeState.value === "complete") return "检测完成";
    return "等待检测";
});
const probeButtonText = computed(() => {
    if (probeState.value === "sampling") return `检测中 ${(probeRemainingMs.value / 1000).toFixed(1)}s`;
    if (probeState.value === "complete") return "重新检测 3 秒";
    return "开始 3 秒检测";
});
const probeTimeline = Array.from({ length: 32 }, (_, index) => ({
    type: index % 3 === 0 ? "video" : "audio",
    keyFrame: index === 0 || index === 24,
    height: index % 3 === 0 ? 52 + (index * 7) % 36 : 22 + (index * 5) % 18,
}));

function clearProbeTimers() {
    if (probeCountdownTimer) window.clearInterval(probeCountdownTimer);
    if (probeFinishTimer) window.clearTimeout(probeFinishTimer);
    probeCountdownTimer = null;
    probeFinishTimer = null;
}

function startProbe() {
    if (phase.value !== "playing" || probeState.value === "sampling") return;
    clearProbeTimers();
    probeState.value = "sampling";
    probeRemainingMs.value = 3000;
    probeCountdownTimer = window.setInterval(() => {
        probeRemainingMs.value = Math.max(0, probeRemainingMs.value - 100);
    }, 100);
    probeFinishTimer = window.setTimeout(() => {
        clearProbeTimers();
        probeRemainingMs.value = 0;
        probeState.value = "complete";
        probeFinishedAt.value = new Date().toLocaleTimeString("zh-CN", { hour12: false });
    }, 3000);
}

/* ────────────────────────── 通用工具 ────────────────────────── */

function formatDuration(ms: number) {
    const s = Math.floor(ms / 1000);
    return `${String(Math.floor(s / 60)).padStart(2, "0")}:${String(s % 60).padStart(2, "0")}`;
}

function beginTimer() {
    if (timer) return;
    timer = window.setInterval(() => {
        now.value = Date.now();
        if (phase.value === "playing") {
            // 模拟探针数据流入(每秒一次采样)
            const bitrate = 1900 + Math.floor(Math.random() * 250);
            const loss = +(Math.random() * 1.2).toFixed(2);
            liveMetrics.value = { bitrate, loss };
        }
    }, 1000);
}

function clearTimer() {
    if (timer) window.clearInterval(timer);
    timer = null;
    liveMetrics.value = { bitrate: 0, loss: 0 };
}

/* ────────────────────────── Mock 会话流程 ────────────────────────── */

async function mockStart() {
    phase.value = "requesting";
    errorMessage.value = "";
    startedAt.value = Date.now();
    now.value = Date.now();
    beginTimer();

    // 模拟 1.5s 后播放成功
    await new Promise((r) => setTimeout(r, 1500));
    if (!props.visible) return;
    phase.value = "playing";
}

function stopSession() {
    phase.value = "idle";
    startedAt.value = null;
    errorMessage.value = "";
    clearProbeTimers();
    probeState.value = "idle";
    probeRemainingMs.value = 3000;
    clearTimer();
}

function reconnect() {
    stopSession();
    void mockStart();
}

function handleClose() {
    endJoystick();
    emit("update:visible", false);
}

/* ────────────────────────── 云台指令(mock) ────────────────────────── */

function sendPtz(action: string) {
    Message.info(`[Mock] 云台指令:${action} · 速度 ${moveSpeed.value}`);
}

const joystickDirectionMap: Record<string, JoystickDirection> = {
    ArrowUp: "上",
    ArrowRight: "右",
    ArrowDown: "下",
    ArrowLeft: "左",
};
const joystickDiagonalDirection = (dx: number, dy: number): JoystickDirection => {
    const angle = (Math.atan2(dx, -dy) * 180 / Math.PI + 360) % 360;
    if (angle < 22.5 || angle >= 337.5) return "上";
    if (angle < 67.5) return "右上";
    if (angle < 112.5) return "右";
    if (angle < 157.5) return "右下";
    if (angle < 202.5) return "下";
    if (angle < 247.5) return "左下";
    if (angle < 292.5) return "左";
    return "左上";
};
function stopJoystickMotion() {
    if (joystickDirection.value) sendPtz("停止");
    joystickDirection.value = "";
}
function resetJoystickPosition() {
    joystickDragging.value = false;
    joystickPointerId.value = null;
    joystickOffsetX.value = 0;
    joystickOffsetY.value = 0;
}
function updateJoystick(event: PointerEvent, stage: HTMLElement) {
    const rect = stage.getBoundingClientRect();
    const centerX = rect.left + rect.width / 2;
    const centerY = rect.top + rect.height / 2;
    const maxRadius = Math.max(12, Math.min(rect.width, rect.height) / 2 - 36);
    let dx = event.clientX - centerX;
    let dy = event.clientY - centerY;
    const distance = Math.hypot(dx, dy);
    const clampedDistance = Math.min(distance, maxRadius);
    if (distance > maxRadius) {
        dx = dx / distance * maxRadius;
        dy = dy / distance * maxRadius;
    }
    joystickOffsetX.value = dx;
    joystickOffsetY.value = dy;
    if (clampedDistance < 8) {
        stopJoystickMotion();
        return;
    }
    const direction = joystickDiagonalDirection(dx, dy);
    if (direction !== joystickDirection.value) {
        joystickDirection.value = direction;
        sendPtz(direction);
    }
}
function startJoystick(event: PointerEvent) {
    const stage = event.currentTarget as HTMLElement;
    joystickDragging.value = true;
    joystickPointerId.value = event.pointerId;
    stage.setPointerCapture?.(event.pointerId);
    updateJoystick(event, stage);
}
function moveJoystick(event: PointerEvent) {
    if (!joystickDragging.value || joystickPointerId.value !== event.pointerId) return;
    updateJoystick(event, event.currentTarget as HTMLElement);
}
function endJoystick(event?: PointerEvent) {
    if (event && joystickPointerId.value !== event.pointerId) return;
    stopJoystickMotion();
    if (event) {
        const stage = event.currentTarget as HTMLElement;
        if (stage.hasPointerCapture?.(event.pointerId)) stage.releasePointerCapture(event.pointerId);
    }
    resetJoystickPosition();
}
function handleJoystickKeydown(event: KeyboardEvent) {
    const direction = joystickDirectionMap[event.key];
    if (!direction) return;
    event.preventDefault();
    joystickDragging.value = true;
    if (direction !== joystickDirection.value) {
        joystickDirection.value = direction;
        sendPtz(direction);
    }
}
function handleJoystickKeyup(event: KeyboardEvent) {
    if (!joystickDirectionMap[event.key]) return;
    event.preventDefault();
    endJoystick();
}

function sendPrecise() {
    Message.success(
        `[Mock] 精准 PTZ · Pan=${precisePan.value}° Tilt=${preciseTilt.value}° Zoom=${preciseZoom.value}×`,
    );
}

function callPreset(id: number) {
    activePresetId.value = id;
    Message.info(`[Mock] 调用预置位 #${id}`);
}
function setPreset() {
    if (!newPresetName.value.trim()) {
        Message.warning("请输入预置位名称");
        return;
    }
    const nextId = presets.value.length ? Math.max(...presets.value.map((p) => p.id)) + 1 : 1;
    presets.value.push({
        id: nextId,
        name: newPresetName.value.trim(),
        setAt: new Date().toLocaleString("zh-CN", { hour12: false }),
    });
    Message.success(`已设置预置位 #${nextId}`);
    newPresetName.value = "";
}
function deletePreset(id: number) {
    presets.value = presets.value.filter((p) => p.id !== id);
    if (activePresetId.value === id) activePresetId.value = null;
    Message.success(`预置位 #${id} 已删除`);
}

function toggleCruise(id: number) {
    activeCruiseId.value = id;
    cruiseState.value = cruiseState.value === "running" ? "paused" : "running";
    Message.info(`[Mock] 巡航 #${id} → ${cruiseState.value}`);
}
function stopCruise() {
    cruiseState.value = "stopped";
    activeCruiseId.value = null;
    Message.info("[Mock] 巡航已停止");
}

function saveHomePosition() {
    if (!homePosition.value.enabled) {
        Message.success("[Mock] 看守位已关闭");
        return;
    }
    Message.success(`[Mock] 看守位已保存 · 预置位 #${homePosition.value.presetId} · ${homePosition.value.delaySec}s 后回位`);
}

/* ────────────────────────── 语音对讲(mock) ────────────────────────── */

function startTalk() {
    if (talkState.value === "talking") return;
    if (!isAudioCapable.value) {
        Message.warning("设备离线或不支持语音对讲");
        return;
    }
    talkState.value = "talking";
    Message.info("[Mock] 语音对讲已开始 · 按住说话");
}

function stopTalk() {
    if (talkState.value === "idle") return;
    talkState.value = "idle";
    Message.info("[Mock] 语音对讲已结束");
}

function switchProtocol(proto: StreamProtocol) {
    if (proto === protocol.value) return;
    protocol.value = proto;
    Message.success(`[Mock] 已切换到 ${protocolOptions.find(p => p.value === proto)?.label} · ${currentProtocolUrl.value}`);
    // 真实场景:销毁旧播放器,用新 URL 重建
}

function copyUrl() {
    navigator.clipboard.writeText(currentProtocolUrl.value).then(() => {
        Message.success(`[Mock] 已复制 ${protocol.value.toUpperCase()} 地址`);
    }).catch(() => {
        Message.error("复制失败");
    });
}


/* ────────────────────────── 生命周期 ────────────────────────── */

watch(
    [() => props.visible, () => props.channel?.id],
    ([visible]) => {
        if (visible && props.channel) void mockStart();
        else { endJoystick(); stopSession(); }
    },
    { immediate: true },
);

onBeforeUnmount(() => {
    endJoystick();
    clearProbeTimers();
    clearTimer();
    stopSession();
});
</script>

<template>
    <a-modal
        :visible="visible"
        width="min(1280px, calc(100vw - 24px))"
        :footer="false"
        :mask-closable="false"
        unmount-on-close
        modal-class="play-console-modal"
        @cancel="handleClose"
    >
        <template #title>
            <div class="console-title">
                <span class="title-icon"><RadioTower :size="18" /></span>
                <div class="title-text">
                    <strong>播放控制台</strong>
                    <span>{{ title }} · {{ channel?.channelId || "未选择通道" }}</span>
                </div>
                <span class="session-badge" :class="sessionStatusClass">
                    <span class="dot"></span>{{ sessionStatusText }}
                </span>
            </div>
        </template>

        <!-- CONSOLE_BODY_PLACEHOLDER -->
        <div class="console-body">
            <!-- 主区(视频 + 控制条 + 会话链路) -->
            <section class="stage" :class="{ 'stage-wide': sideCollapsed }">
                <!-- 视频画面 -->
                <div class="video-frame">
                    <!-- 播放器占位:静态阶段用渐变 + HUD 模拟 -->
                    <div class="video-canvas">
                        <template v-if="phase === 'playing' || phase === 'paused'">
                            <div class="mock-live" />
                            <div v-if="showOverlay" class="hud">
                                <div class="hud-top">
                                    <span class="hud-tag live"><span class="dot" />LIVE</span>
                                    <span class="hud-tag">{{ streamInfo.resolution }} · {{ streamInfo.videoFps }}fps</span>
                                    <span class="hud-tag">{{ streamInfo.videoCodec }} / {{ streamInfo.audioCodec }}</span>
                                    <span class="hud-tag ghost">{{ streamInfo.nodeName }}</span>
                                </div>
                            </div>
                            <div v-if="phase === 'paused'" class="paused-mask">
                                <Pause :size="42" /><span>已暂停</span>
                            </div>
                        </template>
                        <template v-else-if="phase === 'requesting'">
                            <div class="placeholder">
                                <div class="pulse"><Loader2 :size="42" class="spin" /></div>
                                <strong>正在建立国标媒体会话</strong>
                                <span>{{ phaseHint }}</span>
                            </div>
                        </template>
                        <template v-else-if="phase === 'error'">
                            <div class="placeholder error">
                                <AlertTriangle :size="42" />
                                <strong>点播未完成</strong>
                                <span>{{ phaseHint }}</span>
                                <button class="btn-primary" @click="reconnect"><RefreshCcw :size="14" />重试</button>
                            </div>
                        </template>
                        <template v-else>
                            <div class="placeholder">
                                <div class="pulse idle"><Video :size="42" /></div>
                                <strong>准备就绪</strong>
                                <span>{{ phaseHint }}</span>
                            </div>
                        </template>
                    </div>

                    <!-- 多协议切换器(底部) -->
                    <div class="protocol-switcher">
                        <div class="switcher-left">
                            <span class="kicker">播放协议</span>
                            <a-select
                                v-model="protocol"
                                :style="{ width: '160px' }"
                                size="small"
                                @change="switchProtocol"
                            >
                                <a-option
                                    v-for="opt in protocolOptions"
                                    :key="opt.value"
                                    :value="opt.value"
                                    :label="opt.label"
                                >
                                    <div class="protocol-option">
                                        <strong>{{ opt.label }}</strong>
                                        <span class="desc">{{ opt.desc }}</span>
                                    </div>
                                </a-option>
                            </a-select>
                        </div>
                        <div class="switcher-right">
                            <button
                                v-for="opt in protocolOptions.slice(0, 3)"
                                :key="opt.value"
                                class="proto-btn"
                                :class="{ active: protocol === opt.value }"
                                @click="switchProtocol(opt.value)"
                            >
                                {{ opt.label }}
                            </button>
                        </div>
                        <button class="copy-url" title="复制播放地址" @click="copyUrl">
                            <Copy :size="13" />
                        </button>
                    </div>

                </div>

                <!-- 流信息独立区域:与播放器解耦,承载 ZL 实时指标 -->
                <div v-if="phase === 'playing'" class="stream-info-bar">
                    <div class="stream-overview-header">
                        <strong>概况</strong>
                        <button class="stream-refresh" title="刷新流信息" @click="Message.info('[Mock] 流信息已刷新')">
                            <RefreshCcw :size="13" />
                        </button>
                    </div>
                    <div class="stream-overview-metrics">
                        <div class="overview-metric">
                            <span class="stream-label">观众人数</span>
                            <strong class="stream-value">{{ readerCount }}</strong>
                        </div>
                        <div class="overview-metric">
                            <span class="stream-label">网络</span>
                            <strong class="stream-value">{{ liveMetrics.bitrate }} kbps</strong>
                        </div>
                        <div class="overview-metric">
                            <span class="stream-label">持续时间</span>
                            <strong class="stream-value mono">{{ elapsedText }}</strong>
                        </div>
                    </div>
                    <div class="stream-detail-columns">
                        <section class="stream-detail">
                            <h4><Video :size="14" />视频信息</h4>
                            <div class="stream-detail-grid">
                                <div><span class="stream-label">编码</span><strong class="stream-value">{{ streamInfo.videoCodec }}</strong></div>
                                <div><span class="stream-label">分辨率</span><strong class="stream-value">{{ streamInfo.resolution }}</strong></div>
                                <div><span class="stream-label">FPS</span><strong class="stream-value">{{ streamInfo.videoFps }}</strong></div>
                                <div><span class="stream-label">丢包率</span><strong class="stream-value" :class="{ warn: liveMetrics.loss > 2, err: liveMetrics.loss > 5 }">{{ liveMetrics.loss }}%</strong></div>
                            </div>
                        </section>
                        <section class="stream-detail">
                            <h4><Activity :size="14" />音频信息</h4>
                            <div class="stream-detail-grid">
                                <div><span class="stream-label">编码</span><strong class="stream-value">{{ streamInfo.audioCodec }}</strong></div>
                                <div><span class="stream-label">采样率</span><strong class="stream-value">{{ streamInfo.audioSampleRate }}</strong></div>
                            </div>
                        </section>
                    </div>
                </div>

            </section>
            <!-- 右侧功能栏 -->
            <aside v-if="!sideCollapsed" class="sidebar">
                <!-- Tabs -->
                <div class="tabs">
                    <button
                        v-for="t in tabs"
                        :key="t.key"
                        class="tab"
                        :class="{ active: activeTab === t.key }"
                        :data-testid="t.key === 'probe' ? 'probe-tab' : undefined"
                        @click="activeTab = t.key"
                    >
                        <component :is="t.icon" :size="14" />
                        <span>{{ t.label }}</span>
                    </button>
                </div>

                <!-- Tab 面板容器 -->
                <div class="panels">
                    <!-- ═══════════ 云台控制 ═══════════ -->
                    <div v-show="activeTab === 'ptz'" class="panel">
                        <!-- 模式切换:速度控制 / 精准控制(2022) -->
                        <div class="mode-switch">
                            <button :class="{ active: ptzMode === 'speed' }" @click="ptzMode = 'speed'">
                                <Compass :size="13" />速度控制
                            </button>
                            <button :class="{ active: ptzMode === 'precise' }" @click="ptzMode = 'precise'">
                                <Crosshair :size="13" />精准定位<span class="tag-2022">2022</span>
                            </button>
                        </div>

                        <!-- 速度模式:拖拽摇杆 + 变倍 + 速度 -->
                        <div v-show="ptzMode === 'speed'" class="ptz-speed">
                            <div
                                class="joystick-stage"
                                :class="{ active: joystickDragging }"
                                role="group"
                                tabindex="0"
                                aria-label="云台方向摇杆"
                                @pointerdown.prevent="startJoystick"
                                @pointermove.prevent="moveJoystick"
                                @pointerup.prevent="endJoystick"
                                @pointercancel.prevent="endJoystick"
                                @keydown="handleJoystickKeydown"
                                @keyup="handleJoystickKeyup"
                            >
                                <div class="joystick-base"></div>
                                <div class="joystick-dots" aria-hidden="true">
                                    <span class="joystick-dot dot-top"></span>
                                    <span class="joystick-dot dot-top-right"></span>
                                    <span class="joystick-dot dot-right"></span>
                                    <span class="joystick-dot dot-bottom-right"></span>
                                    <span class="joystick-dot dot-bottom"></span>
                                    <span class="joystick-dot dot-bottom-left"></span>
                                    <span class="joystick-dot dot-left"></span>
                                    <span class="joystick-dot dot-top-left"></span>
                                </div>
                                <span class="joystick-label top">上</span>
                                <span class="joystick-label diagonal top-right">右上</span>
                                <span class="joystick-label right">右</span>
                                <span class="joystick-label diagonal bottom-right">右下</span>
                                <span class="joystick-label bottom">下</span>
                                <span class="joystick-label diagonal bottom-left">左下</span>
                                <span class="joystick-label left">左</span>
                                <span class="joystick-label diagonal top-left">左上</span>
                                <div class="joystick-handle" :style="joystickHandleStyle" aria-hidden="true">
                                    <span></span>
                                </div>
                            </div>

                            <button
                                class="talk-button"
                                :class="{ active: talkState === 'talking' }"
                                :disabled="!isAudioCapable"
                                :aria-pressed="talkState === 'talking'"
                                @pointerdown.prevent="startTalk"
                                @pointerup.prevent="stopTalk"
                                @pointerleave="stopTalk"
                                @pointercancel="stopTalk"
                            >
                                <Mic :size="14" />
                                <span>{{ talkState === "talking" ? "对讲中 · 松开结束" : "按住对讲" }}</span>
                            </button>

                            <div class="speed-row">
                                <label>
                                    <span><Gauge :size="12" />移动速度</span>
                                    <input v-model.number="moveSpeed" type="range" min="1" max="10" />
                                    <em>{{ moveSpeed }}</em>
                                </label>
                            </div>

                            <div class="lens-grid">
                                <div class="lens-item">
                                    <span class="lens-label"><ZoomIn :size="12" />变倍</span>
                                    <div class="lens-btns">
                                        <button title="放大" @mousedown="sendPtz('放大')" @mouseup="sendPtz('停止')"><ZoomIn :size="14" /></button>
                                        <button title="缩小" @mousedown="sendPtz('缩小')" @mouseup="sendPtz('停止')"><ZoomOut :size="14" /></button>
                                    </div>
                                </div>
                                <div class="lens-item">
                                    <span class="lens-label"><FocusIcon :size="12" />聚焦</span>
                                    <div class="lens-btns">
                                        <button title="远焦" @click="sendPtz('远焦')">远</button>
                                        <button title="近焦" @click="sendPtz('近焦')">近</button>
                                        <button :class="{ toggled: focusMode === 'auto' }" title="自动聚焦" @click="focusMode = focusMode === 'auto' ? 'manual' : 'auto'">A</button>
                                    </div>
                                </div>
                                <div class="lens-item">
                                    <span class="lens-label"><Circle :size="12" />光圈</span>
                                    <div class="lens-btns">
                                        <button title="开大" @click="sendPtz('光圈+')">+</button>
                                        <button title="缩小" @click="sendPtz('光圈-')">−</button>
                                        <button :class="{ toggled: irisMode === 'auto' }" title="自动光圈" @click="irisMode = irisMode === 'auto' ? 'manual' : 'auto'">A</button>
                                    </div>
                                </div>
                            </div>

                        </div>

                        <!-- 精准控制模式(2022 新增):Pan/Tilt/Zoom 绝对定位 -->
                        <div v-show="ptzMode === 'precise'" class="ptz-precise">
                            <div class="precise-hint">
                                <Info :size="12" />
                                <span>基于 <strong>PTZPreciseCtrl</strong>(2022),精准角度定位。需设备支持精准 PTZ 协议。</span>
                            </div>
                            <div class="axis-row">
                                <label>
                                    <span>Pan 水平角(°)</span>
                                    <div class="axis-ctrl">
                                        <input v-model.number="precisePan" type="range" min="0" max="360" step="0.1" />
                                        <input v-model.number="precisePan" type="number" min="0" max="360" step="0.1" class="axis-num" />
                                    </div>
                                </label>
                            </div>
                            <div class="axis-row">
                                <label>
                                    <span>Tilt 俯仰角(°)</span>
                                    <div class="axis-ctrl">
                                        <input v-model.number="preciseTilt" type="range" min="-90" max="90" step="0.1" />
                                        <input v-model.number="preciseTilt" type="number" min="-90" max="90" step="0.1" class="axis-num" />
                                    </div>
                                </label>
                            </div>
                            <div class="axis-row">
                                <label>
                                    <span>Zoom 变倍(x)</span>
                                    <div class="axis-ctrl">
                                        <input v-model.number="preciseZoom" type="range" min="1" max="32" step="0.1" />
                                        <input v-model.number="preciseZoom" type="number" min="1" max="32" step="0.1" class="axis-num" />
                                    </div>
                                </label>
                            </div>
                            <div class="precise-actions">
                                <button class="btn-primary sm" @click="sendPrecise">
                                    <Target :size="13" />应用定位
                                </button>
                                <button class="btn-ghost sm" @click="Message.info('[Mock] 查询精准状态')">
                                    <Navigation :size="13" />读取当前位置
                                </button>
                            </div>
                        </div>

                        <!-- 预置位管理 -->
                        <div class="section-hd">
                            <span class="section-title"><Hash :size="13" />预置位</span>
                            <span class="section-meta">{{ presets.length }} / 255</span>
                        </div>
                        <div class="preset-grid">
                            <button
                                v-for="p in presets"
                                :key="p.id"
                                class="preset-item"
                                :class="{ active: activePresetId === p.id }"
                                @click="callPreset(p.id)"
                            >
                                <span class="preset-idx">#{{ p.id }}</span>
                                <span class="preset-name">{{ p.name }}</span>
                                <span class="preset-del" @click.stop="deletePreset(p.id)" title="删除"><Trash2 :size="11" /></span>
                            </button>
                            <div class="preset-add">
                                <input v-model="newPresetName" type="text" placeholder="新预置位名称" maxlength="16" />
                                <button class="btn-primary sm" @click="setPreset">
                                    <Target :size="12" />保存当前位置
                                </button>
                            </div>
                        </div>

                        <!-- 巡航轨迹(2022 CruiseTrackListQuery) -->
                        <div class="section-hd">
                            <span class="section-title"><Route :size="13" />巡航轨迹<span class="tag-2022">2022</span></span>
                            <span class="section-meta">{{ cruiseTracks.length }} 条 · 状态 {{ cruiseState === 'running' ? '运行中' : cruiseState === 'paused' ? '已暂停' : '已停止' }}</span>
                        </div>
                        <div class="cruise-list">
                            <div v-for="c in cruiseTracks" :key="c.id" class="cruise-item" :class="{ active: activeCruiseId === c.id, disabled: !c.enabled }">
                                <div class="cruise-info">
                                    <strong>#{{ c.id }} · {{ c.name }}</strong>
                                    <small>途经 {{ c.presets.length }} 个点位 · 停留 {{ c.dwellSec }}s · {{ c.enabled ? '已启用' : '已禁用' }}</small>
                                </div>
                                <div class="cruise-actions">
                                    <button class="btn-ghost sm" :disabled="!c.enabled" @click="toggleCruise(c.id)">
                                        <Play v-if="cruiseState !== 'running' || activeCruiseId !== c.id" :size="12" />
                                        <Pause v-else :size="12" />
                                    </button>
                                </div>
                            </div>
                            <button v-if="cruiseState !== 'stopped'" class="btn-ghost sm block" @click="stopCruise">
                                <Square :size="12" />停止全部巡航
                            </button>
                        </div>

                        <!-- 看守位(2022 HomePositionQuery) -->
                        <div class="section-hd">
                            <span class="section-title"><Home :size="13" />看守位<span class="tag-2022">2022</span></span>
                            <label class="toggle">
                                <input v-model="homePosition.enabled" type="checkbox" />
                                <span></span>
                            </label>
                        </div>
                        <div class="home-config">
                            <div class="home-fields" :class="{ disabled: !homePosition.enabled }">
                                <div class="home-row">
                                    <span>回位预置位</span>
                                    <select v-model.number="homePosition.presetId">
                                        <option v-for="p in presets" :key="p.id" :value="p.id">#{{ p.id }} · {{ p.name }}</option>
                                    </select>
                                </div>
                                <div class="home-row">
                                    <span>空闲触发(秒)</span>
                                    <input v-model.number="homePosition.delaySec" type="number" min="10" max="3600" />
                                </div>
                            </div>
                            <button class="btn-primary sm" @click="saveHomePosition">
                                <ShieldCheck :size="12" />{{ homePosition.enabled ? "保存看守位" : "关闭看守位" }}
                            </button>
                        </div>
                    </div>
                    <!-- ═══════════ 视频探针 ═══════════ -->
                    <div v-show="activeTab === 'probe'" class="panel probe-panel" data-testid="probe-panel">
                        <div class="probe-header">
                            <div class="probe-heading">
                                <span class="probe-heading-icon"><Activity :size="15" /></span>
                                <div>
                                    <strong>逐帧健康检测</strong>
                                    <span>ZLM addProbe · 3 秒采样窗口</span>
                                </div>
                            </div>
                            <span class="probe-status" :class="probeState">
                                <span class="dot"></span>{{ probeStatusText }}
                            </span>
                        </div>

                        <button
                            class="probe-action"
                            data-testid="probe-start"
                            :disabled="phase !== 'playing' || probeState === 'sampling'"
                            @click="startProbe"
                        >
                            <Loader2 v-if="probeState === 'sampling'" :size="14" class="spin" />
                            <Play v-else :size="14" />
                            <span>{{ probeButtonText }}</span>
                        </button>

                        <div class="probe-summary" :class="{ muted: probeState !== 'complete' }">
                            <div>
                                <span>采样时长</span>
                                <strong>{{ probeState === "complete" ? "3.00" : "—" }}<em>s</em></strong>
                            </div>
                            <div>
                                <span>采集帧数</span>
                                <strong>{{ probeState === "complete" ? "246" : "—" }}<em>帧</em></strong>
                            </div>
                            <div>
                                <span>采样流量</span>
                                <strong>{{ probeState === "complete" ? "768" : "—" }}<em>KB</em></strong>
                            </div>
                        </div>

                        <div v-if="probeState === 'complete'" class="probe-verdict">
                            <CheckCircle2 :size="15" />
                            <div>
                                <strong>流健康，帧序与时间戳连续</strong>
                                <span>完成于 {{ probeFinishedAt }} · 未发现异常帧间隔</span>
                            </div>
                        </div>
                        <div v-else class="probe-verdict pending">
                            <Activity :size="15" />
                            <div>
                                <strong>{{ probeState === "sampling" ? "正在采集音视频帧" : "尚未执行深度检测" }}</strong>
                                <span>{{ probeState === "sampling" ? "结果将在采样结束后生成" : "当前仅展示检测项目" }}</span>
                            </div>
                        </div>

                        <div class="section-hd">
                            <span class="section-title"><Video :size="13" />轨道详情</span>
                            <span class="section-meta">{{ probeState === "complete" ? "2 条轨道" : "待采样" }}</span>
                        </div>
                        <div class="probe-tracks">
                            <section class="probe-track video">
                                <div class="probe-track-head">
                                    <span><Video :size="13" />视频轨</span>
                                    <strong>{{ probeState === "complete" ? "H.264" : "—" }}</strong>
                                </div>
                                <div class="probe-data-grid">
                                    <div><span>精确 FPS</span><strong>{{ probeState === "complete" ? "25.3" : "—" }}</strong></div>
                                    <div><span>采样帧</span><strong>{{ probeState === "complete" ? "76" : "—" }}</strong></div>
                                    <div><span>关键帧</span><strong>{{ probeState === "complete" ? "3" : "—" }}</strong></div>
                                    <div><span>GOP</span><strong>{{ probeState === "complete" ? "25 帧" : "—" }}</strong></div>
                                </div>
                            </section>
                            <section class="probe-track audio">
                                <div class="probe-track-head">
                                    <span><Activity :size="13" />音频轨</span>
                                    <strong>{{ probeState === "complete" ? "PCMA" : "—" }}</strong>
                                </div>
                                <div class="probe-data-grid">
                                    <div><span>采样率</span><strong>{{ probeState === "complete" ? "8 kHz" : "—" }}</strong></div>
                                    <div><span>采样帧</span><strong>{{ probeState === "complete" ? "170" : "—" }}</strong></div>
                                    <div><span>帧间隔</span><strong>{{ probeState === "complete" ? "20.0 ms" : "—" }}</strong></div>
                                    <div><span>声道</span><strong>{{ probeState === "complete" ? "1" : "—" }}</strong></div>
                                </div>
                            </section>
                        </div>

                        <div class="section-hd">
                            <span class="section-title"><Gauge :size="13" />时间戳健康</span>
                            <span class="section-meta good">{{ probeState === "complete" ? "平稳" : "待检测" }}</span>
                        </div>
                        <div class="probe-health-grid">
                            <div><span>视频 DTS 间隔</span><strong>{{ probeState === "complete" ? "39.8 ms" : "—" }}</strong><em>均值</em></div>
                            <div><span>帧到达抖动</span><strong>{{ probeState === "complete" ? "3.2 ms" : "—" }}</strong><em>标准差</em></div>
                            <div><span>PTS-DTS</span><strong>{{ probeState === "complete" ? "0.0 ms" : "—" }}</strong><em>最大值</em></div>
                            <div><span>音视频交织</span><strong>{{ probeState === "complete" ? "正常" : "—" }}</strong><em>最大偏差 18 ms</em></div>
                        </div>

                        <div class="section-hd">
                            <span class="section-title"><Signal :size="13" />帧到达时间线</span>
                            <span class="section-meta">{{ probeState === "complete" ? "最近 32 帧" : "无数据" }}</span>
                        </div>
                        <div class="frame-timeline" :class="{ muted: probeState !== 'complete' }">
                            <div class="frame-bars">
                                <span
                                    v-for="(frame, index) in probeTimeline"
                                    :key="index"
                                    :class="[frame.type, { keyframe: frame.keyFrame }]"
                                    :style="{ height: probeState === 'complete' ? `${frame.height}%` : '8%' }"
                                ></span>
                            </div>
                            <div class="frame-legend">
                                <span><i class="key"></i>关键帧</span>
                                <span><i class="video"></i>视频帧</span>
                                <span><i class="audio"></i>音频帧</span>
                            </div>
                        </div>
                    </div>
                    <!-- ═══════════ 高级 ═══════════ -->
                    <div v-show="activeTab === 'advanced'" class="panel">
                        <div class="section-hd first">
                            <span class="section-title"><Settings :size="13" />设备控制</span>
                            <span class="section-meta">GB28181 DeviceControl</span>
                        </div>
                        <div class="adv-actions">
                            <button class="adv-btn" @click="Message.info('[Mock] 请求关键帧(IDR)')">
                                <Video :size="14" />
                                <div><strong>强制关键帧</strong><small>IFrameCmd · 快速刷新画面</small></div>
                            </button>
                            <button class="adv-btn" @click="Message.info('[Mock] 远程录像(设备端)')">
                                <Circle :size="14" />
                                <div><strong>设备端录制</strong><small>RecordCmd · SD 卡录制</small></div>
                            </button>
                            <button class="adv-btn" @click="Message.info('[Mock] 布防')">
                                <ShieldCheck :size="14" />
                                <div><strong>布防 / 撤防</strong><small>GuardCmd · 触发告警</small></div>
                            </button>
                            <button class="adv-btn" @click="Message.info('[Mock] 报警复位')">
                                <AlertTriangle :size="14" />
                                <div><strong>报警复位</strong><small>AlarmCmd · 清除告警</small></div>
                            </button>
                            <button class="adv-btn" @click="Message.info('[Mock] 远程重启')">
                                <RefreshCcw :size="14" />
                                <div><strong>远程重启</strong><small>TeleBootCmd · 重启设备</small></div>
                            </button>
                            <button class="adv-btn" @click="Message.info('[Mock] 3D 定位(拖框放大)')">
                                <Move3d :size="14" />
                                <div><strong>3D 定位</strong><small>2022 · 拖框区域放大</small></div>
                            </button>
                        </div>

                        <div class="section-hd">
                            <span class="section-title"><SlidersHorizontal :size="13" />图像参数</span>
                            <span class="section-meta">ConfigDownload</span>
                        </div>
                        <div class="image-adjust">
                            <label><span>亮度</span><input type="range" min="0" max="255" value="128" /></label>
                            <label><span>对比度</span><input type="range" min="0" max="255" value="128" /></label>
                            <label><span>饱和度</span><input type="range" min="0" max="255" value="128" /></label>
                            <label><span>色度</span><input type="range" min="0" max="255" value="128" /></label>
                        </div>
                        <button class="btn-primary sm" @click="Message.info('[Mock] 图像参数已下发')">
                            <CheckCircle2 :size="12" />下发到设备
                        </button>
                    </div>
                </div>
            </aside>
        </div>
    </a-modal>
</template>

<style scoped lang="scss">
/* 主体壳子 —— 具体面板样式在后续 chunk 中追加 */
.play-console-modal :deep(.arco-modal-body) { padding: 14px 18px 18px; }

.console-title { display: flex; align-items: center; gap: 10px; min-width: 0; }
.title-icon {
    display: inline-grid; place-items: center; width: 32px; height: 32px;
    color: var(--uvp-brand); background: var(--uvp-brand-soft); border-radius: 9px;
}
.title-text { display: grid; gap: 2px; min-width: 0; }
.title-text strong { color: var(--uvp-text-primary); font-size: 14px; }
.title-text span { overflow: hidden; color: var(--uvp-text-tertiary); font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }

.session-badge {
    display: inline-flex; align-items: center; gap: 6px;
    margin-left: auto; padding: 4px 10px;
    color: var(--uvp-text-tertiary); background: var(--uvp-list-toolbar-bg);
    border: 1px solid var(--uvp-panel-border); border-radius: 999px;
    font-size: 11px; white-space: nowrap;
}
.session-badge .dot { width: 6px; height: 6px; background: var(--uvp-text-tertiary); border-radius: 50%; }
.session-badge.active {
    color: var(--uvp-brand-cyan);
    background: color-mix(in srgb, var(--uvp-brand-cyan) 12%, transparent);
    border-color: color-mix(in srgb, var(--uvp-brand-cyan) 30%, transparent);
}
.session-badge.active .dot { background: var(--uvp-brand-cyan); box-shadow: 0 0 0 3px color-mix(in srgb, var(--uvp-brand-cyan) 20%, transparent); animation: pulse 1.6s ease-in-out infinite; }
.session-badge.loading { color: var(--uvp-warning); background: var(--uvp-warning-soft); border-color: var(--uvp-warning-border); }
.session-badge.loading .dot { background: var(--uvp-warning); }
.session-badge.error { color: var(--uvp-danger); background: var(--uvp-danger-soft); border-color: var(--uvp-danger-border); }
.session-badge.error .dot { background: var(--uvp-danger); }
.session-badge.paused { color: #a78bfa; background: rgb(167 139 250 / 12%); border-color: rgb(167 139 250 / 28%); }

.console-body {
    display: grid; grid-template-columns: minmax(0, 1fr) 360px;
    gap: 14px; min-height: 0;
}

/* 主区(视频) */
.stage { display: grid; gap: 10px; min-width: 0; align-content: start; }
.stage.stage-wide { grid-column: 1 / -1; }

.video-frame {
    position: relative; overflow: hidden;
    background: #060b14; border: 1px solid var(--uvp-panel-border);
    border-radius: 14px;
    box-shadow: 0 12px 32px -18px rgb(0 0 0 / 60%);
}
.video-canvas {
    position: relative; width: 100%; aspect-ratio: 16 / 9;
    background:
        radial-gradient(circle at 50% 45%, rgb(30 41 59 / 40%) 0%, rgb(2 6 23 / 96%) 72%),
        #020617;
    display: grid; place-items: center;
}
.mock-live {
    position: absolute; inset: 0;
    background:
        linear-gradient(120deg, rgba(59, 130, 246, 0.16) 0%, transparent 40%, rgba(45, 212, 191, 0.14) 100%),
        radial-gradient(circle at 30% 40%, rgba(59, 130, 246, 0.22) 0%, transparent 50%),
        radial-gradient(circle at 70% 60%, rgba(236, 72, 153, 0.14) 0%, transparent 55%),
        repeating-linear-gradient(45deg, rgba(255, 255, 255, 0.015) 0 2px, transparent 2px 8px),
        #030712;
    animation: mockLive 8s ease-in-out infinite alternate;
}
@keyframes mockLive { 0% { filter: hue-rotate(0deg); } 100% { filter: hue-rotate(24deg); } }

.hud { position: absolute; inset: 0; pointer-events: none; }
.hud-top { position: absolute; top: 12px; left: 12px; display: flex; gap: 6px; flex-wrap: wrap; }
.hud-tag {
    display: inline-flex; align-items: center; gap: 5px;
    padding: 3px 8px; color: #dbeafe;
    background: rgba(6, 11, 20, 0.72); border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 6px; font-size: 10.5px; letter-spacing: 0.02em;
    backdrop-filter: blur(6px);
}
.hud-tag.live { color: #fecaca; background: rgba(220, 38, 38, 0.32); border-color: rgba(248, 113, 113, 0.42); }
.hud-tag.live .dot { width: 6px; height: 6px; background: #f87171; border-radius: 50%; animation: pulse 1.4s ease-in-out infinite; }
.hud-tag.warn { color: #fef3c7; background: rgba(217, 119, 6, 0.32); border-color: rgba(251, 191, 36, 0.4); }
.hud-tag.ghost { color: rgba(219, 234, 254, 0.72); background: transparent; border-color: rgba(255, 255, 255, 0.14); }
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }

.paused-mask {
    position: absolute; inset: 0; display: grid; place-items: center; align-content: center; gap: 10px;
    color: #dbeafe; background: rgba(2, 6, 23, 0.68); backdrop-filter: blur(4px);
}
.paused-mask span { font-size: 13px; letter-spacing: 0.04em; }

.placeholder {
    display: grid; place-items: center; align-content: center; gap: 10px;
    color: var(--uvp-text-tertiary); text-align: center; padding: 30px;
}
.placeholder strong { color: #dbeafe; font-size: 14px; }
.placeholder span { max-width: 420px; color: #94a3b8; font-size: 12px; line-height: 1.55; }
.placeholder.error strong { color: #fecaca; }
.placeholder.error { color: #fca5a5; }
.pulse {
    display: grid; place-items: center; width: 76px; height: 76px;
    color: var(--uvp-brand); background: radial-gradient(circle at 50% 50%, rgb(96 165 250 / 22%) 0%, transparent 65%);
    border-radius: 50%;
}
.pulse.idle { color: rgb(148 163 184 / 62%); background: radial-gradient(circle at 50% 50%, rgb(148 163 184 / 12%) 0%, transparent 65%); }
.spin { animation: spin 1.1s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
@keyframes pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.55; } }

/* 播放器控制条(悬浮画面下) */
.btn-primary {
    display: inline-flex; align-items: center; gap: 6px;
    padding: 7px 14px; margin-top: 4px;
    color: #fff; background: var(--uvp-brand); border: 0; border-radius: 8px; cursor: pointer;
    font-size: 12px; font-weight: 600;
}
.btn-primary:hover { background: var(--uvp-brand-strong); }

/* 多协议切换器 */
.protocol-switcher {
    display: flex; align-items: center; gap: 12px; flex-wrap: wrap;
    padding: 10px 14px;
    background: rgba(15, 23, 42, 0.92); backdrop-filter: blur(12px);
    border-top: 1px solid rgba(255, 255, 255, 0.08);
}
.switcher-left { display: flex; align-items: center; gap: 10px; }
.switcher-left .kicker { color: rgba(203, 213, 225, 0.72); font-size: 11px; letter-spacing: 0.03em; white-space: nowrap; }
.switcher-right { display: flex; gap: 6px; }
.proto-btn {
    padding: 5px 12px;
    color: rgba(219, 234, 254, 0.68); background: rgba(255, 255, 255, 0.04);
    border: 1px solid rgba(255, 255, 255, 0.08); border-radius: 6px;
    cursor: pointer; font-size: 11px; font-weight: 500;
    transition: all 0.15s ease;
}
.proto-btn:hover { color: var(--uvp-brand); background: var(--uvp-brand-soft); border-color: color-mix(in srgb, var(--uvp-brand) 32%, transparent); }
.proto-btn.active { color: var(--uvp-brand); background: var(--uvp-brand-soft); border-color: color-mix(in srgb, var(--uvp-brand) 42%, transparent); }
.copy-url {
    display: inline-grid; place-items: center; width: 28px; height: 28px;
    color: rgba(219, 234, 254, 0.68); background: rgba(255, 255, 255, 0.04);
    border: 1px solid rgba(255, 255, 255, 0.08); border-radius: 6px;
    cursor: pointer; transition: all 0.15s ease;
}
.copy-url:hover { color: var(--uvp-brand); background: var(--uvp-brand-soft); border-color: color-mix(in srgb, var(--uvp-brand) 32%, transparent); }

/* 流信息(协议切换器内) */
.stream-info {
    display: flex; align-items: center; gap: 8px;
    margin-left: auto;
    padding-left: 16px;
    border-left: 1px solid rgba(255, 255, 255, 0.08);
    font-size: 11px;
}

.protocol-option { display: grid; gap: 2px; }
.protocol-option strong { font-size: 12px; font-weight: 500; }
.protocol-option .desc { color: var(--uvp-text-tertiary); font-size: 10.5px; }

/* 流信息独立区域:播放器下方的运行信息面板 */
.stream-info-bar {
    display: grid; gap: 14px;
    padding: 14px 16px 16px;
    background: var(--uvp-panel-bg); border: 1px solid var(--uvp-panel-border);
    border-radius: 10px;
    box-shadow: 0 8px 24px -18px rgb(0 0 0 / 45%);
    font-size: 11px;
}
.stream-overview-header {
    display: flex; align-items: center; justify-content: space-between;
}
.stream-overview-header strong {
    color: var(--uvp-text-primary); font-size: 13px; font-weight: 700;
}
.stream-refresh {
    display: inline-grid; place-items: center; width: 28px; height: 28px;
    color: var(--uvp-text-tertiary); background: transparent;
    border: 1px solid var(--uvp-panel-border); border-radius: 50%; cursor: pointer;
    transition: all 0.15s ease;
}
.stream-refresh:hover {
    color: var(--uvp-brand); border-color: var(--uvp-brand);
    background: var(--uvp-brand-soft);
}
.stream-overview-metrics {
    display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 16px;
}
.overview-metric {
    display: flex; align-items: baseline; gap: 8px; min-width: 0;
}
.overview-metric .stream-label { flex-shrink: 0; }
.overview-metric .stream-value { font-size: 12px; }
.stream-detail-columns {
    display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 28px;
}
.stream-detail { min-width: 0; }
.stream-detail h4 {
    display: flex; align-items: center; gap: 6px; margin: 0 0 12px;
    color: var(--uvp-text-primary); font-size: 12px; font-weight: 700;
}
.stream-detail h4 svg { color: var(--uvp-brand); }
.stream-detail-grid {
    display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px 24px;
}
.stream-detail-grid > div {
    display: flex; align-items: baseline; gap: 8px; min-width: 0;
}
.stream-info-bar .stream-label {
    color: var(--uvp-text-tertiary);
    font-size: 10.5px;
    white-space: nowrap;
}
.stream-info-bar .stream-value {
    overflow: hidden; color: var(--uvp-text-secondary);
    text-overflow: ellipsis; white-space: nowrap;
    font-weight: 500;
}
.stream-info-bar .stream-value.mono {
    font-family: 'SF Mono', 'Consolas', monospace;
    font-size: 10px;
}
.stream-info-bar .stream-value.warn { color: #fbbf24; }
.stream-info-bar .stream-value.err { color: #f87171; }

@media (max-width: 720px) {
    .stream-overview-metrics,
    .stream-detail-columns { grid-template-columns: 1fr; gap: 14px; }
}

/* ═══════════ 右侧栏 ═══════════ */
.sidebar {
    display: grid; gap: 10px; align-content: start;
    max-height: 78vh; overflow-y: auto; padding-right: 2px;
}
.sidebar::-webkit-scrollbar { width: 6px; }
.sidebar::-webkit-scrollbar-thumb { background: color-mix(in srgb, var(--uvp-text-tertiary) 30%, transparent); border-radius: 3px; }

/* Tab 切换 */
.tabs {
    display: grid; grid-template-columns: repeat(3, 1fr); gap: 4px;
    padding: 4px;
    background: var(--uvp-list-toolbar-bg); border: 1px solid var(--uvp-panel-border);
    border-radius: 10px;
}
.tab {
    display: inline-flex; flex-direction: column; align-items: center; justify-content: center; gap: 3px;
    padding: 8px 4px;
    color: var(--uvp-text-tertiary); background: transparent; border: 0; border-radius: 7px;
    cursor: pointer; font-size: 10.5px; letter-spacing: 0.02em;
    transition: all 0.15s ease;
}
.tab:hover { color: var(--uvp-text-secondary); background: color-mix(in srgb, var(--uvp-brand) 6%, transparent); }
.tab.active { color: var(--uvp-brand); background: var(--uvp-brand-soft); box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--uvp-brand) 30%, transparent); }

/* Panel 容器 */
.panels {
    padding: 14px;
    background: var(--uvp-panel-bg); border: 1px solid var(--uvp-panel-border);
    border-radius: 12px;
}
.panel { display: grid; gap: 10px; }
.section-hd {
    display: flex; align-items: center; justify-content: space-between; gap: 8px;
    margin-top: 10px;
}
.section-hd.first { margin-top: 0; }
.section-title {
    display: inline-flex; align-items: center; gap: 6px;
    color: var(--uvp-text-secondary); font-size: 11.5px; font-weight: 600;
}
.section-title .tag-2022 {
    padding: 1px 5px; margin-left: 4px;
    color: var(--uvp-brand-cyan); background: color-mix(in srgb, var(--uvp-brand-cyan) 14%, transparent);
    border: 1px solid color-mix(in srgb, var(--uvp-brand-cyan) 30%, transparent); border-radius: 4px;
    font-size: 9px; font-weight: 700; letter-spacing: 0.06em;
}
.section-meta { color: var(--uvp-text-tertiary); font-size: 10.5px; display: inline-flex; align-items: center; gap: 4px; }
.section-meta .dot { width: 6px; height: 6px; background: var(--uvp-text-tertiary); border-radius: 50%; }

/* ═══════════ 云台面板 ═══════════ */
.mode-switch {
    display: grid; grid-template-columns: 1fr 1fr; gap: 4px;
    padding: 4px;
    background: var(--uvp-list-toolbar-bg); border: 1px solid var(--uvp-panel-border);
    border-radius: 8px;
}
.mode-switch button {
    display: inline-flex; align-items: center; justify-content: center; gap: 5px;
    padding: 6px 8px;
    color: var(--uvp-text-tertiary); background: transparent; border: 0; border-radius: 6px;
    cursor: pointer; font-size: 11px;
    transition: all 0.15s ease;
}
.mode-switch button.active { color: var(--uvp-brand); background: var(--uvp-brand-soft); box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--uvp-brand) 26%, transparent); }
.tag-2022 {
    padding: 1px 4px;
    color: var(--uvp-brand-cyan); background: color-mix(in srgb, var(--uvp-brand-cyan) 14%, transparent);
    border: 1px solid color-mix(in srgb, var(--uvp-brand-cyan) 30%, transparent); border-radius: 3px;
    font-size: 8.5px; font-weight: 700; letter-spacing: 0.05em;
}

/* 拖拽摇杆 */
.joystick-stage {
    position: relative;
    width: min(176px, 100%); aspect-ratio: 1; margin: 10px auto 6px;
    border-radius: 50%; cursor: grab; touch-action: none; user-select: none;
}
.joystick-stage.active { cursor: grabbing; }
.joystick-stage:focus-visible { outline: 2px solid var(--uvp-brand); outline-offset: 3px; }
.joystick-base {
    position: absolute; inset: 0; border-radius: 50%;
    background: radial-gradient(circle at 36% 28%, color-mix(in srgb, white 18%, var(--uvp-brand-soft)) 0%, var(--uvp-brand-soft) 46%, color-mix(in srgb, var(--uvp-text-primary) 12%, var(--uvp-list-toolbar-bg)) 100%);
    border: 2px solid color-mix(in srgb, var(--uvp-brand) 30%, var(--uvp-panel-border));
    box-shadow: inset 0 3px 4px color-mix(in srgb, white 14%, transparent), inset 0 -8px 14px color-mix(in srgb, var(--uvp-text-primary) 14%, transparent), 0 8px 18px color-mix(in srgb, var(--uvp-brand) 15%, transparent), 0 2px 4px color-mix(in srgb, var(--uvp-text-primary) 15%, transparent);
}
.joystick-base::before {
    content: ""; position: absolute; inset: 25px; border-radius: 50%;
    background: radial-gradient(circle at 44% 38%, color-mix(in srgb, var(--uvp-brand-soft) 30%, var(--uvp-list-toolbar-bg)) 0%, var(--uvp-list-toolbar-bg) 62%, color-mix(in srgb, var(--uvp-text-primary) 8%, var(--uvp-list-toolbar-bg)) 100%);
    border: 1px solid color-mix(in srgb, var(--uvp-brand) 18%, var(--uvp-panel-border));
    box-shadow: inset 0 5px 10px color-mix(in srgb, var(--uvp-text-primary) 11%, transparent), inset 0 -2px 4px color-mix(in srgb, white 8%, transparent), 0 1px 0 color-mix(in srgb, white 10%, transparent);
}
.joystick-base::after {
    content: ""; position: absolute; inset: 31px; border-radius: 50%;
    border: 1px solid color-mix(in srgb, var(--uvp-brand) 14%, var(--uvp-panel-border));
    box-shadow: 0 0 0 1px color-mix(in srgb, var(--uvp-text-primary) 4%, transparent);
}
.joystick-dots { position: absolute; inset: 0; z-index: 2; pointer-events: none; }
.joystick-dot {
    position: absolute; width: 7px; height: 7px; border-radius: 50%;
    background: radial-gradient(circle at 34% 28%, color-mix(in srgb, white 42%, var(--uvp-text-tertiary)) 0 18%, var(--uvp-text-tertiary) 58%, color-mix(in srgb, var(--uvp-text-primary) 35%, var(--uvp-text-tertiary)) 100%);
    border: 1px solid color-mix(in srgb, var(--uvp-text-primary) 12%, transparent);
    box-shadow: inset 0 1px 1px color-mix(in srgb, white 26%, transparent), 0 1px 2px color-mix(in srgb, var(--uvp-text-primary) 24%, transparent);
}
.joystick-dot.dot-top { top: 22px; left: 50%; transform: translateX(-50%); }
.joystick-dot.dot-top-right { top: 38px; right: 38px; }
.joystick-dot.dot-right { top: 50%; right: 22px; transform: translateY(-50%); }
.joystick-dot.dot-bottom-right { right: 38px; bottom: 38px; }
.joystick-dot.dot-bottom { bottom: 22px; left: 50%; transform: translateX(-50%); }
.joystick-dot.dot-bottom-left { bottom: 38px; left: 38px; }
.joystick-dot.dot-left { top: 50%; left: 22px; transform: translateY(-50%); }
.joystick-dot.dot-top-left { top: 38px; left: 38px; }
.joystick-label {
    position: absolute; z-index: 3; color: var(--uvp-text-tertiary);
    font-size: 9px; line-height: 1; pointer-events: none;
}
.joystick-label.top { top: 9px; left: 50%; transform: translateX(-50%); }
.joystick-label.top-right { top: 8px; right: 8px; }
.joystick-label.right { top: 50%; right: 9px; transform: translateY(-50%); }
.joystick-label.bottom-right { right: 8px; bottom: 8px; }
.joystick-label.bottom { bottom: 9px; left: 50%; transform: translateX(-50%); }
.joystick-label.bottom-left { bottom: 8px; left: 8px; }
.joystick-label.left { top: 50%; left: 9px; transform: translateY(-50%); }
.joystick-label.top-left { top: 8px; left: 8px; }
.joystick-handle {
    position: absolute; top: 50%; left: 50%; z-index: 4;
    display: grid; place-items: center; width: 52px; height: 52px;
    background: radial-gradient(circle at 34% 26%, color-mix(in srgb, white 88%, var(--uvp-brand)) 0 6%, color-mix(in srgb, white 30%, var(--uvp-brand)) 20%, var(--uvp-brand) 56%, var(--uvp-brand-strong) 100%);
    border: 5px solid color-mix(in srgb, white 58%, var(--uvp-brand));
    border-radius: 50%;
    box-shadow: inset 4px 4px 8px color-mix(in srgb, white 34%, transparent), inset -6px -8px 11px color-mix(in srgb, black 24%, transparent), 0 9px 16px color-mix(in srgb, var(--uvp-brand) 34%, transparent), 0 3px 4px color-mix(in srgb, black 28%, transparent), 0 0 0 4px var(--uvp-brand-soft), 0 0 0 5px color-mix(in srgb, var(--uvp-brand) 30%, transparent);
    transition: transform 0.22s cubic-bezier(.2, .8, .2, 1);
    pointer-events: none;
}
.joystick-stage.active .joystick-handle {
    box-shadow: inset 3px 3px 7px color-mix(in srgb, white 28%, transparent), inset -5px -6px 9px color-mix(in srgb, black 28%, transparent), 0 5px 10px color-mix(in srgb, var(--uvp-brand) 28%, transparent), 0 2px 3px color-mix(in srgb, black 24%, transparent), 0 0 0 4px var(--uvp-brand-soft), 0 0 0 5px color-mix(in srgb, var(--uvp-brand) 38%, transparent);
    transition: none;
}
.joystick-handle span {
    position: absolute; top: 9px; left: 11px; width: 17px; height: 9px;
    background: linear-gradient(145deg, color-mix(in srgb, white 74%, transparent), transparent);
    border-radius: 50%; filter: blur(.2px); opacity: .82;
}
@media (prefers-reduced-motion: reduce) {
    .joystick-handle { transition: none; }
}

.talk-button {
    display: flex; align-items: center; justify-content: center; gap: 7px;
    width: 100%; max-width: 200px; height: 34px; margin: 8px auto 2px;
    color: var(--uvp-brand); background: var(--uvp-brand-soft);
    border: 1px solid color-mix(in srgb, var(--uvp-brand) 34%, var(--uvp-panel-border));
    border-radius: 8px; cursor: pointer; font-size: 11px; font-weight: 600;
    touch-action: none; user-select: none; transition: all 0.15s ease;
}
.talk-button:hover:not(:disabled) { border-color: var(--uvp-brand); }
.talk-button.active {
    color: var(--uvp-danger); background: var(--uvp-danger-soft);
    border-color: var(--uvp-danger-border); box-shadow: 0 0 0 3px color-mix(in srgb, var(--uvp-danger) 10%, transparent);
}
.talk-button:disabled { cursor: not-allowed; opacity: 0.45; }

.speed-row { padding: 6px 2px; }
.speed-row label { display: grid; grid-template-columns: auto 1fr auto; gap: 8px; align-items: center; color: var(--uvp-text-tertiary); font-size: 11px; }
.speed-row label > span { display: inline-flex; align-items: center; gap: 4px; }
.speed-row input[type="range"] { accent-color: var(--uvp-brand); }
.speed-row em { font-style: normal; color: var(--uvp-brand); font-weight: 600; font-family: ui-monospace, Menlo, monospace; }

/* 镜头(变倍/聚焦/光圈) */
.lens-grid {
    display: grid; grid-template-columns: repeat(3, 1fr); gap: 6px;
    margin-top: 6px;
}
.lens-item {
    display: grid; gap: 6px;
    padding: 8px;
    background: var(--uvp-list-toolbar-bg); border: 1px solid var(--uvp-panel-border);
    border-radius: 8px;
}
.lens-label { display: inline-flex; align-items: center; gap: 4px; color: var(--uvp-text-tertiary); font-size: 10.5px; }
.lens-btns { display: flex; gap: 4px; }
.lens-btns button {
    flex: 1; height: 26px; padding: 0;
    color: var(--uvp-text-secondary); background: transparent;
    border: 1px solid var(--uvp-panel-border); border-radius: 5px; cursor: pointer;
    font-size: 11px;
}
.lens-btns button:hover:not(:disabled) { color: var(--uvp-brand); border-color: var(--uvp-brand); }
.lens-btns button.toggled { color: var(--uvp-brand); background: var(--uvp-brand-soft); border-color: color-mix(in srgb, var(--uvp-brand) 30%, var(--uvp-panel-border)); }


/* 精准 PTZ */
.ptz-precise { display: grid; gap: 10px; }
.precise-hint {
    display: flex; align-items: flex-start; gap: 6px;
    padding: 8px 10px;
    color: var(--uvp-text-tertiary); background: color-mix(in srgb, var(--uvp-brand-cyan) 8%, transparent);
    border: 1px solid color-mix(in srgb, var(--uvp-brand-cyan) 24%, transparent); border-radius: 8px;
    font-size: 10.5px; line-height: 1.5;
}
.precise-hint strong { color: var(--uvp-brand-cyan); font-weight: 600; }
.axis-row label { display: grid; gap: 4px; }
.axis-row label > span { color: var(--uvp-text-tertiary); font-size: 10.5px; }
.axis-ctrl { display: grid; grid-template-columns: 1fr 60px; gap: 6px; align-items: center; }
.axis-ctrl input[type="range"] { accent-color: var(--uvp-brand); }
.axis-num {
    height: 26px; padding: 0 6px;
    color: var(--uvp-text-secondary); background: var(--uvp-list-toolbar-bg);
    border: 1px solid var(--uvp-panel-border); border-radius: 5px;
    font-family: ui-monospace, Menlo, monospace; font-size: 11px;
    text-align: right;
}
.precise-actions { display: flex; gap: 6px; }
.precise-actions button { flex: 1; }

/* 预置位 */
.preset-grid {
    display: grid; grid-template-columns: 1fr 1fr; gap: 5px;
}
.preset-item {
    display: grid; grid-template-columns: auto 1fr auto; gap: 6px; align-items: center;
    padding: 8px 10px;
    color: var(--uvp-text-secondary); background: var(--uvp-list-toolbar-bg);
    border: 1px solid var(--uvp-panel-border); border-radius: 7px;
    cursor: pointer; text-align: left; font-size: 11px;
    transition: all 0.15s ease;
}
.preset-item:hover:not(:disabled) { border-color: color-mix(in srgb, var(--uvp-brand) 40%, var(--uvp-panel-border)); }
.preset-item.active { color: var(--uvp-brand); background: var(--uvp-brand-soft); border-color: color-mix(in srgb, var(--uvp-brand) 40%, var(--uvp-panel-border)); }
.preset-item:disabled { opacity: 0.42; cursor: not-allowed; }
.preset-idx { color: var(--uvp-text-tertiary); font-family: ui-monospace, Menlo, monospace; font-size: 10px; }
.preset-item.active .preset-idx { color: var(--uvp-brand); }
.preset-name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.preset-del {
    display: inline-grid; place-items: center; width: 20px; height: 20px;
    color: var(--uvp-text-tertiary); border-radius: 4px;
    transition: all 0.15s ease;
}
.preset-del:hover { color: var(--uvp-danger); background: var(--uvp-danger-soft); }
.preset-add {
    grid-column: 1 / -1;
    display: grid; grid-template-columns: 1fr auto; gap: 6px;
    padding-top: 6px; margin-top: 2px;
    border-top: 1px dashed var(--uvp-panel-border);
}
.preset-add input {
    height: 28px; padding: 0 8px;
    color: var(--uvp-text-secondary); background: var(--uvp-list-toolbar-bg);
    border: 1px solid var(--uvp-panel-border); border-radius: 6px;
    font-size: 11px;
}
.preset-add input:focus { outline: none; border-color: var(--uvp-brand); }

/* 巡航 */
.cruise-list { display: grid; gap: 5px; }
.cruise-item {
    display: grid; grid-template-columns: 1fr auto; gap: 8px; align-items: center;
    padding: 8px 10px;
    background: var(--uvp-list-toolbar-bg); border: 1px solid var(--uvp-panel-border);
    border-radius: 7px;
    transition: all 0.15s ease;
}
.cruise-item.active { border-color: color-mix(in srgb, var(--uvp-brand-cyan) 40%, var(--uvp-panel-border)); background: color-mix(in srgb, var(--uvp-brand-cyan) 6%, transparent); }
.cruise-item.disabled { opacity: 0.5; }
.cruise-info { display: grid; gap: 2px; min-width: 0; }
.cruise-info strong { color: var(--uvp-text-primary); font-size: 11.5px; font-weight: 500; }
.cruise-info small { color: var(--uvp-text-tertiary); font-size: 10px; }
.cruise-actions { display: flex; gap: 4px; }

/* 看守位 */
.home-config { display: grid; gap: 8px; padding-top: 4px; }
.home-fields { display: grid; gap: 8px; }
.home-fields.disabled { opacity: 0.42; pointer-events: none; }
.home-row {
    display: grid; grid-template-columns: 90px 1fr; gap: 8px; align-items: center;
    color: var(--uvp-text-tertiary); font-size: 11px;
}
.home-row select, .home-row input {
    height: 28px; padding: 0 8px;
    color: var(--uvp-text-secondary); background: var(--uvp-list-toolbar-bg);
    border: 1px solid var(--uvp-panel-border); border-radius: 6px;
    font-size: 11px;
}
.toggle { position: relative; display: inline-block; width: 34px; height: 18px; }
.toggle input { position: absolute; opacity: 0; }
.toggle span {
    position: absolute; inset: 0;
    background: var(--uvp-list-toolbar-bg); border: 1px solid var(--uvp-panel-border);
    border-radius: 999px; cursor: pointer; transition: all 0.2s ease;
}
.toggle span::before {
    position: absolute; top: 2px; left: 2px; width: 12px; height: 12px;
    background: var(--uvp-text-tertiary); border-radius: 50%; content: "";
    transition: all 0.2s ease;
}
.toggle input:checked + span { background: var(--uvp-brand-soft); border-color: color-mix(in srgb, var(--uvp-brand) 40%, var(--uvp-panel-border)); }
.toggle input:checked + span::before { transform: translateX(16px); background: var(--uvp-brand); }

/* ═══════════ 探针面板 ═══════════ */
.probe-panel { gap: 12px; }
.probe-header { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.probe-heading { display: flex; align-items: center; gap: 8px; min-width: 0; }
.probe-heading-icon {
    display: inline-grid; place-items: center; flex: 0 0 30px; width: 30px; height: 30px;
    color: var(--uvp-brand-cyan); background: color-mix(in srgb, var(--uvp-brand-cyan) 10%, transparent);
    border: 1px solid color-mix(in srgb, var(--uvp-brand-cyan) 26%, transparent); border-radius: 7px;
}
.probe-heading > div { display: grid; gap: 2px; min-width: 0; }
.probe-heading strong { color: var(--uvp-text-primary); font-size: 12px; font-weight: 650; }
.probe-heading span { overflow: hidden; color: var(--uvp-text-tertiary); font-size: 9.5px; text-overflow: ellipsis; white-space: nowrap; }
.probe-status {
    display: inline-flex; align-items: center; gap: 5px; flex-shrink: 0;
    padding: 3px 7px; color: var(--uvp-text-tertiary); background: var(--uvp-list-toolbar-bg);
    border: 1px solid var(--uvp-panel-border); border-radius: 999px; font-size: 9.5px;
}
.probe-status .dot { width: 5px; height: 5px; background: currentcolor; border-radius: 50%; }
.probe-status.sampling { color: var(--uvp-warning); border-color: var(--uvp-warning-border); }
.probe-status.sampling .dot { animation: pulse 1s ease-in-out infinite; }
.probe-status.complete { color: var(--uvp-brand-cyan); border-color: color-mix(in srgb, var(--uvp-brand-cyan) 30%, var(--uvp-panel-border)); }
.probe-action {
    display: inline-flex; align-items: center; justify-content: center; gap: 6px; width: 100%; min-height: 34px;
    color: #fff; background: var(--uvp-brand); border: 0; border-radius: 7px;
    cursor: pointer; font-size: 11.5px; font-weight: 600; transition: all 0.15s ease;
}
.probe-action:hover:not(:disabled) { background: var(--uvp-brand-strong); }
.probe-action:disabled { cursor: not-allowed; opacity: 0.56; }
.probe-summary {
    display: grid; grid-template-columns: repeat(3, minmax(0, 1fr));
    background: var(--uvp-list-toolbar-bg); border: 1px solid var(--uvp-panel-border); border-radius: 8px;
}
.probe-summary > div { display: grid; gap: 3px; min-width: 0; padding: 9px 10px; }
.probe-summary > div + div { border-left: 1px solid var(--uvp-panel-border); }
.probe-summary span, .probe-data-grid span, .probe-health-grid span { color: var(--uvp-text-tertiary); font-size: 9.5px; }
.probe-summary strong {
    color: var(--uvp-text-primary); font-family: ui-monospace, Menlo, monospace;
    font-size: 15px; font-weight: 650; white-space: nowrap;
}
.probe-summary em { margin-left: 2px; color: var(--uvp-text-tertiary); font-size: 9px; font-style: normal; font-weight: 400; }
.probe-summary.muted { opacity: 0.56; }
.probe-verdict {
    display: grid; grid-template-columns: 18px 1fr; gap: 7px; align-items: start;
    padding: 9px 10px; color: var(--uvp-brand-cyan);
    background: color-mix(in srgb, var(--uvp-brand-cyan) 7%, transparent);
    border: 1px solid color-mix(in srgb, var(--uvp-brand-cyan) 24%, var(--uvp-panel-border)); border-radius: 8px;
}
.probe-verdict.pending { color: var(--uvp-text-tertiary); background: var(--uvp-list-toolbar-bg); border-color: var(--uvp-panel-border); }
.probe-verdict > div { display: grid; gap: 2px; }
.probe-verdict strong { color: var(--uvp-text-primary); font-size: 10.5px; font-weight: 600; }
.probe-verdict span { color: var(--uvp-text-tertiary); font-size: 9.5px; }
.section-meta.good { color: var(--uvp-brand-cyan); }
.probe-tracks { display: grid; gap: 6px; }
.probe-track {
    padding: 9px 10px; background: var(--uvp-list-toolbar-bg);
    border: 1px solid var(--uvp-panel-border); border-radius: 8px;
}
.probe-track.video { border-left: 2px solid var(--uvp-brand); }
.probe-track.audio { border-left: 2px solid var(--uvp-brand-cyan); }
.probe-track-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 8px; }
.probe-track-head span { display: inline-flex; align-items: center; gap: 5px; color: var(--uvp-text-secondary); font-size: 10.5px; }
.probe-track-head strong { color: var(--uvp-text-primary); font-family: ui-monospace, Menlo, monospace; font-size: 10.5px; }
.probe-data-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 6px; }
.probe-data-grid > div { display: grid; gap: 2px; min-width: 0; }
.probe-data-grid strong { overflow: hidden; color: var(--uvp-text-secondary); font-size: 10.5px; text-overflow: ellipsis; white-space: nowrap; }
.probe-health-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 6px; }
.probe-health-grid > div {
    display: grid; grid-template-columns: 1fr auto; gap: 2px 6px; align-items: baseline;
    padding: 8px 9px; background: var(--uvp-list-toolbar-bg);
    border: 1px solid var(--uvp-panel-border); border-radius: 7px;
}
.probe-health-grid strong { color: var(--uvp-text-primary); font-family: ui-monospace, Menlo, monospace; font-size: 10.5px; }
.probe-health-grid em { grid-column: 1 / -1; color: var(--uvp-text-tertiary); font-size: 9px; font-style: normal; }
.frame-timeline {
    display: grid; gap: 7px; padding: 9px 10px;
    background: var(--uvp-list-toolbar-bg); border: 1px solid var(--uvp-panel-border); border-radius: 8px;
}
.frame-timeline.muted { opacity: 0.46; }
.frame-bars { display: flex; align-items: end; gap: 3px; height: 46px; border-bottom: 1px solid var(--uvp-panel-border); }
.frame-bars > span { flex: 1; min-width: 2px; border-radius: 2px 2px 0 0; transition: height 0.2s ease; }
.frame-bars > span.video { background: color-mix(in srgb, var(--uvp-brand) 68%, transparent); }
.frame-bars > span.audio { background: color-mix(in srgb, var(--uvp-brand-cyan) 68%, transparent); }
.frame-bars > span.keyframe { background: var(--uvp-warning); box-shadow: 0 0 5px color-mix(in srgb, var(--uvp-warning) 44%, transparent); }
.frame-legend { display: flex; align-items: center; justify-content: flex-end; gap: 9px; color: var(--uvp-text-tertiary); font-size: 8.5px; }
.frame-legend span { display: inline-flex; align-items: center; gap: 4px; }
.frame-legend i { width: 6px; height: 6px; border-radius: 2px; }
.frame-legend i.key { background: var(--uvp-warning); }
.frame-legend i.video { background: var(--uvp-brand); }
.frame-legend i.audio { background: var(--uvp-brand-cyan); }

/* ═══════════ 录制面板 ═══════════ */
.empty { padding: 24px; color: var(--uvp-text-tertiary); text-align: center; font-size: 11px; }

/* ═══════════ 高级面板 ═══════════ */
.adv-actions { display: grid; gap: 6px; }
.adv-btn {
    display: grid; grid-template-columns: 20px 1fr; gap: 10px; align-items: center;
    padding: 10px;
    color: var(--uvp-text-secondary); background: var(--uvp-list-toolbar-bg);
    border: 1px solid var(--uvp-panel-border); border-radius: 8px;
    cursor: pointer; text-align: left;
    transition: all 0.15s ease;
}
.adv-btn:hover { border-color: color-mix(in srgb, var(--uvp-brand) 40%, var(--uvp-panel-border)); }
.adv-btn > div { display: grid; gap: 2px; }
.adv-btn strong { color: var(--uvp-text-primary); font-size: 11.5px; font-weight: 500; }
.adv-btn small { color: var(--uvp-text-tertiary); font-size: 10px; }
.image-adjust { display: grid; gap: 6px; margin-top: 4px; }
.image-adjust label { display: grid; grid-template-columns: 60px 1fr; gap: 8px; align-items: center; color: var(--uvp-text-tertiary); font-size: 11px; }
.image-adjust input[type="range"] { accent-color: var(--uvp-brand); }

/* ═══════════ 通用按钮 ═══════════ */
.btn-primary, .btn-danger, .btn-ghost {
    display: inline-flex; align-items: center; justify-content: center; gap: 5px;
    padding: 7px 12px;
    color: #fff; background: var(--uvp-brand); border: 0; border-radius: 7px; cursor: pointer;
    font-size: 11.5px; font-weight: 600;
    transition: all 0.15s ease;
}
.btn-primary:hover:not(:disabled) { background: var(--uvp-brand-strong); }
.btn-primary:disabled { cursor: not-allowed; opacity: 0.5; }
.btn-primary.sm, .btn-ghost.sm { padding: 5px 10px; font-size: 11px; }
.btn-primary.xs, .btn-ghost.xs { padding: 4px 8px; font-size: 10.5px; }
.btn-danger { background: var(--uvp-danger); }
.btn-danger:hover:not(:disabled) { background: color-mix(in srgb, var(--uvp-danger) 85%, #000); }
.btn-ghost {
    color: var(--uvp-text-secondary); background: transparent;
    border: 1px solid var(--uvp-panel-border);
}
.btn-ghost:hover:not(:disabled) { color: var(--uvp-brand); border-color: var(--uvp-brand); background: var(--uvp-brand-soft); }
.btn-primary.block, .btn-ghost.block, .btn-danger.block { width: 100%; }

/* 响应式:窄屏堆叠 */
@media (max-width: 1080px) {
    .console-body { grid-template-columns: 1fr; }
    .stage { grid-column: 1; }
    .sidebar { max-height: none; }
}
@media (max-width: 640px) {
    .console-title { flex-wrap: wrap; }
    .tabs { grid-template-columns: repeat(3, 1fr); }
    .probe-data-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
    .lens-grid { grid-template-columns: 1fr; }
    .preset-grid { grid-template-columns: 1fr; }
}
</style>
