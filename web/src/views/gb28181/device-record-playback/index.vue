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
    Video
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

interface RecordEntry extends RecordQueryItem {
    recordKey: string;
}

const route = useRoute();
const router = useRouter();
const channelId = computed(() => Number(route.params.channelId || 31));
const options = ref<RecordQueryOptions | null>(null);
const form = ref<RecordQueryForm | null>(null);
const queryState = ref<RecordQueryUiState>("idle");
const queryMessage = ref("");
const result = ref<RecordQueryResult | null>(null);
const records = ref<RecordEntry[]>([]);
const playback = ref(createPlaybackState());
const downloadNotice = ref("");
const queryToken = ref(0);
const viewport = ref<HTMLElement | null>(null);
let queryAbort: AbortController | null = null;
let playbackTimer: number | null = null;
let progressTimer: number | null = null;
let downloadTimer: number | null = null;
let restorePreviewTheme: (() => void) | null = null;

const selectedRecord = computed(() => records.value.find(item => item.recordKey === playback.value.recordKey) || null);
const queryRange = computed(() => ({
    startTime: form.value?.startTime || options.value?.serverNow || "",
    endTime: form.value?.endTime || options.value?.serverNow || ""
}));
const isPlaying = computed(() => playback.value.status === "playing");
const canPlay = computed(() => Boolean(selectedRecord.value) && !["creating", "buffering", "stopping"].includes(playback.value.status));
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

function recordKey(item: RecordQueryItem, index: number) {
    return [item.filePath, item.startTime, item.endTime, index].join("|");
}

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

async function loadOptions() {
    try {
        const response = await getRecordQueryOptions(channelId.value);
        options.value = response.data;
        form.value = createDefaultRecordQueryForm(response.data);
        if (route.name === "device-record-query-demo") await nextTick(runQuery);
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
    stopPlayback(false);
    queryAbort?.abort();
    const token = ++queryToken.value;
    queryAbort = new AbortController();
    queryState.value = "querying";
    queryMessage.value = "";
    try {
        const response = await queryDeviceRecords(channelId.value, serializeRecordQueryForm(form.value), queryAbort.signal);
        if (token !== queryToken.value) return;
        result.value = response.data;
        records.value = response.data.list.map((item, index) => ({ ...item, recordKey: recordKey(item, index) }));
        queryState.value = response.data.status;
        if (records.value.length) selectRecord(records.value[0]);
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

function selectRecord(record: RecordEntry) {
    if (["creating", "buffering", "playing", "paused", "stopping"].includes(playback.value.status) && playback.value.recordKey !== record.recordKey) {
        stopPlayback(false);
    }
    playback.value = reducePlaybackState(playback.value, { type: "select", recordKey: record.recordKey });
}

function clearPlaybackTimers() {
    if (playbackTimer !== null) window.clearTimeout(playbackTimer);
    if (progressTimer !== null) window.clearInterval(progressTimer);
    playbackTimer = null;
    progressTimer = null;
}

function queueDownload(record: RecordEntry | null = selectedRecord.value) {
    if (!record) return;
    if (downloadTimer !== null) window.clearTimeout(downloadTimer);
    downloadNotice.value = `已创建下载任务 · ${record.name || "未命名录像"}`;
    downloadTimer = window.setTimeout(() => {
        downloadNotice.value = "";
        downloadTimer = null;
    }, 2600);
}

function startProgress() {
    if (progressTimer !== null) window.clearInterval(progressTimer);
    progressTimer = window.setInterval(() => {
        if (playback.value.status !== "playing" || !selectedRecord.value) return;
        const current = Date.parse(playback.value.currentTime || selectedRecord.value.startTime || "");
        const end = Date.parse(selectedRecord.value.endTime || "");
        if (!Number.isFinite(current) || !Number.isFinite(end) || current >= end) {
            playback.value = { ...playback.value, status: "ended", currentTime: selectedRecord.value.endTime };
            clearPlaybackTimers();
            return;
        }
        const start = Date.parse(selectedRecord.value.startTime || "");
        const duration = end - start;
        const next = current + 1000 * playback.value.scale;
        playback.value = {
            ...playback.value,
            currentTime: positionToTime(
                { startTime: selectedRecord.value.startTime!, endTime: selectedRecord.value.endTime! },
                next - start,
                duration
            )
        };
    }, 1000);
}

function startPlayback() {
    if (!selectedRecord.value || !canPlay.value) return;
    if (playback.value.status === "paused") {
        playback.value = reducePlaybackState(playback.value, { type: "resume" });
        startProgress();
        return;
    }
    clearPlaybackTimers();
    playback.value = reducePlaybackState(playback.value, { type: "creating" });
    const sessionId = `mock-${Date.now()}`;
    playbackTimer = window.setTimeout(() => {
        playback.value = reducePlaybackState(playback.value, { type: "session", sessionId, status: "buffering", currentTime: selectedRecord.value?.startTime || undefined });
        playbackTimer = window.setTimeout(() => {
            playback.value = reducePlaybackState(playback.value, { type: "session", sessionId, status: "playing", currentTime: selectedRecord.value?.startTime || undefined });
            startProgress();
        }, 320);
    }, 220);
}

function pausePlayback() {
    playback.value = reducePlaybackState(playback.value, { type: "pause" });
    if (progressTimer !== null) window.clearInterval(progressTimer);
    progressTimer = null;
}

function stopPlayback(keepSelection = true) {
    clearPlaybackTimers();
    if (!playback.value.recordKey) return;
    playback.value = reducePlaybackState(playback.value, { type: "stopped" });
    if (!keepSelection) playback.value = createPlaybackState();
}

function setScale(scale: number) {
    playback.value = reducePlaybackState(playback.value, { type: "scale", scale });
}

function handleTimelineSelect(recordKey: string) {
    const record = records.value.find(item => item.recordKey === recordKey);
    if (record && playback.value.recordKey !== recordKey) selectRecord(record);
}

function handleTimelineLocate(event: TimelineLocateEvent) {
    if (!event.recordKey) return;
    const record = records.value.find(item => item.recordKey === event.recordKey);
    if (!record) return;
    if (playback.value.recordKey !== event.recordKey) selectRecord(record);
    playback.value = { ...playback.value, currentTime: event.time };
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
    clearPlaybackTimers();
    if (downloadTimer !== null) window.clearTimeout(downloadTimer);
    restorePreviewTheme?.();
});
</script>

<template>
    <div class="snow-fill record-playback-page">
        <div class="snow-fill-inner playback-workspace">
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
                        <div class="camera-scene" :class="{ active: isPlaying }" aria-hidden="true">
                            <div class="scene-sky"></div>
                            <div class="scene-building"><span></span><span></span><span></span><span></span></div>
                            <div class="scene-road"><i></i><i></i><i></i></div>
                            <div class="scene-gate"><b></b><b></b></div>
                        </div>
                        <div class="video-overlay top-overlay">
                            <span>CH-01 {{ options?.channel.name || "东门出入口" }}</span>
                            <span>{{ localDateTime(playback.currentTime || selectedRecord?.startTime || options?.serverNow) }}</span>
                        </div>
                        <div class="video-overlay bottom-overlay">
                            <span class="stream-badge">设备录像 MOCK · <b data-testid="playback-status">{{ statusText }}</b></span>
                            <span>{{ options?.device.code }}</span>
                        </div>
                        <div v-if="!isPlaying" class="viewport-state">
                            <LoaderCircle v-if="['creating', 'buffering'].includes(playback.status)" :size="34" class="spin" />
                            <CircleAlert v-else-if="playback.status === 'failed'" :size="34" />
                            <Play v-else :size="38" />
                            <strong>{{ statusText }}</strong>
                            <span v-if="selectedRecord">{{ selectedRecord.name || '未命名录像' }} · {{ shortTime(selectedRecord.startTime) }} - {{ shortTime(selectedRecord.endTime) }}</span>
                            <span v-else>从右侧录像段或下方时间轴选择一段录像</span>
                        </div>
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
                        <span class="control-time" data-testid="playback-time">{{ shortTime(playback.currentTime || selectedRecord?.startTime) }} <i>/</i> {{ shortTime(selectedRecord?.endTime) }}</span>
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
                                type="button"
                                @click="selectRecord(record)"
                                @dblclick="startPlayback"
                            >
                                <span class="segment-marker" :class="record.type || 'unknown'"></span>
                                <span class="segment-content">
                                    <strong>{{ record.name || `录像段 ${index + 1}` }}</strong>
                                    <span class="segment-time"><Clock3 :size="12" />{{ shortTime(record.startTime) }} - {{ shortTime(record.endTime) }}</span>
                                    <span class="segment-meta"><i>{{ recordQueryTypeText(record.type) }}</i><i>{{ durationText(record) }}</i></span>
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
                @select="handleTimelineSelect"
                @locate="handleTimelineLocate"
            />
        </div>
    </div>
</template>

<style scoped>
.record-playback-page { min-height: 100vh; color: var(--uvp-text-primary); overflow: auto; }
.playback-workspace { display: flex; flex-direction: column; min-height: calc(100vh - 16px); padding: 0; overflow: hidden; }
.query-bar { display: flex; align-items: center; gap: 14px; min-height: 72px; padding: 10px 16px; background: var(--uvp-workspace-bg); border-bottom: 1px solid var(--uvp-workspace-border); }
.icon-command, .control-icon, .segment-header button { display: inline-flex; align-items: center; justify-content: center; width: 32px; height: 32px; padding: 0; color: var(--uvp-text-secondary); background: var(--uvp-search-secondary-btn-bg); border: 1px solid var(--uvp-search-secondary-btn-border); border-radius: 6px; cursor: pointer; transition: background-color 180ms ease, border-color 180ms ease, color 180ms ease; }
.icon-command:hover, .control-icon:hover, .segment-header button:hover { color: var(--uvp-brand); background: var(--uvp-search-secondary-btn-hover-bg); border-color: var(--uvp-brand); }
.channel-context { display: flex; align-items: center; gap: 9px; min-width: 260px; padding-right: 14px; border-right: 1px solid var(--uvp-panel-border); }
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
.playback-main { display: grid; grid-template-columns: minmax(0, 1fr) 312px; flex: 1; min-height: 0; padding: 12px 12px 0; background: var(--uvp-shell-muted); }
.player-column { display: flex; flex-direction: column; min-width: 0; min-height: 0; }
.playback-viewport { position: relative; flex: 1; min-height: 340px; overflow: hidden; color: #e8eef5; background: #090d12; }
.camera-scene { position: absolute; inset: 0; overflow: hidden; background: #243542; filter: saturate(.62) brightness(.68); }
.camera-scene::after { position: absolute; inset: 0; background-image: linear-gradient(rgb(255 255 255 / 2%) 1px, transparent 1px), linear-gradient(90deg, rgb(255 255 255 / 2%) 1px, transparent 1px); background-size: 4px 4px; content: ""; }
.camera-scene.active { filter: saturate(.82) brightness(.82); }
.scene-sky { position: absolute; inset: 0 0 48%; background: #647680; }
.scene-building { position: absolute; top: 23%; left: 8%; width: 54%; height: 29%; background: #929995; border-top: 5px solid #5b6667; box-shadow: 0 18px 34px rgb(0 0 0 / 34%); transform: perspective(420px) rotateY(5deg); }
.scene-building::before { position: absolute; top: -19px; left: 4%; width: 74%; height: 16px; background: #596568; content: ""; }
.scene-building span { display: inline-block; width: 13%; height: 44%; margin: 9% 4% 0; background: #344751; border: 2px solid #bac6c7; }
.scene-road { position: absolute; right: -7%; bottom: -15%; left: -7%; height: 63%; background: #364047; transform: perspective(410px) rotateX(54deg); transform-origin: bottom; }
.scene-road::before { position: absolute; top: 0; bottom: 0; left: 54%; width: 4px; background: repeating-linear-gradient(to bottom, #d9d4b9 0 28px, transparent 28px 54px); content: ""; }
.scene-road i { position: absolute; bottom: 31%; width: 82px; height: 38px; background: #59636b; border-radius: 5px 9px 3px 3px; box-shadow: 0 8px 14px rgb(0 0 0 / 32%); }
.scene-road i::before { position: absolute; top: -13px; left: 14px; width: 42px; height: 15px; background: #74828a; border-radius: 5px 6px 0 0; content: ""; }
.scene-road i:nth-child(1) { right: 14%; }.scene-road i:nth-child(2) { right: 41%; bottom: 63%; transform: scale(.72); }.scene-road i:nth-child(3) { right: 68%; bottom: 17%; transform: scale(.9); }
.scene-gate { position: absolute; right: 10%; bottom: 28%; width: 24%; height: 38%; border-right: 5px solid #bbc5c7; border-left: 5px solid #bbc5c7; }
.scene-gate::before { position: absolute; top: 0; left: -8%; width: 116%; height: 8px; background: #c7d0d1; content: ""; }
.scene-gate b { position: absolute; top: 7px; width: 46%; height: 60%; background: rgb(26 38 45 / 76%); border: 2px solid #7d8d91; }.scene-gate b:last-child { right: 0; }
.video-overlay { position: absolute; z-index: 2; right: 14px; left: 14px; display: flex; justify-content: space-between; gap: 12px; color: rgb(239 246 255 / 86%); font: 11px ui-monospace, SFMono-Regular, Menlo, monospace; text-shadow: 0 1px 3px #000; }
.top-overlay { top: 12px; }.bottom-overlay { bottom: 12px; align-items: flex-end; }
.stream-badge { padding: 3px 6px; color: #fff; background: rgb(10 15 20 / 64%); border: 1px solid rgb(255 255 255 / 20%); border-radius: 3px; }
.stream-badge b { font-weight: 500; }
.viewport-state { position: absolute; z-index: 3; top: 50%; left: 50%; display: flex; flex-direction: column; align-items: center; gap: 8px; min-width: 240px; padding: 18px 24px; background: rgb(5 9 13 / 72%); border: 1px solid rgb(255 255 255 / 12%); border-radius: 6px; backdrop-filter: blur(5px); transform: translate(-50%, -50%); }
.viewport-state strong { font-size: 14px; }.viewport-state span { color: #aab8c7; font-size: 11px; }
.download-notice { position: absolute; z-index: 5; top: 42px; right: 14px; display: flex; align-items: center; gap: 6px; max-width: calc(100% - 28px); padding: 7px 10px; overflow: hidden; color: #fff; font-size: 11px; text-overflow: ellipsis; white-space: nowrap; background: rgb(10 15 20 / 82%); border: 1px solid rgb(255 255 255 / 18%); border-radius: 4px; box-shadow: 0 4px 14px rgb(0 0 0 / 24%); }
.playback-controls { display: flex; align-items: center; gap: 7px; height: 48px; padding: 0 10px; color: var(--uvp-text-secondary); background: var(--uvp-panel-bg); border: 1px solid var(--uvp-panel-border); border-top: 0; }
.control-primary { display: inline-flex; align-items: center; justify-content: center; width: 32px; height: 32px; padding: 0; color: #fff; background: var(--uvp-brand); border: 0; border-radius: 50%; cursor: pointer; }
.control-primary:hover { background: var(--uvp-brand-strong); }
.control-time { min-width: 142px; margin-left: 4px; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 11px; }.control-time i { color: var(--uvp-text-tertiary); font-style: normal; }
.control-spacer { flex: 1; }
.scale-select { position: relative; }.scale-select select { width: 62px; height: 30px; padding: 0 23px 0 8px; color: var(--uvp-text-secondary); font-size: 11px; background: var(--uvp-search-control-bg); border: 1px solid var(--uvp-search-secondary-btn-border); border-radius: 6px; appearance: none; }.scale-select svg { position: absolute; top: 9px; right: 6px; pointer-events: none; }
.segment-panel { display: flex; flex-direction: column; min-height: 0; margin-left: 10px; background: var(--uvp-list-panel-bg); border: 1px solid var(--uvp-list-panel-border); }
.segment-header { display: flex; align-items: center; justify-content: space-between; min-height: 53px; padding: 8px 10px 8px 12px; background: var(--uvp-list-toolbar-bg); border-bottom: 1px solid var(--uvp-list-panel-border); }
.segment-header div { display: flex; flex-direction: column; gap: 2px; min-width: 0; }.segment-header strong { font-size: 13px; }.segment-header span { overflow: hidden; color: var(--uvp-text-tertiary); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.segment-header button { width: 28px; height: 28px; }
.segment-list { flex: 1; min-height: 0; overflow-y: auto; }
.segment-row { position: relative; }
.segment-item { position: relative; display: flex; align-items: center; gap: 9px; width: 100%; min-height: 74px; padding: 9px 44px 9px 11px; color: var(--uvp-text-primary); text-align: left; background: var(--uvp-table-row-bg); border: 0; border-bottom: 1px solid var(--uvp-list-panel-border); cursor: pointer; transition: background-color 170ms ease; }
.segment-item:hover { background: var(--uvp-table-row-hover-bg); }.segment-item.selected { background: var(--uvp-table-row-checked-bg); box-shadow: inset 3px 0 0 var(--uvp-brand); }
.segment-marker { width: 3px; height: 42px; background: #708090; border-radius: 3px; }.segment-marker.time { background: var(--uvp-brand); }.segment-marker.alarm { background: var(--uvp-danger); }.segment-marker.manual { background: var(--uvp-warning); }
.segment-content { display: flex; flex: 1; flex-direction: column; gap: 4px; min-width: 0; }.segment-content strong { overflow: hidden; font-size: 12px; line-height: 17px; text-overflow: ellipsis; white-space: nowrap; }.segment-time { display: flex; align-items: center; gap: 4px; color: var(--uvp-text-secondary); font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 10px; }.segment-meta { display: flex; gap: 10px; }.segment-meta i { color: var(--uvp-text-tertiary); font-size: 10px; font-style: normal; }
.segment-download { position: absolute; z-index: 2; top: 23px; right: 8px; display: inline-flex; align-items: center; justify-content: center; width: 28px; height: 28px; padding: 0; color: var(--uvp-text-tertiary); background: transparent; border: 1px solid transparent; border-radius: 5px; cursor: pointer; transition: color 160ms ease, background-color 160ms ease, border-color 160ms ease; }.segment-download:hover, .segment-download:focus-visible, .segment-row.selected .segment-download { color: var(--uvp-brand); background: var(--uvp-search-secondary-btn-bg); border-color: var(--uvp-search-secondary-btn-border); outline: none; }
.segment-empty { display: flex; flex: 1; flex-direction: column; align-items: center; justify-content: center; gap: 8px; padding: 24px; color: var(--uvp-text-tertiary); text-align: center; }.segment-empty strong { font-size: 12px; }.segment-empty span { font-size: 11px; }
.segment-footer { display: flex; justify-content: space-between; padding: 8px 11px; color: var(--uvp-text-tertiary); font-size: 10px; background: var(--uvp-list-toolbar-bg); border-top: 1px solid var(--uvp-list-panel-border); }
.timeline-panel { margin: 10px 12px 12px; }
.spin { animation: spin 900ms linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
@media (max-width: 1180px) {
    .query-bar { flex-wrap: wrap; }.channel-context { flex: 1; border-right: 0; }.query-fields { flex-basis: 100%; justify-content: flex-end; }.playback-main { grid-template-columns: minmax(0, 1fr) 280px; }
}
@media (max-width: 768px) {
    .playback-workspace { min-height: 100vh; border-radius: 0; }.query-bar { align-items: flex-start; padding: 10px 12px; }.channel-context { min-width: 0; }.online-indicator { display: none; }.query-fields { display: grid; grid-template-columns: 1fr 1fr; width: 100%; }.query-field input { width: 100%; }.range-separator { display: none; }.type-field, .query-submit { width: 100%; }.type-field select { width: 100%; }.playback-main { display: flex; flex: none; flex-direction: column; padding: 8px 8px 0; }.playback-viewport { flex: none; min-height: 0; aspect-ratio: 16 / 9; }.segment-panel { height: 280px; margin: 8px 0 0; }.timeline-panel { margin: 8px; }
}
@media (max-width: 430px) {
    .back-command { width: 36px; height: 36px; }.channel-context span:not(.channel-icon) { max-width: 220px; overflow: hidden; text-overflow: ellipsis; }.query-fields { grid-template-columns: 1fr; }.query-field input, .query-field select, .query-submit { height: 44px; }.playback-controls { height: 52px; }.control-icon, .control-primary { width: 36px; height: 36px; }.control-time { min-width: 0; font-size: 9px; }.control-spacer { display: none; }.scale-select { display: none; }.segment-panel { height: 306px; }.viewport-state { min-width: 0; width: 76%; padding: 14px; }.viewport-state span { max-width: 100%; text-align: center; }
}
@media (prefers-reduced-motion: reduce) { *, *::before, *::after { scroll-behavior: auto !important; transition-duration: .01ms !important; animation-duration: .01ms !important; animation-iteration-count: 1 !important; } }
</style>
