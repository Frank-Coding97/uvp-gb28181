<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref } from "vue";
import {
    AlertTriangle,
    Check,
    Grid2X2,
    Grid3X3,
    LayoutGrid,
    Maximize2,
    RefreshCw,
    Square,
    X
} from "lucide-vue-next";
import { startPlay, type PlayResult } from "@/api/gb28181";
import PlayWindow from "../components/PlayWindow.vue";
import PlayConsoleLinked from "../components/PlayConsoleLinked.vue";
import BasicPtzPanel from "./BasicPtzPanel.vue";
import PlaybackSourceTree from "./PlaybackSourceTree.vue";
import UnplayedCover from "./UnplayedCover.vue";
import type { ChannelVO } from "../device-mgmt/api";

type LayoutSize = 1 | 4 | 9;
type SlotStatus = "idle" | "requesting" | "playing" | "error" | "offline";

interface PlaybackSlot {
    index: number;
    channel: ChannelVO | null;
    status: SlotStatus;
    result: PlayResult | null;
    error: string;
    token: number;
}

const layout = ref<LayoutSize>(4);
const focusedIndex = ref<number | null>(null);
const consoleVisible = ref(false);
const consoleChannel = ref<ChannelVO | null>(null);
const toast = ref("");

function createSlot(index: number): PlaybackSlot {
    return { index, channel: null, status: "idle", result: null, error: "", token: 0 };
}

const slots = reactive<PlaybackSlot[]>(Array.from({ length: 9 }, (_, index) => createSlot(index)));
const visibleSlots = computed(() => slots.slice(0, layout.value));
const usedChannelIds = computed(() => slots.flatMap(slot => slot.channel ? [slot.channel.id] : []));
const emptySlotIndex = computed(() => slots.findIndex(slot => !slot.channel));
const focusedSlot = computed(() => focusedIndex.value == null ? null : slots[focusedIndex.value] || null);

const layoutOptions: Array<{ value: LayoutSize; label: string; icon: typeof LayoutGrid }> = [
    { value: 1, label: "单屏", icon: LayoutGrid },
    { value: 4, label: "四分屏", icon: Grid2X2 },
    { value: 9, label: "九分屏", icon: Grid3X3 }
];

function slotGridClass() {
    return `slot-grid layout-${layout.value}`;
}

function channelIsUsed(channel: ChannelVO) {
    return slots.some(slot => slot.channel?.id === channel.id);
}

function resolvePlayUrl(result: PlayResult) {
    const urls = result.urls || {};
    return urls.wsFlv || result.wsflvUrl || urls.httpFlv || result.httpFlvUrl || urls.hls || result.hlsUrl || "";
}

function resetSlot(slot: PlaybackSlot) {
    slot.token += 1;
    slot.channel = null;
    slot.status = "idle";
    slot.result = null;
    slot.error = "";
}

async function assignChannel(channel: ChannelVO) {
    if (channelIsUsed(channel)) {
        toast.value = `${channel.name || channel.channelId} 已在分屏中`;
        return;
    }
    if (channel.status !== 1) {
        toast.value = "离线通道不能开始实时播放";
        return;
    }
    const target = emptySlotIndex.value >= 0
        ? slots[emptySlotIndex.value]
        : (focusedSlot.value?.channel ? focusedSlot.value : null);
    if (!target || target.index >= layout.value) {
        toast.value = "当前布局已满，请先聚焦一个格子再替换";
        return;
    }
    const token = target.token + 1;
    target.token = token;
    target.channel = channel;
    target.status = "requesting";
    target.result = null;
    target.error = "";
    focusedIndex.value = target.index;
    toast.value = "";
    try {
        const response = await startPlay(channel.deviceId, channel.channelId);
        if (target.token !== token || target.channel?.id !== channel.id) return;
        if (response.code !== 0 || !response.data) throw new Error(response.message || "点播失败");
        target.result = response.data;
        target.status = resolvePlayUrl(response.data) ? "playing" : "error";
        target.error = resolvePlayUrl(response.data) ? "" : "后端没有返回浏览器可用的播放地址";
    } catch (error: any) {
        if (target.token !== token || target.channel?.id !== channel.id) return;
        target.status = "error";
        target.error = error?.message || "点播失败，请重试";
    }
}

function removeSlot(slot: PlaybackSlot) {
    resetSlot(slot);
    if (focusedIndex.value === slot.index) focusedIndex.value = null;
}

async function retrySlot(slot: PlaybackSlot) {
    if (!slot.channel) return;
    const channel = slot.channel;
    resetSlot(slot);
    await assignChannel(channel);
}

function focusSlot(slot: PlaybackSlot) {
    focusedIndex.value = slot.index;
}

function openConsole(slot: PlaybackSlot) {
    if (!slot.channel) return;
    focusSlot(slot);
    consoleChannel.value = slot.channel;
    consoleVisible.value = true;
}

function setLayout(value: LayoutSize) {
    layout.value = value;
    if (focusedIndex.value != null && focusedIndex.value >= value) focusedIndex.value = null;
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

onBeforeUnmount(() => {
    slots.forEach(slot => { slot.token += 1; });
});
</script>

<template>
    <div class="snow-fill gb28181-page multi-screen-page">
        <div class="workspace">
            <section class="source-panel" aria-label="设备树和云台控制">
                <PlaybackSourceTree :used-channel-ids="usedChannelIds" @select="assignChannel" />
                <BasicPtzPanel :channel="focusedSlot?.channel || null" />
            </section>

            <main class="monitor-area">
                <div class="monitor-toolbar">
                    <div class="layout-switcher" role="group" aria-label="选择分屏布局">
                        <button v-for="option in layoutOptions" :key="option.value" type="button" :data-test="`layout-${option.value}`" :class="{ active: layout === option.value }" :aria-pressed="layout === option.value" :title="option.label" @click="setLayout(option.value)"><component :is="option.icon" :size="16" aria-hidden="true" /><span>{{ option.label }}</span></button>
                    </div>
                </div>

                <div :class="slotGridClass()" aria-label="多屏播放格子">
                    <article v-for="slot in visibleSlots" :key="slot.index" class="screen-slot" :class="{ focused: focusedIndex === slot.index, empty: !slot.channel }" data-test="screen-slot" tabindex="0" @click="focusSlot(slot)" @keydown.enter="focusSlot(slot)">
                        <template v-if="slot.channel">
                            <div class="slot-topline">
                                <div class="slot-title"><span class="status-dot" :class="statusTone(slot)" aria-hidden="true" /><span class="slot-channel-name">{{ slot.channel.name || slot.channel.alias || slot.channel.channelId }}</span><span class="slot-status">{{ statusLabel(slot) }}</span></div>
                                <div class="slot-actions">
                                    <button class="slot-action" type="button" aria-label="打开通道控制台" title="打开通道控制台" @click.stop="openConsole(slot)"><Maximize2 :size="15" aria-hidden="true" /></button>
                                    <button class="slot-action danger" type="button" :data-test="`slot-remove-${slot.index}`" aria-label="移除这个画面" title="移除这个画面" @click.stop="removeSlot(slot)"><X :size="15" aria-hidden="true" /></button>
                                </div>
                            </div>
                            <div class="slot-body">
                                <PlayWindow v-if="slot.status === 'playing' && slot.result" :url="resolvePlayUrl(slot.result)" :has-audio="slot.channel.audioEnabled" @error="onPlayerError(slot, $event)" />
                                <div v-else-if="slot.status === 'requesting'" class="slot-state"><RefreshCw :size="24" class="spin" aria-hidden="true" /><strong>正在建立媒体链路</strong><span>{{ slot.channel.deviceId }} / {{ slot.channel.channelId }}</span></div>
                                <div v-else-if="slot.status === 'error'" class="slot-state error-state" role="alert"><AlertTriangle :size="24" aria-hidden="true" /><strong>{{ slot.error }}</strong><button class="text-action" type="button" @click.stop="retrySlot(slot)"><RefreshCw :size="14" aria-hidden="true" />重试</button></div>
                                <div v-else class="slot-state"><Square :size="24" aria-hidden="true" /><strong>画面暂不可用</strong></div>
                            </div>
                            <footer class="slot-footer"><span>{{ slot.channel.deviceId }}</span><span v-if="slot.result?.node">节点 {{ slot.result.node.name }}</span><span v-if="focusedIndex === slot.index" class="focused-label"><Check :size="13" aria-hidden="true" />已聚焦</span></footer>
                        </template>
                        <UnplayedCover v-else />
                    </article>
                </div>
                <p v-if="toast" class="workspace-toast" role="status">{{ toast }}</p>
            </main>
        </div>
        <PlayConsoleLinked v-model:visible="consoleVisible" :channel="consoleChannel" />
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
.slot-footer,
.layout-switcher,
.slot-title,
.slot-actions,
.text-action {
    display: flex;
    align-items: center;
}

.workspace { display: grid; grid-template-columns: 280px minmax(0, 1fr); min-height: 0; flex: 1 1 auto; overflow: hidden; background: var(--zlm-card); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-lg); box-shadow: var(--zlm-shadow-sm); }
.source-panel { display: grid; grid-template-rows: minmax(0, 1fr) auto; row-gap: 12px; min-width: 0; min-height: 0; overflow: hidden; background: var(--uvp-navigation-bg); border-right: 1px solid var(--zlm-border); }
.layout-switcher button { min-height: 36px; padding: 0 10px; color: var(--zlm-text-3); background: transparent; border: 1px solid transparent; border-radius: var(--zlm-radius-sm); cursor: pointer; font: inherit; font-size: 12px; }
.layout-switcher button.active { color: var(--zlm-brand-700); background: var(--zlm-brand-50); border-color: var(--zlm-brand-100); font-weight: 700; }
.text-action { gap: 5px; color: var(--zlm-brand-600); background: transparent; border: 0; cursor: pointer; font: inherit; font-size: 12px; }

.monitor-area { position: relative; display: flex; min-width: 0; min-height: 0; flex-direction: column; padding: 16px; background: var(--zlm-bg); }
.monitor-toolbar { justify-content: flex-end; gap: 16px; margin-bottom: 14px; }
.layout-switcher { gap: 4px; }
.layout-switcher button { display: inline-flex; align-items: center; gap: 6px; }
.slot-grid { display: grid; min-height: 0; flex: 1 1 auto; gap: 0; }
.slot-grid.layout-1 { grid-template-columns: minmax(0, 1fr); grid-template-rows: minmax(0, 1fr); }
.slot-grid.layout-4 { grid-template-columns: repeat(2, minmax(0, 1fr)); grid-template-rows: repeat(2, minmax(0, 1fr)); }
.slot-grid.layout-9 { grid-template-columns: repeat(3, minmax(0, 1fr)); grid-template-rows: repeat(3, minmax(0, 1fr)); }
.screen-slot { position: relative; display: flex; min-width: 0; min-height: 0; flex-direction: column; overflow: hidden; background: #020617; border: 1px solid #1E293B; border-radius: 0; outline: 0; }
.screen-slot:focus-visible, .screen-slot.focused { border-color: var(--zlm-brand-500); box-shadow: 0 0 0 2px color-mix(in srgb, var(--zlm-brand-500) 20%, transparent); }
.screen-slot.empty { min-height: 0; background: #020617; }
.slot-topline, .slot-footer { justify-content: space-between; gap: 8px; min-width: 0; padding: 8px 10px; }
.slot-topline { min-height: 40px; color: #F8FAFC; background: #0F172A; }
.slot-title { min-width: 0; gap: 7px; }
.slot-channel-name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 12px; font-weight: 700; }
.slot-status { color: #94A3B8; font-size: 10px; }
.slot-actions { gap: 2px; }
.slot-action { width: 30px; height: 30px; color: #CBD5E1; }
.slot-action:hover { color: #FFFFFF; background: #1E293B; border-color: #334155; }
.slot-action.danger:hover { color: #FCA5A5; }
.slot-body { display: flex; min-width: 0; min-height: 0; flex: 1; align-items: center; justify-content: center; }
.slot-body :deep(.play-window) { width: 100%; aspect-ratio: 16 / 9; border: 0; border-radius: 0; }
.slot-state { display: flex; flex: 1; min-height: 180px; flex-direction: column; align-items: center; justify-content: center; gap: 8px; padding: 20px; color: #CBD5E1; text-align: center; }
.slot-state strong { max-width: 90%; color: #F8FAFC; font-size: 12px; line-height: 1.45; }
.slot-state span { color: #94A3B8; font-family: var(--zlm-font-mono); font-size: 10px; }
.error-state { color: #FCA5A5; }
.error-state strong { color: #FECACA; }
.slot-footer { min-height: 32px; color: #64748B; background: #0F172A; font-size: 10px; }
.slot-footer > span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.focused-label { display: inline-flex; align-items: center; gap: 3px; color: #93C5FD; }
.workspace-toast { position: absolute; right: 20px; bottom: 16px; max-width: min(420px, calc(100% - 40px)); margin: 0; padding: 9px 12px; color: #FEF3C7; background: #451A03; border: 1px solid #92400E; border-radius: var(--zlm-radius-sm); font-size: 12px; }
.spin { animation: spin 900ms linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }

@media (max-width: 1024px) {
    .workspace { grid-template-columns: 240px minmax(0, 1fr); }
}
@media (max-width: 768px) {
    .workspace, .workspace.source-collapsed { grid-template-columns: 1fr; overflow-x: hidden; overflow-y: auto; }
    .source-panel { min-height: 640px; border-right: 0; border-bottom: 1px solid var(--zlm-border); }
    .monitor-area { padding: 12px; }
    .slot-grid.layout-4 { grid-template-columns: 1fr; grid-template-rows: repeat(4, minmax(0, 1fr)); }
    .slot-grid.layout-9 { grid-template-columns: 1fr; grid-template-rows: repeat(9, minmax(0, 1fr)); }
}
@media (max-width: 480px) {
    .monitor-toolbar { align-items: flex-start; flex-direction: column; }
    .layout-switcher { width: 100%; }
    .layout-switcher button { flex: 1; justify-content: center; }
}
@media (prefers-reduced-motion: reduce) {
    .spin { animation: none; }
}
</style>
