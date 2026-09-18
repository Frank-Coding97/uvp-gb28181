<script setup lang="ts">
/**
 * DeviceConfigDrawer - 设备配置中心（专业客户端形态）
 *
 * 形态对标海康/大华的「摄像头属性」窗口：左栏实时预览 + 操作按钮排 + 云台，
 * 中栏分组导航，右栏参数区（滑杆配数字微调）。目的是把 GB/T 28181 的
 * `DeviceConfig` 家族收进**一个入口**，而不是每项各挂一张小卡。
 *
 * ⛔ 两条边界（别被"顺手简化"）：
 *   1. 中栏分组是**标准 ConfigType 家族**，不是摄像机的 ISP 参数。平台管不到
 *      ISP，照搬那套会做出一个点不动的假界面。
 *   2. 目前只有「视频参数属性」(A-5) 接了后端。其余组是**静态形态**：控件齐全、
 *      交互禁用、统一标「未接入」。让人以为能下发是这版最危险的失败模式。
 */
import { computed, onBeforeUnmount, reactive, ref, watch } from "vue";
import {
    Camera,
    ChevronRight,
    Info,
    Minus,
    Plus,
    RefreshCcw,
    RotateCcw,
    Send,
    Wifi,
    WifiOff,
    X
} from "@lucide/vue";
import {
    applyChannelVideoParams,
    getChannelVideoParams,
    type ApplyVideoParamItem,
    type VideoParam,
    type VideoParamReconcile
} from "@/api/gb28181";
import {
    isValidResolutionCode,
    parseStreamNumberList,
    resolutionText,
    validateVideoParamItems,
    type VideoParamCodecItem
} from "../videoParamCodec";
import DeviceConfigSlider from "./DeviceConfigSlider.vue";
import {
    CONFIG_GROUPS,
    CONFIG_GROUP_DEFAULTS,
    WEEKDAY_LABELS,
    findConfigGroup,
    type ConfigField,
    type ConfigFieldKind,
    type ConfigGroup,
    type ConfigSelectOption,
    type FieldValue
} from "./deviceConfigGroups";

const props = withDefaults(defineProps<{
    visible: boolean;
    deviceName?: string;
    deviceCode?: string;
    online?: boolean;
    effectiveVersion?: string;
    channelId?: number | null;
    channelName?: string;
    canRead?: boolean;
    canApply?: boolean;
}>(), {
    deviceName: "",
    deviceCode: "",
    online: false,
    effectiveVersion: "",
    channelId: null,
    channelName: "",
    canRead: true,
    canApply: true
});

const emit = defineEmits<{ "update:visible": [value: boolean] }>();

const PTZ_STEPS = [1, 2, 3, 4, 5, 6, 7, 8];

const VIDEO_FORMAT_OPTIONS: ConfigSelectOption[] = [
    { value: "1", label: "MPEG-4" },
    { value: "2", label: "H.264" },
    { value: "3", label: "SVAC" },
    { value: "4", label: "3GP" },
    { value: "5", label: "H.265" }
];

const RESOLUTION_OPTIONS: ConfigSelectOption[] = [
    { value: "1", label: "QCIF" },
    { value: "2", label: "CIF" },
    { value: "3", label: "4CIF" },
    { value: "4", label: "D1" },
    { value: "5", label: "720P" },
    { value: "6", label: "1080P" }
];

const BIT_RATE_TYPE_OPTIONS: ConfigSelectOption[] = [
    { value: "1", label: "CBR" },
    { value: "2", label: "VBR" }
];

const CUSTOM_RESOLUTION = "__custom__";

/* ─────────────────────── 分组导航 ─────────────────────── */

const activeGroupKey = ref<string>("video-param");
const activeGroup = computed<ConfigGroup>(() => findConfigGroup(activeGroupKey.value) ?? (CONFIG_GROUPS[0] as ConfigGroup));

/**
 * 渲染用的扁平字段形状：把联合类型拍平，模板里就不必做类型窄化。
 * 少了这层，`field.min` 在 `select` 分支上过不了 vue-tsc。
 */
interface RenderField {
    kind: ConfigFieldKind;
    key: string;
    label: string;
    hint: string;
    options: ConfigSelectOption[];
    min: number;
    max: number;
    step: number;
    unit: string;
    placeholder: string;
    maxlength?: number;
}

function toRenderField(field: ConfigField): RenderField {
    const base: RenderField = {
        kind: field.kind,
        key: field.key,
        label: field.label,
        hint: field.hint ?? "",
        options: [],
        min: 0,
        max: 100,
        step: 1,
        unit: "",
        placeholder: ""
    };
    switch (field.kind) {
        case "select":
            return { ...base, options: field.options };
        case "slider":
            return { ...base, min: field.min, max: field.max, step: field.step ?? 1, unit: field.unit ?? "" };
        case "text":
            return { ...base, placeholder: field.placeholder ?? "", maxlength: field.maxlength };
        default:
            return base;
    }
}

const activeFields = computed<RenderField[]>(() => activeGroup.value.fields.map(toRenderField));

/* ─────────────────────── 静态组的取值 ─────────────────────── */

const staticValues = reactive<Record<string, Record<string, FieldValue>>>({});
for (const [groupKey, values] of Object.entries(CONFIG_GROUP_DEFAULTS)) {
    staticValues[groupKey] = { ...values };
}

function sv(groupKey: string, fieldKey: string): FieldValue | undefined {
    return staticValues[groupKey]?.[fieldKey];
}

function setSv(groupKey: string, fieldKey: string, value: FieldValue) {
    const bucket = staticValues[groupKey] ?? (staticValues[groupKey] = {});
    bucket[fieldKey] = value;
}

function textValue(groupKey: string, fieldKey: string): string {
    return String(sv(groupKey, fieldKey) ?? "");
}

function boolValue(groupKey: string, fieldKey: string): boolean {
    return sv(groupKey, fieldKey) === true;
}

function numberValue(groupKey: string, fieldKey: string, fallback: number): number {
    const parsed = Number(sv(groupKey, fieldKey));
    return Number.isFinite(parsed) ? parsed : fallback;
}

function coordsValue(groupKey: string, fieldKey: string): number[] {
    const raw = sv(groupKey, fieldKey);
    if (!Array.isArray(raw)) return [0, 0, 0, 0];
    return raw.map((item) => (typeof item === "number" ? item : Number(item) || 0));
}

function weekdaysValue(groupKey: string, fieldKey: string): string[] {
    const raw = sv(groupKey, fieldKey);
    return Array.isArray(raw) ? raw.map(String) : [];
}

function setCoordValue(groupKey: string, fieldKey: string, index: number, raw: string) {
    const next = coordsValue(groupKey, fieldKey).slice();
    const parsed = Number(raw);
    next[index] = Number.isFinite(parsed) ? parsed : 0;
    setSv(groupKey, fieldKey, next);
}

function toggleWeekday(groupKey: string, fieldKey: string, day: string) {
    const current = weekdaysValue(groupKey, fieldKey);
    setSv(groupKey, fieldKey, current.includes(day) ? current.filter((item) => item !== day) : [...current, day]);
}

const COORD_AXES = ["X", "Y", "W", "H"];

/* ─────────────────────── 视频参数属性（A-5，真实读写）─────────────────────── */

const videoRows = ref<VideoParam[]>([]);
const videoDraft = ref<VideoParamCodecItem[]>([]);
const videoReconcile = ref<VideoParamReconcile | null>(null);
const videoFreshness = ref("");

/** 后端 freshness 枚举的中文口径 —— 状态条是给操作员看的，别把英文枚举丢出去。 */
const FRESHNESS_TEXT: Record<string, string> = {
    fresh: "数据有效",
    stale: "数据已过期",
    unknown: "尚无回读"
};

const freshnessText = computed(() => {
    const raw = videoFreshness.value;
    if (!raw) return "";
    return FRESHNESS_TEXT[raw] ?? raw;
});
const videoStreamNumberList = ref("");
const videoRegisteredVersion = ref("");
const videoObservedAt = ref("");
const videoLoading = ref(false);
const videoApplying = ref(false);
const videoError = ref("");
const streamProfile = ref("0");
const ptzStep = ref(5);

let pollTimer: number | undefined;
let pollCount = 0;

function toDraft(rows: VideoParam[]): VideoParamCodecItem[] {
    return rows
        .slice()
        .sort((left, right) => left.streamNumber - right.streamNumber)
        .map((row) => ({
            streamNumber: row.streamNumber,
            videoFormat: String(row.videoFormat ?? ""),
            resolution: String(row.resolution ?? ""),
            frameRate: String(row.frameRate ?? ""),
            bitRateType: String(row.bitRateType ?? ""),
            videoBitRate: row.videoBitRate ?? null
        }));
}

const videoDirtyCount = computed(() => {
    const original = new Map(videoRows.value.map((row) => [row.streamNumber, row]));
    let changed = 0;
    for (const draft of videoDraft.value) {
        const source = original.get(draft.streamNumber);
        if (!source) {
            changed += 1;
            continue;
        }
        if (
            String(source.videoFormat ?? "") !== draft.videoFormat
            || String(source.resolution ?? "") !== draft.resolution
            || String(source.frameRate ?? "") !== draft.frameRate
            || String(source.bitRateType ?? "") !== draft.bitRateType
            || String(source.videoBitRate ?? "") !== String(draft.videoBitRate ?? "")
        ) {
            changed += 1;
        }
    }
    return changed;
});

function rowChanged(row: VideoParamCodecItem): boolean {
    const source = videoRows.value.find((item) => item.streamNumber === row.streamNumber);
    if (!source) return true;
    return String(source.videoFormat ?? "") !== row.videoFormat
        || String(source.resolution ?? "") !== row.resolution
        || String(source.frameRate ?? "") !== row.frameRate
        || String(source.bitRateType ?? "") !== row.bitRateType
        || String(source.videoBitRate ?? "") !== String(row.videoBitRate ?? "");
}

const visibleVideoRows = computed(() => {
    if (!videoDraft.value.length) return [];
    const target = Number(streamProfile.value);
    const matched = videoDraft.value.filter((row) => row.streamNumber === target);
    return matched.length ? matched : videoDraft.value;
});

function streamNumberText(streamNumber: number): string {
    if (streamNumber === 0) return "主码流";
    return `子码流 ${streamNumber}`;
}

function videoBitRateDisabled(row: VideoParamCodecItem): boolean {
    return String(row.bitRateType ?? "") !== "1";
}

/** 每格的来源徽标：`设备` = 本次回读命中了这一格；`缺省` = 设备没给这个元素。 */
function cellBadge(
    row: VideoParamCodecItem,
    field: "videoFormat" | "resolution" | "frameRate" | "bitRateType" | "videoBitRate"
): string {
    const source = videoRows.value.find((item) => item.streamNumber === row.streamNumber);
    if (!source) return "缺省";
    const raw = source[field];
    return raw === null || raw === undefined || String(raw).trim() === "" ? "缺省" : "设备";
}

/**
 * 徽标的悬浮解释 —— 徽标本身只有两个字（对齐整列宽度），完整口径放 title。
 * 「不发」比来源更强：码率类型为 VBR 时这一格根本不参与下发。
 */
const BADGE_TITLE: Record<string, string> = {
    设备: "本次回读拿到了这一格，值来自设备",
    缺省: "设备未上报此元素，按缺省处理",
    不发: "码率类型为 VBR 时不发此元素"
};

function badgeTitle(badge: string): string {
    return BADGE_TITLE[badge] ?? "";
}

/** 码率格的徽标：VBR 是「不发」，优先于来源判断。 */
function bitRateBadge(row: VideoParamCodecItem): string {
    return videoBitRateDisabled(row) ? "不发" : cellBadge(row, "videoBitRate");
}

/** 视频参数组的五格 —— 徽标、对账、汇总都按这个顺序走，别再各处重列。 */
const VIDEO_CELL_FIELDS = ["videoFormat", "resolution", "frameRate", "bitRateType", "videoBitRate"] as const;

/** 本组已由设备上报的格数 —— 与徽标「设备」同源，避免两处口径漂移。 */
const deviceCellCount = computed(() =>
    visibleVideoRows.value.reduce(
        (total, row) =>
            total +
            VIDEO_CELL_FIELDS.filter(
                (field) =>
                    !(field === "videoBitRate" && videoBitRateDisabled(row)) && cellBadge(row, field) === "设备"
            ).length,
        0
    )
);

function resolutionSelectValue(row: VideoParamCodecItem): string {
    return isValidResolutionCode(row.resolution) ? row.resolution : CUSTOM_RESOLUTION;
}

function onResolutionSelect(row: VideoParamCodecItem, event: Event) {
    const value = (event.target as HTMLSelectElement).value;
    if (value === CUSTOM_RESOLUTION) {
        row.resolution = isValidResolutionCode(row.resolution) ? "" : row.resolution;
        return;
    }
    row.resolution = value;
}

const RECONCILE_TONE: Record<string, string> = {
    read_ok: "ok",
    pending: "pending",
    never_read: "idle",
    type_absent: "warn",
    mismatch: "warn",
    failed: "error"
};

const reconcileTone = computed(() => RECONCILE_TONE[videoReconcile.value?.state ?? "never_read"] ?? "idle");

const reconcileText = computed(() => {
    const reconcile = videoReconcile.value;
    if (!reconcile) return "尚未读取设备视频参数";
    switch (reconcile.state) {
        case "never_read":
            return "尚未读取设备视频参数";
        case "pending":
            return "正在向设备读取参数…";
        case "read_ok":
            return "回读成功：以下为设备当前生效值";
        case "type_absent":
            return "设备未返回该配置类型（可判为不支持 VideoParamAttribute）";
        case "mismatch":
            return `设备已接受命令，但值未生效${reconcile.errorMessage ? `：${reconcile.errorMessage}` : ""}`;
        case "failed":
            return `读取或下发失败${reconcile.deviceError ? `：${reconcile.deviceError}` : ""}`;
        default:
            return "尚未读取设备视频参数";
    }
});

const versionNotice = computed(() => {
    if (videoRegisteredVersion.value && videoRegisteredVersion.value !== "2022") {
        return `平台按 ${videoRegisteredVersion.value} 版处理该设备`;
    }
    return "";
});

function resetVideoDraft() {
    videoDraft.value = toDraft(videoRows.value);
}

function clearVideoPoll() {
    if (pollTimer !== undefined) {
        window.clearTimeout(pollTimer);
        pollTimer = undefined;
    }
}

async function loadVideoParams(refresh: boolean) {
    if (!props.channelId) {
        videoError.value = "未选择通道，无法读取视频参数。";
        return;
    }
    videoLoading.value = true;
    videoError.value = "";
    try {
        const response = await getChannelVideoParams(props.channelId, refresh);
        if (response.code !== 0 || !response.data) throw new Error(response.message || "读取设备视频参数失败");
        const data = response.data;
        videoRows.value = data.list ?? [];
        videoDraft.value = toDraft(videoRows.value);
        videoReconcile.value = data.reconcile ?? null;
        videoFreshness.value = String(data.freshness ?? "");
        videoStreamNumberList.value = data.streamNumberList ?? "";
        videoRegisteredVersion.value = data.registeredVersion ?? "";
        videoObservedAt.value = data.observedAt ?? "";
        if (data.refreshOperationId) scheduleVideoPoll();
    } catch (error) {
        videoError.value = (error as Error)?.message || "读取设备视频参数失败";
    } finally {
        videoLoading.value = false;
    }
}

/** 下发后平台会自动回读对账，必须轮询到 reconcile 终态 —— 200 不是终态。 */
function scheduleVideoPoll() {
    clearVideoPoll();
    pollCount = 0;
    const tick = async () => {
        if (!props.visible || !props.channelId) return;
        pollCount += 1;
        await loadVideoParams(false);
        const state = videoReconcile.value?.state;
        const settled = state === "read_ok" || state === "type_absent" || state === "mismatch" || state === "failed";
        if (!settled && pollCount < 10) pollTimer = window.setTimeout(tick, 1500);
    };
    pollTimer = window.setTimeout(tick, 1200);
}

async function applyVideoParams() {
    if (!props.channelId) return;
    const invalid = validateVideoParamItems(videoDraft.value);
    if (invalid) {
        videoError.value = invalid;
        return;
    }
    videoApplying.value = true;
    videoError.value = "";
    try {
        const items: ApplyVideoParamItem[] = videoDraft.value.map((row) => ({
            streamNumber: row.streamNumber,
            videoFormat: row.videoFormat,
            resolution: row.resolution,
            frameRate: row.frameRate,
            bitRateType: row.bitRateType,
            videoBitRate: row.videoBitRate
        }));
        const response = await applyChannelVideoParams(props.channelId, items, `device-config-${Date.now()}`);
        if (response.code !== 0 || !response.data) throw new Error(response.message || "下发失败");
        if (response.data.reconcilePending) scheduleVideoPoll();
    } catch (error) {
        videoError.value = (error as Error)?.message || "下发失败";
    } finally {
        videoApplying.value = false;
    }
}

/* ─────────────────────── 生命周期 ─────────────────────── */

const titleText = computed(() => props.channelName?.trim() || props.deviceName?.trim() || "未选择设备");

const previewResolution = computed(() => {
    const row = videoRows.value.find((item) => item.streamNumber === Number(streamProfile.value));
    const text = resolutionText(row?.resolution);
    return text && text !== "—" ? text : "1280x720";
});

function close() {
    clearVideoPoll();
    emit("update:visible", false);
}

watch(() => props.visible, (visible) => {
    if (!visible) {
        clearVideoPoll();
        return;
    }
    if (props.channelId && !videoRows.value.length) void loadVideoParams(false);
}, { immediate: true });

watch(() => props.channelId, () => {
    videoRows.value = [];
    videoDraft.value = [];
    videoReconcile.value = null;
    videoStreamNumberList.value = "";
    videoObservedAt.value = "";
    videoError.value = "";
    if (props.visible && props.channelId) void loadVideoParams(false);
});

onBeforeUnmount(clearVideoPoll);
</script>

<template>
    <div v-if="visible" class="dcg-mask" @click.self="close">
        <section class="dcg-window" role="dialog" aria-modal="true" :aria-label="`设备配置 · ${titleText}`">
            <header class="dcg-titlebar">
                <span class="dcg-titlebar-mark" />
                <span class="dcg-titlebar-text">设备配置</span>
                <span class="dcg-titlebar-sep">·</span>
                <span class="dcg-titlebar-target" data-testid="dcg-title-target">{{ titleText }}</span>
                <span class="dcg-titlebar-code">{{ deviceCode || "—" }}</span>
                <span v-if="effectiveVersion || videoRegisteredVersion" class="dcg-titlebar-ver">GB/T 28181-{{ effectiveVersion || videoRegisteredVersion }}</span>
                <button type="button" class="dcg-close" aria-label="关闭" data-testid="dcg-close" @click="close">
                    <X :size="14" />
                </button>
            </header>

            <div class="dcg-body">
                <aside class="dcg-preview">
                    <div class="dcg-screen">
                        <div class="dcg-screen-empty">
                            <Camera :size="26" />
                            <p>实时预览</p>
                            <em>接入取流后在此显示画面</em>
                        </div>
                        <span class="dcg-screen-badge" :class="online ? 'is-on' : 'is-off'">
                            <Wifi v-if="online" :size="11" />
                            <WifiOff v-else :size="11" />
                            {{ online ? "在线" : "离线" }}
                        </span>
                        <span class="dcg-screen-res">{{ previewResolution }}</span>
                    </div>

                    <div class="dcg-actionbar">
                        <button
                            type="button"
                            class="dcg-btn"
                            data-testid="dcg-reset"
                            :disabled="videoApplying || !videoDraft.length"
                            @click="resetVideoDraft"
                        >
                            <RotateCcw :size="12" />还原
                        </button>
                        <button
                            type="button"
                            class="dcg-btn"
                            data-testid="dcg-read"
                            :disabled="videoLoading || !canRead"
                            @click="loadVideoParams(true)"
                        >
                            <RefreshCcw :size="12" />读取
                        </button>
                        <button
                            type="button"
                            class="dcg-btn is-primary"
                            data-testid="dcg-apply"
                            :disabled="videoApplying || !videoDirtyCount || !canApply"
                            @click="applyVideoParams"
                        >
                            <Send :size="12" />下发
                        </button>
                    </div>

                    <div class="dcg-ptz">
                        <div class="dcg-ptz-pad" aria-hidden="true">
                            <span class="dcg-ptz-arrow is-up" />
                            <span class="dcg-ptz-arrow is-down" />
                            <span class="dcg-ptz-arrow is-left" />
                            <span class="dcg-ptz-arrow is-right" />
                            <span class="dcg-ptz-center" />
                        </div>
                        <div class="dcg-ptz-zoom">
                            <div class="dcg-zoom-row">
                                <button type="button" class="dcg-step-btn" aria-label="变倍减小"><Minus :size="11" /></button>
                                <span>变倍</span>
                                <button type="button" class="dcg-step-btn" aria-label="变倍增大"><Plus :size="11" /></button>
                            </div>
                            <div class="dcg-zoom-row">
                                <button type="button" class="dcg-step-btn" aria-label="变焦减小"><Minus :size="11" /></button>
                                <span>变焦</span>
                                <button type="button" class="dcg-step-btn" aria-label="变焦增大"><Plus :size="11" /></button>
                            </div>
                        </div>
                    </div>

                    <div class="dcg-steprow">
                        <span>步长</span>
                        <select
                            class="dcg-select is-mini"
                            :value="ptzStep"
                            aria-label="云台步长"
                            @change="ptzStep = Number(($event.target as HTMLSelectElement).value)"
                        >
                            <option v-for="step in PTZ_STEPS" :key="step" :value="step">{{ step }}</option>
                        </select>
                    </div>
                </aside>

                <nav class="dcg-nav" aria-label="配置分组">
                    <button
                        v-for="group in CONFIG_GROUPS"
                        :key="group.key"
                        type="button"
                        class="dcg-nav-item"
                        :class="{ 'is-active': group.key === activeGroupKey }"
                        :data-testid="`dcg-nav-${group.key}`"
                        :data-state="group.state"
                        @click="activeGroupKey = group.key"
                    >
                        <ChevronRight :size="10" class="dcg-nav-caret" />
                        <span class="dcg-nav-text">
                            <span class="dcg-nav-label">{{ group.label }}</span>
                            <span class="dcg-nav-sub">{{ group.std }}</span>
                        </span>
                        <i
                            class="dcg-nav-flag"
                            :class="`is-${group.state}`"
                            :title="group.state === 'ready' ? '已接入后端' : '未接入后端（静态形态）'"
                        />
                    </button>
                </nav>

                <section class="dcg-params">
                    <header class="dcg-params-head">
                        <span class="dcg-params-title" data-testid="dcg-group-title">{{ activeGroup.label }}</span>
                        <span class="dcg-params-std">{{ activeGroup.std }}</span>
                        <span class="dcg-params-state" :class="`is-${activeGroup.state}`" data-testid="dcg-group-state">
                            {{ activeGroup.state === "ready" ? "已接入" : "未接入" }}
                        </span>
                        <label class="dcg-params-profile">
                            <span>配置文件</span>
                            <select
                                class="dcg-select is-mini"
                                :value="streamProfile"
                                :disabled="activeGroup.state !== 'ready'"
                                aria-label="配置文件"
                                @change="streamProfile = ($event.target as HTMLSelectElement).value"
                            >
                                <option value="0">主码流</option>
                                <option value="1">子码流 1</option>
                                <option value="2">子码流 2</option>
                            </select>
                        </label>
                    </header>

                    <p class="dcg-params-summary">
                        <Info :size="11" />
                        <span>{{ activeGroup.summary }}</span>
                    </p>

                    <div class="dcg-params-body" :class="{ 'is-static': activeGroup.state === 'static' }">
                        <template v-if="activeGroup.key === 'video-param'">
                            <div class="dcg-reconcile" :class="`is-${reconcileTone}`" data-testid="dcg-reconcile">
                                <span class="dcg-reconcile-dot" />
                                <span>{{ reconcileText }}</span>
                            </div>
                            <p v-if="versionNotice" class="dcg-notice" data-testid="dcg-version-notice">{{ versionNotice }}</p>
                            <p v-if="videoError" class="dcg-error" data-testid="dcg-error">{{ videoError }}</p>

                            <div v-if="!visibleVideoRows.length" class="dcg-empty" data-testid="dcg-empty">
                                <p>尚未读取设备视频参数</p>
                                <em>点左栏「读取」从设备拉取当前编码参数</em>
                            </div>

                            <div
                                v-for="row in visibleVideoRows"
                                :key="row.streamNumber"
                                class="dcg-stream"
                                :data-testid="`dcg-stream-${row.streamNumber}`"
                            >
                                <div class="dcg-stream-hd">
                                    <span>码流 {{ row.streamNumber }}</span>
                                    <em>{{ streamNumberText(row.streamNumber) }}</em>
                                    <i v-if="rowChanged(row)" class="dcg-stream-dirty">已改</i>
                                </div>

                                <div class="dcg-row">
                                    <span class="dcg-row-label">编码格式</span>
                                    <div class="dcg-row-control">
                                        <select
                                            class="dcg-select"
                                            :value="row.videoFormat"
                                            :data-testid="`dcg-format-${row.streamNumber}`"
                                            @change="row.videoFormat = ($event.target as HTMLSelectElement).value"
                                        >
                                            <option v-for="option in VIDEO_FORMAT_OPTIONS" :key="option.value" :value="option.value">
                                                {{ option.label }}
                                            </option>
                                        </select>
                                    </div>
                                    <span
                                        class="dcg-row-hint"
                                        :data-source="cellBadge(row, 'videoFormat')"
                                        :title="badgeTitle(cellBadge(row, 'videoFormat'))"
                                    >{{ cellBadge(row, 'videoFormat') }}</span>
                                </div>

                                <div class="dcg-row">
                                    <span class="dcg-row-label">分辨率</span>
                                    <div class="dcg-row-control">
                                        <select
                                            class="dcg-select"
                                            :value="resolutionSelectValue(row)"
                                            :data-testid="`dcg-resolution-${row.streamNumber}`"
                                            @change="onResolutionSelect(row, $event)"
                                        >
                                            <option v-for="option in RESOLUTION_OPTIONS" :key="option.value" :value="option.value">
                                                {{ option.label }}
                                            </option>
                                            <option :value="CUSTOM_RESOLUTION">自定义…</option>
                                        </select>
                                        <input
                                            v-if="!isValidResolutionCode(row.resolution)"
                                            class="dcg-input is-narrow"
                                            :value="row.resolution"
                                            placeholder="1920x1080"
                                            aria-label="自定义分辨率"
                                            :data-testid="`dcg-resolution-custom-${row.streamNumber}`"
                                            @change="row.resolution = ($event.target as HTMLInputElement).value"
                                        />
                                    </div>
                                    <span
                                        class="dcg-row-hint"
                                        :data-source="cellBadge(row, 'resolution')"
                                        :title="badgeTitle(cellBadge(row, 'resolution'))"
                                    >{{ cellBadge(row, 'resolution') }}</span>
                                </div>

                                <div class="dcg-row">
                                    <span class="dcg-row-label">帧率</span>
                                    <div class="dcg-row-control">
                                        <DeviceConfigSlider
                                            :model-value="row.frameRate"
                                            :min="0"
                                            :max="99"
                                            label="帧率"
                                            :data-testid="`dcg-frame-rate-${row.streamNumber}`"
                                            @update:model-value="row.frameRate = $event"
                                        />
                                    </div>
                                    <span
                                        class="dcg-row-hint"
                                        :data-source="cellBadge(row, 'frameRate')"
                                        :title="badgeTitle(cellBadge(row, 'frameRate'))"
                                    >{{ cellBadge(row, 'frameRate') }}</span>
                                </div>

                                <div class="dcg-row">
                                    <span class="dcg-row-label">码率类型</span>
                                    <div class="dcg-row-control">
                                        <div class="dcg-segment" role="group" aria-label="码率类型">
                                            <button
                                                v-for="option in BIT_RATE_TYPE_OPTIONS"
                                                :key="option.value"
                                                type="button"
                                                :class="{ 'is-on': row.bitRateType === option.value }"
                                                :data-testid="`dcg-bit-rate-type-${row.streamNumber}-${option.value}`"
                                                @click="row.bitRateType = option.value"
                                            >
                                                {{ option.label }}
                                            </button>
                                        </div>
                                    </div>
                                    <span
                                        class="dcg-row-hint"
                                        :data-source="cellBadge(row, 'bitRateType')"
                                        :title="badgeTitle(cellBadge(row, 'bitRateType'))"
                                    >{{ cellBadge(row, 'bitRateType') }}</span>
                                </div>

                                <div class="dcg-row">
                                    <span class="dcg-row-label">码率</span>
                                    <div class="dcg-row-control">
                                        <DeviceConfigSlider
                                            :model-value="row.videoBitRate ?? ''"
                                            :min="0"
                                            :max="100000"
                                            :step="100"
                                            unit="kb/s"
                                            label="码率"
                                            :disabled="videoBitRateDisabled(row)"
                                            :data-testid="`dcg-bit-rate-${row.streamNumber}`"
                                            @update:model-value="row.videoBitRate = $event"
                                        />
                                    </div>
                                    <span
                                        class="dcg-row-hint"
                                        :data-source="bitRateBadge(row)"
                                        :title="badgeTitle(bitRateBadge(row))"
                                    >{{ bitRateBadge(row) }}</span>
                                </div>
                            </div>
                        </template>

                        <template v-else>
                            <div v-for="field in activeFields" :key="field.key" class="dcg-row" :data-testid="`dcg-field-${field.key}`">
                                <span class="dcg-row-label">{{ field.label }}</span>
                                <div class="dcg-row-control">
                                    <select
                                        v-if="field.kind === 'select'"
                                        class="dcg-select"
                                        :value="textValue(activeGroup.key, field.key)"
                                        disabled
                                        @change="setSv(activeGroup.key, field.key, ($event.target as HTMLSelectElement).value)"
                                    >
                                        <option v-for="option in field.options" :key="option.value" :value="option.value">
                                            {{ option.label }}
                                        </option>
                                    </select>

                                    <DeviceConfigSlider
                                        v-else-if="field.kind === 'slider'"
                                        :model-value="numberValue(activeGroup.key, field.key, field.min)"
                                        :min="field.min"
                                        :max="field.max"
                                        :step="field.step"
                                        :unit="field.unit"
                                        :label="field.label"
                                        disabled
                                        @update:model-value="setSv(activeGroup.key, field.key, Number($event))"
                                    />

                                    <button
                                        v-else-if="field.kind === 'switch'"
                                        type="button"
                                        class="dcg-switch"
                                        :class="{ 'is-on': boolValue(activeGroup.key, field.key) }"
                                        disabled
                                        :aria-label="field.label"
                                        @click="setSv(activeGroup.key, field.key, !boolValue(activeGroup.key, field.key))"
                                    >
                                        <i />
                                    </button>

                                    <div v-else-if="field.kind === 'coords'" class="dcg-coords">
                                        <label v-for="(axis, index) in COORD_AXES" :key="axis">
                                            <span>{{ axis }}</span>
                                            <input
                                                class="dcg-input is-coord"
                                                type="text"
                                                inputmode="numeric"
                                                disabled
                                                :value="coordsValue(activeGroup.key, field.key)[index]"
                                                @change="setCoordValue(activeGroup.key, field.key, index, ($event.target as HTMLInputElement).value)"
                                            />
                                        </label>
                                    </div>

                                    <div v-else-if="field.kind === 'weekdays'" class="dcg-weekdays">
                                        <button
                                            v-for="day in WEEKDAY_LABELS"
                                            :key="day"
                                            type="button"
                                            :class="{ 'is-on': weekdaysValue(activeGroup.key, field.key).includes(day) }"
                                            disabled
                                            @click="toggleWeekday(activeGroup.key, field.key, day)"
                                        >
                                            {{ day }}
                                        </button>
                                    </div>

                                    <input
                                        v-else
                                        class="dcg-input"
                                        type="text"
                                        disabled
                                        :value="textValue(activeGroup.key, field.key)"
                                        :placeholder="field.placeholder"
                                        :maxlength="field.maxlength"
                                        :aria-label="field.label"
                                        @change="setSv(activeGroup.key, field.key, ($event.target as HTMLInputElement).value)"
                                    />
                                </div>
                                <span class="dcg-row-hint">{{ field.hint }}</span>
                            </div>

                            <p class="dcg-static-note" data-testid="dcg-static-note">
                                <Info :size="11" />
                                <span>该分组尚未接入后端，本页为静态形态预览。控件与字段口径已按标准定型，接入后即可读写。</span>
                            </p>
                        </template>
                    </div>

                    <p class="dcg-params-foot" data-testid="dcg-params-foot">
                        <template v-if="activeGroup.state === 'ready'">
                            <span>本组 5 项 × {{ visibleVideoRows.length }} 路码流</span>
                            <span class="dcg-foot-sep">·</span>
                            <span>设备上报 {{ deviceCellCount }} 项</span>
                            <span class="dcg-foot-sep">·</span>
                            <span :class="{ 'is-dirty': videoDirtyCount > 0 }">待下发 {{ videoDirtyCount }} 项</span>
                        </template>
                        <template v-else>
                            <span>本组 {{ activeFields.length }} 项</span>
                            <span class="dcg-foot-sep">·</span>
                            <span>静态形态，接入后即可读写</span>
                        </template>
                    </p>
                </section>
            </div>

            <footer class="dcg-statusbar">
                <span class="dcg-status-item">
                    <i class="dcg-status-dot" :class="`is-${activeGroup.state}`" />
                    {{ activeGroup.state === "ready" ? "本组已接入后端，可读可写" : "本组为静态形态，尚未接入后端" }}
                </span>
                <span v-if="activeGroup.key === 'video-param'" class="dcg-status-item">
                    已改 {{ videoDirtyCount }} 项 · 回读 {{ videoObservedAt || "—" }}
                </span>
                <span v-if="videoStreamNumberList" class="dcg-status-item">
                    码流声明 {{ parseStreamNumberList(videoStreamNumberList).join(" / ") || "—" }}
                </span>
                <span
                    class="dcg-status-item is-right"
                    :title="videoFreshness ? `后端 freshness=${videoFreshness}` : ''"
                >
                    {{ freshnessText || "—" }}
                </span>
            </footer>
        </section>
    </div>
</template>

<style scoped lang="scss">
.dcg-mask {
    position: fixed;
    inset: 0;
    z-index: 1200;
    display: flex;
    align-items: center;
    justify-content: center;
    background: rgb(15 23 42 / 42%);
}

.dcg-window {
    display: flex;
    flex-direction: column;
    width: min(1100px, 94vw);
    height: min(580px, 88vh);
    overflow: hidden;
    background: var(--uvp-dialog-bg, #fff);
    border: 1px solid var(--uvp-dialog-border, #e6edf7);
    border-radius: 8px;
    box-shadow: var(--uvp-dialog-shadow, 0 24px 72px -32px rgb(15 23 42 / 34%));
}

/* ── 标题栏 ── */
.dcg-titlebar {
    flex: none;
    display: flex;
    align-items: center;
    gap: 8px;
    height: 36px;
    padding: 0 8px 0 12px;
    background: var(--uvp-dialog-header-bg, #fbfdff);
    border-bottom: 1px solid var(--uvp-dialog-border, #e6edf7);
}

.dcg-titlebar-mark {
    width: 3px;
    height: 14px;
    background: var(--uvp-brand, #2563eb);
    border-radius: 2px;
}

.dcg-titlebar-text {
    color: var(--uvp-text-primary);
    font-size: 13px;
    font-weight: 500;
}

.dcg-titlebar-sep,
.dcg-titlebar-code,
.dcg-titlebar-ver {
    color: var(--uvp-text-tertiary);
    font-size: 11px;
}

.dcg-titlebar-target {
    color: var(--uvp-text-secondary);
    font-size: 12px;
}

.dcg-titlebar-code {
    padding: 1px 6px;
    font-family: var(--uvp-font-mono, ui-monospace, monospace);
    background: var(--uvp-dialog-control-bg, #f8fbff);
    border: 1px solid var(--uvp-dialog-border, #e6edf7);
    border-radius: 3px;
}

.dcg-titlebar-ver {
    margin-left: auto;
    padding: 1px 6px;
    color: var(--uvp-brand, #2563eb);
    background: var(--uvp-brand-soft, #e8f2ff);
    border-radius: 3px;
}

.dcg-close {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 24px;
    height: 24px;
    padding: 0;
    color: var(--uvp-text-tertiary);
    background: transparent;
    border: none;
    border-radius: 4px;
    cursor: pointer;

    &:hover {
        color: var(--uvp-danger, #d14343);
        background: var(--uvp-danger-soft, #fff2f2);
    }
}

/* ── 三栏主体 ── */
.dcg-body {
    flex: 1 1 auto;
    display: grid;
    grid-template-columns: minmax(340px, 380px) 250px minmax(0, 1fr);
    min-height: 0;
}

.dcg-preview {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 12px;
    background: var(--uvp-dialog-control-bg, #f8fbff);
    border-right: 1px solid var(--uvp-dialog-border, #e6edf7);
}

.dcg-screen {
    position: relative;
    flex: 1 1 auto;
    min-height: 208px;
    overflow: hidden;
    background: #10161f;
    border: 1px solid #0b1118;
    border-radius: 4px;
}

.dcg-screen-empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 4px;
    height: 100%;
    color: #64748b;

    p {
        margin: 0;
        color: #94a3b8;
        font-size: 12px;
    }

    em {
        color: #556274;
        font-size: 10px;
        font-style: normal;
    }
}

.dcg-screen-badge {
    position: absolute;
    top: 6px;
    right: 6px;
    display: inline-flex;
    align-items: center;
    gap: 3px;
    padding: 1px 6px;
    font-size: 10px;
    border-radius: 3px;

    &.is-on {
        color: #d1fae5;
        background: rgb(16 185 129 / 22%);
    }

    &.is-off {
        color: #fecaca;
        background: rgb(239 68 68 / 22%);
    }
}

.dcg-screen-res {
    position: absolute;
    bottom: 6px;
    left: 6px;
    padding: 1px 5px;
    color: #94a3b8;
    font-family: var(--uvp-font-mono, ui-monospace, monospace);
    font-size: 10px;
    background: rgb(0 0 0 / 42%);
    border-radius: 3px;
}

.dcg-actionbar {
    flex: none;
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 6px;
}

.dcg-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 4px;
    height: 26px;
    padding: 0 8px;
    color: var(--uvp-text-secondary);
    font-size: 12px;
    background: var(--uvp-secondary-action-bg, #fff);
    border: 1px solid var(--uvp-secondary-action-border, #dbe4f0);
    border-radius: 4px;
    cursor: pointer;

    &:hover:not(:disabled) {
        color: var(--uvp-brand);
        border-color: var(--uvp-brand);
    }

    &:disabled {
        cursor: not-allowed;
        opacity: 0.5;
    }

    &.is-primary:not(:disabled) {
        color: #fff;
        background: var(--uvp-brand, #2563eb);
        border-color: var(--uvp-brand-strong, #1d4ed8);
    }

    &.is-primary:hover:not(:disabled) {
        color: #fff;
        background: var(--uvp-brand-strong, #1d4ed8);
    }
}

.dcg-ptz {
    flex: none;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding-top: 4px;
}

.dcg-ptz-pad {
    position: relative;
    width: 84px;
    height: 84px;
    background: linear-gradient(180deg, #fff 0%, #eef2f8 100%);
    border: 1px solid var(--uvp-secondary-action-border, #cdd8e6);
    border-radius: 50%;
    box-shadow: inset 0 1px 2px rgb(15 23 42 / 8%);
}

.dcg-ptz-arrow {
    position: absolute;
    width: 0;
    height: 0;
    border: 5px solid transparent;
    opacity: 0.62;

    &.is-up,
    &.is-down {
        left: 50%;
        margin-left: -5px;
    }

    &.is-up {
        top: 9px;
        border-bottom-color: #64748b;
    }

    &.is-down {
        bottom: 9px;
        border-top-color: #64748b;
    }

    &.is-left,
    &.is-right {
        top: 50%;
        margin-top: -5px;
    }

    &.is-left {
        left: 9px;
        border-right-color: #64748b;
    }

    &.is-right {
        right: 9px;
        border-left-color: #64748b;
    }
}

.dcg-ptz-center {
    position: absolute;
    top: 50%;
    left: 50%;
    width: 20px;
    height: 20px;
    margin: -10px 0 0 -10px;
    background: linear-gradient(180deg, #fff 0%, #e2e8f0 100%);
    border: 1px solid #c3cede;
    border-radius: 50%;
}

.dcg-ptz-zoom {
    display: flex;
    flex-direction: column;
    gap: 8px;
}

.dcg-zoom-row {
    display: flex;
    align-items: center;
    gap: 8px;

    span {
        min-width: 28px;
        color: var(--uvp-text-secondary);
        font-size: 12px;
        text-align: center;
    }
}

.dcg-step-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 22px;
    height: 22px;
    padding: 0;
    color: var(--uvp-text-secondary);
    background: var(--uvp-secondary-action-bg, #fff);
    border: 1px solid var(--uvp-secondary-action-border, #cdd8e6);
    border-radius: 50%;
    cursor: pointer;

    &:hover {
        color: var(--uvp-brand);
        border-color: var(--uvp-brand);
    }
}

.dcg-steprow {
    flex: none;
    display: flex;
    align-items: center;
    gap: 8px;
    padding-top: 2px;

    > span {
        color: var(--uvp-text-secondary);
        font-size: 12px;
    }
}

/* ── 中栏：分组导航 ── */
.dcg-nav {
    display: flex;
    flex-direction: column;
    padding: 8px 0;
    overflow-y: auto;
    background: var(--uvp-dialog-control-bg, #f8fbff);
    border-right: 1px solid var(--uvp-dialog-border, #e6edf7);
}

.dcg-nav-item {
    display: flex;
    align-items: center;
    gap: 6px;
    height: 40px;
    padding: 0 10px;
    color: var(--uvp-text-secondary);
    font-size: 12px;
    text-align: left;
    background: transparent;
    border: none;
    cursor: pointer;

    &:hover {
        background: var(--uvp-table-row-hover-bg, #f8fbff);
    }

    &.is-active {
        color: #fff;
        background: var(--uvp-brand, #2563eb);

        .dcg-nav-caret {
            transform: rotate(90deg);
        }
    }
}

.dcg-nav-caret {
    flex: none;
    transition: transform 0.15s;
}

.dcg-nav-label {
    flex: 1 1 auto;
    min-width: 0;
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
}

.dcg-nav-text {
    flex: 1 1 auto;
    display: flex;
    flex-direction: column;
    gap: 1px;
    min-width: 0;
}

/* 条款号：借用标准编号当副标题，中栏不至于只有一列光秃秃的分组名 */
.dcg-nav-sub {
    color: var(--uvp-text-tertiary);
    font-family: var(--uvp-font-mono, ui-monospace, monospace);
    font-size: 9.5px;
    opacity: 0.85;
}

.dcg-nav-item.is-active .dcg-nav-sub {
    color: rgb(255 255 255 / 70%);
}

.dcg-nav-flag {
    flex: none;
    width: 6px;
    height: 6px;
    border-radius: 50%;

    &.is-ready {
        background: #10b981;
    }

    &.is-static {
        background: #cbd5e1;
    }
}

.dcg-nav-item.is-active .dcg-nav-flag.is-static {
    background: rgb(255 255 255 / 55%);
}

/* ── 右栏：参数区 ── */
.dcg-params {
    display: flex;
    flex-direction: column;
    min-width: 0;
}

.dcg-params-head {
    flex: none;
    display: flex;
    align-items: center;
    gap: 8px;
    height: 38px;
    padding: 0 12px;
    border-bottom: 1px solid var(--uvp-dialog-border, #e6edf7);
}

.dcg-params-title {
    color: var(--uvp-text-primary);
    font-size: 13px;
    font-weight: 500;
}

.dcg-params-std {
    padding: 1px 5px;
    color: var(--uvp-text-tertiary);
    font-family: var(--uvp-font-mono, ui-monospace, monospace);
    font-size: 10px;
    background: var(--uvp-dialog-control-bg, #f8fbff);
    border-radius: 3px;
}

.dcg-params-state {
    padding: 1px 6px;
    font-size: 10px;
    border-radius: 3px;

    &.is-ready {
        color: #047857;
        background: #d1fae5;
    }

    &.is-static {
        color: var(--uvp-text-tertiary);
        background: #eef1f6;
    }
}

.dcg-params-profile {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    margin-left: auto;

    > span {
        color: var(--uvp-text-secondary);
        font-size: 11px;
    }
}

.dcg-params-summary {
    flex: none;
    display: flex;
    align-items: flex-start;
    gap: 5px;
    margin: 0;
    padding: 7px 12px;
    color: var(--uvp-text-tertiary);
    font-size: 11px;
    line-height: 1.5;
    background: var(--uvp-dialog-control-bg, #f8fbff);
    border-bottom: 1px solid var(--uvp-dialog-border, #e6edf7);
}

.dcg-params-body {
    flex: 1 1 auto;
    min-height: 0;
    padding: 12px 14px 16px;
    overflow-y: auto;
}

/* 静态组的控件没有真实读写，视觉上必须比可用的组弱一档 */
.dcg-params-body.is-static .dcg-row-control {
    opacity: 0.7;
}

/* 参数区底部汇总：内容少时也把底边压实，不留空档 */
.dcg-params-foot {
    flex: none;
    display: flex;
    align-items: center;
    gap: 6px;
    margin: 0;
    padding: 8px 14px;
    color: var(--uvp-text-tertiary);
    font-size: 11px;
    border-top: 1px solid var(--uvp-dialog-border, #e6edf7);
}

.dcg-foot-sep {
    color: var(--uvp-panel-border, #dbe4f0);
}

.dcg-params-foot .is-dirty {
    color: var(--uvp-warning, #b66b12);
}

.dcg-reconcile {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-bottom: 8px;
    padding: 6px 8px;
    color: var(--uvp-text-secondary);
    font-size: 11.5px;
    background: var(--uvp-dialog-control-bg, #f8fbff);
    border-radius: 4px;

    .dcg-reconcile-dot {
        flex: none;
        width: 6px;
        height: 6px;
        border-radius: 50%;
        background: #94a3b8;
    }

    &.is-ok {
        color: #047857;
        background: #ecfdf5;

        .dcg-reconcile-dot {
            background: #10b981;
        }
    }

    &.is-pending {
        color: var(--uvp-brand, #2563eb);
        background: var(--uvp-brand-soft, #e8f2ff);

        .dcg-reconcile-dot {
            background: var(--uvp-brand, #2563eb);
        }
    }

    &.is-warn {
        color: var(--uvp-warning, #b66b12);
        background: var(--uvp-warning-soft, #fff7ed);

        .dcg-reconcile-dot {
            background: var(--uvp-warning, #b66b12);
        }
    }

    &.is-error {
        color: var(--uvp-danger, #d14343);
        background: var(--uvp-danger-soft, #fff2f2);

        .dcg-reconcile-dot {
            background: var(--uvp-danger, #d14343);
        }
    }
}

.dcg-notice,
.dcg-error {
    margin: 0 0 8px;
    padding: 5px 8px;
    font-size: 11px;
    border-radius: 4px;
}

.dcg-notice {
    color: var(--uvp-warning, #b66b12);
    background: var(--uvp-warning-soft, #fff7ed);
}

.dcg-error {
    color: var(--uvp-danger, #d14343);
    background: var(--uvp-danger-soft, #fff2f2);
}

.dcg-empty {
    padding: 26px 0;
    text-align: center;

    p {
        margin: 0 0 4px;
        color: var(--uvp-text-secondary);
        font-size: 12px;
    }

    em {
        color: var(--uvp-text-tertiary);
        font-size: 11px;
        font-style: normal;
    }
}

.dcg-stream {
    margin-bottom: 12px;
    padding: 8px 10px 10px;
    background: var(--uvp-dialog-control-bg, #f8fbff);
    border: 1px solid var(--uvp-dialog-border, #e6edf7);
    border-radius: 6px;
}

.dcg-stream-hd {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-bottom: 6px;
    padding-bottom: 6px;
    border-bottom: 1px solid var(--uvp-dialog-border, #e6edf7);

    span {
        color: var(--uvp-text-primary);
        font-size: 12px;
        font-weight: 500;
    }

    em {
        color: var(--uvp-text-tertiary);
        font-size: 11px;
        font-style: normal;

        &::before {
            content: "· ";
        }
    }
}

.dcg-stream-dirty {
    margin-left: auto;
    padding: 1px 5px;
    color: var(--uvp-warning, #b66b12);
    font-size: 10px;
    font-style: normal;
    background: var(--uvp-warning-soft, #fff7ed);
    border-radius: 3px;
}

.dcg-row {
    display: grid;
    grid-template-columns: 76px minmax(0, 1fr) auto;
    align-items: center;
    gap: 10px;
    min-height: 30px;
}

.dcg-row-label {
    color: var(--uvp-text-secondary);
    font-size: 12px;
    text-align: right;
}

.dcg-row-control {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
}

.dcg-row-hint {
    padding: 1px 5px;
    color: var(--uvp-text-tertiary);
    font-size: 10px;
    white-space: nowrap;
    border-radius: 3px;
}

.dcg-row-hint[data-source="设备"] {
    color: #047857;
    background: #ecfdf5;
}

.dcg-row-hint[data-source="缺省"] {
    color: #64748b;
    background: #f1f5f9;
}

.dcg-row-hint[data-source="不发"] {
    color: var(--uvp-warning, #b66b12);
    background: var(--uvp-warning-soft, #fff7ed);
}

.dcg-select,
.dcg-input {
    height: 24px;
    min-width: 0;
    color: var(--uvp-text-primary);
    font-size: 12px;
    background: var(--uvp-dialog-control-bg, #fff);
    border: 1px solid var(--uvp-panel-border, #dbe4f0);
    border-radius: 4px;

    &:focus {
        border-color: var(--uvp-brand);
        outline: none;
    }

    &:disabled {
        color: var(--uvp-text-tertiary);
        cursor: not-allowed;
    }
}

.dcg-select {
    flex: 1 1 auto;
    max-width: 240px;
    padding: 0 4px;

    &.is-mini {
        flex: none;
        width: 88px;
    }
}

.dcg-input {
    flex: 1 1 auto;
    max-width: 240px;
    padding: 0 6px;

    &.is-narrow {
        flex: none;
        width: 106px;
    }
}

.dcg-segment {
    display: inline-flex;
    overflow: hidden;
    border: 1px solid var(--uvp-panel-border, #dbe4f0);
    border-radius: 4px;

    button {
        min-width: 48px;
        height: 22px;
        padding: 0 10px;
        color: var(--uvp-text-secondary);
        font-size: 11.5px;
        background: var(--uvp-dialog-control-bg, #fff);
        border: none;
        cursor: pointer;

        & + button {
            border-left: 1px solid var(--uvp-panel-border, #dbe4f0);
        }

        &.is-on {
            color: #fff;
            background: var(--uvp-brand, #2563eb);
        }
    }
}

.dcg-switch {
    position: relative;
    width: 34px;
    height: 18px;
    padding: 0;
    background: #cbd5e1;
    border: none;
    border-radius: 9px;
    cursor: pointer;
    transition: background 0.15s;

    i {
        position: absolute;
        top: 2px;
        left: 2px;
        width: 14px;
        height: 14px;
        background: #fff;
        border-radius: 50%;
        box-shadow: 0 1px 2px rgb(15 23 42 / 25%);
        transition: transform 0.15s;
    }

    &.is-on {
        background: var(--uvp-brand, #2563eb);

        i {
            transform: translateX(16px);
        }
    }

    &:disabled {
        cursor: not-allowed;
        opacity: 0.6;
    }
}

.dcg-coords {
    display: flex;
    align-items: center;
    gap: 6px;

    label {
        display: inline-flex;
        align-items: center;
        gap: 3px;

        span {
            color: var(--uvp-text-tertiary);
            font-size: 10.5px;
        }
    }
}

.dcg-input.is-coord {
    flex: none;
    width: 52px;
    text-align: right;
}

.dcg-weekdays {
    display: inline-flex;
    gap: 3px;

    button {
        width: 24px;
        height: 22px;
        padding: 0;
        color: var(--uvp-text-secondary);
        font-size: 11px;
        background: var(--uvp-dialog-control-bg, #fff);
        border: 1px solid var(--uvp-panel-border, #dbe4f0);
        border-radius: 3px;
        cursor: pointer;

        &.is-on {
            color: #fff;
            background: var(--uvp-brand, #2563eb);
            border-color: var(--uvp-brand);
        }

        &:disabled {
            cursor: not-allowed;
        }
    }
}

.dcg-static-note {
    display: flex;
    align-items: flex-start;
    gap: 5px;
    margin: 12px 0 0;
    padding: 7px 9px;
    color: var(--uvp-text-tertiary);
    font-size: 11px;
    line-height: 1.55;
    background: #f6f8fb;
    border: 1px dashed var(--uvp-panel-border, #dbe4f0);
    border-radius: 4px;
}

/* ── 底部状态条 ── */
.dcg-statusbar {
    flex: none;
    display: flex;
    align-items: center;
    gap: 14px;
    height: 26px;
    padding: 0 12px;
    color: var(--uvp-text-tertiary);
    font-size: 11px;
    background: var(--uvp-dialog-footer-bg, #fbfdff);
    border-top: 1px solid var(--uvp-dialog-border, #e6edf7);
}

.dcg-status-item {
    display: inline-flex;
    align-items: center;
    gap: 5px;

    &.is-right {
        margin-left: auto;
        white-space: nowrap;
    }
}

.dcg-status-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;

    &.is-ready {
        background: #10b981;
    }

    &.is-static {
        background: #cbd5e1;
    }
}
</style>
