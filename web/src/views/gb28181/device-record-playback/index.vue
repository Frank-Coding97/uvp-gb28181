<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import {
    ArrowLeft,
    CalendarRange,
    ChevronDown,
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
import { positionToTime } from "./timeline";
import RecordTimeline, { type TimelineLocateEvent } from "./components/RecordTimeline.vue";
import PlayWindow from "../components/PlayWindow.vue";
import { resolvePlaybackSource, type PlaybackSource } from "../playbackProtocol";
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
const downloadNotice = ref("");
const queryToken = ref(0);
const viewport = ref<HTMLElement | null>(null);
let queryAbort: AbortController | null = null;
let sessionToken = 0;
let sessionPollTimer: number | null = null;
let downloadTimer: number | null = null;
let restorePreviewTheme: (() => void) | null = null;
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
const isPlaying = computed(() => playback.value.status === "playing");
const isPlaybackLoading = computed(() => ["creating", "buffering", "stopping"].includes(playback.value.status));
const canPlay = computed(() => Boolean(selectedRecord.value) && !controlPending.value && !["creating", "buffering", "stopping"].includes(playback.value.status));
const statusText = computed(() => ({
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
} as Record<string, string>)[playback.value.status]);
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
    return records.find(record => {
        const duration = Date.parse(record.endTime || "") - Date.parse(record.startTime || "");
        return Number.isFinite(duration) && duration > 1000;
    }) || records[0];
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
    if (!form.value || !options.value) return;
    const errors = validateRecordQueryForm(form.value, options.value);
    const firstError = Object.values(errors)[0];
    if (firstError) {
        queryState.value = "error";
        queryMessage.value = firstError;
        return;
    }
    await stopPlayback(false);
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
        if (record) await playRecord(record);
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
    if (playback.value.recordKey === record.recordKey) return true;
    if (["creating", "buffering", "playing", "paused", "stopping"].includes(playback.value.status) && playback.value.recordKey !== record.recordKey) {
        if (!await stopPlayback(false)) return false;
    }
    playback.value = reducePlaybackState(playback.value, { type: "select", recordKey: record.recordKey });
    return true;
}

async function playRecord(record: RecordQueryItem) {
    if (playback.value.recordKey === record.recordKey && ["creating", "buffering", "playing"].includes(playback.value.status)) return;
    if (!await selectRecord(record)) return;
    await startPlayback();
}

function clearPlaybackTimers() {
    if (sessionPollTimer !== null) window.clearTimeout(sessionPollTimer);
    sessionPollTimer = null;
}

function queueDownload(record: RecordQueryItem | null = selectedRecord.value) {
    if (!record) return;
    if (downloadTimer !== null) window.clearTimeout(downloadTimer);
    downloadNotice.value = `已创建下载任务 · ${record.name || "未命名录像"}`;
    downloadTimer = window.setTimeout(() => {
        downloadNotice.value = "";
        downloadTimer = null;
    }, 2600);
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
        playerClock.sessionId !== playback.value.sessionId
        || playerClock.sourceTimestamp === null
        || playerClock.recordTimestamp === null
        || timestamp < playerClock.sourceTimestamp
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
            const response = await getPlaybackSession(playbackChannelId, sessionId);
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
        playbackSource.value = resolvePlaybackSource({
            defaultProtocol: session.media?.defaultProtocol,
            protocol: session.media?.protocol,
            url: session.media?.url,
            zlmWebrtc: session.media?.zlmWebrtc,
            urls: session.media?.urls
        }, window.location.protocol === "https:");
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

async function startPlayback() {
    if (!selectedRecord.value || !canPlay.value) return;
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
        const response = await createPlaybackSession(playbackChannelId, {
            recordKey: record.recordKey,
            playFrom: playback.value.currentTime || record.startTime || ""
        }, createIdempotencyKey(record));
        applySession(response.data, token);
    } catch (error) {
        if (token !== sessionToken) return;
        playback.value = { ...playback.value, status: "failed", error: playbackErrorMessage(error) };
        playbackMediaUrl.value = "";
        playbackSource.value = null;
    }
}

async function sendPlaybackAction(action: PlaybackActionRequest, syncPosition = false) {
    const sessionId = playback.value.sessionId;
    if (!sessionId || controlPending.value) return;
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

async function stopPlayback(keepSelection = true) {
    const sessionId = playback.value.sessionId;
    const selectedKey = playback.value.recordKey;
    ++sessionToken;
    clearPlaybackTimers();
    resetPlayerClock();
    playbackBuffering.value = false;
    playbackMediaUrl.value = "";
    playbackSource.value = null;
    if (sessionId) {
        playback.value = reducePlaybackState(playback.value, { type: "stopping" });
        try {
            await deletePlaybackSession(playbackChannelId, sessionId);
        } catch (error) {
            console.warn("停止设备录像回放会话失败", error);
            playback.value = { ...playback.value, status: "failed", error: playbackErrorMessage(error) };
            return false;
        }
    }
    if (keepSelection && selectedKey) {
        playback.value = reducePlaybackState(playback.value, { type: "stopped" });
    } else {
        playback.value = createPlaybackState();
    }
    return true;
}

async function setScale(scale: number) {
    if (playback.value.sessionId && ["playing", "paused"].includes(playback.value.status)) {
        await sendPlaybackAction({ action: "scale", scale });
        return;
    }
    playback.value = reducePlaybackState(playback.value, { type: "scale", scale });
}

async function handleTimelineLocate(event: TimelineLocateEvent) {
    if (!event.recordKey) return;
    const record = records.value.find(item => item.recordKey === event.recordKey);
    if (!record) return;
    if (playback.value.recordKey !== event.recordKey && !await selectRecord(record)) return;
    playback.value = { ...playback.value, currentTime: event.time };
    if (!playback.value.sessionId) {
        await startPlayback();
        return;
    }
    if (!record.startTime || !record.endTime) return;
    const duration = Math.max(0, (Date.parse(record.endTime) - Date.parse(record.startTime)) / 1000);
    const positionSeconds = Math.min(Math.max(0, duration - 0.001), Math.max(0, (Date.parse(event.time) - Date.parse(record.startTime)) / 1000));
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
    const target = registeredRoutes.find((item: { name?: string | symbol }) => item.name === "device-mgmt-list")
        ?? registeredRoutes.find((item: { name?: string | symbol }) => item.name === "device-mgmt");
    if (!target?.name) {
        router.back();
        return;
    }
    router.push({ name: target.name, query: route.query.returnKey ? { returnKey: String(route.query.returnKey) } : undefined });
}

function applyDemoPreviewTheme() {
    if (route.name !== "device-record-query-demo") return;
    const previewTheme = typeof route.query.previewTheme === "string" ? route.query.previewTheme : "";
    if (!["light", "dark", "frostedBlack"].includes(previewTheme)) return;
    const oldTheme = document.body.getAttribute("arco-theme");
    const oldDarkStyle = document.body.getAttribute("uvp-dark-style");
    if (previewTheme === "light") {
        document.body.removeAttribute("arco-theme");
        document.body.removeAttribute("uvp-dark-style");
    } else {
        document.body.setAttribute("arco-theme", "dark");
        if (previewTheme === "frostedBlack") document.body.setAttribute("uvp-dark-style", "frostedBlack");
        else document.body.removeAttribute("uvp-dark-style");
    }
    restorePreviewTheme = () => {
        if (oldTheme === null) document.body.removeAttribute("arco-theme");
        else document.body.setAttribute("arco-theme", oldTheme);
        if (oldDarkStyle === null) document.body.removeAttribute("uvp-dark-style");
        else document.body.setAttribute("uvp-dark-style", oldDarkStyle);
    };
}

onMounted(() => {
    applyDemoPreviewTheme();
    loadOptions();
});
onUnmounted(() => {
    queryAbort?.abort();
    const sessionId = playback.value.sessionId;
    ++sessionToken;
    clearPlaybackTimers();
    if (sessionId) void deletePlaybackSession(playbackChannelId, sessionId).catch(() => undefined);
    if (downloadTimer !== null) window.clearTimeout(downloadTimer);
    restorePreviewTheme?.();
});
</script>

<template>
    <div class="snow-fill record-playback-page">
        <div class="snow-fill-inner uvp-page-shell-flat playback-workspace">
            <header class="query-bar" data-testid="playback-query-bar">
                <button class="icon-command back-command" type="button" aria-label="返回设备管理" title="返回设备管理" @click="goBack">
                    <ArrowLeft :size="18" />
                </button>
                <div class="channel-context">
                    <span class="channel-icon"><Video :size="17" /></span>
                    <div>
                        <strong>{{ options?.channel.name || `通道 ${channelId}` }}</strong>
                        <span>{{ options?.device.name || "正在加载设备" }} · {{ options?.channel.code || "--" }}</span>
                    </div>
                    <i :class="['online-indicator', { offline: options && !options.device.online }]">{{ options?.device.online === false ? "离线" : "在线" }}</i>
                </div>

                <div v-if="form" class="query-fields">
                    <label class="query-field time-field">
                        <span><CalendarRange :size="13" />开始时间</span>
                        <input v-model="form.startTime" type="datetime-local" step="1" :disabled="queryState === 'querying'" />
                    </label>
                    <span class="range-separator">至</span>
                    <label class="query-field time-field">
                        <span><CalendarRange :size="13" />结束时间</span>
                        <input v-model="form.endTime" type="datetime-local" step="1" :disabled="queryState === 'querying'" />
                    </label>
                    <label class="query-field type-field">
                        <span>录像类型</span>
                        <span class="select-wrap">
                            <select v-model="form.type" :disabled="queryState === 'querying'">
                                <option v-for="item in recordQueryRequestTypes" :key="item.value" :value="item.value">{{ item.label }}</option>
                            </select>
                            <ChevronDown :size="14" />
                        </span>
                    </label>
                    <button data-testid="record-query-submit" class="query-submit" type="button" :disabled="queryState === 'querying'" @click="runQuery">
                        <LoaderCircle v-if="queryState === 'querying'" :size="15" class="spin" />
                        <Search v-else :size="15" />
                        查询录像
                    </button>
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
                        <div v-else class="playback-idle-cover" data-testid="playback-idle-cover" role="img" :aria-label="isPlaybackLoading ? statusText : '录像未播放'">
                            <div v-if="isPlaybackLoading" class="playback-loading" data-testid="playback-loading">
                                <LoaderCircle :size="42" class="spin" aria-hidden="true" />
                                <span>{{ statusText }}</span>
                            </div>
                            <CircleAlert v-else-if="playback.status === 'failed'" :size="48" aria-hidden="true" />
                            <Videotape v-else :size="64" :stroke-width="1.35" aria-hidden="true" />
                        </div>
                        <span class="playback-status-sr" data-testid="playback-status" aria-live="polite">{{ statusText }}</span>
                        <div v-if="downloadNotice" class="download-notice" data-testid="download-notice"><Download :size="14" />{{ downloadNotice }}</div>
                    </div>

                    <div class="playback-controls" data-testid="playback-controls">
                        <button
                            data-testid="playback-primary-action"
                            class="control-primary"
                            type="button"
                            :disabled="!canPlay"
                            :aria-label="isPlaying ? '暂停' : '播放'"
                            @click="isPlaying ? pausePlayback() : startPlayback()"
                        >
                            <Pause v-if="isPlaying" :size="17" fill="currentColor" />
                            <Play v-else :size="17" fill="currentColor" />
                        </button>
                        <button class="control-icon" type="button" aria-label="停止" title="停止" :disabled="!selectedRecord" @click="stopPlayback()"><Square :size="15" fill="currentColor" /></button>
                        <div class="control-spacer"></div>
                        <label class="scale-select" title="播放倍速">
                            <select :value="playback.scale" @change="setScale(Number(($event.target as HTMLSelectElement).value))">
                                <option v-for="scale in [0.25, 0.5, 1, 2, 4]" :key="scale" :value="scale">{{ scale }}x</option>
                            </select>
                            <ChevronDown :size="13" />
                        </label>
                        <button data-testid="playback-download" class="control-icon" type="button" aria-label="下载当前录像" title="下载当前录像" :disabled="!selectedRecord" @click="queueDownload()"><Download :size="16" /></button>
                        <button class="control-icon" type="button" aria-label="全屏" title="全屏" @click="toggleFullscreen"><Fullscreen :size="16" /></button>
                    </div>
                </section>

                <aside class="segment-panel" data-testid="record-segment-list">
                    <div class="segment-header">
                        <div><strong>录像段</strong><span>{{ querySummary }}</span></div>
                        <button type="button" aria-label="重新查询" title="重新查询" :disabled="queryState === 'querying'" @click="runQuery"><RotateCcw :size="14" /></button>
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
                                type="button"
                                @click="playRecord(record)"
                            >
                                <span class="segment-content">
                                    <strong>{{ record.name || `录像段 ${index + 1}` }}</strong>
                                    <span class="segment-time"><Clock3 :size="12" />{{ shortTime(record.startTime) }} - {{ shortTime(record.endTime) }}</span>
                                    <span class="segment-meta">
                                        <i class="segment-type"><span class="segment-marker" :class="record.type || 'unknown'"></span>{{ recordQueryTypeText(record.type) }}</i>
                                        <i>{{ durationText(record) }}</i>
                                        <i v-if="record.fileSize != null && record.fileSize >= 0" :data-testid="`record-segment-size-${index}`">{{ fileSizeText(record.fileSize) }}</i>
                                    </span>
                                </span>
                            </button>
                            <button
                                :data-testid="`record-segment-download-${index}`"
                                class="segment-download"
                                type="button"
                                :aria-label="`下载 ${record.name || `录像段 ${index + 1}`}`"
                                :title="`下载 ${record.name || `录像段 ${index + 1}`}`"
                                @click="queueDownload(record)"
                            ><Download :size="15" /></button>
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
        </div>
    </div>
</template>

<style scoped>
.record-playback-page { height: 100%; min-height: 0; color: var(--uvp-text-primary); overflow: hidden; }
.playback-workspace { display: flex; flex-direction: column; height: 100%; min-height: 0; padding: 0; overflow: hidden; }
.query-bar { display: flex; flex: none; align-items: center; gap: 14px; min-height: 72px; margin: 0 8px 10px; padding: 10px 16px; background: var(--uvp-search-panel-bg); border: 1px solid var(--uvp-list-panel-border); border-radius: var(--uvp-panel-radius); box-shadow: var(--uvp-search-panel-shadow); }
.icon-command, .control-icon, .segment-header button { display: inline-flex; align-items: center; justify-content: center; width: 32px; height: 32px; padding: 0; color: var(--uvp-text-secondary); background: var(--uvp-search-secondary-btn-bg); border: 1px solid var(--uvp-search-secondary-btn-border); border-radius: 6px; cursor: pointer; transition: background-color 180ms ease, border-color 180ms ease, color 180ms ease; }
.icon-command:hover, .control-icon:hover, .segment-header button:hover { color: var(--uvp-brand); background: var(--uvp-search-secondary-btn-hover-bg); border-color: var(--uvp-brand); }
.channel-context { display: flex; align-items: center; gap: 9px; min-width: 260px; }
.channel-icon { display: inline-flex; align-items: center; justify-content: center; width: 34px; height: 34px; color: var(--uvp-brand); background: var(--uvp-brand-soft); border-radius: 6px; }
.channel-context div { display: flex; flex-direction: column; min-width: 0; }
.channel-context strong { font-size: 14px; line-height: 20px; }
.channel-context span:not(.channel-icon) { color: var(--uvp-text-tertiary); font-size: 11px; line-height: 17px; white-space: nowrap; }
.online-indicator { margin-left: auto; padding: 2px 7px; color: #168456; font-size: 11px; font-style: normal; background: rgb(22 132 86 / 10%); border-radius: 4px; }
.online-indicator.offline { color: var(--uvp-danger); background: var(--uvp-danger-soft); }
.query-fields { display: flex; align-items: flex-end; gap: 8px; min-width: 0; margin-left: auto; }
.query-field { display: flex; flex-direction: column; gap: 4px; color: var(--uvp-text-tertiary); font-size: 11px; }
.query-field > span:first-child { display: flex; align-items: center; gap: 4px; }
.query-field input, .query-field select { box-sizing: border-box; height: 32px; color: var(--uvp-text-primary); font: inherit; font-size: 12px; background: var(--uvp-search-control-bg); border: 1px solid var(--uvp-search-secondary-btn-border); border-radius: 6px; outline: none; }
.query-field input { width: 177px; padding: 0 8px; }
.query-field select { width: 108px; padding: 0 28px 0 9px; appearance: none; }
.query-field input:focus, .query-field select:focus { border-color: var(--uvp-brand); box-shadow: var(--uvp-search-control-focus-shadow); }
.select-wrap { position: relative; display: block; }
.select-wrap svg { position: absolute; top: 9px; right: 8px; pointer-events: none; }
.range-separator { align-self: flex-end; height: 32px; color: var(--uvp-text-tertiary); font-size: 11px; line-height: 32px; }
.query-submit { display: inline-flex; align-items: center; justify-content: center; gap: 6px; height: 32px; padding: 0 14px; color: #fff; font-size: 12px; font-weight: 600; background: var(--uvp-brand); border: 0; border-radius: 6px; cursor: pointer; transition: background-color 180ms ease; }
.query-submit:hover { background: var(--uvp-brand-strong); }
button:disabled, input:disabled, select:disabled { cursor: not-allowed; opacity: .55; }
.playback-main { display: grid; grid-template-columns: minmax(0, 1fr) 312px; flex: 1; min-height: 0; margin: 0 8px; padding: 12px; background: var(--uvp-shell-muted); border: 1px solid var(--uvp-panel-border); border-radius: var(--uvp-panel-radius); overflow: hidden; }
.player-column { display: flex; flex-direction: column; min-width: 0; min-height: 0; }
.playback-viewport { position: relative; flex: 1; min-height: clamp(220px, 30vh, 340px); overflow: hidden; color: #e8eef5; background: #000; }
.playback-idle-cover { position: absolute; inset: 0; display: grid; place-items: center; color: #89939d; background: #000; }
.playback-loading { display: flex; align-items: center; flex-direction: column; gap: 12px; color: #c9d1d9; font-size: 12px; }
.playback-buffering { position: absolute; z-index: 4; top: 12px; left: 50%; display: flex; align-items: center; gap: 7px; padding: 6px 9px; color: #fff; font-size: 11px; background: rgb(10 15 20 / 76%); border: 1px solid rgb(255 255 255 / 18%); border-radius: 5px; transform: translateX(-50%); }
.playback-idle-cover .lucide-circle-alert { color: #e05d5d; }
.playback-status-sr { position: absolute; width: 1px; height: 1px; padding: 0; overflow: hidden; white-space: nowrap; border: 0; clip: rect(0, 0, 0, 0); clip-path: inset(50%); }
.video-overlay { position: absolute; z-index: 2; right: 14px; left: 14px; display: flex; justify-content: space-between; gap: 12px; color: rgb(239 246 255 / 86%); font: 11px ui-monospace, SFMono-Regular, Menlo, monospace; text-shadow: 0 1px 3px #000; }
.top-overlay { top: 12px; }
.download-notice { position: absolute; z-index: 5; top: 42px; right: 14px; display: flex; align-items: center; gap: 6px; max-width: calc(100% - 28px); padding: 7px 10px; overflow: hidden; color: #fff; font-size: 11px; text-overflow: ellipsis; white-space: nowrap; background: rgb(10 15 20 / 82%); border: 1px solid rgb(255 255 255 / 18%); border-radius: 4px; box-shadow: 0 4px 14px rgb(0 0 0 / 24%); }
.playback-controls { display: flex; align-items: center; gap: 7px; height: 48px; padding: 0 10px; color: var(--uvp-text-secondary); background: var(--uvp-panel-bg); border: 1px solid var(--uvp-panel-border); border-top: 0; }
.control-primary { display: inline-flex; align-items: center; justify-content: center; width: 32px; height: 32px; padding: 0; color: #fff; background: var(--uvp-brand); border: 0; border-radius: 50%; cursor: pointer; }
.control-primary:hover { background: var(--uvp-brand-strong); }
.control-spacer { flex: 1; }
.scale-select { position: relative; }.scale-select select { width: 62px; height: 30px; padding: 0 23px 0 8px; color: var(--uvp-text-secondary); font-size: 11px; background: var(--uvp-search-control-bg); border: 1px solid var(--uvp-search-secondary-btn-border); border-radius: 6px; appearance: none; }.scale-select svg { position: absolute; top: 9px; right: 6px; pointer-events: none; }
.segment-panel { display: flex; flex-direction: column; min-height: 0; margin-left: 10px; background: var(--uvp-list-panel-bg); border: 1px solid var(--uvp-list-panel-border); }
.segment-header { display: flex; align-items: center; justify-content: space-between; min-height: 53px; padding: 8px 10px 8px 12px; background: var(--uvp-list-toolbar-bg); border-bottom: 1px solid var(--uvp-list-panel-border); }
.segment-header div { display: flex; flex-direction: column; gap: 2px; min-width: 0; }.segment-header strong { font-size: 13px; }.segment-header span { overflow: hidden; color: var(--uvp-text-tertiary); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.segment-header button { width: 28px; height: 28px; }
.segment-list { flex: 1; min-height: 0; padding: 4px; overflow-y: auto; }
.segment-row { position: relative; }
.segment-item { position: relative; display: flex; align-items: center; width: 100%; min-height: 74px; padding: 9px 40px 9px 11px; color: var(--uvp-text-primary); text-align: left; background: var(--uvp-table-row-bg); border: 1px solid transparent; border-bottom-color: var(--uvp-list-panel-border); border-radius: 5px; cursor: pointer; transition: background-color 170ms ease, border-color 170ms ease, box-shadow 170ms ease; }
.segment-item:hover { background: var(--uvp-table-row-hover-bg); }.segment-item:focus-visible { border-color: var(--uvp-brand); outline: 2px solid var(--uvp-brand); outline-offset: -2px; }.segment-item.selected { z-index: 1; background: var(--uvp-table-row-checked-bg); border-color: var(--uvp-brand); box-shadow: 0 2px 8px rgb(29 100 235 / 12%); }
.segment-marker { flex: none; width: 7px; height: 7px; background: #708090; border-radius: 50%; }.segment-marker.time { background: var(--uvp-brand); }.segment-marker.alarm { background: var(--uvp-danger); }.segment-marker.manual { background: var(--uvp-warning); }
.segment-content { display: flex; flex: 1; flex-direction: column; gap: 4px; min-width: 0; }.segment-content strong { overflow: hidden; font-size: 12px; line-height: 17px; text-overflow: ellipsis; white-space: nowrap; }.segment-time { display: flex; align-items: center; gap: 4px; color: var(--uvp-text-secondary); font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 10px; }.segment-meta { display: flex; gap: 10px; }.segment-meta i { color: var(--uvp-text-tertiary); font-size: 10px; font-style: normal; }.segment-type { display: inline-flex; align-items: center; gap: 5px; }
.segment-download { position: absolute; z-index: 2; top: 23px; right: 8px; display: inline-flex; align-items: center; justify-content: center; width: 28px; height: 28px; padding: 0; color: var(--uvp-text-tertiary); background: transparent; border: 1px solid transparent; border-radius: 5px; cursor: pointer; transition: color 160ms ease, background-color 160ms ease, border-color 160ms ease; }.segment-download:hover, .segment-download:focus-visible, .segment-row.selected .segment-download { color: var(--uvp-brand); background: var(--uvp-search-secondary-btn-bg); border-color: var(--uvp-search-secondary-btn-border); outline: none; }
.segment-empty { display: flex; flex: 1; flex-direction: column; align-items: center; justify-content: center; gap: 8px; padding: 24px; color: var(--uvp-text-tertiary); text-align: center; }.segment-empty strong { font-size: 12px; }.segment-empty span { font-size: 11px; }
.segment-footer { display: flex; justify-content: space-between; padding: 8px 11px; color: var(--uvp-text-tertiary); font-size: 10px; background: var(--uvp-list-toolbar-bg); border-top: 1px solid var(--uvp-list-panel-border); }
.timeline-panel { flex: none; margin: 10px 8px 12px; border-radius: var(--uvp-panel-radius); overflow: hidden; }
.spin { animation: spin 900ms linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
@media (max-width: 1180px) {
    .query-bar { flex-wrap: wrap; }.channel-context { flex: 1; }.query-fields { flex-basis: 100%; justify-content: flex-end; }.playback-main { grid-template-columns: minmax(0, 1fr) 280px; }
}
@media (max-width: 768px) {
    .record-playback-page { height: auto; min-height: 100%; overflow: auto; }.playback-workspace { height: auto; min-height: 100%; border-radius: 0; }.query-bar { align-items: flex-start; margin: 0 8px 8px; padding: 10px 12px; }.channel-context { min-width: 0; }.online-indicator { display: none; }.query-fields { display: grid; grid-template-columns: 1fr 1fr; width: 100%; }.query-field input { width: 100%; }.range-separator { display: none; }.type-field, .query-submit { width: 100%; }.type-field select { width: 100%; }.playback-main { display: flex; flex: none; flex-direction: column; margin: 0 8px; padding: 8px; }.playback-viewport { flex: none; min-height: 0; aspect-ratio: 16 / 9; }.segment-panel { height: 280px; margin: 8px 0 0; }.timeline-panel { margin: 8px; }
}
@media (max-width: 430px) {
    .back-command { width: 36px; height: 36px; }.channel-context span:not(.channel-icon) { max-width: 220px; overflow: hidden; text-overflow: ellipsis; }.query-fields { grid-template-columns: 1fr; }.query-field input, .query-field select, .query-submit { height: 44px; }.playback-controls { height: 52px; }.control-icon, .control-primary { width: 36px; height: 36px; }.control-spacer { display: none; }.scale-select { display: none; }.segment-panel { height: 306px; }
}
@media (prefers-reduced-motion: reduce) { *, *::before, *::after { scroll-behavior: auto !important; transition-duration: .01ms !important; animation-duration: .01ms !important; animation-iteration-count: 1 !important; } }
</style>
