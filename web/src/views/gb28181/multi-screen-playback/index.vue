<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from "vue";
import {
    AlertTriangle,
    CircleStop,
    Maximize2,
    Minimize2,
    Play,
    RefreshCw,
    Repeat2,
    SlidersHorizontal,
    Square,
    Star,
    X
} from "lucide-vue-next";
import { startPlay, type PlayResult } from "@/api/gb28181";
import { Message } from "@arco-design/web-vue";
import { usePlaybackConsoleStore } from "@/store/modules/playback-console";
import { useUserStoreHook } from "@/store/modules/user";
import PlayWindow from "../components/PlayWindow.vue";
import BasicPtzPanel from "./BasicPtzPanel.vue";
import PlaybackSourceTree from "./PlaybackSourceTree.vue";
import UnplayedCover from "./UnplayedCover.vue";
import { listChannels, type ChannelVO } from "../device-mgmt/api";
import { resolvePlaybackSource, type PlaybackSource } from "../playbackProtocol";
import { useWorkRecording } from "./useWorkRecording";

type LayoutSize = 1 | 4 | 6 | 8 | 9 | 16;
type SlotStatus = "idle" | "requesting" | "playing" | "error" | "offline";
type PtzDirection = "上" | "右上" | "右" | "右下" | "下" | "左下" | "左" | "左上";

interface PlaybackSlot {
    index: number;
    channel: ChannelVO | null;
    status: SlotStatus;
    result: PlayResult | null;
    source: PlaybackSource | null;
    error: string;
    token: number;
}

interface FavoriteChannelGroup {
    id: number;
    name: string;
    channels: ChannelVO[];
}

const layout = ref<LayoutSize>(4);
const focusedIndex = ref<number | null>(null);
const playbackConsole = usePlaybackConsoleStore();
const userStore = useUserStoreHook();
const permissions = computed(() => userStore.account.permissions ?? []);
const hasPermission = (permission: string) => permissions.value.includes("*:*:*") || permissions.value.includes(permission);
const canViewDevices = computed(() => hasPermission("gb28181:device:view"));
const canStartPlayback = computed(() => hasPermission("gb28181:play:start"));
const canStartWorkRecording = computed(() => hasPermission("gb28181:work-recording:start"));
const canStopWorkRecording = computed(() => hasPermission("gb28181:work-recording:stop"));
const canManageFavorites = computed(() => hasPermission("gb28181:channel-favorite:manage"));
const canRenderPtz = computed(() => hasPermission("gb28181:ptz:view") || hasPermission("gb28181:ptz:control"));
const monitorAreaRef = ref<HTMLElement | null>(null);
const isFullscreen = ref(false);
const pollingVisible = ref(false);
type SourceView = "devices" | "national" | "custom" | "favorites";
const sourceView = ref<SourceView>("devices");
const sourceTreeRef = ref<{ openFavoriteDialogForChannels?: (channels: ChannelVO[]) => void } | null>(null);
const pollingDraft = reactive({ enabled: false, intervalSeconds: 30, skipOffline: true });
const pollingSettings = reactive({ enabled: false, intervalSeconds: 30, skipOffline: true });
const pollingChannels = ref<ChannelVO[]>([]);
const pollingCursor = ref(0);
const pollingSaving = ref(false);
const pollingError = ref("");
const pollingRemainingSeconds = ref(0);
const playAllLoading = ref(false);
const ptzMotion = ref<{ channelId: number; direction: PtzDirection } | null>(null);
const workRecording = useWorkRecording();
let pollingTimer: ReturnType<typeof setInterval> | null = null;
let pollingCountdownTimer: ReturnType<typeof setInterval> | null = null;
let pollingNextCycleAt = 0;
let pollingCycleRunning = false;
let playAllToken = 0;

function createSlot(index: number): PlaybackSlot {
    return { index, channel: null, status: "idle", result: null, source: null, error: "", token: 0 };
}

const slots = reactive<PlaybackSlot[]>(Array.from({ length: 16 }, (_, index) => createSlot(index)));
const visibleSlots = computed(() => slots.slice(0, layout.value));
const usedChannelIds = computed(() => slots.flatMap(slot => slot.channel ? [slot.channel.id] : []));
const focusedSlot = computed(() => focusedIndex.value == null ? null : slots[focusedIndex.value] || null);
const focusedWorkChannel = computed(() => focusedSlot.value?.channel || null);
const focusedWorkSnapshot = computed(() => {
    const channel = focusedWorkChannel.value;
    return channel ? workRecording.snapshot(channel.id) : null;
});
const focusedWorkLoadState = computed(() => {
    const channel = focusedWorkChannel.value;
    return channel ? workRecording.loadState(channel.id) : "idle";
});
const focusedWorkAction = computed(() => {
    const channel = focusedWorkChannel.value;
    const snapshot = focusedWorkSnapshot.value;
    if (!channel) return "开始录制";
    const action = workRecording.pendingAction(channel.id);
    if (action === "start" || snapshot?.state === "starting") return "开始中";
    if (action === "stop" || snapshot?.state === "stopping") return "结束中";
    if (focusedWorkLoadState.value !== "ready") return "待核实";
    if (snapshot?.state === "unknown") return snapshot.id && canStopWorkRecording.value ? "重试结束" : "待核实";
    return snapshot?.state === "recording" ? "结束录制" : "开始录制";
});
const focusedWorkStatus = computed(() => {
    const channel = focusedWorkChannel.value;
    const snapshot = focusedWorkSnapshot.value;
    if (!channel) return "请选择播放窗口";
    if (focusedWorkLoadState.value === "loading") return "正在查询录像状态";
    if (focusedWorkLoadState.value !== "ready") return "录像状态待核实";
    if (!snapshot) return "录像状态待核实";
    if (snapshot.state === "unknown") return "录像状态待核实";
    if (snapshot.state === "recording") return "录像中";
    if (snapshot.state === "starting") return "开始中";
    if (snapshot.state === "stopping") return "结束中";
    if (snapshot.state === "stopped") return "已结束";
    if (snapshot.state === "failed") return "开始失败";
    return "未录制";
});
const focusedWorkTargetLabel = computed(() => {
    const channel = focusedWorkChannel.value;
    return channel ? (channel.name || channel.alias || channel.channelId) : "未选择播放窗口";
});
const workRecordingToggleDisabled = computed(() => {
    const channel = focusedWorkChannel.value;
    if (!channel) return true;
    const snapshot = focusedWorkSnapshot.value;
    const action = workRecording.pendingAction(channel.id);
    if (action || focusedWorkLoadState.value !== "ready" || !snapshot || snapshot.state === "starting" || snapshot.state === "stopping") return true;
    if (snapshot.state === "recording" || snapshot.state === "unknown") return !snapshot.id || !canStopWorkRecording.value;
    return !canStartWorkRecording.value;
});
const workRecordingToggleIsStop = computed(() => {
    const snapshot = focusedWorkSnapshot.value;
    return snapshot?.state === "recording" || (snapshot?.state === "unknown" && Boolean(snapshot.id));
});
const workRecordingToggleLabel = computed(() => {
    const target = focusedWorkTargetLabel.value;
    return `${focusedWorkAction.value}（${target}）`;
});
const hasPlayingSlots = computed(() => slots.some(slot => slot.channel && (slot.status === "playing" || slot.status === "requesting" || slot.status === "error" || slot.status === "offline")));
const pollingProgressDegrees = computed(() => {
    if (!pollingSettings.intervalSeconds) return 0;
    return Math.round(Math.min(1, pollingRemainingSeconds.value / pollingSettings.intervalSeconds) * 360);
});
const ptzDirectionByAction: Record<string, PtzDirection> = {
    left_up: "左上",
    up: "上",
    right_up: "右上",
    left: "左",
    right: "右",
    left_down: "左下",
    down: "下",
    right_down: "右下"
};

const layoutOptions: Array<{ value: LayoutSize; label: string; cells: number }> = [
    { value: 1, label: "一分屏", cells: 1 },
    { value: 4, label: "四分屏", cells: 4 },
    { value: 6, label: "六分屏", cells: 6 },
    { value: 8, label: "八分屏", cells: 8 },
    { value: 9, label: "九分屏", cells: 9 },
    { value: 16, label: "十六分屏", cells: 16 }
];

function slotGridClass() {
    return `slot-grid layout-${layout.value}`;
}

function channelIsUsed(channel: ChannelVO) {
    return slots.some(slot => slot.channel?.id === channel.id);
}

function resetSlot(slot: PlaybackSlot) {
    slot.token += 1;
    slot.channel = null;
    slot.status = "idle";
    slot.result = null;
    slot.source = null;
    slot.error = "";
}

async function playSlot(target: PlaybackSlot, silentRequestError = false) {
    if (!canStartPlayback.value || !target.channel) return false;
    const channel = target.channel;
    const token = target.token + 1;
    target.token = token;
    target.status = "requesting";
    target.result = null;
    target.source = null;
    target.error = "";
    try {
        const response = silentRequestError
            ? await startPlay(channel.deviceId, channel.channelId, { silent: true })
            : await startPlay(channel.deviceId, channel.channelId);
        if (!canStartPlayback.value || target.token !== token || target.channel?.id !== channel.id) return false;
        if (response.code !== 0 || !response.data) throw new Error(response.message || "点播失败");
        target.result = response.data;
        target.source = resolvePlaybackSource(response.data, window.location.protocol === "https:");
        target.status = target.source ? "playing" : "error";
        target.error = target.source ? "" : "后端没有返回浏览器可用的播放地址";
        return Boolean(target.source);
    } catch (error: any) {
        if (target.token !== token || target.channel?.id !== channel.id) return false;
        target.status = "error";
        target.error = error?.message || "点播失败，请重试";
        return false;
    }
}

async function assignChannel(channel: ChannelVO) {
    if (!canStartPlayback.value) return;
    playAllToken += 1;
    if (pollingSettings.enabled) stopPolling();
    if (channelIsUsed(channel)) {
        Message.warning(`${channel.name || channel.channelId} 已在分屏中`);
        return;
    }
    if (channel.status !== 1) {
        Message.warning("离线通道不能开始实时播放");
        return;
    }
    const target = visibleSlots.value.find(slot => !slot.channel)
        || (focusedSlot.value?.channel && focusedSlot.value.index < layout.value ? focusedSlot.value : null);
    if (!target) {
        Message.warning("当前布局已满，请先聚焦一个格子再替换");
        return;
    }
    target.channel = channel;
    focusedIndex.value = target.index;
    void workRecording.refresh([channel.id]);
    await playSlot(target);
}

function removeSlot(slot: PlaybackSlot) {
    resetSlot(slot);
    if (focusedIndex.value === slot.index) focusedIndex.value = null;
}

async function retrySlot(slot: PlaybackSlot) {
    if (!canStartPlayback.value || !slot.channel) return;
    await playSlot(slot);
}

function focusSlot(slot: PlaybackSlot) {
    focusedIndex.value = slot.index;
    if (slot.channel) {
        const channelId = slot.channel.id;
        const current = workRecording.snapshot(channelId);
        if (workRecording.loadState(channelId) !== "ready" || current?.state === "unknown") void workRecording.refresh([channelId]);
        else void workRecording.ensureStatus(channelId);
    }
}

async function toggleWorkRecording() {
    const channel = focusedWorkChannel.value;
    if (!channel || workRecordingToggleDisabled.value) return;
    const result = workRecordingToggleIsStop.value
        ? await workRecording.stop(channel.id)
        : await workRecording.start(channel.id);
    if (result) return;
    const reason = workRecording.error(channel.id);
    if (reason) Message.error(reason);
}

function handlePtzActionChange(value: { channelId: number; action: string } | null) {
    const direction = value ? ptzDirectionByAction[value.action] : null;
    ptzMotion.value = value && direction ? { channelId: value.channelId, direction } : null;
}

function openConsole(slot: PlaybackSlot) {
    if (!canStartPlayback.value || !slot.channel) return;
    focusSlot(slot);
    playbackConsole.open(slot.channel);
}

function setLayout(value: LayoutSize) {
    layout.value = value;
    if (focusedIndex.value != null && focusedIndex.value >= value) focusedIndex.value = null;
    if (pollingSettings.enabled) {
        pollingCursor.value = 0;
        void runPollingCycle();
        schedulePolling();
    }
}

function stopAll() {
    const hadChannels = slots.some(slot => slot.channel);
    playAllToken += 1;
    stopPolling();
    slots.forEach(resetSlot);
    focusedIndex.value = null;
    playbackConsole.close();
    Message.info(hadChannels ? "已停止并清空全部画面" : "当前没有可停止的画面");
}

async function playAll() {
    if (!canStartPlayback.value || !canViewDevices.value || playAllLoading.value) return;
    const token = playAllToken + 1;
    playAllToken = token;
    stopPolling();
    playAllLoading.value = true;
    try {
        const channels = await loadPlaybackChannels(true);
        if (token !== playAllToken) return;
        slots.forEach(resetSlot);
        focusedIndex.value = null;
        const batch = channels.slice(0, layout.value);
        if (!batch.length) {
            Message.info("当前没有在线通道");
            return;
        }
        await Promise.all(batch.map((channel, index) => {
            const slot = slots[index];
            slot.channel = channel;
            return playSlot(slot);
        }));
    } catch (reason: any) {
        if (token === playAllToken) Message.error(reason?.message || "加载在线通道失败");
    } finally {
        playAllLoading.value = false;
    }
}

async function openFavorites() {
    if (!canManageFavorites.value) return;
    const channels = slots.flatMap(slot => slot.channel ? [slot.channel] : []);
    if (!channels.length) {
        Message.info("请先播放至少一路通道，再收藏当前播放通道");
        return;
    }
    sourceTreeRef.value?.openFavoriteDialogForChannels?.(channels);
}

function handleFavoriteSaved(groupName: string, addedCount: number, skippedCount: number) {
    if (skippedCount && addedCount) {
        Message.success(`已将 ${addedCount} 个通道加入收藏组“${groupName}”，${skippedCount} 个通道已在组内`);
        return;
    }
    if (skippedCount) {
        Message.info(`收藏组“${groupName}”已包含当前通道，无需重复收藏`);
        return;
    }
    Message.success(`已将 ${addedCount} 个通道加入收藏组“${groupName}”`);
}

async function playFavoriteGroup(group: FavoriteChannelGroup) {
    if (!canManageFavorites.value || !canStartPlayback.value || playAllLoading.value) return;
    const token = playAllToken + 1;
    playAllToken = token;
    stopPolling();
    playAllLoading.value = true;
    try {
        const channels = group.channels || [];
        if (token !== playAllToken) return;
        slots.forEach(resetSlot);
        focusedIndex.value = null;
        const batch = channels.slice(0, layout.value);
        if (!batch.length) {
            Message.info(`收藏组“${group.name}”暂无在线通道`);
            return;
        }
        await Promise.all(batch.map((channel, index) => {
            const slot = slots[index];
            slot.channel = channel;
            return playSlot(slot);
        }));
        Message.success(`已播放收藏组“${group.name}”的 ${batch.length} 路通道`);
    } catch (reason: any) {
        if (token === playAllToken) Message.error(reason?.message || "加载收藏组通道失败");
    } finally {
        playAllLoading.value = false;
    }
}

async function toggleFullscreen() {
    try {
        if (document.fullscreenElement) {
            await document.exitFullscreen();
        } else if (monitorAreaRef.value?.requestFullscreen) {
            await monitorAreaRef.value.requestFullscreen();
        } else {
            Message.warning("当前浏览器不支持全屏显示");
        }
    } catch {
        Message.error("浏览器未允许进入全屏");
    } finally {
        syncFullscreenState();
    }
}

function syncFullscreenState() {
    isFullscreen.value = document.fullscreenElement === monitorAreaRef.value;
}

function openPollingSettings() {
    if (!canStartPlayback.value || !canViewDevices.value) return;
    pollingDraft.enabled = pollingSettings.enabled;
    pollingDraft.intervalSeconds = pollingSettings.intervalSeconds;
    pollingDraft.skipOffline = pollingSettings.skipOffline;
    pollingError.value = "";
    pollingVisible.value = true;
}

function stopPolling() {
    if (pollingTimer) clearInterval(pollingTimer);
    if (pollingCountdownTimer) clearInterval(pollingCountdownTimer);
    pollingTimer = null;
    pollingCountdownTimer = null;
    pollingNextCycleAt = 0;
    pollingRemainingSeconds.value = 0;
    pollingSettings.enabled = false;
}

watch([canStartPlayback, canViewDevices], ([canPlay, canView]) => {
    if (!canPlay || !canView) stopPolling();
});

function togglePolling() {
    if (!canStartPlayback.value || !canViewDevices.value) return;
    if (!pollingSettings.enabled) {
        openPollingSettings();
        return;
    }
    stopPolling();
    pollingVisible.value = false;
    Message.info("轮询已停止");
}

function resetPollingCountdown() {
    pollingNextCycleAt = Date.now() + pollingSettings.intervalSeconds * 1000;
    pollingRemainingSeconds.value = pollingSettings.intervalSeconds;
}

function updatePollingCountdown() {
    pollingRemainingSeconds.value = Math.max(0, Math.ceil((pollingNextCycleAt - Date.now()) / 1000));
}

function schedulePolling() {
    if (pollingTimer) clearInterval(pollingTimer);
    if (pollingCountdownTimer) clearInterval(pollingCountdownTimer);
    pollingTimer = null;
    pollingCountdownTimer = null;
    if (!pollingSettings.enabled) return;
    resetPollingCountdown();
    pollingTimer = setInterval(() => {
        resetPollingCountdown();
        void runPollingCycle();
    }, pollingSettings.intervalSeconds * 1000);
    pollingCountdownTimer = setInterval(updatePollingCountdown, 1000);
}

function channelPage(response: any) {
    if (response?.code !== 0) throw new Error(response?.message || "加载轮询通道失败");
    return response.data;
}

async function loadPlaybackChannels(onlineOnly: boolean) {
    if (!canViewDevices.value) return [];
    const params = { status: onlineOnly ? "online" as const : undefined, page: 1, pageSize: 200 };
    const firstPage = channelPage(await listChannels(params));
    const channels = [...(firstPage?.list || [])] as ChannelVO[];
    const pageCount = Math.ceil((firstPage?.total || channels.length) / params.pageSize);
    if (pageCount > 1) {
        const responses = await Promise.all(Array.from({ length: pageCount - 1 }, (_, index) =>
            listChannels({ ...params, page: index + 2 })
        ));
        responses.forEach(response => channels.push(...(channelPage(response)?.list || [])));
    }
    return channels;
}

async function runPollingCycle() {
    if (!canStartPlayback.value || !canViewDevices.value || !pollingSettings.enabled || pollingCycleRunning || !pollingChannels.value.length) return;
    pollingCycleRunning = true;
    try {
        const channelCount = pollingChannels.value.length;
        const cycleStart = pollingCursor.value;
        let attemptedCount = 0;
        let failedSlots = slots.slice(0, Math.min(layout.value, channelCount));
        slots.forEach(resetSlot);
        focusedIndex.value = null;
        while (pollingSettings.enabled && failedSlots.length && attemptedCount < channelCount) {
            const attemptSlots = failedSlots.slice(0, channelCount - attemptedCount);
            const results = await Promise.all(attemptSlots.map(slot => {
                const channel = pollingChannels.value[(cycleStart + attemptedCount) % channelCount];
                attemptedCount += 1;
                slot.channel = channel;
                if (channel.status !== 1) {
                    slot.status = "offline";
                    return false;
                }
                return playSlot(slot, true);
            }));
            const failedChannels = attemptSlots.flatMap((slot, index) =>
                !results[index] && slot.channel ? [slot.channel] : []
            );
            if (failedChannels.length) {
                const names = failedChannels.map(channel => `“${channel.name || channel.channelId}”`).join("、");
                const hasNextChannel = attemptedCount < channelCount;
                const skippedLabel = failedChannels.length > 1 ? "这些通道" : "该通道";
                Message.warning(hasNextChannel
                    ? `${names}点播失败，已跳过${skippedLabel}，继续点播下一个通道`
                    : `${names}点播失败，本轮已无其他候选通道`);
            }
            failedSlots = attemptSlots.filter((_slot, index) => !results[index]);
        }
        pollingCursor.value = (cycleStart + attemptedCount) % channelCount;
    } finally {
        pollingCycleRunning = false;
    }
}

async function savePollingSettings() {
    if (!canStartPlayback.value || !canViewDevices.value) return;
    pollingError.value = "";
    const intervalSeconds = Math.min(3600, Math.max(5, Number(pollingDraft.intervalSeconds) || 30));
    pollingSettings.intervalSeconds = intervalSeconds;
    pollingSettings.skipOffline = pollingDraft.skipOffline;
    if (!pollingDraft.enabled) {
        stopPolling();
        pollingVisible.value = false;
        Message.success(`轮询设置已保存，间隔 ${intervalSeconds} 秒`);
        return;
    }
    pollingSaving.value = true;
    try {
        const channels = await loadPlaybackChannels(pollingDraft.skipOffline);
        if (!channels.length) throw new Error("当前没有可轮询的通道");
        pollingChannels.value = channels;
        pollingCursor.value = 0;
        pollingSettings.enabled = true;
        await runPollingCycle();
        schedulePolling();
        pollingVisible.value = false;
        Message.success(`轮询已启动，共 ${channels.length} 路通道`);
    } catch (reason: any) {
        stopPolling();
        pollingError.value = reason?.message || "启动轮询失败";
    } finally {
        pollingSaving.value = false;
    }
}

function statusLabel(slot: PlaybackSlot) {
    return {
        idle: "空闲",
        requesting: "建立中",
        playing: "播放中",
        error: "播放失败",
        offline: "流已离线"
    }[slot.status];
}

function statusTone(slot: PlaybackSlot) {
    return {
        idle: "idle",
        requesting: "loading",
        playing: "playing",
        error: "error",
        offline: "offline"
    }[slot.status];
}

function onPlayerError(slot: PlaybackSlot, message: string) {
    if (!slot.channel) return;
    slot.status = "error";
    slot.error = message || "播放器拉流失败";
}

onMounted(() => document.addEventListener("fullscreenchange", syncFullscreenState));
onBeforeUnmount(() => {
    playAllToken += 1;
    stopPolling();
    document.removeEventListener("fullscreenchange", syncFullscreenState);
    slots.forEach(slot => { slot.token += 1; });
});
</script>

<template>
    <div class="snow-fill gb28181-page multi-screen-page">
        <div class="workspace">
            <section class="source-panel" aria-label="设备树和云台控制">
                <PlaybackSourceTree ref="sourceTreeRef" v-model:view="sourceView" :used-channel-ids="usedChannelIds" @select="assignChannel" @select-group="playFavoriteGroup" @favorite-saved="handleFavoriteSaved" />
                <BasicPtzPanel v-if="canRenderPtz" :channel="focusedSlot?.channel || null" @action-change="handlePtzActionChange" />
            </section>

            <main ref="monitorAreaRef" class="monitor-area" :class="{ fullscreen: isFullscreen }">
                <div class="monitor-toolbar">
                    <div class="layout-switcher" role="group" aria-label="选择分屏布局">
                        <button v-for="option in layoutOptions" :key="option.value" type="button" :data-test="`layout-${option.value}`" :class="{ active: layout === option.value }" :aria-label="option.label" :aria-pressed="layout === option.value" :title="option.label" @click="setLayout(option.value)">
                            <span class="layout-glyph" :class="`glyph-${option.value}`" aria-hidden="true"><i v-for="cell in option.cells" :key="cell" /></span>
                        </button>
                    </div>
                    <span class="toolbar-divider" aria-hidden="true" />
                    <div class="work-recording-actions" role="group" aria-label="作业录像控制">
                        <span class="work-recording-target" data-test="work-recording-target">{{ focusedWorkTargetLabel }}</span>
                        <button type="button" data-test="work-recording-toggle" :class="{ active: workRecordingToggleIsStop }" :disabled="workRecordingToggleDisabled" :aria-label="workRecordingToggleLabel" :title="workRecordingToggleLabel" @click="toggleWorkRecording">
                            <CircleStop v-if="workRecordingToggleIsStop" :size="16" aria-hidden="true" />
                            <Play v-else :size="16" aria-hidden="true" />
                            <span class="work-recording-label">{{ focusedWorkAction }}</span>
                        </button>
                        <span class="work-recording-status" data-test="work-recording-status" role="status">{{ focusedWorkStatus }}</span>
                    </div>
                    <div class="playback-actions" role="group" aria-label="批量播放控制">
                        <button v-if="canManageFavorites" type="button" data-test="my-favorites" aria-label="收藏当前播放通道" title="收藏当前播放通道" @click="openFavorites"><Star :size="17" aria-hidden="true" /></button>
                        <button type="button" data-test="play-all" :disabled="playAllLoading || pollingSaving" :aria-label="playAllLoading ? '正在播放全部' : '播放全部'" :title="playAllLoading ? '正在加载在线通道' : '播放全部'" @click="playAll"><RefreshCw v-if="playAllLoading" :size="17" class="spin" aria-hidden="true" /><Play v-else :size="17" aria-hidden="true" /></button>
                        <button type="button" data-test="stop-all" :disabled="!hasPlayingSlots" aria-label="停止全部" title="停止全部" @click="stopAll"><CircleStop :size="17" aria-hidden="true" /></button>
                        <button type="button" data-test="fullscreen" :aria-label="isFullscreen ? '退出全屏' : '视频墙全屏'" :title="isFullscreen ? '退出全屏' : '视频墙全屏'" @click="toggleFullscreen"><Minimize2 v-if="isFullscreen" :size="17" aria-hidden="true" /><Maximize2 v-else :size="17" aria-hidden="true" /></button>
                        <button type="button" class="polling-control" data-test="polling-settings" :class="{ active: pollingSettings.enabled, counting: pollingRemainingSeconds > 0 }" :aria-label="pollingSettings.enabled ? (pollingRemainingSeconds > 0 ? `停止轮询，距离下一轮还有 ${pollingRemainingSeconds} 秒` : '停止轮询') : '轮询设置'" :title="pollingSettings.enabled ? '停止轮询' : '轮询设置'" @click="togglePolling">
                            <span v-if="pollingSettings.enabled && pollingRemainingSeconds > 0" class="polling-countdown" data-test="polling-countdown" role="timer" aria-live="off" :aria-label="`距离下一轮轮询还有 ${pollingRemainingSeconds} 秒`" :data-remaining-seconds="pollingRemainingSeconds">
                                <span class="countdown-ring" :style="{ '--polling-progress': `${pollingProgressDegrees}deg` }" aria-hidden="true"><strong>{{ pollingRemainingSeconds }}</strong></span>
                            </span>
                            <RefreshCw v-else-if="pollingSettings.enabled" data-test="polling-starting" :size="17" class="spin" aria-hidden="true" />
                            <Repeat2 v-else :size="17" aria-hidden="true" />
                        </button>
                    </div>
                </div>

                <div :class="slotGridClass()" aria-label="多屏播放格子">
                    <article v-for="slot in visibleSlots" :key="slot.index" class="screen-slot" :class="{ focused: focusedIndex === slot.index, empty: !slot.channel }" data-test="screen-slot" tabindex="0" @click="focusSlot(slot)" @keydown.enter="focusSlot(slot)">
                        <template v-if="slot.channel">
                            <div class="slot-topline">
                                <div class="slot-title"><span class="status-dot" :class="statusTone(slot)" aria-hidden="true" /><span class="slot-channel-name">{{ slot.channel.name || slot.channel.alias || slot.channel.channelId }}</span><span class="slot-status">{{ statusLabel(slot) }}</span><span v-if="slot.result?.node" class="slot-node-name">节点 {{ slot.result.node.name }}</span></div>
                                <div class="slot-actions">
                                    <button class="slot-action" type="button" :data-test="`slot-console-${slot.index}`" aria-label="打开通道控制台" title="打开通道控制台" @click.stop="openConsole(slot)"><SlidersHorizontal :size="15" aria-hidden="true" /></button>
                                    <button class="slot-action danger" type="button" :data-test="`slot-remove-${slot.index}`" aria-label="关闭当前播放" title="关闭当前播放" @click.stop="removeSlot(slot)"><X :size="15" aria-hidden="true" /></button>
                                </div>
                            </div>
                            <div class="slot-body">
                                <PlayWindow v-if="slot.status === 'playing' && slot.source" :url="slot.source.url" :zlm-webrtc="slot.source.zlmWebrtc" :has-audio="slot.channel.audioEnabled" @error="onPlayerError(slot, $event)" />
                                <div v-else-if="slot.status === 'requesting'" class="slot-state"><RefreshCw :size="24" class="spin" aria-hidden="true" /><strong>正在建立媒体链路</strong><span>{{ slot.channel.deviceId }} / {{ slot.channel.channelId }}</span></div>
                                <div v-else-if="slot.status === 'error'" class="slot-state error-state" role="alert"><AlertTriangle :size="24" aria-hidden="true" /><strong>{{ slot.error }}</strong><button class="text-action" type="button" @click.stop="retrySlot(slot)"><RefreshCw :size="14" aria-hidden="true" />重试</button></div>
                                <div v-else-if="slot.status === 'offline'" class="slot-state offline-state" role="status"><AlertTriangle :size="24" aria-hidden="true" /><strong>{{ slot.error || "通道离线" }}</strong><span>{{ slot.channel.deviceId }} / {{ slot.channel.channelId }}</span></div>
                                <div v-else class="slot-state"><Square :size="24" aria-hidden="true" /><strong>画面暂不可用</strong></div>
                                <div v-if="slot.status === 'playing' && ptzMotion?.channelId === slot.channel.id" class="ptz-direction-indicator" data-test="ptz-direction-indicator" :data-direction="ptzMotion.direction" role="status" :aria-label="`云台正在向${ptzMotion.direction}移动`">
                                    <span class="ptz-direction-stack">
                                        <span class="ptz-direction-chevron front" aria-hidden="true" />
                                        <span class="ptz-direction-chevron middle" aria-hidden="true" />
                                        <span class="ptz-direction-chevron back" aria-hidden="true" />
                                    </span>
                                </div>
                            </div>
                        </template>
                        <UnplayedCover v-else :index="slot.index" />
                    </article>
                </div>
                <div v-if="pollingVisible" class="polling-backdrop" @click.self="pollingVisible = false">
                    <section class="polling-settings" role="dialog" aria-modal="true" aria-labelledby="polling-title">
                        <header><div><Repeat2 :size="18" aria-hidden="true" /><strong id="polling-title">轮询设置</strong></div><button type="button" aria-label="关闭轮询设置" title="关闭" @click="pollingVisible = false"><X :size="17" aria-hidden="true" /></button></header>
                        <div class="polling-form">
                            <label class="checkbox-row polling-enabled-row"><input v-model="pollingDraft.enabled" data-test="polling-enabled" type="checkbox" /><span>启用轮询</span></label>
                            <label for="polling-interval">轮询间隔</label>
                            <div class="interval-input"><input id="polling-interval" v-model.number="pollingDraft.intervalSeconds" data-test="polling-interval" type="number" min="5" max="3600" step="5" /><span>秒</span></div>
                            <label class="checkbox-row"><input v-model="pollingDraft.skipOffline" type="checkbox" /><span>跳过离线通道</span></label>
                            <p v-if="pollingError" class="polling-error" role="alert">{{ pollingError }}</p>
                        </div>
                        <footer><button type="button" class="dialog-secondary" :disabled="pollingSaving" @click="pollingVisible = false">取消</button><button type="button" class="dialog-primary" data-test="save-polling" :disabled="pollingSaving" @click="savePollingSettings">{{ pollingSaving ? "加载中" : "保存" }}</button></footer>
                    </section>
                </div>
            </main>
        </div>
    </div>
</template>

<style scoped>
@import "@/styles/zlm-tokens.css";

.multi-screen-page {
    display: flex;
    min-height: 0;
    flex-direction: column;
    gap: 12px;
    padding: 0;
    color: var(--zlm-text-1);
    background: var(--uvp-navigation-bg);
    font-family: var(--zlm-font-body);
}

.monitor-toolbar,
.slot-topline,
.layout-switcher,
.playback-actions,
.work-recording-actions,
.slot-title,
.slot-actions,
.text-action {
    display: flex;
    align-items: center;
}

.workspace { display: grid; grid-template-columns: 280px minmax(0, 1fr); min-height: 0; flex: 1 1 auto; overflow: hidden; background: var(--zlm-card); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-lg); box-shadow: var(--zlm-shadow-sm); }
.source-panel { display: grid; grid-template-rows: minmax(0, 1fr) auto; row-gap: 12px; min-width: 0; min-height: 0; overflow: hidden; background: var(--uvp-navigation-bg); border-right: 1px solid var(--zlm-border); }
.text-action { gap: 5px; color: var(--zlm-brand-600); background: transparent; border: 0; cursor: pointer; font: inherit; font-size: 12px; }

.monitor-area { position: relative; display: flex; min-width: 0; min-height: 0; flex-direction: column; padding: 16px; background: var(--zlm-bg); }
.monitor-area:fullscreen { padding: 16px; background: var(--zlm-bg); }
.monitor-toolbar { flex: 0 0 auto; justify-content: flex-end; gap: 10px; margin-bottom: 12px; }
.work-recording-actions { min-width: 0; gap: 6px; }
.work-recording-actions button { display: inline-flex; min-width: 104px; height: 34px; align-items: center; justify-content: center; gap: 6px; padding: 0 10px; color: var(--zlm-text-3); background: transparent; border: 1px solid transparent; border-radius: var(--zlm-radius-sm); cursor: pointer; font: inherit; font-size: 12px; }
.work-recording-actions button:hover { color: var(--zlm-text-1); background: var(--zlm-fill-2); border-color: var(--zlm-border); }
.work-recording-actions button:focus-visible { outline: 2px solid var(--zlm-brand-500); outline-offset: 2px; }
.work-recording-actions button.active { color: var(--zlm-danger-600, #DC2626); background: var(--zlm-danger-50, #FEF2F2); border-color: var(--zlm-danger-200, #FECACA); }
.work-recording-actions button:disabled { color: var(--zlm-text-4); cursor: not-allowed; opacity: 0.65; }
.work-recording-actions button:disabled:hover { background: transparent; border-color: transparent; }
.work-recording-target { max-width: 140px; overflow: hidden; color: var(--zlm-text-2); text-overflow: ellipsis; white-space: nowrap; font-size: 12px; }
.work-recording-label { white-space: nowrap; }
.work-recording-status { color: var(--zlm-text-3); font-size: 11px; white-space: nowrap; }
.layout-switcher { gap: 4px; }
.layout-switcher button,
.playback-actions button,
.polling-settings header button { display: inline-flex; width: 34px; height: 34px; align-items: center; justify-content: center; padding: 0; color: var(--zlm-text-3); background: transparent; border: 1px solid transparent; border-radius: var(--zlm-radius-sm); cursor: pointer; }
.layout-switcher button:hover,
.playback-actions button:hover,
.polling-settings header button:hover { color: var(--zlm-text-1); background: var(--zlm-fill-2); border-color: var(--zlm-border); }
.layout-switcher button:focus-visible,
.playback-actions button:focus-visible,
.polling-settings button:focus-visible,
.polling-settings input:focus-visible { outline: 2px solid var(--zlm-brand-500); outline-offset: 2px; }
.layout-switcher button.active { color: var(--zlm-brand-600); background: var(--zlm-brand-50); border-color: var(--zlm-brand-100); }
.playback-actions { gap: 4px; }
.playback-actions button.active { color: var(--zlm-brand-600); background: var(--zlm-brand-50); border-color: var(--zlm-brand-200); box-shadow: inset 0 -2px 0 var(--zlm-brand-500); }
.playback-actions button.polling-control.counting { width: 44px; flex-shrink: 0; padding: 0; }
.polling-countdown { display: flex; align-items: center; justify-content: center; }
.countdown-ring { position: relative; display: grid; width: 26px; height: 26px; flex: 0 0 26px; background: conic-gradient(var(--zlm-brand-500) var(--polling-progress), var(--zlm-brand-100) 0); border-radius: 50%; place-items: center; }
.countdown-ring::before { position: absolute; background: var(--zlm-brand-50); border-radius: 50%; content: ""; inset: 3px; }
.countdown-ring strong { position: relative; z-index: 1; min-width: 2ch; color: var(--zlm-brand-700); font-size: 10px; font-variant-numeric: tabular-nums; font-weight: 700; line-height: 1; text-align: center; }
.playback-actions button:disabled { color: var(--zlm-text-4); cursor: not-allowed; opacity: 0.52; }
.playback-actions button:disabled:hover { background: transparent; border-color: transparent; }
.toolbar-divider { width: 1px; height: 20px; background: var(--zlm-border); }
.layout-glyph { display: grid; width: 18px; height: 18px; gap: 1px; }
.layout-glyph i { display: block; min-width: 0; min-height: 0; background: currentColor; }
.glyph-1 { grid-template: 1fr / 1fr; }
.glyph-4 { grid-template: repeat(2, 1fr) / repeat(2, 1fr); }
.glyph-6, .glyph-9 { grid-template: repeat(3, 1fr) / repeat(3, 1fr); }
.glyph-8, .glyph-16 { grid-template: repeat(4, 1fr) / repeat(4, 1fr); }
.glyph-6 i:first-child { grid-column: span 2; grid-row: span 2; }
.glyph-8 i:first-child { grid-column: span 3; grid-row: span 3; }
.slot-grid { display: grid; min-height: 0; flex: 1 1 auto; gap: 1px; overflow: hidden; background: #4B5563; border: 1px solid #4B5563; border-radius: var(--zlm-radius-md); box-shadow: 0 8px 24px rgb(15 23 42 / 10%); }
.slot-grid.layout-1 { grid-template-columns: minmax(0, 1fr); grid-template-rows: minmax(0, 1fr); }
.slot-grid.layout-4 { grid-template-columns: repeat(2, minmax(0, 1fr)); grid-template-rows: repeat(2, minmax(0, 1fr)); }
.slot-grid.layout-6, .slot-grid.layout-9 { grid-template-columns: repeat(3, minmax(0, 1fr)); grid-template-rows: repeat(3, minmax(0, 1fr)); }
.slot-grid.layout-8, .slot-grid.layout-16 { grid-template-columns: repeat(4, minmax(0, 1fr)); grid-template-rows: repeat(4, minmax(0, 1fr)); }
.slot-grid.layout-6 .screen-slot:first-child { grid-column: span 2; grid-row: span 2; }
.slot-grid.layout-8 .screen-slot:first-child { grid-column: span 3; grid-row: span 3; }
.screen-slot { position: relative; display: flex; min-width: 0; min-height: 0; flex-direction: column; overflow: hidden; background: #0E1014; border: 0; border-radius: 0; outline: 0; }
.screen-slot::after { position: absolute; z-index: 5; content: ""; inset: 0; border: 2px solid transparent; border-radius: var(--zlm-radius-md); pointer-events: none; transition: border-color var(--zlm-dur-fast) var(--zlm-ease-out), box-shadow var(--zlm-dur-fast) var(--zlm-ease-out); }
.screen-slot:hover::after { border-color: rgb(148 163 184 / 38%); }
.screen-slot:focus-visible, .screen-slot.focused { z-index: 1; }
.screen-slot:focus-visible::after, .screen-slot.focused::after { border-color: var(--zlm-brand-500); box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--zlm-brand-500) 30%, transparent); }
.screen-slot.empty { min-height: 0; background: #090B0F; }
.slot-topline { position: absolute; z-index: 6; top: 0; right: 0; left: 0; display: flex; min-height: 28px; align-items: center; justify-content: space-between; gap: 8px; min-width: 0; padding: 2px 8px; color: #F8FAFC; background: rgb(15 23 42 / 72%); backdrop-filter: blur(4px); opacity: 0; pointer-events: none; transform: translateY(-6px); transition: opacity var(--zlm-dur-fast) var(--zlm-ease-out), transform var(--zlm-dur-fast) var(--zlm-ease-out), visibility var(--zlm-dur-fast) var(--zlm-ease-out); visibility: hidden; }
.screen-slot:hover .slot-topline, .screen-slot:focus-within .slot-topline { opacity: 1; pointer-events: auto; transform: translateY(0); visibility: visible; }
.slot-title { min-width: 0; gap: 7px; }
.status-dot { width: 6px; height: 6px; flex: 0 0 auto; background: #64748B; border-radius: 50%; }
.status-dot.loading { background: #38BDF8; }
.status-dot.playing { background: #22C55E; box-shadow: 0 0 0 3px rgb(34 197 94 / 12%); }
.status-dot.error, .status-dot.offline { background: #EF4444; }
.slot-channel-name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 12px; font-weight: 700; }
.slot-status { color: #94A3B8; font-size: 10px; }
.slot-node-name { overflow: hidden; color: #94A3B8; text-overflow: ellipsis; white-space: nowrap; font-size: 10px; }
.slot-actions { flex: 0 0 auto; gap: 4px; }
.slot-action { display: inline-grid; width: 28px; height: 28px; padding: 0; color: #94A3B8; background: transparent; border: 1px solid transparent; border-radius: 5px; cursor: pointer; transition: color var(--zlm-dur-fast) var(--zlm-ease-out), background-color var(--zlm-dur-fast) var(--zlm-ease-out), border-color var(--zlm-dur-fast) var(--zlm-ease-out); place-items: center; }
.slot-action:hover { color: #F8FAFC; background: rgb(51 65 85 / 68%); border-color: rgb(100 116 139 / 36%); }
.slot-action.danger:hover { color: #FCA5A5; background: rgb(127 29 29 / 28%); border-color: rgb(248 113 113 / 24%); }
.slot-action:focus-visible { outline: 2px solid #60A5FA; outline-offset: 1px; }
.slot-body { position: relative; display: flex; min-width: 0; min-height: 0; flex: 1; align-items: center; justify-content: center; }
.slot-body :deep(.play-window) { width: 100%; height: 100%; min-height: 100%; aspect-ratio: auto; border: 0; border-radius: 0; }
.slot-body :deep(.play-window .player), .slot-body :deep(.play-window video), .slot-body :deep(.play-window canvas) { width: 100% !important; height: 100% !important; }
.slot-body :deep(.play-window video) { object-fit: contain !important; }
.ptz-direction-indicator { --ptz-direction-rotation: 0deg; position: absolute; top: 50%; left: 50%; z-index: 5; display: grid; width: clamp(72px, 12%, 104px); aspect-ratio: 1; transform: translate(-50%, -50%) rotate(var(--ptz-direction-rotation)); pointer-events: none; place-items: center; }
.ptz-direction-stack { position: relative; display: block; width: 100%; height: 100%; animation: ptz-direction-flow 0.95s ease-in-out infinite; will-change: opacity, transform; }
.ptz-direction-chevron { position: absolute; left: 50%; display: block; width: 76%; height: 34%; background: rgb(96 165 250 / 82%); clip-path: polygon(0 68%, 50% 0, 100% 68%, 80% 100%, 50% 58%, 20% 100%); transform: translateX(-50%); }
.ptz-direction-chevron.front { top: 4%; filter: drop-shadow(0 2px 8px rgb(15 23 42 / 42%)) drop-shadow(0 0 9px rgb(59 130 246 / 34%)); }
.ptz-direction-chevron.middle { top: 32%; width: 66%; background: rgb(147 197 253 / 48%); }
.ptz-direction-chevron.back { top: 58%; width: 56%; background: rgb(191 219 254 / 22%); }
.ptz-direction-indicator[data-direction="右上"] { --ptz-direction-rotation: 45deg; }
.ptz-direction-indicator[data-direction="右"] { --ptz-direction-rotation: 90deg; }
.ptz-direction-indicator[data-direction="右下"] { --ptz-direction-rotation: 135deg; }
.ptz-direction-indicator[data-direction="下"] { --ptz-direction-rotation: 180deg; }
.ptz-direction-indicator[data-direction="左下"] { --ptz-direction-rotation: 225deg; }
.ptz-direction-indicator[data-direction="左"] { --ptz-direction-rotation: 270deg; }
.ptz-direction-indicator[data-direction="左上"] { --ptz-direction-rotation: 315deg; }
@keyframes ptz-direction-flow {
    0%, 100% { opacity: 0.48; transform: translateY(5px) scale(0.94); }
    50% { opacity: 1; transform: translateY(-4px) scale(1); }
}
.slot-state { display: flex; flex: 1; min-height: 180px; flex-direction: column; align-items: center; justify-content: center; gap: 8px; padding: 20px; color: #CBD5E1; text-align: center; }
.slot-state strong { max-width: 90%; color: #F8FAFC; font-size: 12px; line-height: 1.45; }
.slot-state span { color: #94A3B8; font-family: var(--zlm-font-mono); font-size: 10px; }
.error-state { color: #FCA5A5; }
.error-state strong { color: #FECACA; }
.offline-state { color: #FBBF24; }
.offline-state strong { color: #FDE68A; }
.polling-backdrop { position: absolute; z-index: 20; display: grid; background: rgb(15 23 42 / 34%); inset: 0; place-items: center; }
.polling-settings { width: min(360px, calc(100% - 32px)); color: var(--zlm-text-1); background: var(--zlm-card); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-md); box-shadow: var(--zlm-shadow-lg); }
.polling-settings header, .polling-settings footer { display: flex; align-items: center; padding: 14px 16px; }
.polling-settings header { justify-content: space-between; border-bottom: 1px solid var(--zlm-border); }
.polling-settings header > div { display: flex; align-items: center; gap: 8px; }
.polling-settings footer { justify-content: flex-end; gap: 8px; border-top: 1px solid var(--zlm-border); }
.polling-form { display: grid; grid-template-columns: minmax(0, 1fr) auto; align-items: center; gap: 18px 12px; padding: 20px 16px; font-size: 13px; }
.interval-input { display: flex; align-items: center; gap: 8px; }
.interval-input input { width: 96px; height: 34px; padding: 0 10px; color: var(--zlm-text-1); background: var(--zlm-bg); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-sm); font: inherit; }
.interval-input span { color: var(--zlm-text-3); }
.checkbox-row { display: flex; grid-column: 1 / -1; align-items: center; gap: 8px; cursor: pointer; }
.polling-enabled-row { padding-bottom: 2px; border-bottom: 1px solid var(--zlm-border); }
.checkbox-row input { width: 16px; height: 16px; accent-color: var(--zlm-brand-600); }
.polling-error { grid-column: 1 / -1; margin: -6px 0 0; color: var(--zlm-danger); font-size: 12px; }
.polling-settings footer button { min-width: 64px; height: 34px; padding: 0 14px; border-radius: var(--zlm-radius-sm); cursor: pointer; font: inherit; font-size: 12px; }
.polling-settings footer button:disabled { cursor: wait; opacity: 0.6; }
.dialog-secondary { color: var(--zlm-text-2); background: var(--zlm-card); border: 1px solid var(--zlm-border); }
.dialog-primary { color: #FFFFFF; background: var(--zlm-brand-600); border: 1px solid var(--zlm-brand-600); }
.spin { animation: spin 900ms linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }

@media (max-width: 1024px) {
    .workspace { grid-template-columns: 240px minmax(0, 1fr); }
}
@media (max-width: 768px) {
    .workspace, .workspace.source-collapsed { grid-template-columns: 1fr; overflow-x: hidden; overflow-y: auto; }
    .source-panel { min-height: 640px; border-right: 0; border-bottom: 1px solid var(--zlm-border); }
    .monitor-area { padding: 12px; }
    .slot-grid { flex: 0 0 auto; grid-auto-rows: clamp(220px, 56vw, 360px); }
    .slot-grid.layout-1, .slot-grid.layout-4, .slot-grid.layout-6, .slot-grid.layout-8, .slot-grid.layout-9, .slot-grid.layout-16 { grid-template-columns: 1fr; grid-template-rows: none; }
    .slot-grid.layout-6 .screen-slot:first-child, .slot-grid.layout-8 .screen-slot:first-child { grid-column: auto; grid-row: auto; }
}
@media (max-width: 480px) {
    .monitor-toolbar { align-items: center; justify-content: space-between; gap: 6px; overflow-x: hidden; }
    .layout-switcher, .playback-actions, .work-recording-actions { flex: 0 0 auto; }
    .layout-switcher, .playback-actions { gap: 2px; }
    .layout-switcher button, .playback-actions button { width: 30px; height: 30px; }
    .work-recording-target, .work-recording-status { display: none; }
    .work-recording-actions button { min-width: 30px; width: 30px; height: 30px; padding: 0; }
    .work-recording-label { display: none; }
    .playback-actions button.polling-control.counting { width: 30px; padding: 0; }
    .countdown-ring { width: 24px; height: 24px; flex-basis: 24px; }
    .layout-glyph { width: 16px; height: 16px; }
    .toolbar-divider { display: none; }
}
@media (prefers-reduced-motion: reduce) {
    .spin { animation: none; }
    .screen-slot::after { transition: none; }
}
</style>
