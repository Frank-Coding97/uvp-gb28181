<script setup lang="ts">
/**
 * PlayConsoleLinked - 播放控制台双区联动业务弹窗
 *
 * 设计定位:平台核心功能弹窗,承担点播 + 云台 + 探针 + 诊断 + 录制的一体化操作台
 *
 * 数据来源:点播、概况、探针、PTZ、能力、高级控制和对讲均走受保护后端接口。
 * 图像参数四个滑杆按产品决定保留为本地占位，不伪报设备下发成功。
 *
 * 视觉语言:深色为主,青色作强调,毛玻璃卡片,状态用色带 + 脉冲呼吸
 * 布局:右侧保留高频操作,播放器下方随 Tab 联动展示详情
 */
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import PlayWindow from "./PlayWindow.vue";
import {
    controlDevice,
    controlPtz,
    controlPtzCruise,
    controlPtzWiper,
    createCruiseTrack,
    controlPtzPrecise,
    callPtzPreset,
    createTalkSession,
    createPtzPreset,
    deletePtzPreset,
    deleteTalkSession,
    getControlCapabilities,
    getHomePosition,
    getPtzOperation,
    getPtzPreciseStatus,
    getTalkSession,
    getStreamMonitor,
    listCruiseTracks,
    listPtzPresets,
    runStreamProbe,
    startPlay,
    stopPlay,
    updateHomePosition,
    type ControlCapability,
    type CruiseTrackDetailResource,
    type CruiseTrackPointResource,
    type DeviceControlCapabilities,
    type HomePositionConfig,
    type HomePositionFreshness,
    type HomePositionPatch,
    type HomePositionResult,
    type HomePositionSupport,
    type PlayResult,
    type ProbeSnapshot,
    type PTZOperation,
    type PTZResourceFreshness,
    type StreamMonitorSnapshot,
    type TalkCreateResult,
} from "@/api/gb28181";
import {
    Activity,
    AlertTriangle,
    ArrowDown,
    ArrowDownLeft,
    ArrowDownRight,
    ArrowLeft,
    ArrowRight,
    ArrowUp,
    ArrowUpLeft,
    ArrowUpRight,
    CheckCircle2,
    ChevronDown,
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
    Inbox,
    Pause,
    Play,
    Plus,
    RadioTower,
    RefreshCcw,
    Route,
    Search,
    Settings,
    ShieldCheck,
    Signal,
    Square,
    Target,
    Trash2,
    Video,
    X,
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

type SessionPhase = "idle" | "requesting" | "playing" | "paused" | "error" | "stopping";
const phase = ref<SessionPhase>("idle");
const errorMessage = ref("");
const startedAt = ref<number | null>(null);
const now = ref(Date.now());
const playResult = ref<PlayResult | null>(null);
let timer: number | null = null;
let monitorTimer: number | null = null;
let sessionToken = 0;
// 关闭弹窗只做本地清理;记录阈值,阻止迟到的点播响应补发 stopPlay。
let localCleanupThroughToken = 0;

/* ────────────────────────── Tab 切换 ────────────────────────── */

type TabKey = "stream" | "ptz" | "probe" | "advanced";
const activeTab = ref<TabKey>("ptz");
const sideCollapsed = ref(false);

const tabs: Array<{ key: TabKey; label: string; icon: any; description: string }> = [
    { key: "stream", label: "流信息", icon: Signal, description: "实时媒体与会话指标" },
    { key: "ptz", label: "云台控制", icon: Compass, description: "GB28181-2022 全能力" },
    { key: "probe", label: "视频探针", icon: Activity, description: "逐帧采样 · 时间戳" },
    { key: "advanced", label: "高级", icon: Settings, description: "关键帧 · 布防 · 重启" },
];

/* ────────────────────────── 视频区状态 ────────────────────────── */

type StreamProtocol = "ws-flv" | "http-flv" | "hls" | "webrtc" | "rtmp" | "rtsp";
const protocol = ref<StreamProtocol>("ws-flv");
const protocolUrls = computed<Record<StreamProtocol, string | null>>(() => {
    const result = playResult.value;
    return {
        "ws-flv": result?.urls?.wsFlv || result?.wsflvUrl || null,
        "http-flv": result?.urls?.httpFlv || result?.httpFlvUrl || null,
        hls: result?.urls?.hls || result?.hlsUrl || null,
        webrtc: result?.urls?.webrtc || null,
        rtmp: result?.urls?.rtmp || null,
        rtsp: result?.urls?.rtsp || null,
    };
});

const protocolOptions: Array<{ value: StreamProtocol; label: string; desc: string }> = [
    { value: "ws-flv", label: "WS-FLV", desc: "延迟最低,适合实时监控" },
    { value: "http-flv", label: "HTTP-FLV", desc: "兼容性好,延迟较低" },
    { value: "hls", label: "HLS", desc: "兼容性最佳,延迟较高" },
    { value: "webrtc", label: "WebRTC", desc: "超低延迟,需 HTTPS" },
    { value: "rtmp", label: "RTMP", desc: "传统直播协议" },
    { value: "rtsp", label: "RTSP", desc: "监控设备标准" },
];

const currentProtocolUrl = computed(() => protocolUrls.value[protocol.value] || "");
function isBrowserPlayable(proto: StreamProtocol) {
    return proto === "ws-flv" || proto === "http-flv" || proto === "hls";
}

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
    if (phase.value === "paused") return "已暂停";
    if (phase.value === "stopping") return "停播中";
    if (phase.value === "error") return "异常";
    if (phase.value === "playing") {
        if (monitorState.value === "offline") return "媒体流已离线";
        if (monitorState.value === "stale") return "监控数据过期";
        return "播放中";
    }
    return "待播放";
});

const sessionStatusClass = computed(() => ({
    active: phase.value === "playing" && monitorState.value !== "offline" && monitorState.value !== "stale",
    loading: phase.value === "requesting" || phase.value === "stopping",
    error: phase.value === "error" || (phase.value === "playing" && monitorState.value === "offline"),
    paused: phase.value === "paused",
    warn: phase.value === "playing" && monitorState.value === "stale",
}));

const monitorAlive = ref<{ seconds: number; receivedAt: number } | null>(null);
const elapsedText = computed(() => {
    if (monitorAlive.value) {
        const seconds = monitorAlive.value.seconds + Math.floor(Math.max(0, now.value - monitorAlive.value.receivedAt) / 1000);
        return formatDuration(seconds * 1000);
    }
    return startedAt.value ? formatDuration(Math.max(0, now.value - startedAt.value)) : "00:00";
});
const monitorCollectedAtText = computed(() => {
    const value = monitorSnapshot.value?.collectedAt;
    if (!value) return "—";
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? value : date.toLocaleTimeString("zh-CN", { hour12: false });
});

/* ────────────────────────── 云台与设备能力 ────────────────────────── */

const capabilities = ref<DeviceControlCapabilities | null>(null);
function capability(key: keyof DeviceControlCapabilities): ControlCapability {
    const value = capabilities.value?.[key];
    return value || { state: "unknown", reason: "能力尚未读取" };
}
const isAudioCapable = computed(() => {
    const broadcast = capability("broadcast").state;
    const talk = capability("talk").state;
    return props.channel?.status === 1 && (broadcast === "supported" || talk === "supported");
});
const broadcastAvailable = computed(() => capability("broadcast").state === "supported");
const talkAvailable = computed(() => capability("talk").state === "supported");

const ptzMode = ref<"speed" | "precise">("speed"); // 速度模式 / 精准模式
const moveSpeed = ref(6); // 1-10 步进,转发时 * 25 得 GB28181 1-255
const focusMode = ref<"auto" | "manual">("auto");
const irisMode = ref<"auto" | "manual">("auto");

// 精准 PTZ(2022)
const precisePan = ref(180);   // 0-360
const preciseTilt = ref(0);    // -90 to 90
const preciseZoom = ref(1);    // 1-32x

// 预置位与巡航只展示后端返回的真实资源;摘要和抽屉的布局保持原设计。
type Preset = { id: number; name: string; setAt?: string };
const presets = ref<Preset[]>([]);
const activePresetId = ref<number | null>(null);
// 「保存当前位置」小对话框的草稿态:自动生成默认名,用户可改可不改
const presetDraft = ref<{ id: number; name: string; submitting: boolean } | null>(null);
const savePresetDialogVisible = ref(false);
const presetNameTouched = ref(false);
// 详情区固定为紧凑三行九格;溢出时保留八个预置位,将“更多”放在最后一格。
// 不按网格自身高度反推数量,避免内容撑高网格后又放入更多项的反馈回路。
const PRESET_GRID_SLOTS = 9;
const maxVisibleTiles = PRESET_GRID_SLOTS;
const hasMorePresets = computed(() => presets.value.length > maxVisibleTiles);
const visiblePresets = computed(() =>
    hasMorePresets.value ? presets.value.slice(0, maxVisibleTiles - 1) : presets.value,
);
const presetMoreVisible = ref(false);
function nextPresetId(): number {
    return presets.value.length ? Math.max(...presets.value.map((p) => p.id)) + 1 : 1;
}

// 巡航轨迹(2022 CruiseTrackListQuery / CruiseTrackQuery)
type CruisePoint = { presetId: number; speed: number | null; dwellSec: number | null };
type CruiseTrack = {
    id: number;
    name: string;
    enabled: boolean;
    pending: boolean;
    points: CruisePoint[] | null;
    source: string;
};
const cruiseTracks = ref<CruiseTrack[]>([]);
const activeCruiseId = ref<number | null>(null);
// HTTP 成功只表示控制指令已发送，不能据此宣称设备正在运行。
const cruiseState = ref<"stopped" | "start-sent">("stopped");
const cruiseFreshness = ref<PTZResourceFreshness>("unknown");
const cruiseRefreshPending = ref(false);
const cruiseLoadError = ref("");
const cruiseRefreshError = ref("");
const unconfirmedCruiseCount = computed(() => cruiseTracks.value.filter((track) => track.pending).length);
const cruiseSyncLabel = computed(() => {
    if (cruiseLoadError.value) return "加载失败";
    if (cruiseRefreshError.value) return "同步失败";
    if (unconfirmedCruiseCount.value > 0) return `${unconfirmedCruiseCount.value} 条未验证`;
    if (cruiseRefreshPending.value) return "正在同步";
    if (cruiseFreshness.value === "fresh") return "设备数据已同步";
    if (cruiseFreshness.value === "stale") return "缓存数据已过期";
    return "设备状态待查询";
});
const CRUISE_RECONCILE_DELAYS_MS = [1000, 2000, 4000, 8000] as const;
let cruiseReconcileTimer: number | null = null;
let cruiseReconcileAttempt = 0;
function clearCruiseReconcilePolling() {
    if (cruiseReconcileTimer !== null) window.clearTimeout(cruiseReconcileTimer);
    cruiseReconcileTimer = null;
    cruiseReconcileAttempt = 0;
}
function scheduleCruiseReconcilePolling(channelId: number, token: number) {
    clearCruiseReconcilePolling();
    const pollNext = () => {
        if (cruiseReconcileAttempt >= CRUISE_RECONCILE_DELAYS_MS.length) return;
        const delay = CRUISE_RECONCILE_DELAYS_MS[cruiseReconcileAttempt];
        cruiseReconcileTimer = window.setTimeout(async () => {
            cruiseReconcileTimer = null;
            if (token !== sessionToken || props.channel?.id !== channelId) return;
            await loadCruises(channelId, token, false);
            if (token !== sessionToken || props.channel?.id !== channelId) return;
            cruiseReconcileAttempt += 1;
            pollNext();
        }, delay);
    };
    pollNext();
}
// 巡航卡片布局 1:1 复刻预置位 —— 3 列 × 3 行 = 9 格,溢出保留 8 条 tile + 1 「更多」chip
const CRUISE_GRID_SLOTS = 9;
const RESOURCE_TOOLTIP_ENTER_DELAY_MS = 80;
const maxVisibleCruiseTracks = CRUISE_GRID_SLOTS;
const hasMoreCruises = computed(() => cruiseTracks.value.length > maxVisibleCruiseTracks);
const visibleCruiseTracks = computed(() =>
    hasMoreCruises.value ? cruiseTracks.value.slice(0, maxVisibleCruiseTracks - 1) : cruiseTracks.value,
);
const cruiseMoreVisible = ref(false);
function cruiseTileState(c: { id: number; enabled: boolean; pending: boolean }): "stop" | "idle" | "disabled" | "pending" {
    if (activeCruiseId.value === c.id && cruiseState.value === "start-sent") return "stop";
    if (c.pending) return "pending";
    if (!c.enabled) return "disabled";
    return "idle";
}
function cruiseTileTitle(c: { id: number; name: string; enabled: boolean; pending: boolean }): string {
    const state = cruiseTileState(c);
    if (state === "pending") return `#${c.id} ${c.name} · 配置指令已发送，但设备尚未返回 GB/T 28181-2022 巡航轨迹查询结果；点击试运行验证`;
    if (state === "disabled") return `#${c.id} ${c.name} · 已禁用`;
    if (state === "stop") return `#${c.id} ${c.name} · 启动指令已发送,点击停止`;
    return `#${c.id} ${c.name} · 点击开始巡航`;
}

function parseCruiseDetail(raw: string | CruiseTrackDetailResource | null | undefined): {
    points: CruisePoint[] | null;
    source: string;
} {
    let detail: CruiseTrackDetailResource | null = null;
    if (typeof raw === "string" && raw.trim()) {
        try {
            detail = JSON.parse(raw) as CruiseTrackDetailResource;
        } catch {
            detail = null;
        }
    } else if (raw && typeof raw === "object") {
        detail = raw;
    }
    if (!detail) return { points: null, source: "" };

    const rawPoints = Array.isArray(detail.cruisePoints)
        ? detail.cruisePoints
        : Array.isArray(detail.stops)
            ? detail.stops
            : null;
    if (!rawPoints) return { points: null, source: detail.source || "" };

    const globalSpeed = Number.isFinite(Number(detail.speed)) ? Number(detail.speed) : null;
    const globalDwell = Number.isFinite(Number(detail.dwellSec)) ? Number(detail.dwellSec) : null;
    const points = rawPoints.flatMap((item: CruiseTrackPointResource) => {
        const presetId = Number(item.presetIndex ?? item.presetId);
        if (!Number.isInteger(presetId) || presetId < 1 || presetId > 255) return [];
        const speed = Number.isFinite(Number(item.speed)) ? Number(item.speed) : globalSpeed;
        const dwell = Number.isFinite(Number(item.stayTime ?? item.dwellSec))
            ? Number(item.stayTime ?? item.dwellSec)
            : globalDwell;
        return [{ presetId, speed, dwellSec: dwell }];
    });
    return { points, source: detail.source || "" };
}

function cruiseTrackDetailText(track: CruiseTrack): string {
    if (track.points === null) return "详情待查询";
    if (track.points.length === 0) return "暂无点位";
    const dwellValues = [...new Set(track.points.map((point) => point.dwellSec).filter((value): value is number => value !== null))];
    const dwellText = dwellValues.length === 1 ? ` · 停留 ${dwellValues[0]}s` : dwellValues.length > 1 ? " · 按点位停留" : "";
    return `${track.points.length} 个点位${dwellText}`;
}

function cruiseTrackMeta(track: CruiseTrack): string {
    const detail = cruiseTrackDetailText(track);
    return track.pending ? `${detail} · 已下发,未验证` : detail;
}

// 建立巡航 modal:trackId 使用国标 0-255，stops 按顺序引用已有预置位。
type CruiseDraftStop = { key: string; presetId: number };
type CruiseDraft = {
    trackId: number;
    name: string;
    speed: number;
    dwellSec: number;
    sendSpeed: boolean;
    sendDwell: boolean;
    stops: CruiseDraftStop[];
    replaceExisting: boolean;
    submitting: boolean;
    idempotencyKey: string;
};
const saveCruiseDialogVisible = ref(false);
const cruiseDraft = ref<CruiseDraft | null>(null);
const cruiseDraftTouched = ref(false);
const cruiseDraftSubmitError = ref("");
const cruiseStopsListEl = ref<HTMLElement | null>(null);
function nextCruiseTrackId(): number {
    if (cruiseTracks.value.length === 0) return 1;
    const used = new Set(cruiseTracks.value.map((track) => track.id));
    const afterMax = Math.max(...used) + 1;
    if (afterMax <= 255) return afterMax;
    for (let id = 0; id <= 255; id++) {
        if (!used.has(id)) return id;
    }
    return 0;
}
function newCruiseStop(presetId: number): CruiseDraftStop {
    return { key: `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`, presetId };
}
function newCruiseIdempotencyKey(): string {
    return `cruise-create-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
}
const cruiseDraftError = computed(() => {
    if (!cruiseDraftTouched.value || !cruiseDraft.value) return "";
    const draft = cruiseDraft.value;
    if (!Number.isInteger(draft.trackId) || draft.trackId < 0 || draft.trackId > 255) return "巡航编号必须在 0-255 之间";
    if (!draft.replaceExisting && cruiseTracks.value.some((c) => c.id === draft.trackId)) return `巡航编号 #${draft.trackId} 已存在,请换一个或勾选清空重建`;
    if (draft.name.length > 32) return "名称最多 32 个字符";
    if (draft.stops.length === 0) return "至少选择 1 个站点";
    if (draft.stops.length > 32) return "站点数量最多 32 个";
    if (draft.stops.some((s) => !presets.value.some((p) => p.id === s.presetId))) return "存在无效的预置位站点";
    if (draft.sendSpeed && (!Number.isInteger(draft.speed) || draft.speed < 1 || draft.speed > 4095)) return "速度必须在 1-4095 之间";
    if (draft.sendDwell && (!Number.isInteger(draft.dwellSec) || draft.dwellSec < 1 || draft.dwellSec > 4095)) return "停留时间必须在 1-4095 秒之间";
    return "";
});
function openSaveCruiseDialog() {
    if (!props.channel || presets.value.length === 0) return;
    const firstPreset = presets.value[0].id;
    cruiseDraft.value = {
        trackId: nextCruiseTrackId(),
        name: `巡航 ${nextCruiseTrackId()}`,
        speed: 128,
        dwellSec: 5,
        sendSpeed: true,
        sendDwell: true,
        stops: [newCruiseStop(firstPreset)],
        replaceExisting: false,
        submitting: false,
        idempotencyKey: newCruiseIdempotencyKey(),
    };
    cruiseDraftTouched.value = false;
    cruiseDraftSubmitError.value = "";
    saveCruiseDialogVisible.value = true;
}
function closeSaveCruiseDialog() {
    if (cruiseDraft.value?.submitting) return;
    saveCruiseDialogVisible.value = false;
    cruiseDraft.value = null;
    cruiseDraftTouched.value = false;
    cruiseDraftSubmitError.value = "";
}
function canCloseSaveCruiseDialog() {
    return !cruiseDraft.value?.submitting;
}
async function addCruiseStop() {
    if (!cruiseDraft.value || cruiseDraft.value.submitting || cruiseDraft.value.stops.length >= 32) return;
    const usedPresetIds = new Set(cruiseDraft.value.stops.map((stop) => stop.presetId));
    const fallback = presets.value.find((preset) => !usedPresetIds.has(preset.id))?.id ?? presets.value[0]?.id;
    if (!fallback) return;
    cruiseDraft.value.stops.push(newCruiseStop(fallback));
    await nextTick();
    if (cruiseStopsListEl.value) cruiseStopsListEl.value.scrollTop = cruiseStopsListEl.value.scrollHeight;
}
function removeCruiseStop(index: number) {
    if (!cruiseDraft.value || cruiseDraft.value.submitting) return;
    cruiseDraft.value.stops.splice(index, 1);
}
function moveCruiseStop(index: number, delta: number) {
    if (!cruiseDraft.value || cruiseDraft.value.submitting) return;
    const stops = cruiseDraft.value.stops;
    const target = index + delta;
    if (target < 0 || target >= stops.length) return;
    const [item] = stops.splice(index, 1);
    stops.splice(target, 0, item);
}
async function handleSaveCruiseBeforeOk(done: (closable?: boolean) => void) {
    if (!cruiseDraft.value || !props.channel) { done(false); return; }
    if (cruiseDraft.value.submitting) { done(false); return; }
    cruiseDraftTouched.value = true;
    if (cruiseDraftError.value) { done(false); return; }
    const draft = cruiseDraft.value;
    const channelId = props.channel.id;
    const token = sessionToken;
    draft.submitting = true;
    cruiseDraftSubmitError.value = "";
    try {
        const response = await createCruiseTrack(channelId, {
            trackId: draft.trackId,
            name: draft.name.trim(),
            speed: draft.sendSpeed ? draft.speed : 0,
            dwellSec: draft.sendDwell ? draft.dwellSec : 0,
            stops: draft.stops.map((s) => ({ presetId: s.presetId })),
            replaceExisting: draft.replaceExisting,
            idempotencyKey: draft.idempotencyKey,
        });
        if (token !== sessionToken || props.channel?.id !== channelId || cruiseDraft.value !== draft) {
            done(false);
            return;
        }
        if (response.code === 0) {
            Message.info(`巡航 #${draft.trackId} 配置指令已发送,待设备对账 (${response.data?.completedStops ?? 0}/${response.data?.totalStops ?? 0} 站)`);
            await loadCruises(channelId, token, false);
            if (token !== sessionToken || props.channel?.id !== channelId || cruiseDraft.value !== draft) {
                done(false);
                return;
            }
            scheduleCruiseReconcilePolling(channelId, token);
            done(true);
            cruiseDraft.value = null;
            cruiseDraftTouched.value = false;
            cruiseDraftSubmitError.value = "";
            return;
        }
        const data = response.data;
        const completed = data?.completedStops ?? 0;
        const total = data?.totalStops ?? draft.stops.length;
        cruiseDraftSubmitError.value = `已有 ${completed}/${total} 站指令发送,请关闭后刷新设备状态再重建:${response.message || data?.error || "未知原因"}`;
        Message.warning(`巡航 #${draft.trackId} 配置指令部分发送 (${completed}/${total} 站),请先核对设备状态`);
        scheduleCruiseReconcilePolling(channelId, token);
        done(false);
    } catch (error: any) {
        if (token === sessionToken && props.channel?.id === channelId && cruiseDraft.value === draft) {
            cruiseDraftSubmitError.value = error?.message || "建立巡航失败";
            Message.error(cruiseDraftSubmitError.value);
        }
        done(false);
    } finally {
        if (cruiseDraft.value === draft) draft.submitting = false;
    }
}

type AssetManagerTab = "preset" | "cruise";
const assetManagerVisible = ref(false);
const assetManagerTab = ref<AssetManagerTab>("preset");
const assetSearch = ref("");
const filteredPresets = computed(() => {
    const keyword = assetSearch.value.trim().toLowerCase();
    if (!keyword) return presets.value;
    return presets.value.filter((item) => item.name.toLowerCase().includes(keyword) || String(item.id).includes(keyword));
});
const filteredCruiseTracks = computed(() => {
    const keyword = assetSearch.value.trim().toLowerCase();
    if (!keyword) return cruiseTracks.value;
    return cruiseTracks.value.filter((item) => item.name.toLowerCase().includes(keyword) || String(item.id).includes(keyword));
});

// 看守位(2022 HomePositionQuery):设备确认值与用户草稿必须独立。
type HomePositionPhase = "unknown" | "loading" | "pending" | "accepted" | "enabled" | "disabled" | "error";
type HomePositionPending = { kind: "control" | "refresh"; operationId: string | null; deadlineAt: string | null };
type HomePositionDraft = { enabled: boolean; presetId: number | null; resetTime: number | null };

const unknownHomeSupport = (): HomePositionSupport => ({ status: "unknown", reason: "能力尚未确认" });
const homeConfirmed = ref<HomePositionConfig | null>(null);
const homeDraft = ref<HomePositionDraft>({ enabled: false, presetId: null, resetTime: 300 });
const homePhase = ref<HomePositionPhase>("unknown");
const homeFreshness = ref<HomePositionFreshness>("unknown");
const homeControlSupport = ref<HomePositionSupport>(unknownHomeSupport());
const homeQuerySupport = ref<HomePositionSupport>(unknownHomeSupport());
const homePending = ref<HomePositionPending | null>(null);
const homeOperationId = ref<string | null>(null);
const homeError = ref("");
const homeMismatch = ref("");
const homeFailureLabel = ref("加载失败");

const homePositionCanSave = computed(() => {
    if (!homeDraft.value.enabled) return true;
    const { presetId, resetTime } = homeDraft.value;
    return Number.isInteger(presetId)
        && Number(presetId) >= 0
        && Number(presetId) <= 255
        && Number.isInteger(resetTime)
        && Number(resetTime) >= 10
        && Number(resetTime) <= 3600;
});
const homeControlPending = computed(() => homePending.value?.kind === "control");
const homeCanSubmit = computed(() => props.channel?.status === 1 && homePending.value === null && homePositionCanSave.value);
const homeCanRefresh = computed(() => props.channel?.status === 1 && homePending.value === null);
const homeIsBusy = computed(() => homePhase.value === "loading" || homePending.value !== null);
const homePhaseText = computed(() => {
    if (homePhase.value === "unknown") return homeOperationId.value ? "结果未知" : "待查询";
    return ({
        loading: "正在加载",
        pending: "等待设备确认",
        accepted: "设备已确认，状态待读取",
        enabled: "已启用",
        disabled: "已关闭",
        error: homeFailureLabel.value,
    } as const)[homePhase.value];
});
const homeFreshnessText = computed(() => ({ fresh: "数据新鲜", stale: "缓存已过期", unknown: "新鲜度未知" })[homeFreshness.value]);
const homeConfirmedStatusText = computed(() => {
    if (!homeConfirmed.value) return "尚无设备确认配置";
    return homeConfirmed.value.enabled ? "设备确认已启用" : "设备确认已关闭";
});
const homeConfirmedValuesText = computed(() => {
    const confirmed = homeConfirmed.value;
    if (!confirmed) return "预置位 未返回 · 等待时间 未返回";
    const preset = confirmed.presetId == null ? "未返回" : `#${confirmed.presetId}`;
    const resetTime = confirmed.resetTime == null ? "未返回" : `${confirmed.resetTime} 秒`;
    return `预置位 ${preset} · 等待时间 ${resetTime}`;
});
const homeConfirmedOutsideEditableRange = computed(() => {
    const confirmed = homeConfirmed.value;
    if (!confirmed?.enabled) return false;
    return confirmed.presetId == null
        || !Number.isInteger(confirmed.presetId)
        || confirmed.presetId < 0
        || confirmed.presetId > 255
        || confirmed.resetTime == null
        || !Number.isInteger(confirmed.resetTime)
        || confirmed.resetTime < 10
        || confirmed.resetTime > 3600;
});
const homeSupportHasRisk = computed(() =>
    homeControlSupport.value.status !== "supported" || homeQuerySupport.value.status !== "supported",
);
const homeVerificationText = computed(() => {
    if (homeConfirmed.value?.verification === "unverified") return "设备已确认，查询未验证";
    if (homeConfirmed.value?.verification === "verified") return "设备查询已验证";
    return "尚未验证";
});

function resetHomePositionState() {
    clearHomePositionPolling(true);
    homeConfirmed.value = null;
    homeDraft.value = { enabled: false, presetId: null, resetTime: 300 };
    homePhase.value = "unknown";
    homeFreshness.value = "unknown";
    homeControlSupport.value = unknownHomeSupport();
    homeQuerySupport.value = unknownHomeSupport();
    homePending.value = null;
    homeOperationId.value = null;
    homeError.value = "";
    homeMismatch.value = "";
    homeFailureLabel.value = "加载失败";
}

function applyHomePositionResult(result: HomePositionResult) {
    homeConfirmed.value = result.homePosition ? { ...result.homePosition } : null;
    homeDraft.value = result.homePosition
        ? { enabled: result.homePosition.enabled, presetId: result.homePosition.presetId, resetTime: result.homePosition.resetTime }
        : { enabled: false, presetId: null, resetTime: 300 };
    homeFreshness.value = result.freshness;
    homeControlSupport.value = result.controlSupport;
    homeQuerySupport.value = result.querySupport;
    homeError.value = "";
    homeMismatch.value = "";
    homeFailureLabel.value = "操作失败";

    if (result.control.status === "pending" && result.control.operationId) {
        homePending.value = { kind: "control", operationId: result.control.operationId, deadlineAt: result.control.deadlineAt };
    } else if (result.refresh.status === "pending" && result.refresh.operationId) {
        homePending.value = { kind: "refresh", operationId: result.refresh.operationId, deadlineAt: result.refresh.deadlineAt };
    } else {
        homePending.value = null;
    }

    homeOperationId.value = homePending.value?.operationId
        ?? result.control.operationId
        ?? result.refresh.operationId;

    const terminalError = result.control.errorCode ?? result.refresh.errorCode;
    if (terminalError === "HOME_POSITION_RECONCILE_MISMATCH") {
        homeMismatch.value = "设备未按请求应用配置";
    } else if (terminalError) {
        homeError.value = terminalError;
    }

    if (homePending.value) {
        homePhase.value = "pending";
    } else if (result.control.status === "unknown") {
        homePhase.value = "unknown";
    } else if (["rejected", "timeout", "cancelled"].includes(result.control.status)) {
        homePhase.value = "error";
    } else if (!result.homePosition && ["failed", "timeout"].includes(result.refresh.status)) {
        homePhase.value = "error";
    } else if (result.homePosition) {
        homePhase.value = result.homePosition.enabled ? "enabled" : "disabled";
    } else {
        homePhase.value = "unknown";
    }
}

const HOME_POSITION_POLL_INTERVAL_MS = 1000;
const HOME_POSITION_INITIAL_QUEUE_MS = 5000;
const HOME_POSITION_DEADLINE_GRACE_MS = 2000;
let homePositionPollTimer: number | null = null;
let homePositionRequestDeadlineTimer: number | null = null;
let homePositionGeneration = 0;
let homePositionInsuranceDeadlineMs: number | null = null;
let homePositionDeadlineRecheckUsed = false;
let homePositionTrackedOperationId: string | null = null;
let homePositionIdempotencySequence = 0;

function clearHomePositionPolling(invalidate = false) {
    if (homePositionPollTimer !== null) window.clearTimeout(homePositionPollTimer);
    if (homePositionRequestDeadlineTimer !== null) window.clearTimeout(homePositionRequestDeadlineTimer);
    homePositionPollTimer = null;
    homePositionRequestDeadlineTimer = null;
    homePositionInsuranceDeadlineMs = null;
    homePositionDeadlineRecheckUsed = false;
    homePositionTrackedOperationId = null;
    if (invalidate) homePositionGeneration += 1;
}

function getHomePositionOperationWithDeadline(channelId: number, operationId: string) {
    const request = getPtzOperation(channelId, operationId);
    if (homePositionInsuranceDeadlineMs === null) return request;
    const remainingMs = homePositionInsuranceDeadlineMs - Date.now();
    if (remainingMs <= 0) return Promise.reject(new Error("操作状态请求超过服务端截止时间"));
    return new Promise<Awaited<ReturnType<typeof getPtzOperation>>>((resolve, reject) => {
        let settled = false;
        const deadlineTimer = window.setTimeout(() => {
            if (settled) return;
            settled = true;
            if (homePositionRequestDeadlineTimer === deadlineTimer) homePositionRequestDeadlineTimer = null;
            reject(new Error("操作状态请求超过服务端截止时间"));
        }, remainingMs);
        homePositionRequestDeadlineTimer = deadlineTimer;
        request.then(
            (response) => {
                if (settled) return;
                settled = true;
                window.clearTimeout(deadlineTimer);
                if (homePositionRequestDeadlineTimer === deadlineTimer) homePositionRequestDeadlineTimer = null;
                resolve(response);
            },
            (error) => {
                if (settled) return;
                settled = true;
                window.clearTimeout(deadlineTimer);
                if (homePositionRequestDeadlineTimer === deadlineTimer) homePositionRequestDeadlineTimer = null;
                reject(error);
            },
        );
    });
}

function beginHomePositionOperation() {
    clearHomePositionPolling(false);
    homePositionGeneration += 1;
    return homePositionGeneration;
}

function isCurrentHomePositionContext(channelId: number, token: number, generation: number) {
    return props.visible
        && token === sessionToken
        && generation === homePositionGeneration
        && props.channel?.id === channelId;
}

function nextHomePositionIdempotencyKey(kind: "control" | "refresh", channelId: number) {
    homePositionIdempotencySequence += 1;
    const nonce = globalThis.crypto?.randomUUID?.()
        ?? `${Date.now()}-${homePositionIdempotencySequence}-${Math.random().toString(36).slice(2)}`;
    return `home-${kind}-${channelId}-${nonce}`;
}

function restoreHomeDraftFromConfirmed() {
    const confirmed = homeConfirmed.value;
    homeDraft.value = confirmed
        ? { enabled: confirmed.enabled, presetId: confirmed.presetId, resetTime: confirmed.resetTime }
        : { enabled: false, presetId: null, resetTime: 300 };
}

function prepareHomePositionOperation(operationId: string) {
    if (homePositionTrackedOperationId === operationId) return;
    homePositionTrackedOperationId = operationId;
    homePositionInsuranceDeadlineMs = null;
    homePositionDeadlineRecheckUsed = false;
}

function updateHomePositionDeadline(deadlineAt: string | null) {
    if (!deadlineAt) return false;
    const deadlineMs = Date.parse(deadlineAt);
    if (!Number.isFinite(deadlineMs) || deadlineMs <= Date.now()) return false;
    homePositionInsuranceDeadlineMs = deadlineMs + HOME_POSITION_DEADLINE_GRACE_MS;
    return true;
}

function markHomePositionUnknown(pending: HomePositionPending, message: string) {
    const operationId = pending.operationId;
    clearHomePositionPolling(false);
    homePending.value = null;
    homeOperationId.value = operationId;
    homeFailureLabel.value = "操作结果未知";
    homePhase.value = "unknown";
    homeError.value = message;
    homeMismatch.value = "";
    if (pending.kind === "control") restoreHomeDraftFromConfirmed();
    if (pending.kind === "refresh" && homeConfirmed.value) homeFreshness.value = "stale";
}

function finishHomePositionAcceptedReadFailure(pending: HomePositionPending, message: string) {
    const operationId = pending.operationId;
    clearHomePositionPolling(false);
    homePending.value = null;
    homeOperationId.value = operationId;
    homeFailureLabel.value = "确认状态读取失败";
    homePhase.value = "accepted";
    homeFreshness.value = homeConfirmed.value ? "stale" : "unknown";
    homeError.value = `设备已确认，但确认状态读取失败，可重试${message ? `：${message}` : ""}`;
    homeMismatch.value = "";
}

function finishHomePositionFailure(pending: HomePositionPending, operation: PTZOperation) {
    clearHomePositionPolling(false);
    homePending.value = null;
    homeOperationId.value = operation.operationId;
    homeFailureLabel.value = "操作失败";
    homeError.value = operation.errorCode || operation.errorMessage || `操作状态: ${operation.status}`;
    homeMismatch.value = "";
    if (pending.kind === "control") {
        restoreHomeDraftFromConfirmed();
        homePhase.value = "error";
        return;
    }
    homeFreshness.value = "stale";
    homePhase.value = homeConfirmed.value
        ? (homeConfirmed.value.enabled ? "enabled" : "disabled")
        : "error";
}

function scheduleHomePositionPoll(channelId: number, token: number, generation: number) {
    if (!isCurrentHomePositionContext(channelId, token, generation)) return;
    if (homePositionPollTimer !== null) window.clearTimeout(homePositionPollTimer);
    const pending = homePending.value;
    if (!pending?.operationId) return;
    const nowMs = Date.now();
    if (homePositionInsuranceDeadlineMs !== null && nowMs >= homePositionInsuranceDeadlineMs) {
        markHomePositionUnknown(pending, "操作超过服务端截止时间，结果未知，可重试");
        return;
    }
    const delay = homePositionInsuranceDeadlineMs === null
        ? HOME_POSITION_POLL_INTERVAL_MS
        : Math.min(HOME_POSITION_POLL_INTERVAL_MS, homePositionInsuranceDeadlineMs - nowMs);
    homePositionPollTimer = window.setTimeout(() => {
        homePositionPollTimer = null;
        void pollHomePositionOperation(channelId, token, generation);
    }, delay);
}

async function recheckHomePositionDeadline(channelId: number, token: number, generation: number) {
    const pending = homePending.value;
    if (!pending?.operationId || !isCurrentHomePositionContext(channelId, token, generation)) return;
    if (homePositionDeadlineRecheckUsed) {
        markHomePositionUnknown(pending, "服务端未返回有效操作截止时间，结果未知，可重试");
        return;
    }
    homePositionDeadlineRecheckUsed = true;
    try {
        const response = await getHomePosition(channelId);
        if (!isCurrentHomePositionContext(channelId, token, generation)) return;
        if (response.code !== 0 || !response.data) throw new Error(response.message || "重读看守位状态失败");
        applyHomePositionResult(response.data);
        const latest = homePending.value;
        if (!latest?.operationId) {
            clearHomePositionPolling(false);
            return;
        }
        prepareHomePositionOperation(latest.operationId);
        if (!updateHomePositionDeadline(latest.deadlineAt)) {
            markHomePositionUnknown(latest, "服务端未返回有效操作截止时间，结果未知，可重试");
            return;
        }
        scheduleHomePositionPoll(channelId, token, generation);
    } catch (error: any) {
        if (!isCurrentHomePositionContext(channelId, token, generation)) return;
        const latest = homePending.value;
        if (!latest) return;
        if (homePositionInsuranceDeadlineMs !== null && Date.now() < homePositionInsuranceDeadlineMs) {
            scheduleHomePositionPoll(channelId, token, generation);
            return;
        }
        markHomePositionUnknown(latest, error?.message || "无法读取操作截止时间，结果未知，可重试");
    }
}

function resumeHomePositionPending(channelId: number, token: number, generation: number) {
    const pending = homePending.value;
    if (!pending?.operationId || !isCurrentHomePositionContext(channelId, token, generation)) {
        if (!pending) clearHomePositionPolling(false);
        return;
    }
    prepareHomePositionOperation(pending.operationId);
    if (!updateHomePositionDeadline(pending.deadlineAt)) {
        void recheckHomePositionDeadline(channelId, token, generation);
        return;
    }
    scheduleHomePositionPoll(channelId, token, generation);
}

async function refreshHomePositionAfterAccepted(channelId: number, token: number, generation: number) {
    const completedPending = homePending.value;
    if (!completedPending || !isCurrentHomePositionContext(channelId, token, generation)) return;
    try {
        const response = await getHomePosition(channelId);
        if (!isCurrentHomePositionContext(channelId, token, generation)) return;
        if (response.code !== 0 || !response.data) throw new Error(response.message || "读取确认状态失败");
        applyHomePositionResult(response.data);
        resumeHomePositionPending(channelId, token, generation);
    } catch (error: any) {
        if (!isCurrentHomePositionContext(channelId, token, generation)) return;
        if (completedPending.kind === "refresh" && homeConfirmed.value) {
            clearHomePositionPolling(false);
            homePending.value = null;
            homeFreshness.value = "stale";
            homePhase.value = homeConfirmed.value.enabled ? "enabled" : "disabled";
            homeFailureLabel.value = "操作失败";
            homeError.value = error?.message || "读取刷新结果失败";
            return;
        }
        if (completedPending.kind === "control") {
            finishHomePositionAcceptedReadFailure(completedPending, error?.message || "请重试");
            return;
        }
        markHomePositionUnknown(completedPending, error?.message || "设备已应答，但确认状态读取失败");
    }
}

async function pollHomePositionOperation(channelId: number, token: number, generation: number) {
    const pending = homePending.value;
    if (!pending?.operationId || !isCurrentHomePositionContext(channelId, token, generation)) return;
    if (homePositionInsuranceDeadlineMs !== null && Date.now() >= homePositionInsuranceDeadlineMs) {
        markHomePositionUnknown(pending, "操作超过服务端截止时间，结果未知，可重试");
        return;
    }
    try {
        const response = await getHomePositionOperationWithDeadline(channelId, pending.operationId);
        if (!isCurrentHomePositionContext(channelId, token, generation)) return;
        if (response.code !== 0 || !response.data) throw new Error(response.message || "查询操作状态失败");
        const operation = response.data;
        if (operation.operationId !== pending.operationId) {
            markHomePositionUnknown(pending, "服务端返回了不匹配的 operation ID");
            return;
        }
        homeOperationId.value = operation.operationId;
        homeError.value = "";
        if (operation.status === "queued" || operation.status === "sent") {
            homePending.value = { ...pending, deadlineAt: operation.deadlineAt };
            prepareHomePositionOperation(operation.operationId);
            if (!updateHomePositionDeadline(operation.deadlineAt)) {
                void recheckHomePositionDeadline(channelId, token, generation);
                return;
            }
            scheduleHomePositionPoll(channelId, token, generation);
            return;
        }
        if (operation.status === "accepted") {
            await refreshHomePositionAfterAccepted(channelId, token, generation);
            return;
        }
        if (operation.status === "unknown") {
            markHomePositionUnknown(pending, operation.errorCode || operation.errorMessage || "操作结果未知，可重试");
            return;
        }
        finishHomePositionFailure(pending, operation);
    } catch (error: any) {
        if (!isCurrentHomePositionContext(channelId, token, generation)) return;
        const latest = homePending.value;
        if (!latest) return;
        if (homePositionInsuranceDeadlineMs === null) {
            void recheckHomePositionDeadline(channelId, token, generation);
            return;
        }
        if (Date.now() >= homePositionInsuranceDeadlineMs) {
            markHomePositionUnknown(latest, "网络持续失败至服务端截止时间，结果未知，可重试");
            return;
        }
        homeError.value = error?.message || "操作状态暂时不可用，正在重试";
        scheduleHomePositionPoll(channelId, token, generation);
    }
}

const wiperPending = ref<"on" | "off" | null>(null);
let wiperRequestToken = 0;
const imageParams = ref({ brightness: 128, contrast: 128, saturation: 128, hue: 128 });

const deviceRecording = ref(false);
const guardArmed = ref(false);
const advancedPending = ref(new Set<string>());
const advancedOperationStatus = ref<Record<string, string>>({});
const dragZoomMode = ref(false);
const dragZoomStart = ref<{ x: number; y: number } | null>(null);
const dragZoomCurrent = ref<{ x: number; y: number } | null>(null);
const dragZoomBoxStyle = computed(() => {
    if (!dragZoomStart.value || !dragZoomCurrent.value) return {};
    const left = Math.min(dragZoomStart.value.x, dragZoomCurrent.value.x);
    const top = Math.min(dragZoomStart.value.y, dragZoomCurrent.value.y);
    const width = Math.abs(dragZoomCurrent.value.x - dragZoomStart.value.x);
    const height = Math.abs(dragZoomCurrent.value.y - dragZoomStart.value.y);
    return { left: `${left * 100}%`, top: `${top * 100}%`, width: `${width * 100}%`, height: `${height * 100}%` };
});

/* ────────────────────────── 流信息 ────────────────────────── */

const streamInfo = ref({
    streamId: "", ssrc: "", nodeId: 0, nodeName: "—", nodeHost: "—", videoCodec: "—", audioCodec: "—",
    audioSampleRate: 0, resolution: "—", videoFps: 0, urls: {},
});

/* ────────────────────────── 流概况 + 视频探针 ────────────────────────── */

type MonitorState = "idle" | "fresh" | "stale" | "offline";
const liveMetrics = ref<{ bitrate: number; videoLoss: number | null; audioLoss: number | null }>({ bitrate: 0, videoLoss: null, audioLoss: null });
const readerCount = ref(0);
const monitorSnapshot = ref<StreamMonitorSnapshot | null>(null);
const monitorState = ref<MonitorState>("idle");
const monitorVideoTrack = computed(() => monitorSnapshot.value?.tracks.find((track) => track.kind === "video"));
const monitorAudioTrack = computed(() => monitorSnapshot.value?.tracks.find((track) => track.kind === "audio"));
const totalReaderCount = computed(() => monitorSnapshot.value?.network.totalReaderCount ?? 0);

function formatBytes(value: number | undefined, suffix = "") {
    if (value == null || value < 0) return "—";
    if (value < 1024) return `${value} B${suffix}`;
    if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB${suffix}`;
    return `${(value / (1024 * 1024)).toFixed(2)} MB${suffix}`;
}

const monitorBytesSpeedText = computed(() => formatBytes(monitorSnapshot.value?.network.bytesSpeed, "/s"));
const monitorTotalBytesText = computed(() => formatBytes(monitorSnapshot.value?.network.totalBytes));
const recordingText = computed(() => {
    const recording = monitorSnapshot.value?.recording;
    if (!recording) return "—";
    const formats = [recording.mp4 ? "MP4" : "", recording.hls ? "HLS" : ""].filter(Boolean);
    return formats.length ? formats.join(" / ") : "未录制";
});

type ProbeState = "idle" | "sampling" | "complete";
const probeState = ref<ProbeState>("idle");
const probeRemainingMs = ref(3000);
const probeFinishedAt = ref("");
const probeSnapshot = ref<ProbeSnapshot | null>(null);
let probeCountdownTimer: number | null = null;
let probeFinishTimer: number | null = null;
let probeToken = 0;

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
const probeResult = computed(() => probeSnapshot.value);
const probeTimeline = computed(() => (probeResult.value?.timeline || []).map((frame) => ({
    type: frame.trackType,
    keyFrame: frame.keyFrame,
    height: frame.trackType === "video" ? 62 : 30,
})));

function clearProbeTimers() {
    if (probeCountdownTimer) window.clearInterval(probeCountdownTimer);
    if (probeFinishTimer) window.clearTimeout(probeFinishTimer);
    probeCountdownTimer = null;
    probeFinishTimer = null;
}

async function startProbe() {
    if (phase.value !== "playing" || probeState.value === "sampling" || !playResult.value?.streamId) return;
    const streamId = playResult.value.streamId;
    const session = sessionToken;
    const token = ++probeToken;
    clearProbeTimers();
    probeState.value = "sampling";
    probeSnapshot.value = null;
    probeRemainingMs.value = 3000;
    probeCountdownTimer = window.setInterval(() => {
        probeRemainingMs.value = Math.max(0, probeRemainingMs.value - 100);
    }, 100);
    try {
        const response = await runStreamProbe(streamId);
        if (token !== probeToken || session !== sessionToken || playResult.value?.streamId !== streamId) return;
        if (response.code !== 0 || !response.data) throw new Error(response.message || "视频探针执行失败");
        probeSnapshot.value = response.data;
        probeState.value = "complete";
        probeFinishedAt.value = new Date(response.data.completedAt).toLocaleTimeString("zh-CN", { hour12: false });
    } catch (error: any) {
        if (token !== probeToken) return;
        probeState.value = "idle";
        Message.error(error?.message || "视频探针执行失败");
    } finally {
        if (token !== probeToken) return;
        clearProbeTimers();
        probeRemainingMs.value = 0;
    }
}

/* ────────────────────────── 通用工具 ────────────────────────── */

function formatDuration(ms: number) {
    const s = Math.floor(ms / 1000);
    return `${String(Math.floor(s / 60)).padStart(2, "0")}:${String(s % 60).padStart(2, "0")}`;
}

function formatLoss(value: number | null) {
    if (value == null || value < 0) return "—";
    return `${(value * 100).toFixed(value > 0 && value < 0.01 ? 2 : 1)}%`;
}

function beginTimer() {
    if (timer) return;
    timer = window.setInterval(() => { now.value = Date.now(); }, 1000);
}

function clearTimer() {
    if (timer) window.clearInterval(timer);
    timer = null;
    liveMetrics.value = { bitrate: 0, videoLoss: null, audioLoss: null };
}

function applyMonitor(snapshot: StreamMonitorSnapshot) {
    monitorSnapshot.value = snapshot;
    liveMetrics.value = {
        bitrate: Math.round(snapshot.quality.bitrateKbps * 10) / 10,
        videoLoss: snapshot.tracks.find((track) => track.kind === "video")?.loss ?? null,
        audioLoss: snapshot.tracks.find((track) => track.kind === "audio")?.loss ?? null,
    };
    monitorState.value = snapshot.status === "online" ? "fresh" : "offline";
    monitorAlive.value = { seconds: snapshot.network.aliveSecond, receivedAt: Date.now() };
    readerCount.value = snapshot.network.readerCount;
    const video = snapshot.tracks.find((track) => track.kind === "video");
    const audio = snapshot.tracks.find((track) => track.kind === "audio");
    streamInfo.value = {
        ...streamInfo.value,
        nodeId: snapshot.node.id,
        nodeName: snapshot.node.name || snapshot.node.host,
        nodeHost: snapshot.node.host,
        videoCodec: video?.codec || "—",
        audioCodec: audio?.codec || "—",
        audioSampleRate: audio?.sampleRate || 0,
        resolution: video?.width && video?.height ? `${video.width}×${video.height}` : "—",
        videoFps: video?.fps || 0,
    };
}

async function refreshMonitor() {
    const streamId = playResult.value?.streamId;
    const token = sessionToken;
    if (!streamId || phase.value !== "playing") return;
    try {
        const response = await getStreamMonitor(streamId);
        if (token !== sessionToken || playResult.value?.streamId !== streamId) return;
        if (response.code === 0 && response.data) applyMonitor(response.data);
    } catch (error: any) {
        if (token !== sessionToken || playResult.value?.streamId !== streamId) return;
        const status = error?.response?.status;
        const message = String(error?.response?.data?.message || error?.message || "");
        if (status === 404 || message.includes("离线")) {
            monitorState.value = "offline";
            monitorSnapshot.value = null;
            monitorAlive.value = null;
            liveMetrics.value = { bitrate: 0, videoLoss: null, audioLoss: null };
            readerCount.value = 0;
        } else {
            monitorState.value = monitorSnapshot.value ? "stale" : "idle";
        }
    }
}

function beginMonitor() {
    if (monitorTimer) return;
    void refreshMonitor();
    monitorTimer = window.setInterval(() => void refreshMonitor(), 2000);
}

function clearMonitor() {
    if (monitorTimer) window.clearInterval(monitorTimer);
    monitorTimer = null;
    monitorSnapshot.value = null;
    monitorState.value = "idle";
    monitorAlive.value = null;
}

/* ────────────────────────── 真实点播会话 ────────────────────────── */

async function startSession() {
    const channel = props.channel;
    if (!channel || !props.visible) return;
    const previousStreamId = playResult.value?.streamId;
    const token = ++sessionToken;
    resetSessionState();
    await stopTalk();
    if (previousStreamId) await stopPlay(previousStreamId).catch(() => undefined);
    if (token !== sessionToken || !props.visible || props.channel?.id !== channel.id) return;
    phase.value = "requesting";
    errorMessage.value = "";
    startedAt.value = Date.now();
    now.value = Date.now();
    beginTimer();
    try {
        const response = await startPlay(channel.deviceId, channel.channelId);
        if (token !== sessionToken || !props.visible) {
            if (token > localCleanupThroughToken && response.data?.streamId) {
                await stopPlay(response.data.streamId).catch(() => undefined);
            }
            return;
        }
        if (response.code !== 0 || !response.data) throw new Error(response.message || "点播失败");
        playResult.value = response.data;
        streamInfo.value.streamId = response.data.streamId;
        streamInfo.value.ssrc = response.data.ssrc;
        streamInfo.value.urls = response.data.urls || {};
        protocol.value = protocolUrls.value["ws-flv"] ? "ws-flv" : protocolUrls.value["http-flv"] ? "http-flv" : "hls";
        phase.value = "playing";
        beginMonitor();
        void loadPanelData();
    } catch (error: any) {
        if (token !== sessionToken) return;
        phase.value = "error";
        errorMessage.value = error?.message || "点播失败,请检查设备在线状态";
        clearMonitor();
        clearTimer();
    }
}

function resetSessionState() {
    clearMonitor();
    clearTimer();
    playResult.value = null;
    phase.value = "idle";
    startedAt.value = null;
    errorMessage.value = "";
    assetManagerVisible.value = false;
    assetSearch.value = "";
    presetDraft.value = null;
    savePresetDialogVisible.value = false;
    activePresetId.value = null;
    saveCruiseDialogVisible.value = false;
    cruiseDraft.value = null;
    cruiseDraftTouched.value = false;
    cruiseDraftSubmitError.value = "";
    activeCruiseId.value = null;
    cruiseState.value = "stopped";
    cruiseFreshness.value = "unknown";
    cruiseRefreshPending.value = false;
    cruiseLoadError.value = "";
    cruiseRefreshError.value = "";
    cruiseMoreVisible.value = false;
    clearCruiseReconcilePolling();
    probeToken++;
    clearProbeTimers();
    probeState.value = "idle";
    probeRemainingMs.value = 3000;
    probeSnapshot.value = null;
    liveMetrics.value = { bitrate: 0, videoLoss: null, audioLoss: null };
    readerCount.value = 0;
    capabilities.value = null;
    presets.value = [];
    cruiseTracks.value = [];
    resetHomePositionState();
    wiperRequestToken++;
    wiperPending.value = null;
    deviceRecording.value = false;
    guardArmed.value = false;
    advancedPending.value = new Set();
    advancedOperationStatus.value = {};
    dragZoomMode.value = false;
    dragZoomStart.value = null;
    dragZoomCurrent.value = null;
}

function cleanupSessionLocally() {
    localCleanupThroughToken = Math.max(localCleanupThroughToken, sessionToken);
    sessionToken++;
    resetSessionState();
}

function reconnect() {
    void startSession();
}

function handleClose() {
    cleanupTalkLocally();
    cleanupSessionLocally();
    emit("update:visible", false);
}

function handlePlayerError(message: string) {
    if (phase.value !== "playing" && phase.value !== "paused") return;
    phase.value = "error";
    errorMessage.value = message || "播放器拉流失败";
    clearMonitor();
    clearTimer();
}

async function loadPanelData() {
    const channel = props.channel;
    if (!channel) return;
    const token = sessionToken;
    try {
        const response = await getControlCapabilities(channel.id);
        if (token === sessionToken && props.channel?.id === channel.id && response.code === 0 && response.data) {
            capabilities.value = response.data;
            if (!broadcastAvailable.value && talkAvailable.value) talkMode.value = "talk";
            else if (broadcastAvailable.value) talkMode.value = "broadcast";
        }
    } catch {
        if (token === sessionToken) capabilities.value = null;
    }
    await Promise.all([loadPresets(channel.id, token), loadCruises(channel.id, token), loadHomePosition(channel.id, token)]);
}

async function loadPresets(channelId = props.channel?.id, token = sessionToken) {
    if (!channelId) return;
    try {
        const response = await listPtzPresets(channelId);
        if (token === sessionToken && props.channel?.id === channelId && response.code === 0 && response.data) {
            presets.value = response.data.list.map((item) => ({
                id: Number(item.presetId ?? item.id), name: String(item.name || `预置位 ${item.presetId ?? item.id}`), setAt: item.updatedAt ? String(item.updatedAt) : undefined,
            })).filter((item) => item.id > 0);
        }
    } catch {
        if (token === sessionToken) presets.value = [];
    }
}

async function loadCruises(channelId = props.channel?.id, token = sessionToken, refresh = true) {
    if (!channelId || token !== sessionToken || props.channel?.id !== channelId) return;
    cruiseRefreshPending.value = false;
    cruiseLoadError.value = "";
    cruiseRefreshError.value = "";
    try {
        const response = await listCruiseTracks(channelId, refresh);
        if (token !== sessionToken || props.channel?.id !== channelId) return;
        if (response.code !== 0 || !response.data) {
            cruiseLoadError.value = response.message || "加载巡航轨迹失败,请重试";
            return;
        }
        if (!Array.isArray(response.data.list)) {
            cruiseLoadError.value = "巡航轨迹数据格式错误,请重试";
            return;
        }
        cruiseFreshness.value = response.data.freshness || "unknown";
        cruiseRefreshPending.value = Boolean(response.data.refreshOperationId);
        cruiseRefreshError.value = response.data.refreshError || "";
        cruiseTracks.value = response.data.list.map((item) => {
            const detail = parseCruiseDetail(item.detail);
            // 待对账标记必须同时仍是未知/禁用状态;标准列表确认存在后 enabled=true,
            // 即使详情 JSON 保留旧 source 也不能把设备已确认的轨迹继续锁死。
            const pending = item.enabled !== true && (detail.source === "reconcile-pending" || detail.source === "optimistic-pending");
            return {
                id: Number(item.trackId ?? item.id),
                name: String(item.name || `巡航轨迹 ${item.trackId ?? item.id}`),
                enabled: item.enabled !== false && !pending,
                pending,
                points: detail.points,
                source: detail.source,
            };
        }).filter((item) => Number.isInteger(item.id) && item.id >= 0 && item.id <= 255);
        if (refresh && response.data.refreshOperationId) scheduleCruiseReconcilePolling(channelId, token);
    } catch {
        if (token === sessionToken && props.channel?.id === channelId) {
            cruiseLoadError.value = "加载巡航轨迹失败,请重试";
            cruiseRefreshPending.value = false;
            cruiseRefreshError.value = "";
        }
    }
}

async function loadHomePosition(channelId = props.channel?.id, token = sessionToken) {
    if (!channelId) return;
    const generation = homePositionGeneration;
    if (token === sessionToken && generation === homePositionGeneration && props.channel?.id === channelId) {
        homePhase.value = "loading";
        homeError.value = "";
    }
    try {
        const response = await getHomePosition(channelId);
        if (token !== sessionToken || generation !== homePositionGeneration || props.channel?.id !== channelId) return;
        if (response.code !== 0 || !response.data) throw new Error(response.message || "加载看守位失败");
        applyHomePositionResult(response.data);
        resumeHomePositionPending(channelId, token, generation);
    } catch (error: any) {
        if (token === sessionToken && generation === homePositionGeneration && props.channel?.id === channelId) {
            clearHomePositionPolling(false);
            homePending.value = null;
            homePhase.value = "error";
            homeFailureLabel.value = "加载失败";
            homeError.value = error?.message || "加载看守位失败";
        }
    }
}

/* ────────────────────────── 云台指令 ────────────────────────── */

const ptzActions: Record<string, string> = {
    "左上": "left_up", "上": "up", "右上": "right_up", "左": "left", "右": "right", "左下": "left_down", "下": "down", "右下": "right_down",
    "放大": "zoom_in", "缩小": "zoom_out", "远焦": "focus_far", "近焦": "focus_near", "光圈+": "iris_open", "光圈-": "iris_close", "停止": "stop",
};
let activePtzAction = "";

async function sendPtz(action: string) {
    if (!props.channel) return;
    if (action === "停止") activePtzAction = "";
    else activePtzAction = action;
    try {
        const response = await controlPtz(props.channel.id, { action: ptzActions[action] || action, speed: Math.max(1, Math.min(255, moveSpeed.value * 25)), idempotencyKey: `${props.channel.id}-${action}-${Date.now()}` });
        if (response.code !== 0) throw new Error(response.message || "云台指令失败");
    } catch (error: any) {
        Message.error(error?.message || `云台指令 ${action} 失败`);
    }
}

async function sendPrecise() {
    if (!props.channel) return;
    try {
        const response = await controlPtzPrecise(props.channel.id, { pan: precisePan.value, tilt: preciseTilt.value, zoom: preciseZoom.value, speed: moveSpeed.value * 25 });
        if (response.code !== 0) throw new Error(response.message || "精准定位失败");
        Message.success("精准定位请求已受理");
    } catch (error: any) {
        Message.error(error?.message || "精准定位失败");
    }
}

async function callPreset(id: number) {
    if (!props.channel) return;
    try {
        const response = await callPtzPreset(props.channel.id, id);
        if (response.code !== 0) throw new Error(response.message || "调用预置位失败");
        activePresetId.value = id;
    } catch (error: any) { Message.error(error?.message || "调用预置位失败"); }
}
function openSavePresetDialog() {
    if (!props.channel) return;
    const nextId = nextPresetId();
    presetDraft.value = { id: nextId, name: `预置位 ${nextId}`, submitting: false };
    presetNameTouched.value = false;
    savePresetDialogVisible.value = true;
}
function closeSavePresetDialog() {
    savePresetDialogVisible.value = false;
    presetDraft.value = null;
    presetNameTouched.value = false;
}
const presetNameError = computed(() => {
    if (!presetNameTouched.value || !presetDraft.value) return "";
    const name = presetDraft.value.name.trim();
    if (!name) return "请填写预置位名称";
    if (name.length > 16) return "名称最多 16 个字符";
    return "";
});
async function handleSavePresetBeforeOk(done: (closable?: boolean) => void) {
    if (!presetDraft.value || !props.channel) { done(false); return; }
    presetNameTouched.value = true;
    if (presetNameError.value) { done(false); return; }
    const name = presetDraft.value.name.trim() || `预置位 ${presetDraft.value.id}`;
    const presetId = presetDraft.value.id;
    presetDraft.value.submitting = true;
    try {
        const response = await createPtzPreset(props.channel.id, { presetId, name });
        if (response.code !== 0) throw new Error(response.message || "设置预置位失败");
        Message.success(`预置位 #${presetId} 请求已受理`);
        presetDraft.value = null;
        presetNameTouched.value = false;
        await loadPresets();
        done(true);
    } catch (error: any) {
        Message.error(error?.message || "设置预置位失败");
        if (presetDraft.value) presetDraft.value.submitting = false;
        done(false);
    }
}
async function deletePreset(id: number) {
    if (!props.channel) return;
    Modal.warning({ title: `确认删除预置位 #${id}?`, content: "删除命令会下发到设备。", okText: "确认删除", cancelText: "取消", onOk: async () => {
        try {
            const response = await deletePtzPreset(props.channel!.id, id);
            if (response.code !== 0) throw new Error(response.message || "删除预置位失败");
            if (activePresetId.value === id) activePresetId.value = null;
            await loadPresets();
        } catch (error: any) { Message.error(error?.message || "删除预置位失败"); }
    } });
}

async function executeCruiseToggle(id: number) {
    if (!props.channel) return;
    const isCurrent = activeCruiseId.value === id;
    const action = isCurrent && cruiseState.value === "start-sent" ? "stop" : "start";
    const unconfirmed = cruiseTracks.value.some((track) => track.id === id && track.pending);
    const channelId = props.channel.id;
    const token = sessionToken;
    try {
        const response = await controlPtzCruise(channelId, { action, trackId: id });
        if (token !== sessionToken || props.channel?.id !== channelId) return;
        if (response.code !== 0) throw new Error(response.message || "巡航指令失败");
        if (["rejected", "timeout", "cancelled"].includes(String(response.data?.status))) {
            throw new Error(response.message || `巡航${action === "stop" ? "停止" : "启动"}未被设备接受`);
        }
        if (action === "stop") {
            cruiseState.value = "stopped";
            activeCruiseId.value = null;
            Message.info(`巡航 #${id} 停止指令已发送`);
        } else {
            activeCruiseId.value = id;
            cruiseState.value = "start-sent";
            Message.info(unconfirmed
                ? `巡航 #${id} 试运行指令已发送,请观察设备是否开始巡航`
                : `巡航 #${id} 启动指令已发送,待设备确认`);
        }
    } catch (error: any) { Message.error(error?.message || "巡航指令失败"); }
}
function toggleCruise(id: number) {
    const isCurrent = activeCruiseId.value === id && cruiseState.value === "start-sent";
    const unconfirmed = cruiseTracks.value.some((track) => track.id === id && track.pending);
    if (!isCurrent && unconfirmed) {
        Modal.warning({
            title: "试运行未验证轨迹",
            content: `轨迹 #${id} 尚未收到设备确认。试运行可能调用设备中原有的同编号轨迹,请确认现场允许云台移动。`,
            hideCancel: false,
            okText: "继续试运行",
            cancelText: "取消",
            onOk: () => executeCruiseToggle(id),
        });
        return;
    }
    return executeCruiseToggle(id);
}
async function stopCruise() {
    const trackId = activeCruiseId.value;
    if (!props.channel || trackId === null) return;
    const channelId = props.channel.id;
    const token = sessionToken;
    try {
        const response = await controlPtzCruise(channelId, { action: "stop", trackId });
        if (token !== sessionToken || props.channel?.id !== channelId) return;
        if (response.code !== 0) throw new Error(response.message || "停止巡航失败");
        cruiseState.value = "stopped";
        activeCruiseId.value = null;
        Message.info(`巡航 #${trackId} 停止指令已发送`);
    } catch (error: any) { Message.error(error?.message || "停止巡航失败"); }
}
async function deleteCruise(id: number) {
    if (!props.channel) return;
    const channelId = props.channel.id;
    const token = sessionToken;
    Modal.warning({ title: "删除巡航轨迹", content: `确认删除巡航轨迹 #${id}?此操作会下发到设备,不可撤销。`, hideCancel: false, okText: "删除", cancelText: "取消", onOk: async () => {
        try {
            const response = await controlPtzCruise(channelId, { action: "delete", trackId: id });
            if (token !== sessionToken || props.channel?.id !== channelId) return;
            if (response.code !== 0) throw new Error(response.message || "删除巡航失败");
            if (activeCruiseId.value === id) { activeCruiseId.value = null; cruiseState.value = "stopped"; }
            Message.info(`巡航 #${id} 删除指令已发送,待设备对账`);
            await loadCruises(channelId, token, false);
        } catch (error: any) { Message.error(error?.message || "删除巡航失败"); }
    } });
}

// 抽屉打开入口暂时被 tile grid + 更多 popover 替代,函数保留供未来管理面板复用
function openAssetManager(tab: AssetManagerTab) {
    assetManagerTab.value = tab;
    assetSearch.value = "";
    assetManagerVisible.value = true;
}
void openAssetManager;

function switchAssetManagerTab(tab: AssetManagerTab) {
    assetManagerTab.value = tab;
    assetSearch.value = "";
}

function closeAssetManager() {
    assetManagerVisible.value = false;
    assetSearch.value = "";
}

async function sendWiperCommand(action: "on" | "off") {
    if (!props.channel || wiperPending.value !== null) return;
    const channelId = props.channel.id;
    const session = sessionToken;
    const requestToken = ++wiperRequestToken;
    const isCurrentRequest = () => requestToken === wiperRequestToken && session === sessionToken && props.channel?.id === channelId;
    wiperPending.value = action;
    try {
        const response = await controlPtzWiper(channelId, { action });
        if (!isCurrentRequest()) return;
        if (response.code !== 0) throw new Error(response.message || "雨刷控制指令失败");
        Message.success(`雨刷${action === "on" ? "开启" : "关闭"}指令已发送`);
    } catch (error: any) {
        if (!isCurrentRequest()) return;
        Message.error(error?.message || "雨刷控制指令失败");
    } finally {
        if (isCurrentRequest()) wiperPending.value = null;
    }
}

async function saveHomePosition() {
    if (!props.channel || !homeCanSubmit.value) return;
    const channelId = props.channel.id;
    const token = sessionToken;
    const generation = beginHomePositionOperation();
    const data: HomePositionPatch = homeDraft.value.enabled
        ? { enabled: true as const, resetTime: homeDraft.value.resetTime!, presetId: homeDraft.value.presetId! }
        : { enabled: false as const };
    const idempotencyKey = nextHomePositionIdempotencyKey("control", channelId);
    homePending.value = { kind: "control", operationId: null, deadlineAt: null };
    homeOperationId.value = null;
    homePhase.value = "pending";
    homeError.value = "";
    homeMismatch.value = "";
    try {
        const response = await updateHomePosition(channelId, data, idempotencyKey);
        if (!isCurrentHomePositionContext(channelId, token, generation)) return;
        if (response.code !== 0 || !response.data?.operationId) throw new Error(response.message || "保存看守位失败");
        const operationId = response.data.operationId;
        homePending.value = { kind: "control", operationId, deadlineAt: null };
        homeOperationId.value = operationId;
        prepareHomePositionOperation(operationId);
        if (response.data.status === "accepted") {
            await refreshHomePositionAfterAccepted(channelId, token, generation);
        } else if (response.data.status === "unknown") {
            markHomePositionUnknown(homePending.value, "操作结果未知，可重试");
        } else if (["rejected", "timeout", "cancelled"].includes(response.data.status)) {
            finishHomePositionFailure(homePending.value, {
                operationId,
                status: response.data.status,
                errorCode: null,
                errorMessage: response.message || null,
                completedAt: null,
                deadlineAt: null,
            });
        } else {
            homePositionInsuranceDeadlineMs = Date.now()
                + HOME_POSITION_INITIAL_QUEUE_MS
                + HOME_POSITION_DEADLINE_GRACE_MS;
            Message.info("看守位请求已受理，等待设备确认");
            scheduleHomePositionPoll(channelId, token, generation);
        }
    } catch (error: any) {
        if (!isCurrentHomePositionContext(channelId, token, generation)) return;
        clearHomePositionPolling(false);
        homePending.value = null;
        homePhase.value = "error";
        homeFailureLabel.value = "操作失败";
        homeError.value = error?.message || "保存看守位失败";
        restoreHomeDraftFromConfirmed();
        Message.error(homeError.value);
    }
}

async function refreshHomePosition() {
    if (!props.channel || !homeCanRefresh.value) return;
    const channelId = props.channel.id;
    const token = sessionToken;
    const generation = beginHomePositionOperation();
    const idempotencyKey = nextHomePositionIdempotencyKey("refresh", channelId);
    homePending.value = { kind: "refresh", operationId: null, deadlineAt: null };
    homeOperationId.value = null;
    homePhase.value = "pending";
    homeError.value = "";
    homeMismatch.value = "";
    try {
        const response = await getHomePosition(channelId, true, idempotencyKey);
        if (!isCurrentHomePositionContext(channelId, token, generation)) return;
        if (response.code !== 0 || !response.data) throw new Error(response.message || "刷新看守位失败");
        applyHomePositionResult(response.data);
        resumeHomePositionPending(channelId, token, generation);
    } catch (error: any) {
        if (!isCurrentHomePositionContext(channelId, token, generation)) return;
        clearHomePositionPolling(false);
        homePending.value = null;
        homeFailureLabel.value = "操作失败";
        homeError.value = error?.message || "刷新看守位失败";
        homeFreshness.value = homeConfirmed.value ? "stale" : "unknown";
        homePhase.value = homeConfirmed.value
            ? (homeConfirmed.value.enabled ? "enabled" : "disabled")
            : "error";
        Message.error(homeError.value);
    }
}

async function readPreciseStatus() {
    if (!props.channel) return;
    try {
        const response = await getPtzPreciseStatus(props.channel.id, true);
        if (response.code !== 0) throw new Error(response.message || "读取当前位置失败");
        const state = response.data?.state;
        if (!state) { Message.info("已下发查询，设备状态尚未回传"); return; }
        if (state.pan != null) precisePan.value = Number(state.pan);
        if (state.tilt != null) preciseTilt.value = Number(state.tilt);
        if (state.zoom != null) preciseZoom.value = Number(state.zoom);
    } catch (error: any) { Message.error(error?.message || "读取当前位置失败"); }
}

function setAdvancedPending(action: string, pending: boolean) {
    const next = new Set(advancedPending.value);
    if (pending) next.add(action);
    else next.delete(action);
    advancedPending.value = next;
}

function pointInDragLayer(event: PointerEvent) {
    const layer = event.currentTarget as HTMLElement;
    const rect = layer.getBoundingClientRect();
    return {
        x: Math.max(0, Math.min(1, (event.clientX - rect.left) / Math.max(rect.width, 1))),
        y: Math.max(0, Math.min(1, (event.clientY - rect.top) / Math.max(rect.height, 1))),
    };
}

function toggleDragZoomMode() {
    dragZoomMode.value = !dragZoomMode.value;
    dragZoomStart.value = null;
    dragZoomCurrent.value = null;
}

function beginDragZoom(event: PointerEvent) {
    if (!dragZoomMode.value || event.button !== 0) return;
    const layer = event.currentTarget as HTMLElement;
    const point = pointInDragLayer(event);
    dragZoomStart.value = point;
    dragZoomCurrent.value = point;
    layer.setPointerCapture?.(event.pointerId);
}

function updateDragZoom(event: PointerEvent) {
    if (!dragZoomStart.value) return;
    dragZoomCurrent.value = pointInDragLayer(event);
}

async function finishDragZoom(event: PointerEvent) {
    if (!dragZoomStart.value) return;
    updateDragZoom(event);
    const start = dragZoomStart.value;
    const current = dragZoomCurrent.value || start;
    dragZoomStart.value = null;
    dragZoomCurrent.value = null;
    const left = Math.min(start.x, current.x);
    const top = Math.min(start.y, current.y);
    const width = Math.abs(current.x - start.x);
    const height = Math.abs(current.y - start.y);
    if (width < 0.02 || height < 0.02) {
        Message.warning("拖框范围太小,请重新选择");
        return;
    }
    const video = monitorSnapshot.value?.tracks.find((track) => track.kind === "video");
    const length = video?.width || 1000;
    const windowWidth = video?.height || 1000;
    await runAdvancedAction("drag_zoom_in", {
        length,
        width: windowWidth,
        midPointX: Math.round((left + width / 2) * length),
        midPointY: Math.round((top + height / 2) * windowWidth),
        lengthX: Math.max(1, Math.round(width * length)),
        lengthY: Math.max(1, Math.round(height * windowWidth)),
    });
    dragZoomMode.value = false;
}

async function runAdvancedAction(action: string, region?: Record<string, number>) {
    if (!props.channel) return;
    if (advancedPending.value.has(action)) return;
    const execute = async () => {
        setAdvancedPending(action, true);
        try {
            const response = await controlDevice(props.channel!.id, {
                action,
                confirmed: action === "teleboot",
                idempotencyKey: `${props.channel!.id}-${action}-${Date.now()}`,
                ...(action === "drag_zoom_in" && region ? { region } : {}),
            });
            if (response.code !== 0) throw new Error(response.message || "设备控制失败");
            if (action === "record_start") deviceRecording.value = true;
            if (action === "record_stop") deviceRecording.value = false;
            if (action === "guard_set") guardArmed.value = true;
            if (action === "guard_reset") guardArmed.value = false;
            advancedOperationStatus.value = { ...advancedOperationStatus.value, [action]: response.data?.operationId ? `已受理 · ${response.data.operationId}` : "已受理" };
            Message.success(`请求已受理${response.data?.operationId ? ` · ${response.data.operationId}` : ""}`);
        } catch (error: any) { Message.error(error?.message || "设备控制失败"); }
        finally { setAdvancedPending(action, false); }
    };
    if (action === "teleboot") {
        Modal.warning({ title: "确认远程重启设备?", content: "设备会短暂离线，正在观看的画面将中断。", okText: "确认重启", cancelText: "取消", onOk: execute });
        return;
    }
    await execute();
}

/* ────────────────────────── 语音对讲 ────────────────────────── */

type TalkState = "idle" | "connecting" | "talking";
const talkState = ref<TalkState>("idle");
const talkMode = ref<"broadcast" | "talk">("broadcast");
const talkSession = ref<TalkCreateResult | null>(null);
let talkConnection: RTCPeerConnection | null = null;
let talkStream: MediaStream | null = null;
let talkPollTimer: number | null = null;
let talkToken = 0;
// 关闭弹窗只销毁本地对讲资源,阻止迟到的创建响应补发 deleteTalkSession。
let localTalkCleanupThroughToken = 0;

function clearTalkPoll() {
    if (talkPollTimer) window.clearInterval(talkPollTimer);
    talkPollTimer = null;
}

async function waitTalkActive(channelId: number, sessionId: string, token: number) {
    for (let attempt = 0; attempt < 17; attempt++) {
        if (token !== talkToken) return false;
        const response = await getTalkSession(channelId, sessionId);
        if (token !== talkToken) return false;
        const state = response.data?.state;
        if (state === "active") return true;
        if (["failed", "expired", "ended"].includes(String(state))) throw new Error(response.data?.error || `对讲会话已${state}`);
        await new Promise((resolve) => window.setTimeout(resolve, 300));
    }
    throw new Error("等待设备对讲信令超时");
}

function beginTalkPoll(channelId: number, sessionId: string) {
    clearTalkPoll();
    talkPollTimer = window.setInterval(async () => {
        try {
            const response = await getTalkSession(channelId, sessionId);
            if (["failed", "expired", "ended"].includes(String(response.data?.state))) await stopTalk();
        } catch {
            // 短暂轮询失败不打断正在说话，租约超时由后端最终收敛。
        }
    }, 10000);
}

async function startTalk() {
    if (talkState.value !== "idle") return;
    if (!isAudioCapable.value) {
        Message.warning("设备离线或不支持语音对讲");
        return;
    }
    if (!props.channel) return;
    const token = ++talkToken;
    talkState.value = "connecting";
    try {
        const response = await createTalkSession(props.channel.id, talkMode.value);
        if (response.code !== 0 || !response.data) throw new Error(response.message || "对讲会话创建失败");
        if (token !== talkToken) {
            if (token > localTalkCleanupThroughToken) {
                await deleteTalkSession(props.channel.id, response.data.sessionId).catch(() => undefined);
            }
            return;
        }
        talkSession.value = response.data;
        talkStream = await navigator.mediaDevices.getUserMedia({ audio: { channelCount: 1 } });
        if (token !== talkToken) {
            talkStream.getTracks().forEach((track) => track.stop());
            talkStream = null;
            return;
        }
        talkConnection = new RTCPeerConnection();
        talkStream.getTracks().forEach((track) => talkConnection!.addTrack(track, talkStream!));
        const offer = await talkConnection.createOffer();
        await talkConnection.setLocalDescription(offer);
        const answer = await fetch(response.data.publishUrl, { method: "POST", headers: { "Content-Type": "application/sdp" }, body: offer.sdp || "" });
        if (!answer.ok) throw new Error(`WHIP 发布失败(${answer.status})`);
        const answerSdp = await answer.text();
        await talkConnection.setRemoteDescription({ type: "answer", sdp: answerSdp });
        if (!await waitTalkActive(props.channel.id, response.data.sessionId, token)) return;
        talkState.value = "talking";
        beginTalkPoll(props.channel.id, response.data.sessionId);
    } catch (error: any) {
        await stopTalk();
        Message.error(error?.message || "对讲启动失败");
    }
}

function cleanupTalkLocally(suppressBackendCleanup = true) {
    if (suppressBackendCleanup) {
        localTalkCleanupThroughToken = Math.max(localTalkCleanupThroughToken, talkToken);
    }
    talkToken++;
    clearTalkPoll();
    if (talkStream) talkStream.getTracks().forEach((track) => track.stop());
    talkStream = null;
    talkConnection?.close();
    talkConnection = null;
    talkSession.value = null;
    talkState.value = "idle";
}

async function stopTalk() {
    const session = talkSession.value;
    cleanupTalkLocally(false);
    if (props.channel && session?.sessionId) await deleteTalkSession(props.channel.id, session.sessionId).catch(() => undefined);
}

function switchProtocol(proto: StreamProtocol) {
    if (proto === protocol.value) return;
    if (!isBrowserPlayable(proto)) {
        Message.warning("该协议仅供外部客户端使用，浏览器内不可直接播放");
        return;
    }
    if (!protocolUrls.value[proto]) {
        Message.warning("当前节点未返回该协议地址");
        return;
    }
    protocol.value = proto;
}

function copyProtocolUrl(proto: StreamProtocol) {
    const url = protocolUrls.value[proto];
    if (!url) { Message.warning("当前协议地址不可用"); return; }
    navigator.clipboard?.writeText(url).then(() => {
        Message.success(`${proto.toUpperCase()} 地址已复制`);
    }).catch(() => {
        Message.error("复制失败");
    });
}

function releaseContinuousControls() {
    if (activePtzAction) void sendPtz("停止");
    void stopTalk();
}

function handleVisibilityChange() {
    if (document.hidden) releaseContinuousControls();
}


/* ────────────────────────── 生命周期 ────────────────────────── */

watch(
    [() => props.visible, () => props.channel?.id],
    ([visible]) => {
        if (visible && props.channel) void startSession();
        else { cleanupTalkLocally(); cleanupSessionLocally(); }
    },
    { immediate: true },
);

onMounted(() => {
    window.addEventListener("blur", releaseContinuousControls);
    document.addEventListener("visibilitychange", handleVisibilityChange);
});

onBeforeUnmount(() => {
    window.removeEventListener("blur", releaseContinuousControls);
    document.removeEventListener("visibilitychange", handleVisibilityChange);
    clearProbeTimers();
    clearTimer();
    clearMonitor();
    cleanupTalkLocally();
    cleanupSessionLocally();
});
</script>

<template>
    <a-modal
        :visible="visible"
        width="min(1280px, calc(100vw - 32px))"
        :footer="false"
        :mask-closable="false"
        unmount-on-close
        modal-class="uvp-system-dialog play-console-modal"
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
                    <em v-if="phase === 'playing'" class="session-elapsed mono">{{ elapsedText }}</em>
                </span>
            </div>
        </template>

        <div class="console-body">
            <!-- 主区(视频 + 控制条 + 会话链路) -->
            <section class="stage" :class="{ 'stage-wide': sideCollapsed }">
                <!-- 视频画面 -->
                <div class="video-frame">
                    <!-- 真实播放器；无地址时仍保留稳定的加载/空/错误态布局 -->
                    <div class="video-canvas">
                        <div
                            v-if="dragZoomMode"
                            class="drag-zoom-layer"
                            data-testid="drag-zoom-layer"
                            @pointerdown="beginDragZoom"
                            @pointermove="updateDragZoom"
                            @pointerup="finishDragZoom"
                            @pointercancel="finishDragZoom"
                        >
                            <span class="drag-zoom-box" :style="dragZoomBoxStyle"></span>
                            <span class="drag-zoom-hint">拖动选择 3D 定位区域</span>
                        </div>
                        <template v-if="phase === 'playing' || phase === 'paused'">
                            <PlayWindow :url="currentProtocolUrl" @error="handlePlayerError" />
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
                                :model-value="protocol"
                                :style="{ width: '160px' }"
                                size="small"
                                @change="switchProtocol"
                            >
                                <a-option
                                    v-for="opt in protocolOptions"
                                    :key="opt.value"
                                    :value="opt.value"
                                    :label="opt.label"
                                    :disabled="!protocolUrls[opt.value] || !isBrowserPlayable(opt.value)"
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
                                :disabled="!protocolUrls[opt.value]"
                                @click="switchProtocol(opt.value)"
                            >
                                {{ opt.label }}
                            </button>
                        </div>
                        <a-dropdown trigger="click" position="br">
                            <button class="copy-url" title="复制协议地址">
                                <Copy :size="13" />
                            </button>
                            <template #content>
                                <a-doption
                                    v-for="opt in protocolOptions"
                                    :key="opt.value"
                                    :disabled="!protocolUrls[opt.value]"
                                    @click="copyProtocolUrl(opt.value)"
                                >
                                    复制 {{ opt.label }} 地址
                                </a-doption>
                            </template>
                        </a-dropdown>
                    </div>

                </div>

                <!-- 双区联动详情:所有 Tab 共用下方详情区，保持结构与高度稳定 -->
                <div v-if="phase === 'playing'" class="stream-info-bar linked-info-bar">
                    <div v-show="activeTab === 'stream'" class="linked-detail" data-testid="linked-detail-stream">
                        <p class="linked-detail-hint">2 秒刷新 · 最近采集 {{ monitorCollectedAtText }}</p>
                        <div class="linked-stream-metrics">
                            <div class="stream-live-metric"><span>输出码率</span><strong>{{ liveMetrics.bitrate ? `${liveMetrics.bitrate} kbps` : "—" }}</strong><small>ZLM 到浏览器</small></div>
                            <div class="stream-live-metric"><span>视频接收丢包</span><strong :class="{ warn: (liveMetrics.videoLoss ?? 0) > 0.005, err: (liveMetrics.videoLoss ?? 0) > 0.02 }">{{ formatLoss(liveMetrics.videoLoss) }}</strong><small>设备到 ZLM · 局域网常为 0</small></div>
                            <div class="stream-live-metric"><span>音频接收丢包</span><strong :class="{ warn: (liveMetrics.audioLoss ?? 0) > 0.005, err: (liveMetrics.audioLoss ?? 0) > 0.02 }">{{ formatLoss(liveMetrics.audioLoss) }}</strong><small>设备到 ZLM · 局域网常为 0</small></div>
                            <div class="stream-live-metric"><span>当前观看</span><strong>{{ readerCount }}</strong><small>包含本会话 · 累计 {{ totalReaderCount }}</small></div>
                        </div>
                    </div>

                    <div v-show="activeTab === 'ptz'" class="linked-detail" data-testid="linked-detail-ptz">
                        <div class="linked-ptz-layout">
                            <section class="linked-section linked-card">
                                <header class="linked-card-hd">
                                    <span class="section-title"><Hash :size="13" />预置位<em v-if="presets.length" class="preset-count">{{ presets.length }}</em></span>
                                    <button
                                        class="preset-save-btn"
                                        data-testid="preset-save-btn"
                                        @click="openSavePresetDialog"
                                    >
                                        <Plus :size="12" /><span>添加</span>
                                    </button>
                                </header>
                                    <div v-if="presets.length === 0" class="preset-empty" data-testid="preset-empty">
                                        <Inbox :size="24" class="preset-empty-glyph" />
                                        <p class="preset-empty-line">暂无预置位</p>
                                    </div>
                                    <div v-else class="preset-grid">
                                        <div
                                            v-for="p in visiblePresets"
                                            :key="p.id"
                                            class="preset-tile"
                                            :class="{ active: activePresetId === p.id }"
                                        >
                                            <a-tooltip
                                                :content="`#${p.id} ${p.name}`"
                                                position="top"
                                                :mouse-enter-delay="RESOURCE_TOOLTIP_ENTER_DELAY_MS"
                                                :data-testid="`preset-tile-tooltip-${p.id}`"
                                            >
                                                <button
                                                    class="preset-tile-hit preset-item"
                                                    @click="callPreset(p.id)"
                                                >
                                                    <span class="preset-idx">#{{ p.id }}</span>
                                                    <span class="preset-name">{{ p.name }}</span>
                                                </button>
                                            </a-tooltip>
                                            <button
                                                class="preset-tile-del"
                                                :title="`删除 #${p.id}`"
                                                @click.stop="deletePreset(p.id)"
                                            >
                                                <X :size="11" />
                                            </button>
                                        </div>
                                        <a-popover
                                            v-if="hasMorePresets"
                                            v-model:popup-visible="presetMoreVisible"
                                            trigger="click"
                                            position="bottom"
                                            :content-style="{ padding: 0 }"
                                            class="preset-more-popover-trigger"
                                        >
                                            <button class="preset-tile-more" data-testid="preset-more-btn">
                                                <span>更多 · {{ presets.length }}</span>
                                                <ChevronDown :size="11" />
                                            </button>
                                            <template #content>
                                                <div class="preset-popover" data-testid="preset-popover">
                                                    <header class="preset-popover-hd">
                                                        <span><Hash :size="12" />全部预置位<em class="preset-count">{{ presets.length }}</em></span>
                                                    </header>
                                                    <div class="preset-popover-list">
                                                        <div
                                                            v-for="p in presets"
                                                            :key="p.id"
                                                            class="preset-popover-row"
                                                            :class="{ active: activePresetId === p.id }"
                                                            data-testid="preset-popover-row"
                                                        >
                                                            <span class="preset-popover-idx">#{{ p.id }}</span>
                                                            <a-tooltip
                                                                :content="`#${p.id} ${p.name}`"
                                                                position="top"
                                                                :mouse-enter-delay="RESOURCE_TOOLTIP_ENTER_DELAY_MS"
                                                                :data-testid="`preset-popover-tooltip-${p.id}`"
                                                            >
                                                                <span class="preset-popover-name">{{ p.name }}</span>
                                                            </a-tooltip>
                                                            <div class="preset-popover-actions">
                                                                <button
                                                                    class="preset-popover-call"
                                                                    title="调用此预置位"
                                                                    @click="callPreset(p.id)"
                                                                >
                                                                    <Navigation :size="11" />
                                                                </button>
                                                                <button
                                                                    class="preset-popover-del"
                                                                    title="删除此预置位"
                                                                    @click="deletePreset(p.id)"
                                                                >
                                                                    <Trash2 :size="11" />
                                                                </button>
                                                            </div>
                                                        </div>
                                                    </div>
                                                </div>
                                            </template>
                                        </a-popover>
                                    </div>
                            </section>

                            <section class="linked-section linked-card">
                                <header class="linked-card-hd">
                                    <span class="section-title">
                                        <Route :size="13" />巡航轨迹<em v-if="cruiseTracks.length" class="preset-count">{{ cruiseTracks.length }}</em>
                                        <small class="cruise-freshness" data-testid="cruise-freshness">{{ cruiseSyncLabel }}</small>
                                        <button
                                            v-if="activeCruiseId !== null && cruiseState !== 'stopped'"
                                            class="cruise-running-chip"
                                            data-testid="cruise-running-chip"
                                            :title="`巡航 #${activeCruiseId} 启动指令已发送,点击停止`"
                                            @click="stopCruise"
                                        >
                                            <span class="cruise-running-dot" :class="cruiseState" />
                                            启动已下发
                                            <Square :size="10" />
                                        </button>
                                    </span>
                                    <button
                                        class="preset-save-btn"
                                        data-testid="cruise-add-btn"
                                        :disabled="presets.length === 0"
                                        :title="presets.length === 0 ? '需要先添加预置位才能新建巡航轨迹' : '新建巡航轨迹(按顺序串联多个预置位)'"
                                        @click="openSaveCruiseDialog"
                                    >
                                        <Plus :size="12" /><span>添加</span>
                                    </button>
                                </header>
                                <div v-if="cruiseLoadError" class="preset-empty" data-testid="cruise-load-error">
                                    <AlertTriangle :size="24" class="preset-empty-glyph" />
                                    <p class="preset-empty-line">{{ cruiseLoadError }}</p>
                                </div>
                                <div v-else-if="cruiseTracks.length === 0" class="preset-empty" data-testid="cruise-empty">
                                    <Inbox :size="24" class="preset-empty-glyph" />
                                    <p class="preset-empty-line">暂无巡航轨迹</p>
                                </div>
                                <div v-else class="preset-grid">
                                    <a-tooltip
                                        v-for="c in visibleCruiseTracks"
                                        :key="c.id"
                                        :content="cruiseTileTitle(c)"
                                        position="top"
                                        :mouse-enter-delay="RESOURCE_TOOLTIP_ENTER_DELAY_MS"
                                        :data-testid="`cruise-tile-tooltip-${c.id}`"
                                    >
                                        <div
                                            class="preset-tile cruise-tile"
                                            :class="{
                                                active: activeCruiseId === c.id && cruiseState !== 'stopped',
                                                disabled: !c.enabled && !c.pending,
                                                pending: c.pending,
                                            }"
                                            :data-testid="`cruise-tile-${c.id}`"
                                        >
                                            <button
                                                class="preset-tile-hit cruise-item"
                                                :disabled="!c.enabled && !c.pending"
                                                @click="toggleCruise(c.id)"
                                            >
                                                <Square v-if="cruiseTileState(c) === 'stop'" :size="10" class="cruise-tile-icon" />
                                                <Play v-else :size="10" class="cruise-tile-icon" />
                                                <span class="preset-name">{{ c.name }}</span>
                                                <span v-if="c.pending" class="cruise-status-badge">未验证</span>
                                            </button>
                                            <button
                                                class="preset-tile-del"
                                                :title="`删除巡航 #${c.id}`"
                                                @click.stop="deleteCruise(c.id)"
                                            >
                                                <X :size="11" />
                                            </button>
                                        </div>
                                    </a-tooltip>
                                    <a-popover
                                        v-if="hasMoreCruises"
                                        v-model:popup-visible="cruiseMoreVisible"
                                        trigger="click"
                                        position="bottom"
                                        :content-style="{ padding: 0 }"
                                        class="preset-more-popover-trigger"
                                    >
                                        <button class="preset-tile-more" data-testid="cruise-more-btn">
                                            <span>更多 · {{ cruiseTracks.length }}</span>
                                            <ChevronDown :size="11" />
                                        </button>
                                        <template #content>
                                            <div class="preset-popover" data-testid="cruise-popover">
                                                <header class="preset-popover-hd">
                                                    <span><Route :size="12" />全部巡航轨迹<em class="preset-count">{{ cruiseTracks.length }}</em></span>
                                                </header>
                                                <div class="preset-popover-list">
                                                    <a-tooltip
                                                        v-for="c in cruiseTracks"
                                                        :key="c.id"
                                                        :content="cruiseTileTitle(c)"
                                                        position="top"
                                                        :mouse-enter-delay="RESOURCE_TOOLTIP_ENTER_DELAY_MS"
                                                        :data-testid="`cruise-popover-tooltip-${c.id}`"
                                                    >
                                                        <div
                                                            class="preset-popover-row"
                                                            :class="{ active: activeCruiseId === c.id && cruiseState !== 'stopped' }"
                                                            data-testid="cruise-popover-row"
                                                        >
                                                            <span class="preset-popover-idx">#{{ c.id }}</span>
                                                            <span class="preset-popover-name">{{ c.name }}</span>
                                                            <div class="preset-popover-actions">
                                                                <button
                                                                    class="preset-popover-call"
                                                                    :disabled="!c.enabled && !c.pending"
                                                                    @click="toggleCruise(c.id)"
                                                                >
                                                                    <Square v-if="cruiseTileState(c) === 'stop'" :size="11" />
                                                                    <Play v-else :size="11" />
                                                                </button>
                                                                <button
                                                                    class="preset-popover-del"
                                                                    title="删除此巡航轨迹"
                                                                    @click="deleteCruise(c.id)"
                                                                >
                                                                    <Trash2 :size="11" />
                                                                </button>
                                                            </div>
                                                        </div>
                                                    </a-tooltip>
                                                </div>
                                            </div>
                                        </template>
                                    </a-popover>
                                </div>
                            </section>

                            <section class="linked-section linked-card" data-testid="home-card">
                                <div class="section-hd first">
                                    <span class="section-title"><Home :size="13" />看守位<span class="tag-2022">2022</span></span>
                                    <label class="toggle">
                                        <input
                                            v-model="homeDraft.enabled"
                                            data-testid="home-toggle"
                                            type="checkbox"
                                            :disabled="props.channel?.status !== 1 || homeControlPending"
                                        />
                                        <span></span>
                                    </label>
                                </div>
                                <div
                                    class="home-config"
                                    data-testid="home-status"
                                    aria-live="polite"
                                    :aria-busy="homeIsBusy"
                                >
                                    <div class="home-status-line">
                                        <strong data-testid="home-phase">{{ homePhaseText }}</strong>
                                        <span data-testid="home-freshness">{{ homeFreshnessText }}</span>
                                    </div>
                                    <div class="home-confirmed">
                                        <strong data-testid="home-confirmed-status">{{ homeConfirmedStatusText }}</strong>
                                        <span data-testid="home-confirmed-values">{{ homeConfirmedValuesText }}</span>
                                        <span data-testid="home-verification">{{ homeVerificationText }}</span>
                                    </div>
                                    <p v-if="homeConfirmedOutsideEditableRange" class="home-warning" data-testid="home-range-warning">
                                        设备原值缺失或超出平台可编辑范围；关闭仍可下发，再次启用前请修改。
                                    </p>
                                    <div class="home-supports">
                                        <span data-testid="home-control-support">控制 {{ homeControlSupport.status }} · {{ homeControlSupport.reason }}</span>
                                        <span data-testid="home-query-support">查询 {{ homeQuerySupport.status }} · {{ homeQuerySupport.reason }}</span>
                                    </div>
                                    <p v-if="homeSupportHasRisk" class="home-warning" data-testid="home-support-risk">
                                        能力信息仅作风险提示，仍可尝试下发，以设备应答为准。
                                    </p>
                                    <p v-if="homeOperationId" class="home-operation" data-testid="home-operation-id">
                                        Operation {{ homeOperationId }}
                                    </p>
                                    <p v-if="homeMismatch" class="home-error" data-testid="home-mismatch">{{ homeMismatch }}</p>
                                    <p v-else-if="homeError" class="home-error" data-testid="home-error">{{ homeError }}</p>
                                    <div class="home-fields" :class="{ disabled: !homeDraft.enabled }">
                                        <div class="home-row">
                                            <span>回位预置位</span>
                                            <select
                                                v-model.number="homeDraft.presetId"
                                                data-testid="home-preset"
                                                :disabled="!homeDraft.enabled || props.channel?.status !== 1 || homeControlPending"
                                            >
                                                <option disabled :value="null">请选择预置位</option>
                                                <option :value="0">#0 · 标准预置位 0</option>
                                                <option v-for="p in presets" :key="p.id" :value="p.id">#{{ p.id }} · {{ p.name }}</option>
                                            </select>
                                        </div>
                                        <div class="home-row">
                                            <span>空闲触发</span>
                                            <input
                                                v-model.number="homeDraft.resetTime"
                                                data-testid="home-reset-time"
                                                type="number"
                                                min="10"
                                                max="3600"
                                                :disabled="!homeDraft.enabled || props.channel?.status !== 1 || homeControlPending"
                                            />
                                        </div>
                                    </div>
                                    <p v-if="homeDraft.enabled && !homePositionCanSave" class="home-error" data-testid="home-validation">
                                        启用需要预置位 #0..#255，等待时间为 10..3600 秒整数。
                                    </p>
                                    <div class="home-actions">
                                        <button class="btn-primary sm" data-testid="home-save" :disabled="!homeCanSubmit" @click="saveHomePosition">
                                            <ShieldCheck :size="12" />{{ homeDraft.enabled ? "保存看守位" : "关闭看守位" }}
                                        </button>
                                        <button class="btn-ghost sm" data-testid="home-refresh" :disabled="!homeCanRefresh" @click="refreshHomePosition">
                                            <RefreshCcw :size="12" />刷新设备状态
                                        </button>
                                    </div>
                                </div>
                            </section>

                            <section class="linked-section linked-card" data-testid="wiper-control">
                                <div class="section-hd first">
                                    <span class="section-title"><RefreshCcw :size="13" />雨刷控制</span>
                                    <span class="section-meta">国标辅助编号 1</span>
                                </div>
                                <div class="wiper-actions">
                                    <button class="btn-ghost sm" data-testid="wiper-on" :disabled="wiperPending !== null" @click="sendWiperCommand('on')">
                                        <Play :size="12" />开启
                                    </button>
                                    <button class="btn-ghost sm" data-testid="wiper-off" :disabled="wiperPending !== null" @click="sendWiperCommand('off')">
                                        <Square :size="12" />关闭
                                    </button>
                                </div>
                            </section>
                        </div>
                    </div>

                    <div v-show="activeTab === 'probe'" class="linked-detail" data-testid="linked-detail-probe">
                        <p class="linked-detail-hint">{{ probeState === "complete" ? `最近检测完成于 ${probeFinishedAt}` : "右侧启动检测后在此查看逐帧结果" }}</p>
                        <div class="linked-probe-layout">
                            <section class="linked-section">
                                <div class="section-hd first">
                                    <span class="section-title"><Video :size="13" />视频探针详情</span>
                                    <span class="section-meta">{{ probeResult ? `${[probeResult.video, probeResult.audio].filter(Boolean).length} 条轨道` : "待采样" }}</span>
                                </div>
                                <div class="probe-tracks linked-probe-tracks">
                                    <section class="probe-track video">
                                        <div class="probe-track-head"><span><Video :size="13" />视频轨</span><strong>{{ probeResult?.video?.codec || "—" }}</strong></div>
                                        <div class="probe-data-grid">
                                            <div><span>精确 FPS</span><strong>{{ probeResult?.video?.fps == null ? "—" : probeResult.video.fps.toFixed(1) }}</strong></div>
                                            <div><span>采样帧</span><strong>{{ probeResult?.video?.frameCount ?? "—" }}</strong></div>
                                            <div><span>关键帧</span><strong>{{ probeResult?.video?.keyFrameCount ?? "—" }}</strong></div>
                                            <div><span>GOP</span><strong>{{ probeResult?.video?.gop == null ? "—" : `${probeResult.video.gop.toFixed(1)} 帧` }}</strong></div>
                                        </div>
                                    </section>
                                    <section class="probe-track audio">
                                        <div class="probe-track-head"><span><Activity :size="13" />音频轨</span><strong>{{ probeResult?.audio?.codec || "—" }}</strong></div>
                                        <div class="probe-data-grid">
                                            <div><span>采样率</span><strong>{{ monitorSnapshot?.tracks.find(track => track.kind === 'audio')?.sampleRate ? `${monitorSnapshot.tracks.find(track => track.kind === 'audio')?.sampleRate} Hz` : "—" }}</strong></div>
                                            <div><span>采样帧</span><strong>{{ probeResult?.audio?.frameCount ?? "—" }}</strong></div>
                                            <div><span>帧间隔</span><strong>{{ probeResult?.audio?.averageIntervalMs == null ? "—" : `${probeResult.audio.averageIntervalMs.toFixed(1)} ms` }}</strong></div>
                                            <div><span>声道</span><strong>{{ monitorSnapshot?.tracks.find(track => track.kind === 'audio')?.channels || "—" }}</strong></div>
                                        </div>
                                    </section>
                                </div>
                            </section>

                            <section class="linked-section">
                                <div class="section-hd first">
                                    <span class="section-title"><Signal :size="13" />帧到达时间线</span>
                                    <span class="section-meta">{{ probeState === "complete" ? "最近 32 帧" : "无数据" }}</span>
                                </div>
                                <div class="frame-timeline linked-timeline" :class="{ muted: probeState !== 'complete' }">
                                    <div class="frame-bars">
                                        <span v-for="(frame, index) in probeTimeline" :key="index" :class="[frame.type, { keyframe: frame.keyFrame }]" :style="{ height: probeState === 'complete' ? `${frame.height}%` : '8%' }"></span>
                                    </div>
                                    <div class="frame-legend">
                                        <span><i class="key"></i>关键帧</span><span><i class="video"></i>视频帧</span><span><i class="audio"></i>音频帧</span>
                                    </div>
                                </div>
                            </section>
                        </div>
                    </div>

                    <div v-show="activeTab === 'advanced'" class="linked-detail" data-testid="linked-detail-advanced">
                        <p class="linked-detail-hint">本地占位 · ConfigDownload 接口待接入</p>
                        <div class="linked-image-layout">
                            <div class="image-adjust linked-image-adjust">
                                <label><span>亮度</span><input v-model.number="imageParams.brightness" type="range" min="0" max="255" /><em>{{ imageParams.brightness }}</em></label>
                                <label><span>对比度</span><input v-model.number="imageParams.contrast" type="range" min="0" max="255" /><em>{{ imageParams.contrast }}</em></label>
                                <label><span>饱和度</span><input v-model.number="imageParams.saturation" type="range" min="0" max="255" /><em>{{ imageParams.saturation }}</em></label>
                                <label><span>色度</span><input v-model.number="imageParams.hue" type="range" min="0" max="255" /><em>{{ imageParams.hue }}</em></label>
                            </div>
                            <button class="btn-primary sm linked-apply" disabled title="图像参数接口待接入">
                                <CheckCircle2 :size="12" />接口待接入
                            </button>
                        </div>
                    </div>
                </div>

            </section>
            <!-- 右侧功能栏 -->
            <aside v-if="!sideCollapsed" class="sidebar" :class="{ 'sidebar-stream': activeTab === 'stream' }">
                <!-- Tabs -->
                <div class="tabs">
                    <button
                        v-for="t in tabs"
                        :key="t.key"
                        class="tab"
                        :class="{ active: activeTab === t.key }"
                        :data-testid="`linked-tab-${t.key}`"
                        @click="activeTab = t.key"
                    >
                        <component :is="t.icon" :size="14" />
                        <span>{{ t.label }}</span>
                    </button>
                </div>

                <!-- Tab 面板容器 -->
                <div class="panels">
                    <!-- ═══════════ 流信息 ═══════════ -->
                    <div v-show="activeTab === 'stream'" class="panel stream-panel" data-testid="linked-side-stream">
                        <div class="stream-panel-header">
                            <span class="section-title"><Signal :size="13" />媒体参数</span>
                            <button class="stream-refresh" title="刷新流状态" :disabled="phase !== 'playing'" @click="refreshMonitor"><RefreshCcw :size="13" /></button>
                        </div>
                        <div class="stream-metrics-grid">
                            <div><span>媒体节点</span><strong>{{ streamInfo.nodeName }}</strong><small>{{ streamInfo.nodeHost }}</small></div>
                            <div><span>流 ID</span><strong class="mono">{{ streamInfo.streamId || "—" }}</strong><small>SSRC {{ streamInfo.ssrc || "—" }} · APP {{ playResult?.app || "—" }}</small></div>
                            <div><span>视频</span><strong>{{ streamInfo.videoCodec }}</strong><small>{{ streamInfo.resolution }} · {{ streamInfo.videoFps || "—" }} fps · {{ monitorVideoTrack?.frames ?? "—" }} 帧</small></div>
                            <div><span>音频</span><strong>{{ streamInfo.audioCodec }}</strong><small>{{ streamInfo.audioSampleRate ? `${streamInfo.audioSampleRate} Hz` : "—" }} · {{ monitorAudioTrack?.channels || "—" }} 声道 · {{ monitorAudioTrack?.frames ?? "—" }} 帧</small></div>
                            <div><span>数据速率</span><strong>{{ monitorBytesSpeedText }}</strong><small>累计 {{ monitorTotalBytesText }}</small></div>
                            <div><span>录制状态</span><strong>{{ recordingText }}</strong><small>播放协议 {{ protocol.toUpperCase() }}</small></div>
                        </div>
                    </div>

                    <!-- ═══════════ 云台控制 ═══════════ -->
                    <div v-show="activeTab === 'ptz'" class="panel" data-testid="linked-side-ptz">
                        <!-- 模式切换:速度控制 / 精准控制(2022) -->
                        <div class="mode-switch">
                            <button :class="{ active: ptzMode === 'speed' }" @click="ptzMode = 'speed'">
                                <Compass :size="13" />速度控制
                            </button>
                            <button :class="{ active: ptzMode === 'precise' }" @click="ptzMode = 'precise'">
                                <Crosshair :size="13" />精准定位<span class="tag-2022">2022</span>
                            </button>
                        </div>

                        <!-- 速度模式:方向盘 + 变倍 + 速度 -->
                        <div v-show="ptzMode === 'speed'" class="ptz-speed">
                            <div class="ptz-pad">
                                <button title="左上" @pointerdown.prevent="sendPtz('左上')" @pointerup.prevent="sendPtz('停止')" @pointerleave="sendPtz('停止')" @pointercancel="sendPtz('停止')"><ArrowUpLeft :size="17" /></button>
                                <button title="上" @pointerdown.prevent="sendPtz('上')" @pointerup.prevent="sendPtz('停止')" @pointerleave="sendPtz('停止')" @pointercancel="sendPtz('停止')"><ArrowUp :size="17" /></button>
                                <button title="右上" @pointerdown.prevent="sendPtz('右上')" @pointerup.prevent="sendPtz('停止')" @pointerleave="sendPtz('停止')" @pointercancel="sendPtz('停止')"><ArrowUpRight :size="17" /></button>
                                <button title="左" @pointerdown.prevent="sendPtz('左')" @pointerup.prevent="sendPtz('停止')" @pointerleave="sendPtz('停止')" @pointercancel="sendPtz('停止')"><ArrowLeft :size="17" /></button>
                                <button class="ptz-stop" title="停止" @click="sendPtz('停止')"><span></span></button>
                                <button title="右" @pointerdown.prevent="sendPtz('右')" @pointerup.prevent="sendPtz('停止')" @pointerleave="sendPtz('停止')" @pointercancel="sendPtz('停止')"><ArrowRight :size="17" /></button>
                                <button title="左下" @pointerdown.prevent="sendPtz('左下')" @pointerup.prevent="sendPtz('停止')" @pointerleave="sendPtz('停止')" @pointercancel="sendPtz('停止')"><ArrowDownLeft :size="17" /></button>
                                <button title="下" @pointerdown.prevent="sendPtz('下')" @pointerup.prevent="sendPtz('停止')" @pointerleave="sendPtz('停止')" @pointercancel="sendPtz('停止')"><ArrowDown :size="17" /></button>
                                <button title="右下" @pointerdown.prevent="sendPtz('右下')" @pointerup.prevent="sendPtz('停止')" @pointerleave="sendPtz('停止')" @pointercancel="sendPtz('停止')"><ArrowDownRight :size="17" /></button>
                            </div>

                            <div class="talk-mode-switch" aria-label="对讲模式">
                                <button :class="{ active: talkMode === 'broadcast' }" :disabled="talkState !== 'idle' || !broadcastAvailable" :title="capability('broadcast').reason" @click="talkMode = 'broadcast'">广播</button>
                                <button :class="{ active: talkMode === 'talk' }" :disabled="talkState !== 'idle' || !talkAvailable" :title="capability('talk').reason" @click="talkMode = 'talk'">Talk</button>
                            </div>
                            <button
                                class="talk-button"
                                data-testid="talk-button"
                                :class="{ active: talkState !== 'idle' }"
                                :disabled="!isAudioCapable"
                                :aria-pressed="talkState === 'talking'"
                                @pointerdown.prevent="startTalk"
                                @pointerup.prevent="stopTalk"
                                @pointerleave="stopTalk"
                                @pointercancel="stopTalk"
                            >
                                <Mic :size="14" />
                                <span>{{ talkState === "connecting" ? "正在建立对讲" : talkState === "talking" ? "对讲中 · 松开结束" : "按住对讲" }}</span>
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
                                        <button title="放大" @pointerdown.prevent="sendPtz('放大')" @pointerup.prevent="sendPtz('停止')" @pointerleave="sendPtz('停止')" @pointercancel="sendPtz('停止')"><ZoomIn :size="14" /></button>
                                        <button title="缩小" @pointerdown.prevent="sendPtz('缩小')" @pointerup.prevent="sendPtz('停止')" @pointerleave="sendPtz('停止')" @pointercancel="sendPtz('停止')"><ZoomOut :size="14" /></button>
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
                                <button class="btn-ghost sm" @click="readPreciseStatus">
                                    <Navigation :size="13" />读取当前位置
                                </button>
                            </div>
                        </div>

                    </div>
                    <!-- ═══════════ 视频探针 ═══════════ -->
                    <div v-show="activeTab === 'probe'" class="panel probe-panel" data-testid="linked-side-probe">
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
                                <strong>{{ probeResult ? (probeResult.summary.sampleDurationMs / 1000).toFixed(2) : "—" }}<em>s</em></strong>
                            </div>
                            <div>
                                <span>采集帧数</span>
                                <strong>{{ probeResult?.summary.frameCount ?? "—" }}<em>帧</em></strong>
                            </div>
                            <div>
                                <span>采样流量</span>
                                <strong>{{ probeResult ? Math.round(probeResult.summary.totalBytes / 1024) : "—" }}<em>KB</em></strong>
                            </div>
                        </div>

                        <div v-if="probeState === 'complete'" class="probe-verdict" :class="{ warning: probeResult?.health.status === 'warning', error: probeResult?.health.status === 'error' }">
                            <CheckCircle2 v-if="probeResult?.health.status === 'ok'" :size="15" />
                            <AlertTriangle v-else :size="15" />
                            <div>
                                <strong>{{ probeResult?.health.status === 'ok' ? '流健康，帧序与时间戳连续' : '检测发现需要关注的问题' }}</strong>
                                <span>完成于 {{ probeFinishedAt }} · {{ probeResult?.health.issues?.[0]?.message || '未发现异常帧间隔' }}</span>
                            </div>
                        </div>
                        <div v-else class="probe-verdict pending">
                            <Activity :size="15" />
                            <div>
                                <strong>{{ probeState === "sampling" ? "正在采集音视频帧" : "尚未执行深度检测" }}</strong>
                                <span>{{ probeState === "sampling" ? "结果将在采样结束后生成" : "当前仅展示检测项目" }}</span>
                            </div>
                        </div>

                        <section class="side-probe-health">
                            <div class="section-hd first">
                                <span class="section-title"><Gauge :size="13" />时间戳监控</span>
                                <span class="section-meta good">{{ probeResult ? (probeResult.health.status === 'ok' ? '平稳' : '需关注') : "待检测" }}</span>
                            </div>
                            <div class="probe-health-grid">
                                <div><span>视频 DTS 间隔</span><strong>{{ probeResult?.timestamps.videoDtsIntervalMeanMs == null ? "—" : `${probeResult.timestamps.videoDtsIntervalMeanMs.toFixed(1)} ms` }}</strong><em>均值</em></div>
                                <div><span>帧到达抖动</span><strong>{{ probeResult?.timestamps.arrivalJitterMs == null ? "—" : `${probeResult.timestamps.arrivalJitterMs.toFixed(1)} ms` }}</strong><em>标准差</em></div>
                                <div><span>PTS-DTS</span><strong>{{ probeResult?.timestamps.ptsDtsMaxMs == null ? "—" : `${probeResult.timestamps.ptsDtsMaxMs.toFixed(1)} ms` }}</strong><em>最大值</em></div>
                                <div><span>音视频交织</span><strong>{{ probeResult?.timestamps.avArrivalSkewMaxMs == null ? "—" : `${probeResult.timestamps.avArrivalSkewMaxMs.toFixed(1)} ms` }}</strong><em>最大偏差</em></div>
                            </div>
                        </section>

                    </div>
                    <!-- ═══════════ 高级 ═══════════ -->
                    <div v-show="activeTab === 'advanced'" class="panel" data-testid="linked-side-advanced">
                        <div class="section-hd first">
                            <span class="section-title"><Settings :size="13" />设备控制</span>
                            <span class="section-meta">GB28181 DeviceControl</span>
                        </div>
                        <div class="adv-actions">
                            <button class="adv-btn" @click="runAdvancedAction('iframe')">
                                <Video :size="14" />
                                <div><strong>强制关键帧</strong><small>IFrameCmd · 快速刷新画面</small></div>
                            </button>
                            <button class="adv-btn" data-testid="advanced-record" :disabled="advancedPending.has(deviceRecording ? 'record_stop' : 'record_start')" @click="runAdvancedAction(deviceRecording ? 'record_stop' : 'record_start')">
                                <Circle :size="14" />
                                <div><strong>{{ deviceRecording ? '停止设备录制' : '开始设备端录制' }}</strong><small>{{ (deviceRecording ? advancedOperationStatus.record_start : advancedOperationStatus.record_stop) || 'RecordCmd · SD 卡录制' }}</small></div>
                            </button>
                            <button class="adv-btn" data-testid="advanced-guard" :disabled="advancedPending.has(guardArmed ? 'guard_reset' : 'guard_set')" @click="runAdvancedAction(guardArmed ? 'guard_reset' : 'guard_set')">
                                <ShieldCheck :size="14" />
                                <div><strong>{{ guardArmed ? '撤防' : '布防' }}</strong><small>{{ (guardArmed ? advancedOperationStatus.guard_set : advancedOperationStatus.guard_reset) || 'GuardCmd · 触发告警' }}</small></div>
                            </button>
                            <button class="adv-btn" @click="runAdvancedAction('alarm_reset')">
                                <AlertTriangle :size="14" />
                                <div><strong>报警复位</strong><small>AlarmCmd · 清除告警</small></div>
                            </button>
                            <button class="adv-btn" @click="runAdvancedAction('teleboot')">
                                <RefreshCcw :size="14" />
                                <div><strong>远程重启</strong><small>TeleBootCmd · 重启设备</small></div>
                            </button>
                            <button class="adv-btn" data-testid="advanced-drag-zoom" @click="toggleDragZoomMode">
                                <Move3d :size="14" />
                                <div><strong>{{ dragZoomMode ? '取消 3D 定位' : '3D 定位' }}</strong><small>2022 · {{ dragZoomMode ? '在画面拖框后下发' : '拖框区域放大' }}</small></div>
                            </button>
                        </div>

                    </div>
                </div>
            </aside>

            <Transition name="asset-drawer">
                <div v-if="assetManagerVisible" class="asset-manager-layer" data-testid="asset-manager">
                    <button class="asset-manager-mask" aria-label="关闭资源管理" @click="closeAssetManager"></button>
                    <aside class="asset-manager-drawer" role="dialog" aria-modal="true" aria-label="云台资源管理">
                        <header class="asset-manager-header">
                            <div>
                                <span>云台资源管理</span>
                                <strong>{{ assetManagerTab === "preset" ? "预置位管理" : "巡航轨迹管理" }}</strong>
                            </div>
                            <button data-testid="asset-manager-close" title="关闭" @click="closeAssetManager"><X :size="16" /></button>
                        </header>

                        <div class="asset-manager-tabs">
                            <button
                                data-testid="asset-manager-tab-preset"
                                :class="{ active: assetManagerTab === 'preset' }"
                                @click="switchAssetManagerTab('preset')"
                            >
                                <Hash :size="13" /><span>预置位</span><em>{{ presets.length }}</em>
                            </button>
                            <button
                                data-testid="asset-manager-tab-cruise"
                                :class="{ active: assetManagerTab === 'cruise' }"
                                @click="switchAssetManagerTab('cruise')"
                            >
                                <Route :size="13" /><span>巡航轨迹</span><em>{{ cruiseTracks.length }}</em>
                            </button>
                        </div>

                        <label class="asset-manager-search">
                            <Search :size="14" />
                            <input v-model="assetSearch" type="search" :placeholder="assetManagerTab === 'preset' ? '搜索预置位名称或编号' : '搜索巡航名称或编号'" />
                        </label>

                        <div v-if="assetManagerTab === 'preset'" class="asset-manager-inline-actions">
                            <button
                                class="asset-manager-add"
                                data-testid="asset-manager-add-preset"
                                @click="openSavePresetDialog"
                            >
                                <Plus :size="12" /><span>添加预置位</span>
                            </button>
                        </div>

                        <div class="asset-manager-list">
                            <template v-if="assetManagerTab === 'preset'">
                                <div
                                    v-for="p in filteredPresets"
                                    :key="p.id"
                                    class="asset-manager-row"
                                    :class="{ active: activePresetId === p.id }"
                                    data-testid="asset-manager-row"
                                >
                                    <span class="asset-manager-index">#{{ p.id }}</span>
                                    <a-tooltip
                                        :content="`#${p.id} ${p.name}`"
                                        position="top"
                                        :mouse-enter-delay="RESOURCE_TOOLTIP_ENTER_DELAY_MS"
                                        :data-testid="`preset-manager-tooltip-${p.id}`"
                                    >
                                        <div class="asset-manager-info">
                                            <strong>{{ p.name }}</strong>
                                            <small>{{ p.setAt || "尚未记录更新时间" }}</small>
                                        </div>
                                    </a-tooltip>
                                    <div class="asset-manager-actions">
                                        <button class="btn-ghost xs" @click="callPreset(p.id)">
                                            <Navigation :size="11" />调用
                                        </button>
                                        <button class="asset-manager-delete" title="删除预置位" @click="deletePreset(p.id)"><Trash2 :size="12" /></button>
                                    </div>
                                </div>
                                <div v-if="filteredPresets.length === 0" class="asset-manager-empty preset-empty-large" data-testid="asset-manager-preset-empty">
                                    <Inbox :size="28" class="preset-empty-glyph" />
                                    <p class="preset-empty-line-primary">{{ assetSearch ? "没有匹配的预置位" : "暂无预置位" }}</p>
                                </div>
                            </template>

                            <template v-else>
                                <a-tooltip
                                    v-for="c in filteredCruiseTracks"
                                    :key="c.id"
                                    :content="cruiseTileTitle(c)"
                                    position="top"
                                    :mouse-enter-delay="RESOURCE_TOOLTIP_ENTER_DELAY_MS"
                                    :data-testid="`cruise-manager-tooltip-${c.id}`"
                                >
                                    <div
                                        class="asset-manager-row"
                                        :class="{ active: activeCruiseId === c.id, disabled: !c.enabled && !c.pending, pending: c.pending }"
                                        data-testid="asset-manager-row"
                                    >
                                        <span class="asset-manager-index">#{{ c.id }}</span>
                                        <div class="asset-manager-info">
                                            <strong>{{ c.name }}</strong>
                                            <small>{{ cruiseTrackMeta(c) }} · {{ c.pending ? "可试运行" : c.enabled ? "可调用" : "已禁用" }}</small>
                                        </div>
                                        <button class="btn-ghost xs" :disabled="!c.enabled && !c.pending" @click="toggleCruise(c.id)">
                                            <Square v-if="activeCruiseId === c.id && cruiseState === 'start-sent'" :size="11" />
                                            <Play v-else :size="11" />
                                            {{ activeCruiseId === c.id && cruiseState === "start-sent" ? "停止" : c.pending ? "试运行" : "启动" }}
                                        </button>
                                    </div>
                                </a-tooltip>
                                <div v-if="filteredCruiseTracks.length === 0" class="asset-manager-empty">没有匹配的巡航轨迹</div>
                            </template>
                        </div>

                        <footer class="asset-manager-footer">
                            <span>{{ assetManagerTab === "preset" ? `${filteredPresets.length} 个预置位` : `${filteredCruiseTracks.length} 条巡航轨迹 · ${cruiseSyncLabel}` }}</span>
                            <button v-if="assetManagerTab === 'cruise' && activeCruiseId !== null && cruiseState !== 'stopped'" class="btn-ghost xs" @click="stopCruise">
                                <Square :size="11" />停止全部
                            </button>
                        </footer>
                    </aside>
                </div>
            </Transition>

            <a-modal
                v-model:visible="savePresetDialogVisible"
                title="保存预置位"
                ok-text="保存"
                cancel-text="取消"
                modal-class="uvp-system-dialog preset-save-modal"
                :width="380"
                :mask-closable="false"
                :ok-loading="presetDraft?.submitting || false"
                :on-before-ok="handleSavePresetBeforeOk"
                unmount-on-close
                @cancel="closeSavePresetDialog"
                @close="closeSavePresetDialog"
            >
                <div v-if="presetDraft" class="preset-save-form" data-testid="preset-save-dialog">
                    <div class="preset-save-row">
                        <label class="preset-save-label">编号</label>
                        <span class="preset-save-index">#{{ presetDraft.id }}</span>
                    </div>
                    <div class="preset-save-row">
                        <label class="preset-save-label">名称</label>
                        <div class="preset-save-field">
                            <a-input
                                v-model="presetDraft.name"
                                allow-clear
                                :max-length="16"
                                :placeholder="`预置位 ${presetDraft.id}`"
                                :disabled="presetDraft.submitting"
                                :error="!!presetNameError"
                                data-testid="preset-save-name-input"
                                @blur="presetNameTouched = true"
                                @press-enter="presetNameTouched = true"
                            >
                                <template #suffix>
                                    <span class="preset-save-count" :class="{ ok: presetDraft.name.trim().length > 0 && presetDraft.name.trim().length <= 16 }">
                                        {{ presetDraft.name.length }}/16
                                    </span>
                                </template>
                            </a-input>
                            <p v-if="presetNameError" class="preset-save-error">{{ presetNameError }}</p>
                            <p v-else class="preset-save-hint">留空将使用默认名「预置位 {{ presetDraft.id }}」</p>
                        </div>
                    </div>
                </div>
            </a-modal>

            <a-modal
                v-model:visible="saveCruiseDialogVisible"
                title="新建巡航轨迹"
                ok-text="创建并下发"
                cancel-text="取消"
                modal-class="uvp-system-dialog cruise-save-modal"
                :width="480"
                :mask-closable="false"
                :closable="!cruiseDraft?.submitting"
                :esc-to-close="!cruiseDraft?.submitting"
                :cancel-button-props="{ disabled: cruiseDraft?.submitting || false }"
                :ok-loading="cruiseDraft?.submitting || false"
                :on-before-ok="handleSaveCruiseBeforeOk"
                :on-before-cancel="canCloseSaveCruiseDialog"
                unmount-on-close
                @cancel="closeSaveCruiseDialog"
                @close="closeSaveCruiseDialog"
            >
                <div v-if="cruiseDraft" class="cruise-save-form" data-testid="cruise-save-dialog">
                    <div class="cruise-save-notice">
                        <Info :size="14" />
                        <span>配置会直接下发到设备。部分旧版设备无法回传确认,下发后可通过“试运行”验证。</span>
                    </div>
                    <div class="cruise-save-row">
                        <label class="cruise-save-label">名称</label>
                        <div class="cruise-save-field">
                            <a-input
                                v-model="cruiseDraft.name"
                                allow-clear
                                :max-length="32"
                                :placeholder="`巡航 ${cruiseDraft.trackId}`"
                                :disabled="cruiseDraft.submitting"
                                data-testid="cruise-save-name-input"
                                @blur="cruiseDraftTouched = true"
                            >
                                <template #suffix>
                                    <span class="preset-save-count" :class="{ ok: cruiseDraft.name.trim().length > 0 && cruiseDraft.name.trim().length <= 32 }">
                                        {{ cruiseDraft.name.length }}/32
                                    </span>
                                </template>
                            </a-input>
                            <p class="preset-save-hint">名称仅在本平台显示,不会同步到设备。</p>
                        </div>
                    </div>
                    <div class="cruise-save-row">
                        <label class="cruise-save-label">站点</label>
                        <div class="cruise-save-field">
                            <div ref="cruiseStopsListEl" class="cruise-stops-list" data-testid="cruise-stops-list">
                                <div v-for="(stop, index) in cruiseDraft.stops" :key="stop.key" class="cruise-stop-row" data-testid="cruise-stop-row">
                                    <span class="cruise-stop-idx">{{ index + 1 }}</span>
                                    <a-select
                                        v-model="stop.presetId"
                                        :style="{ flex: '1 1 auto', minWidth: '0' }"
                                        :disabled="cruiseDraft.submitting"
                                        data-testid="cruise-stop-select"
                                        placeholder="选择预置位"
                                    >
                                        <a-option v-for="p in presets" :key="p.id" :value="p.id">#{{ p.id }} · {{ p.name }}</a-option>
                                    </a-select>
                                    <button class="cruise-stop-move" :disabled="cruiseDraft.submitting || index === 0" title="上移" aria-label="上移巡航点" @click="moveCruiseStop(index, -1)">↑</button>
                                    <button class="cruise-stop-move" :disabled="cruiseDraft.submitting || index === cruiseDraft.stops.length - 1" title="下移" aria-label="下移巡航点" @click="moveCruiseStop(index, 1)">↓</button>
                                    <button class="cruise-stop-del" :disabled="cruiseDraft.submitting || cruiseDraft.stops.length <= 1" title="删除该站点" aria-label="删除巡航点" @click="removeCruiseStop(index)">
                                        <X :size="11" />
                                    </button>
                                </div>
                            </div>
                            <button
                                class="cruise-stop-add"
                                data-testid="cruise-stop-add-btn"
                                :disabled="cruiseDraft.submitting || cruiseDraft.stops.length >= 32"
                                @click="addCruiseStop"
                            >
                                <Plus v-if="cruiseDraft.stops.length < 32" :size="14" />
                                <span>{{ cruiseDraft.stops.length >= 32 ? "已达到 32 站上限" : "添加巡航点" }}</span>
                                <small v-if="cruiseDraft.stops.length < 32" class="cruise-stop-add-count">还可添加 {{ 32 - cruiseDraft.stops.length }} 个</small>
                            </button>
                            <p class="preset-save-hint">已选择 {{ cruiseDraft.stops.length }} 个巡航点,设备将按列表顺序循环执行。</p>
                        </div>
                    </div>
                    <div class="cruise-save-row">
                        <label class="cruise-save-label">巡航速度</label>
                        <div class="cruise-save-field">
                            <div class="cruise-param-line">
                                <a-input-number
                                    v-model="cruiseDraft.speed"
                                    :min="1"
                                    :max="4095"
                                    :step="1"
                                    :style="{ width: '112px' }"
                                    :disabled="cruiseDraft.submitting || !cruiseDraft.sendSpeed"
                                    data-testid="cruise-save-speed"
                                    @blur="cruiseDraftTouched = true"
                                />
                                <span class="cruise-param-mode">
                                    <a-switch
                                        v-model="cruiseDraft.sendSpeed"
                                        size="small"
                                        :disabled="cruiseDraft.submitting"
                                        aria-label="是否下发巡航速度设置"
                                        data-testid="cruise-send-speed"
                                    />
                                    <span>{{ cruiseDraft.sendSpeed ? "下发设置" : "不下发" }}</span>
                                </span>
                            </div>
                            <p class="preset-save-hint">
                                {{ cruiseDraft.sendSpeed
                                    ? "国标 12 位协议值,范围 1-4095;无统一物理单位,实际快慢由设备决定。"
                                    : "本次不下发速度设置,设备保持当前设置。" }}
                            </p>
                        </div>
                    </div>
                    <div class="cruise-save-row">
                        <label class="cruise-save-label">每站停留</label>
                        <div class="cruise-save-field">
                            <div class="cruise-param-line">
                                <a-input-number
                                    v-model="cruiseDraft.dwellSec"
                                    :min="1"
                                    :max="4095"
                                    :step="1"
                                    :style="{ width: '112px' }"
                                    :disabled="cruiseDraft.submitting || !cruiseDraft.sendDwell"
                                    data-testid="cruise-save-dwell"
                                    @blur="cruiseDraftTouched = true"
                                />
                                <span class="cruise-save-unit">秒</span>
                                <span class="cruise-param-mode">
                                    <a-switch
                                        v-model="cruiseDraft.sendDwell"
                                        size="small"
                                        :disabled="cruiseDraft.submitting"
                                        aria-label="是否下发巡航停留时间设置"
                                        data-testid="cruise-send-dwell"
                                    />
                                    <span>{{ cruiseDraft.sendDwell ? "下发设置" : "不下发" }}</span>
                                </span>
                            </div>
                            <p class="preset-save-hint">
                                {{ cruiseDraft.sendDwell
                                    ? "国标单位为秒,范围 1-4095 秒,最长 68 分 15 秒。"
                                    : "本次不下发停留时间设置,设备保持当前设置。" }}
                            </p>
                        </div>
                    </div>
                    <details class="cruise-save-advanced">
                        <summary><Settings :size="13" />高级设置</summary>
                        <div class="cruise-save-advanced-body">
                            <div class="cruise-save-row">
                                <label class="cruise-save-label">编号</label>
                                <div class="cruise-save-field">
                                    <a-input-number
                                        v-model="cruiseDraft.trackId"
                                        :min="0"
                                        :max="255"
                                        :step="1"
                                        :style="{ width: '96px' }"
                                        :disabled="cruiseDraft.submitting"
                                        data-testid="cruise-save-track-id"
                                        @blur="cruiseDraftTouched = true"
                                    />
                                    <p class="preset-save-hint">轨迹编号由平台自动分配,通常无需修改。</p>
                                </div>
                            </div>
                            <div class="cruise-save-row">
                                <label class="cruise-save-label">覆盖</label>
                                <div class="cruise-save-field">
                                    <label class="cruise-save-replace">
                                        <input v-model="cruiseDraft.replaceExisting" type="checkbox" :disabled="cruiseDraft.submitting" data-testid="cruise-save-replace" />
                                        <span>覆盖同编号轨迹</span>
                                    </label>
                                    <p class="preset-save-hint">启用后会先清空设备中的同编号轨迹,此操作不可撤销。</p>
                                </div>
                            </div>
                        </div>
                    </details>
                    <p v-if="cruiseDraftError || cruiseDraftSubmitError" class="preset-save-error" data-testid="cruise-save-error">{{ cruiseDraftError || cruiseDraftSubmitError }}</p>
                </div>
            </a-modal>
        </div>
    </a-modal>
</template>

<style scoped lang="scss">
/* 主体壳子 —— 具体面板样式在后续 chunk 中追加 */
.play-console-modal :deep(.arco-modal-body) {
    padding: 14px 18px 18px;
    max-height: calc(100vh - 96px);
    overflow-y: auto;
}

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
.session-badge.warn { color: var(--uvp-warning); background: var(--uvp-warning-soft); border-color: var(--uvp-warning-border); }
.session-badge.warn .dot { background: var(--uvp-warning); }
.session-elapsed {
    padding-left: 8px; margin-left: 2px;
    color: color-mix(in srgb, currentcolor 70%, transparent);
    border-left: 1px solid color-mix(in srgb, currentcolor 24%, transparent);
    font-size: 10.5px; font-style: normal; font-weight: 600;
}

.console-body {
    position: relative; isolation: isolate;
    display: grid; grid-template-columns: minmax(0, 1fr) 336px;
    gap: 14px; min-height: 0;
}

.asset-manager-layer {
    position: absolute; z-index: 20; inset: 0;
    display: flex; justify-content: flex-end; overflow: hidden;
    border-radius: 12px;
}
.asset-manager-mask {
    position: absolute; inset: 0; padding: 0;
    background: rgb(15 23 42 / 34%); border: 0; cursor: pointer;
}
.asset-manager-drawer {
    position: relative; z-index: 1;
    display: grid; grid-template-rows: auto auto auto auto minmax(0, 1fr) auto;
    width: min(430px, 100%); min-width: 0; height: 100%;
    background: var(--uvp-panel-bg); border-left: 1px solid var(--uvp-panel-border);
    box-shadow: -18px 0 38px -24px rgb(15 23 42 / 52%);
}
.asset-manager-header {
    display: flex; align-items: center; justify-content: space-between; gap: 12px;
    padding: 14px 16px; border-bottom: 1px solid var(--uvp-panel-border);
}
.asset-manager-header > div { display: grid; gap: 2px; }
.asset-manager-header span { color: var(--uvp-text-tertiary); font-size: 9.5px; }
.asset-manager-header strong { color: var(--uvp-text-primary); font-size: 14px; }
.asset-manager-header > button {
    display: grid; place-items: center; width: 30px; height: 30px;
    color: var(--uvp-text-tertiary); background: transparent;
    border: 1px solid var(--uvp-panel-border); border-radius: 7px; cursor: pointer;
}
.asset-manager-header > button:hover { color: var(--uvp-text-primary); background: var(--uvp-list-toolbar-bg); }
.asset-manager-tabs {
    display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 4px;
    margin: 12px 16px 0; padding: 4px;
    background: var(--uvp-list-toolbar-bg); border: 1px solid var(--uvp-panel-border); border-radius: 8px;
}
.asset-manager-tabs button {
    display: grid; grid-template-columns: auto 1fr auto; gap: 6px; align-items: center;
    padding: 7px 9px; color: var(--uvp-text-tertiary); background: transparent;
    border: 0; border-radius: 6px; cursor: pointer; font-size: 11px; text-align: left;
}
.asset-manager-tabs button.active { color: var(--uvp-brand); background: var(--uvp-brand-soft); }
.asset-manager-tabs em {
    min-width: 20px; padding: 1px 5px; color: inherit;
    background: color-mix(in srgb, currentcolor 8%, transparent); border-radius: 4px;
    font-size: 9px; font-style: normal; text-align: center;
}
.asset-manager-search {
    display: grid; grid-template-columns: auto 1fr; gap: 8px; align-items: center;
    margin: 10px 16px 0; padding: 0 10px; height: 34px;
    color: var(--uvp-text-tertiary); background: var(--uvp-list-toolbar-bg);
    border: 1px solid var(--uvp-panel-border); border-radius: 7px;
}
.asset-manager-search:focus-within { color: var(--uvp-brand); border-color: var(--uvp-brand); }
.asset-manager-search input,
.asset-manager-create input {
    min-width: 0; color: var(--uvp-text-primary); background: transparent; border: 0; outline: none; font-size: 11px;
}
.asset-manager-search input::placeholder { color: var(--uvp-text-tertiary); }

/* 抽屉内「保存当前位置」按钮,弹弹窗提交 */
.asset-manager-inline-actions { display: flex; justify-content: flex-end; margin: 10px 16px 0; }
.asset-manager-add {
    display: inline-flex; align-items: center; gap: 4px;
    padding: 5px 12px;
    color: #fff; background: var(--uvp-brand);
    border: 0; border-radius: 6px; cursor: pointer;
    font-size: 11px; transition: background 0.12s ease;
}
.asset-manager-add:hover:not(:disabled) { background: color-mix(in srgb, var(--uvp-brand) 90%, #000); }
.asset-manager-add:disabled { opacity: 0.55; cursor: not-allowed; }

/* 保存预置位对话框 */
.preset-save-form { display: grid; gap: 14px; padding: 4px 2px 0; }
.preset-save-row { display: grid; grid-template-columns: 48px minmax(0, 1fr); gap: 12px; align-items: start; }
.preset-save-label { padding-top: 6px; color: var(--uvp-text-secondary); font-size: 12px; }
.preset-save-index {
    display: inline-flex; align-items: center;
    padding: 4px 10px;
    color: var(--uvp-brand); background: var(--uvp-brand-soft);
    border-radius: 5px; font-family: ui-monospace, Menlo, monospace; font-size: 12px; font-weight: 600;
}
.preset-save-field { display: grid; gap: 4px; }
.preset-save-count { color: var(--uvp-text-tertiary); font-size: 11px; }
.preset-save-count.ok { color: #059669; font-weight: 600; }
.preset-save-hint { margin: 0; color: var(--uvp-text-tertiary); font-size: 11px; }
.preset-save-error { margin: 0; color: var(--uvp-danger); font-size: 11px; }

/* 新建巡航轨迹对话框 */
.cruise-save-form {
    display: grid; gap: 14px;
    max-height: calc(100dvh - 180px); padding: 4px 6px 2px 2px;
    overflow-y: auto; overscroll-behavior: contain; scrollbar-gutter: stable;
}
.cruise-save-notice {
    display: flex; align-items: flex-start; gap: 8px;
    padding: 9px 10px;
    color: var(--uvp-text-secondary); background: var(--uvp-warning-soft);
    border: 1px solid var(--uvp-warning-border); border-radius: 6px;
    font-size: 11.5px; line-height: 1.5;
}
.cruise-save-notice > svg { flex: 0 0 auto; margin-top: 1px; color: var(--uvp-warning); }
.cruise-save-row { display: grid; grid-template-columns: 66px minmax(0, 1fr); gap: 12px; align-items: start; }
.cruise-save-label { padding-top: 6px; color: var(--uvp-text-secondary); font-size: 12px; }
.cruise-save-field { display: grid; gap: 4px; min-width: 0; }
.cruise-param-line { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; min-height: 32px; }
.cruise-param-mode { display: inline-flex; align-items: center; gap: 6px; margin-left: auto; color: var(--uvp-text-secondary); font-size: 11.5px; white-space: nowrap; }
.cruise-save-field > .preset-save-hint { line-height: 1.45; }
.cruise-save-unit { color: var(--uvp-text-secondary); font-size: 11.5px; }
.cruise-save-replace { display: inline-flex; align-items: center; gap: 5px; color: var(--uvp-text-secondary); font-size: 11.5px; cursor: pointer; }
.cruise-save-replace input { accent-color: var(--uvp-brand-cyan); }
.cruise-stops-list {
    display: grid; gap: 5px;
    max-height: clamp(168px, 30vh, 260px); padding-right: 3px;
    overflow-y: auto; overscroll-behavior: contain; scrollbar-gutter: stable;
}
.cruise-stop-row {
    display: flex; align-items: center; gap: 6px;
    padding: 4px 6px;
    background: color-mix(in srgb, var(--uvp-brand-cyan) 3%, var(--uvp-panel-bg));
    border: 1px solid color-mix(in srgb, var(--uvp-brand-cyan) 16%, var(--uvp-panel-border));
    border-radius: 6px;
}
.cruise-stop-idx {
    display: inline-grid; place-items: center; width: 22px; height: 22px;
    color: var(--uvp-brand-cyan); background: color-mix(in srgb, var(--uvp-brand-cyan) 10%, transparent);
    border-radius: 50%;
    font-family: ui-monospace, Menlo, monospace; font-size: 11px; font-weight: 600;
}
.cruise-stop-move,
.cruise-stop-del {
    display: inline-grid; place-items: center; width: 24px; height: 24px;
    background: transparent; border: 1px solid transparent;
    border-radius: 5px; cursor: pointer;
    color: var(--uvp-text-tertiary); font-size: 12px; font-weight: 600;
    transition: color 0.12s ease, background 0.12s ease, border-color 0.12s ease;
}
.cruise-stop-move:hover:not(:disabled) { color: var(--uvp-brand-cyan); background: color-mix(in srgb, var(--uvp-brand-cyan) 10%, transparent); }
.cruise-stop-del:hover:not(:disabled) { color: var(--uvp-danger); background: var(--uvp-danger-soft); border-color: var(--uvp-danger-border); }
.cruise-stop-move:disabled,
.cruise-stop-del:disabled { opacity: 0.35; cursor: not-allowed; }
.cruise-stop-add {
    display: inline-flex; align-items: center; justify-content: center; gap: 6px;
    width: 100%; min-height: 44px; padding: 8px 12px; margin-top: 3px;
    color: var(--uvp-brand); background: var(--uvp-brand-soft);
    border: 1px solid color-mix(in srgb, var(--uvp-brand) 44%, var(--uvp-panel-border));
    border-radius: 6px; cursor: pointer;
    font-size: 12px; font-weight: 600;
    transition: background 0.18s ease, border-color 0.18s ease, color 0.18s ease;
}
.cruise-stop-add:hover:not(:disabled) { color: #fff; background: var(--uvp-brand); border-color: var(--uvp-brand); }
.cruise-stop-add:focus-visible { outline: 2px solid color-mix(in srgb, var(--uvp-brand) 52%, transparent); outline-offset: 2px; }
.cruise-stop-add:disabled { opacity: 0.65; cursor: not-allowed; }
.cruise-stop-add-count { margin-left: auto; color: currentColor; font-size: 10px; font-weight: 400; opacity: 0.72; }
.cruise-save-advanced {
    padding-top: 8px; border-top: 1px solid var(--uvp-panel-border);
}
.cruise-save-advanced > summary {
    display: inline-flex; align-items: center; gap: 5px;
    color: var(--uvp-text-secondary); cursor: pointer;
    font-size: 11.5px; font-weight: 600;
}
.cruise-save-advanced > summary::marker { display: none; }
.cruise-save-advanced-body { display: grid; gap: 12px; padding-top: 12px; }
@media (max-width: 560px) {
    .cruise-save-form { max-height: calc(100dvh - 210px); }
    .cruise-save-row { grid-template-columns: minmax(0, 1fr); gap: 5px; }
    .cruise-save-label { padding-top: 0; }
}
.asset-manager-list {
    min-height: 0; overflow-y: auto; margin-top: 10px; padding: 0 16px;
    scrollbar-width: thin; scrollbar-color: color-mix(in srgb, var(--uvp-text-tertiary) 28%, transparent) transparent;
}
.asset-manager-row {
    display: grid; grid-template-columns: 34px minmax(0, 1fr) auto; gap: 8px; align-items: center;
    min-height: 48px; padding: 7px 0; border-bottom: 1px solid var(--uvp-panel-border);
}
.asset-manager-row.active { background: color-mix(in srgb, var(--uvp-brand) 5%, transparent); }
.asset-manager-row.disabled { opacity: 0.56; }
.asset-manager-index { color: var(--uvp-text-tertiary); font-family: ui-monospace, Menlo, monospace; font-size: 10px; }
.asset-manager-info { display: grid; gap: 2px; min-width: 0; }
.asset-manager-info strong { overflow: hidden; color: var(--uvp-text-primary); font-size: 11.5px; font-weight: 550; text-overflow: ellipsis; white-space: nowrap; }
.asset-manager-info small { overflow: hidden; color: var(--uvp-text-tertiary); font-size: 9.5px; text-overflow: ellipsis; white-space: nowrap; }
.asset-manager-actions { display: flex; align-items: center; gap: 4px; }
.asset-manager-delete {
    display: grid; place-items: center; width: 26px; height: 26px;
    color: var(--uvp-text-tertiary); background: transparent;
    border: 1px solid transparent; border-radius: 5px; cursor: pointer;
}
.asset-manager-delete:hover { color: var(--uvp-danger); background: var(--uvp-danger-soft); border-color: var(--uvp-danger-border); }
.asset-manager-empty { padding: 40px 12px; color: var(--uvp-text-tertiary); font-size: 11px; text-align: center; }
.asset-manager-empty.preset-empty-large { padding: 44px 16px 28px; }
.preset-empty-glyph { display: block; margin: 0 auto 6px; color: var(--uvp-text-tertiary); opacity: 0.55; }
.preset-empty-line-primary { margin: 0; color: var(--uvp-text-tertiary); font-size: 12px; }
.asset-manager-footer {
    display: flex; align-items: center; justify-content: space-between; gap: 10px;
    min-height: 42px; padding: 8px 16px; color: var(--uvp-text-tertiary);
    background: var(--uvp-list-toolbar-bg); border-top: 1px solid var(--uvp-panel-border); font-size: 10px;
}
.asset-drawer-enter-active, .asset-drawer-leave-active { transition: opacity 0.18s ease; }
.asset-drawer-enter-active .asset-manager-drawer, .asset-drawer-leave-active .asset-manager-drawer { transition: transform 0.18s ease; }
.asset-drawer-enter-from, .asset-drawer-leave-to { opacity: 0; }
.asset-drawer-enter-from .asset-manager-drawer, .asset-drawer-leave-to .asset-manager-drawer { transform: translateX(100%); }

/* 主区(视频):stage 仅保留语义,子项直接参与外层网格 */
.stage { display: contents; }
.stage.stage-wide .video-frame { grid-column: 1 / -1; }

.video-frame {
    position: relative; align-self: start; grid-column: 1; grid-row: 1; overflow: hidden;
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
.video-canvas :deep(.play-window) {
    width: 100%; height: 100%; min-height: 100%; aspect-ratio: auto;
    border: 0; border-radius: 0;
}
.drag-zoom-layer {
    position: absolute; inset: 0; z-index: 6; cursor: crosshair;
    background: rgb(8 47 73 / 12%); touch-action: none;
}
.drag-zoom-box {
    position: absolute; display: block; box-sizing: border-box;
    border: 1px solid var(--uvp-brand-cyan); background: rgb(34 211 238 / 14%);
    box-shadow: 0 0 0 1px rgb(34 211 238 / 22%);
}
.drag-zoom-hint {
    position: absolute; top: 10px; left: 50%; padding: 4px 8px;
    color: var(--uvp-brand-cyan); background: rgb(2 6 23 / 72%); border: 1px solid var(--uvp-panel-border);
    border-radius: 4px; font-size: 10px; transform: translateX(-50%);
}
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

/* 播放器下方的运行信息面板 */
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

/* 双区联动版:随 Tab 切换的全宽等高详情 —— 干掉外层白面板,4 张卡片直接躺在 tab 里 */
.linked-info-bar {
    --linked-detail-height: 148px;
    grid-column: 1 / -1; grid-row: 2;
    gap: 0; overflow: hidden;
    padding: 0;
    background: transparent; border: 0; border-radius: 0; box-shadow: none;
}
.linked-detail {
    display: flex; flex-direction: column; gap: 6px;
    box-sizing: border-box; height: var(--linked-detail-height); overflow: hidden;
    padding: 0;
}
.linked-detail > .linked-ptz-layout,
.linked-detail > .linked-probe-layout,
.linked-detail > .linked-image-layout,
.linked-detail > .linked-advanced-empty { flex: 1 1 0; min-height: 0; }
.linked-detail-hint {
    margin: 0 0 8px; padding: 0;
    color: var(--uvp-text-tertiary); font-size: 10.5px; line-height: 1.4;
}
.linked-stream-metrics { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); min-height: 0; }
.stream-live-metric { display: grid; align-content: center; gap: 3px; min-width: 0; padding: 0 14px; }
.stream-live-metric + .stream-live-metric { border-left: 1px solid var(--uvp-panel-border); }
.stream-live-metric:first-child { padding-left: 0; }
.stream-live-metric:last-child { padding-right: 0; }
.stream-live-metric span,
.stream-live-metric small { overflow: hidden; color: var(--uvp-text-tertiary); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.stream-live-metric strong { overflow: hidden; color: var(--uvp-text-primary); font-size: 15px; font-weight: 650; text-overflow: ellipsis; white-space: nowrap; }
.stream-live-metric strong.mono { font-family: 'SF Mono', 'Consolas', monospace; }
.stream-live-metric strong.warn,
.stream-live-metric strong.state-stale { color: var(--uvp-warning); }
.stream-live-metric strong.err,
.stream-live-metric strong.state-offline { color: var(--uvp-danger); }
.stream-live-metric strong.state-fresh { color: var(--uvp-brand-cyan); }
.linked-ptz-layout {
    box-sizing: border-box;
    display: grid; grid-template-columns: repeat(4, minmax(0, 1fr));
    grid-template-rows: minmax(0, 1fr);
    gap: 10px; min-height: 0; height: 100%; align-items: stretch;
}
.linked-section { min-width: 0; padding: 0; }
.linked-section .section-hd.compact { margin-top: 12px; }

/* 4 张 PTZ 卡片统一容器:实线淡蓝框 + 微蓝底,header 定高 + 主体 flex-1 填充,主体 overflow: hidden 保护 */
.linked-card {
    box-sizing: border-box;
    display: flex; flex-direction: column; gap: 8px;
    padding: 8px 10px 10px;
    background: color-mix(in srgb, var(--uvp-brand) 3%, var(--uvp-panel-bg));
    border: 1px solid color-mix(in srgb, var(--uvp-brand) 22%, var(--uvp-panel-border));
    border-radius: 8px;
    min-width: 0; min-height: 0; height: 100%;
    overflow: hidden;
}
.linked-card > .linked-card-hd,
.linked-card > .section-hd { flex: 0 0 auto; margin: 0; }
.linked-card > .section-hd.first { margin-top: 0; }
.linked-section .preset-grid {
    display: grid; grid-template-columns: repeat(3, minmax(0, 1fr));
    grid-auto-rows: min-content;
    gap: 5px;
    flex: 1 1 auto; min-height: 0; overflow: hidden;
    align-content: start;
}
.linked-section .preset-add { grid-column: 1 / -1; }
.preset-go { display: inline-grid; place-items: center; color: var(--uvp-text-tertiary); }

/* 紧凑胶囊 tile:名字省略 + 右侧红色 X 删除 */
.preset-tile {
    box-sizing: border-box;
    display: inline-flex; align-items: stretch; max-width: 100%; min-width: 0;
    background: var(--uvp-panel-bg);
    border: 1px solid var(--uvp-panel-border); border-radius: 5px; overflow: hidden;
    transition: border-color 0.12s ease;
}
.preset-tile:hover:not(.disabled) { border-color: color-mix(in srgb, var(--uvp-brand) 40%, var(--uvp-panel-border)); }
.preset-tile.active { background: var(--uvp-brand-soft); border-color: color-mix(in srgb, var(--uvp-brand) 45%, var(--uvp-panel-border)); }
.preset-tile.disabled { opacity: 0.5; }
.preset-tile .preset-tile-hit,
.preset-tile > .preset-tile-hit.preset-item,
.preset-tile > .preset-tile-hit.cruise-item {
    display: inline-flex; align-items: center; gap: 4px; min-width: 0; max-width: none;
    flex: 1 1 auto;
    padding: 3px 6px;
    color: var(--uvp-text-primary); background: transparent;
    border: 0; border-radius: 0; cursor: pointer; font-size: 10.5px; line-height: 1.4;
    grid-template-columns: unset;
    transition: none;
}
.preset-tile > .preset-tile-hit.preset-item:hover:not(:disabled),
.preset-tile > .preset-tile-hit.cruise-item:hover:not(:disabled) { border-color: transparent; background: transparent; }
.preset-tile > .preset-tile-hit.preset-item:disabled,
.preset-tile > .preset-tile-hit.cruise-item:disabled { opacity: 1; cursor: not-allowed; }
.preset-tile.active .preset-tile-hit { color: var(--uvp-brand); }
.preset-tile .preset-idx { color: var(--uvp-text-tertiary); font-family: ui-monospace, Menlo, monospace; font-size: 10px; flex-shrink: 0; }
.preset-tile.active .preset-idx { color: var(--uvp-brand); }
.preset-tile .preset-name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0; }
.preset-tile-del {
    display: inline-grid; place-items: center; width: 20px; padding: 0;
    color: color-mix(in srgb, var(--uvp-danger) 65%, var(--uvp-text-tertiary));
    background: transparent;
    border: 0; border-left: 1px solid var(--uvp-panel-border);
    cursor: pointer; transition: color 0.12s ease, background 0.12s ease;
}
.preset-tile-del:hover:not(:disabled) { color: var(--uvp-danger); background: var(--uvp-danger-soft); }
.preset-tile-del:disabled { opacity: 0.4; cursor: not-allowed; }

/* 「更多」chip:作为 preset-grid 的最后一个 cell,只在预置位溢出(> 9)时出现 */
.preset-more-popover-trigger {
    display: block; width: 100%; min-width: 0; box-sizing: border-box;
}
.preset-tile-more {
    box-sizing: border-box;
    display: inline-flex; align-items: center; justify-content: center; gap: 4px;
    width: 100%; padding: 3px 6px;
    color: var(--uvp-brand); background: var(--uvp-brand-soft);
    border: 1px solid color-mix(in srgb, var(--uvp-brand) 22%, var(--uvp-panel-border)); border-radius: 5px;
    cursor: pointer; font-size: 10.5px; line-height: 1.4;
    transition: background 0.12s ease, border-color 0.12s ease;
}
.preset-tile-more:hover { border-color: var(--uvp-brand); }
.preset-popover {
    display: grid; grid-template-rows: auto minmax(0, 1fr);
    min-width: 240px; max-width: 320px;
}
.preset-popover-hd {
    display: flex; align-items: center; justify-content: space-between; gap: 8px;
    padding: 8px 12px; color: var(--uvp-text-primary);
    background: var(--uvp-list-toolbar-bg);
    border-bottom: 1px solid var(--uvp-panel-border);
    font-size: 11.5px; font-weight: 600;
}
.preset-popover-hd > span { display: inline-flex; align-items: center; gap: 5px; }
.preset-popover-list {
    max-height: 280px; overflow-y: auto;
    scrollbar-width: thin;
    scrollbar-color: color-mix(in srgb, var(--uvp-text-tertiary) 28%, transparent) transparent;
}
.preset-popover-row {
    display: grid; grid-template-columns: auto minmax(0, 1fr) auto; gap: 8px; align-items: center;
    padding: 6px 12px;
    border-bottom: 1px solid var(--uvp-panel-border);
    transition: background 0.12s ease;
}
.preset-popover-row:last-child { border-bottom: 0; }
.preset-popover-row:hover { background: color-mix(in srgb, var(--uvp-brand) 4%, transparent); }
.preset-popover-row.active { background: color-mix(in srgb, var(--uvp-brand) 8%, transparent); }
.preset-popover-idx { color: var(--uvp-text-tertiary); font-family: ui-monospace, Menlo, monospace; font-size: 10.5px; }
.preset-popover-name { overflow: hidden; color: var(--uvp-text-primary); font-size: 11.5px; text-overflow: ellipsis; white-space: nowrap; min-width: 0; }
.preset-popover-actions { display: inline-flex; gap: 4px; }
.preset-popover-call,
.preset-popover-del {
    display: inline-grid; place-items: center; width: 24px; height: 24px;
    background: transparent;
    border: 1px solid transparent; border-radius: 5px; cursor: pointer;
    transition: color 0.12s ease, background 0.12s ease, border-color 0.12s ease;
}
.preset-popover-call { color: var(--uvp-brand); }
.preset-popover-call:hover:not(:disabled) { background: var(--uvp-brand-soft); border-color: color-mix(in srgb, var(--uvp-brand) 30%, transparent); }
.preset-popover-del { color: color-mix(in srgb, var(--uvp-danger) 65%, var(--uvp-text-tertiary)); }
.preset-popover-del:hover:not(:disabled) { color: var(--uvp-danger); background: var(--uvp-danger-soft); border-color: var(--uvp-danger-border); }
.preset-popover-call:disabled,
.preset-popover-del:disabled { opacity: 0.4; cursor: not-allowed; }

/* 预置位卡片头部行(标题 + 保存按钮),复用 .linked-card 提供的实线蓝框容器 */
.linked-card-hd {
    display: flex; align-items: center; justify-content: space-between; gap: 8px;
}
.linked-card-hd .section-title { display: inline-flex; align-items: center; gap: 5px; color: var(--uvp-text-primary); font-size: 11.5px; font-weight: 600; }
.preset-count {
    display: inline-flex; align-items: center; padding: 1px 6px; margin-left: 4px;
    color: var(--uvp-brand); background: var(--uvp-brand-soft);
    border-radius: 999px; font-family: ui-monospace, Menlo, monospace; font-size: 10px; font-weight: 600; font-style: normal;
}
.preset-save-btn {
    display: inline-flex; align-items: center; gap: 4px;
    padding: 3px 9px;
    color: var(--uvp-brand); background: transparent;
    border: 1px solid color-mix(in srgb, var(--uvp-brand) 40%, var(--uvp-panel-border)); border-radius: 5px;
    cursor: pointer; font-size: 10.5px; font-weight: 500;
    transition: background 0.12s ease, border-color 0.12s ease;
}
.preset-save-btn:hover:not(:disabled) { color: #fff; background: var(--uvp-brand); border-color: var(--uvp-brand); }
.preset-save-btn:disabled { opacity: 0.45; cursor: not-allowed; }
.preset-empty {
    display: grid; place-items: center; gap: 2px;
    padding: 18px 12px 14px;
}
.resource-summary-action {
    display: flex; align-items: center; justify-content: space-between; gap: 6px;
    min-height: 22px; padding: 2px 7px;
    color: var(--uvp-brand); background: var(--uvp-brand-soft);
    border: 1px solid color-mix(in srgb, var(--uvp-brand) 24%, var(--uvp-panel-border)); border-radius: 6px;
    cursor: pointer; font-size: 9.5px;
}
.preset-grid > .resource-summary-action { grid-column: 1 / -1; }
.resource-summary-action:hover { border-color: var(--uvp-brand); }
.linked-detail .home-config { gap: 5px; padding-top: 2px; }
.linked-detail .home-row select,
.linked-detail .home-row input { height: 26px; }
.wiper-actions { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 6px; margin-top: 6px; }
.wiper-actions button { width: 100%; justify-content: center; }
.linked-probe-layout { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0; min-height: 0; align-items: center; }
.linked-probe-tracks { grid-template-columns: repeat(2, minmax(0, 1fr)); }
.linked-timeline .frame-bars { height: 46px; }
.linked-image-layout { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 18px; align-items: center; }
.linked-image-adjust { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px 24px; margin: 0; }
.linked-image-adjust label { grid-template-columns: 48px minmax(0, 1fr) 28px; }
.linked-image-adjust em { color: var(--uvp-text-secondary); font-family: ui-monospace, Menlo, monospace; font-size: 10px; font-style: normal; text-align: right; }
.linked-apply { min-width: 120px; }

@media (max-width: 720px) {
    .stream-overview-metrics,
    .stream-detail-columns { grid-template-columns: 1fr; gap: 14px; }
    .linked-detail-hint { margin-bottom: 6px; }
    .linked-detail { height: auto; overflow: visible; }
    .linked-ptz-layout,
    .linked-probe-layout,
    .linked-probe-tracks,
    .linked-image-layout { grid-template-columns: 1fr; }
    .linked-stream-metrics { grid-template-columns: repeat(2, minmax(0, 1fr)); }
    .stream-live-metric { padding: 7px 12px; }
    .stream-live-metric:first-child { padding-left: 0; }
    .stream-live-metric:nth-child(odd) { border-left: 0; }
    .linked-section { padding: 12px 0; }
    .linked-section:first-child { padding-top: 0; }
    .linked-section:last-child { padding-bottom: 0; }
    .linked-section + .linked-section { border-top: 1px solid var(--uvp-panel-border); border-left: 0; }
    .linked-image-adjust { grid-template-columns: 1fr; }
    .linked-apply { width: 100%; }
    .asset-manager-layer { position: fixed; inset: 12px; border: 1px solid var(--uvp-panel-border); }
    .asset-manager-drawer { width: 100%; border-left: 0; }
}

/* ═══════════ 右侧栏 ═══════════ */
.sidebar {
    display: grid; grid-column: 2; grid-row: 1; gap: 10px; align-content: start;
    max-height: none; overflow: visible; padding-right: 2px;
}
.sidebar.sidebar-stream { grid-template-rows: auto minmax(0, 1fr); align-content: stretch; }
.sidebar::-webkit-scrollbar { width: 6px; }
.sidebar::-webkit-scrollbar-thumb { background: color-mix(in srgb, var(--uvp-text-tertiary) 30%, transparent); border-radius: 3px; }

/* Tab 切换 */
.tabs {
    display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 4px;
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
.sidebar .panels { padding: 12px 14px; }
.panel { display: grid; gap: 10px; }
.stream-panel-header { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.stream-metrics-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; }
.stream-metrics-grid > div {
    display: grid; gap: 4px; min-width: 0; padding: 10px 11px;
    background: var(--uvp-list-toolbar-bg); border: 1px solid var(--uvp-panel-border); border-radius: 8px;
}
.stream-metrics-grid span { color: var(--uvp-text-tertiary); font-size: 9.5px; }
.stream-metrics-grid strong { overflow: hidden; color: var(--uvp-text-secondary); font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }
.stream-metrics-grid strong.warn { color: var(--uvp-warning); }
.stream-metrics-grid strong.err { color: var(--uvp-danger); }
.stream-metrics-grid small { overflow: hidden; color: var(--uvp-text-tertiary); font-size: 9px; text-overflow: ellipsis; white-space: nowrap; }
.sidebar-stream .panels { height: 100%; min-height: 0; box-sizing: border-box; }
.sidebar-stream .stream-panel { grid-template-rows: auto minmax(0, 1fr); height: 100%; min-height: 0; }
.sidebar-stream .stream-metrics-grid { min-height: 0; }
.sidebar-stream .stream-metrics-grid > div { align-content: center; }
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

/* 方向盘 */
.ptz-pad {
    display: grid; grid-template-columns: repeat(3, 1fr); gap: 6px;
    max-width: 200px; margin: 8px auto 4px;
}
.ptz-pad.disabled { opacity: 0.42; pointer-events: none; }
.ptz-pad button {
    display: grid; place-items: center; height: 40px;
    color: var(--uvp-text-secondary); background: var(--uvp-list-toolbar-bg);
    border: 1px solid var(--uvp-panel-border); border-radius: 8px;
    cursor: pointer; transition: all 0.12s ease;
    user-select: none;
}
.ptz-pad button:hover:not(:disabled) { color: var(--uvp-brand); background: var(--uvp-brand-soft); border-color: color-mix(in srgb, var(--uvp-brand) 40%, var(--uvp-panel-border)); }
.ptz-pad button:active:not(:disabled) { transform: scale(0.94); }
.ptz-stop { background: transparent !important; border-color: transparent !important; }
.ptz-stop span { width: 10px; height: 10px; background: var(--uvp-danger); border-radius: 2px; }

.talk-mode-switch {
    display: grid; grid-template-columns: repeat(2, minmax(0, 1fr));
    width: 100%; max-width: 200px; margin: 6px auto 0;
    padding: 2px; background: var(--uvp-list-toolbar-bg);
    border: 1px solid var(--uvp-panel-border); border-radius: 7px;
}
.talk-mode-switch button {
    height: 22px; color: var(--uvp-text-tertiary); background: transparent;
    border: 0; border-radius: 5px; cursor: pointer; font-size: 10px;
}
.talk-mode-switch button.active { color: var(--uvp-brand); background: var(--uvp-brand-soft); font-weight: 600; }
.talk-mode-switch button:disabled { cursor: not-allowed; opacity: 0.42; }

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
.lens-grid.disabled { opacity: 0.42; pointer-events: none; }
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
/* 巡航 tile:复用 .preset-tile 尺寸/结构,active 用 brand-cyan 与预置位区分 */
.preset-tile.cruise-tile.active { background: color-mix(in srgb, var(--uvp-brand-cyan) 8%, var(--uvp-panel-bg)); border-color: color-mix(in srgb, var(--uvp-brand-cyan) 45%, var(--uvp-panel-border)); }
.preset-tile.cruise-tile.active .preset-tile-hit { color: var(--uvp-brand-cyan); }
.preset-tile.cruise-tile.active .cruise-tile-icon { color: var(--uvp-brand-cyan); }
.preset-tile.cruise-tile:hover:not(.disabled) { border-color: color-mix(in srgb, var(--uvp-brand-cyan) 40%, var(--uvp-panel-border)); }
.preset-tile.cruise-tile.pending { background: var(--uvp-warning-soft); border-color: var(--uvp-warning-border); }
.cruise-tile-icon { color: var(--uvp-text-tertiary); flex-shrink: 0; }
.cruise-freshness { color: var(--uvp-text-tertiary); font-size: 9.5px; font-weight: 500; white-space: nowrap; }
.cruise-status-badge {
    flex: 0 0 auto; padding: 0 4px;
    color: var(--uvp-warning); background: var(--uvp-warning-soft);
    border: 1px solid var(--uvp-warning-border); border-radius: 3px;
    font-size: 8.5px; font-weight: 600; line-height: 1.5;
}

/* 卡片头运行中标签:brand-cyan chip + 呼吸点,点击停止全部巡航 */
.cruise-running-chip {
    display: inline-flex; align-items: center; gap: 4px;
    padding: 1px 7px 1px 6px; margin-left: 6px;
    color: var(--uvp-brand-cyan);
    background: color-mix(in srgb, var(--uvp-brand-cyan) 10%, transparent);
    border: 1px solid color-mix(in srgb, var(--uvp-brand-cyan) 32%, transparent);
    border-radius: 999px; cursor: pointer;
    font-size: 10px; font-weight: 600; line-height: 1.5;
    transition: background 0.12s ease, border-color 0.12s ease;
}
.cruise-running-chip:hover { background: color-mix(in srgb, var(--uvp-brand-cyan) 18%, transparent); border-color: var(--uvp-brand-cyan); }
.cruise-running-dot {
    display: inline-block; width: 6px; height: 6px;
    background: var(--uvp-brand-cyan); border-radius: 50%;
}
.cruise-running-dot.start-sent { animation: cruise-pulse 1.4s ease-in-out infinite; }
@keyframes cruise-pulse {
    0%, 100% { opacity: 1; box-shadow: 0 0 0 0 color-mix(in srgb, var(--uvp-brand-cyan) 50%, transparent); }
    50% { opacity: 0.55; box-shadow: 0 0 0 4px color-mix(in srgb, var(--uvp-brand-cyan) 0%, transparent); }
}

/* 看守位 */
.home-config { display: grid; gap: 8px; padding-top: 4px; }
.home-status-line { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.home-status-line strong { color: var(--uvp-text-primary); font-size: 11px; }
.home-status-line span { color: var(--uvp-text-tertiary); font-size: 9.5px; }
.home-confirmed { display: grid; gap: 2px; color: var(--uvp-text-tertiary); font-size: 9.5px; line-height: 1.5; }
.home-confirmed strong { color: var(--uvp-text-secondary); font-size: 10.5px; }
.home-supports { display: grid; gap: 3px; color: var(--uvp-text-tertiary); font-size: 9.5px; line-height: 1.45; }
.home-warning, .home-error, .home-operation { margin: 0; font-size: 9.5px; line-height: 1.5; overflow-wrap: anywhere; }
.home-warning { color: var(--uvp-warning); }
.home-error { color: var(--uvp-danger); }
.home-operation { color: var(--uvp-text-tertiary); font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
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
.toggle input:disabled + span { cursor: not-allowed; opacity: 0.5; }
.home-actions { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 6px; }
.home-actions button { min-width: 0; }

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
.sidebar .probe-panel { gap: 8px; }
.sidebar .probe-summary > div { padding: 7px 8px; }
.sidebar .probe-verdict { padding: 7px 8px; }
.side-probe-health { display: grid; gap: 6px; padding-top: 2px; }
.side-probe-health .probe-health-grid { gap: 5px; }
.side-probe-health .probe-health-grid > div { padding: 5px 7px; }
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
.sidebar [data-testid="linked-side-advanced"] .adv-actions { grid-template-columns: 1fr; }
.sidebar [data-testid="linked-side-advanced"] .adv-btn { gap: 7px; padding: 8px; }
.adv-btn {
    display: grid; grid-template-columns: 20px 1fr; gap: 10px; align-items: center;
    padding: 10px;
    color: var(--uvp-text-secondary); background: var(--uvp-list-toolbar-bg);
    border: 1px solid var(--uvp-panel-border); border-radius: 8px;
    cursor: pointer; text-align: left;
    transition: all 0.15s ease;
}
.adv-btn:hover { border-color: color-mix(in srgb, var(--uvp-brand) 40%, var(--uvp-panel-border)); }
.adv-btn:disabled { cursor: not-allowed; opacity: 0.45; }
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
    .video-frame { grid-column: 1; grid-row: 1; }
    .sidebar { grid-column: 1; grid-row: 2; max-height: none; }
    .sidebar.sidebar-stream { grid-template-rows: auto auto; }
    .sidebar-stream .panels,
    .sidebar-stream .stream-panel { height: auto; }
    .linked-info-bar { grid-column: 1; grid-row: 3; }
}
@media (max-width: 640px) {
    .console-title { flex-wrap: wrap; }
    .tabs { grid-template-columns: repeat(4, minmax(0, 1fr)); }
    .probe-data-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
    .lens-grid { grid-template-columns: 1fr; }
    .linked-section .preset-grid { grid-template-columns: 1fr; }
}
</style>
