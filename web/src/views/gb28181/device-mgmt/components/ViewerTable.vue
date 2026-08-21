<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import { ChevronDown, LogOut, RefreshCw, Users } from "lucide-vue-next";
import { getCurrentViewers, kickCurrentViewer, type CurrentViewer, type CurrentViewerStream } from "../trafficApi";
import { formatBytes, latestRequestGuard } from "../trafficState";

const props = defineProps<{ deviceId: string; channelId?: string }>();
const initialLoading = ref(true);
const manualRefreshing = ref(false);
const requesting = ref(false);
const error = ref("");
const streams = ref<CurrentViewerStream[]>([]);
const totalViewers = ref(0);
const canKick = ref(false);
const kicking = ref(new Set<string>());
const expandedKeys = ref<string[]>([]);
const guard = latestRequestGuard();
let abortController: AbortController | null = null;
let refreshTimer: number | undefined;
let streamFingerprint = "";

type LoadMode = "initial" | "manual" | "auto";

async function load(mode: LoadMode) {
    if (requesting.value && mode !== "initial") return;
    if (mode === "initial") abortController?.abort();
    const controller = new AbortController();
    abortController = controller;
    const version = guard.next();
    requesting.value = true;
    if (mode === "initial") {
        initialLoading.value = true;
        error.value = "";
    }
    if (mode === "manual") manualRefreshing.value = true;
    try {
        const result = await getCurrentViewers({ deviceId: props.deviceId, channelId: props.channelId || undefined }, controller.signal);
        if (!guard.current(version)) return;
        if (result.code !== 0) throw new Error(result.message || "当前观看加载失败");
        const nextStreams = result.data?.list || [];
        const nextFingerprint = JSON.stringify(nextStreams);
        if (nextFingerprint !== streamFingerprint) {
            streams.value = nextStreams;
            streamFingerprint = nextFingerprint;
        }
        totalViewers.value = result.data?.totalViewers || 0;
        canKick.value = Boolean(result.data?.canKick);
        error.value = "";
        const available = new Set(nextStreams.map(stream => stream.channelId));
        expandedKeys.value = expandedKeys.value.filter(key => available.has(key));
    } catch (cause: unknown) {
        if (!guard.current(version) || controller.signal.aborted) return;
        if (mode === "initial" || streams.value.length === 0) {
            error.value = cause instanceof Error ? cause.message : "当前观看加载失败";
            streams.value = [];
            streamFingerprint = "";
            totalViewers.value = 0;
        } else if (mode === "manual") {
            Message.error(cause instanceof Error ? cause.message : "当前观看刷新失败");
        }
    } finally {
        if (guard.current(version)) {
            requesting.value = false;
            if (mode === "initial") initialLoading.value = false;
            if (mode === "manual") manualRefreshing.value = false;
        }
    }
}

function formatDateTime(value?: string | null) {
    if (!value) return "—";
    const time = new Date(value);
    return Number.isNaN(time.getTime()) ? "—" : time.toLocaleString("zh-CN", { hour12: false });
}

function formatDuration(seconds: number) {
    if (!seconds) return "—";
    if (seconds < 60) return `${seconds} 秒`;
    const minutes = Math.floor(seconds / 60);
    if (minutes < 60) return `${minutes} 分钟`;
    const hours = Math.floor(minutes / 60);
    if (hours < 24) return `${hours} 小时 ${minutes % 60} 分`;
    return `${Math.floor(hours / 24)} 天 ${hours % 24} 小时`;
}

function formatBitrate(value: number) {
    if (!value) return "0 Kbps";
    return value >= 1000 ? `${(value / 1000).toFixed(1)} Mbps` : `${Math.round(value)} Kbps`;
}

function toggleStream(stream: CurrentViewerStream) {
    expandedKeys.value = expandedKeys.value.includes(stream.channelId)
        ? expandedKeys.value.filter(key => key !== stream.channelId)
        : [...expandedKeys.value, stream.channelId];
}

function refreshIfVisible() {
    if (document.visibilityState === "hidden" || requesting.value) return;
    load("auto");
}

function kick(viewer: CurrentViewer) {
    Modal.confirm({
        title: "强退观看连接",
        content: `确认断开 ${viewer.remote} 的观看连接？同通道其他观看连接不会受影响。`,
        okText: "强退",
        okButtonProps: { status: "danger" },
        async onOk() {
            const next = new Set(kicking.value);
            next.add(viewer.id);
            kicking.value = next;
            try {
                const result = await kickCurrentViewer(
                    { deviceId: props.deviceId, channelId: viewer.channelId },
                    { id: viewer.id, schema: viewer.schema }
                );
                if (result.code !== 0) throw new Error(result.message || "强退失败");
                Message.success("目标观看连接已强退");
                await load("auto");
            } catch (cause: unknown) {
                Message.error(cause instanceof Error ? cause.message : "强退失败");
                throw cause;
            } finally {
                const remaining = new Set(kicking.value);
                remaining.delete(viewer.id);
                kicking.value = remaining;
            }
        }
    });
}

watch(() => [props.deviceId, props.channelId], () => {
    streamFingerprint = "";
    load("initial");
}, { immediate: true });
onMounted(() => { refreshTimer = window.setInterval(refreshIfVisible, 1000); });
onBeforeUnmount(() => {
    if (refreshTimer !== undefined) window.clearInterval(refreshTimer);
    guard.cancel();
    abortController?.abort();
});
</script>

<template>
    <section class="viewer-table" aria-label="当前观看连接">
        <div class="viewer-toolbar">
            <span>当前 {{ streams.length }} 个在播通道 · {{ totalViewers }} 个观看连接</span>
            <a-button class="viewer-refresh" type="primary" size="small" aria-label="刷新当前观看" :loading="manualRefreshing" @click="load('manual')">
                <template #icon><RefreshCw :size="15" /></template>刷新
            </a-button>
        </div>
        <div v-if="error" class="viewer-error" role="alert"><span>{{ error }}</span><a-button size="small" @click="load('initial')">重试</a-button></div>
        <a-table
            v-else
            v-model:expanded-keys="expandedKeys"
            :data="streams"
            :loading="initialLoading"
            :pagination="false"
            row-key="channelId"
            :expandable="{ width: 36 }"
            :scroll="{ x: 886, y: 330 }"
            class="uvp-data-table viewer-data-table"
        >
            <template #columns>
                <a-table-column title="通道" :width="200">
                    <template #cell="{ record }"><div class="viewer-channel"><strong>{{ record.channelName || record.channelId }}</strong><span>{{ record.channelId }}</span></div></template>
                </a-table-column>
                <a-table-column title="开始时间" :width="155"><template #cell="{ record }">{{ formatDateTime(record.startedAt) }}</template></a-table-column>
                <a-table-column title="持续时长" :width="105"><template #cell="{ record }">{{ formatDuration(record.aliveSecond) }}</template></a-table-column>
                <a-table-column title="实时码率" :width="100" align="right"><template #cell="{ record }">{{ formatBitrate(record.bitrateKbps) }}</template></a-table-column>
                <a-table-column title="累计收流" :width="100" align="right"><template #cell="{ record }">{{ formatBytes(record.totalBytes) }}</template></a-table-column>
                <a-table-column title="观看人数" :width="105" align="center">
                    <template #cell="{ record }">
                        <button class="viewer-count" type="button" :aria-label="`查看 ${record.channelId} 的观看连接`" @click.stop="toggleStream(record)">
                            <Users :size="14" /><span>{{ record.viewerCount || 0 }} 人</span><ChevronDown :size="14" :class="{ expanded: expandedKeys.includes(record.channelId) }" />
                        </button>
                    </template>
                </a-table-column>
                <a-table-column title="流状态" :width="85"><template #cell><span class="viewer-online"><i></i>传输中</span></template></a-table-column>
            </template>
            <template #expand-row="{ record }">
                <div class="viewer-detail">
                    <div class="viewer-detail-head"><strong>观看连接明细</strong><span>可单独断开指定观看端，不影响同通道其他连接</span></div>
                    <div v-if="record.viewers?.length" class="viewer-connections">
                        <div v-for="viewer in record.viewers || []" :key="`${viewer.schema}:${viewer.id}`" class="viewer-connection">
                            <div><span>观看来源</span><strong>{{ viewer.remote }}</strong></div>
                            <span class="viewer-online"><i></i>观看中</span>
                            <a-tooltip v-if="canKick && viewer.kickable" content="强退此连接" position="top">
                                <a-button status="danger" type="primary" size="small" :loading="kicking.has(viewer.id)" :aria-label="`强退 ${viewer.remote} 的观看连接`" @click="kick(viewer)">
                                    <template #icon><LogOut :size="15" /></template>强退
                                </a-button>
                            </a-tooltip>
                            <span v-else class="not-kickable">{{ canKick ? '不可强退' : '' }}</span>
                        </div>
                    </div>
                    <a-empty v-else description="暂无可识别的连接明细" />
                </div>
            </template>
            <template #empty><a-empty description="当前没有正在观看的通道" /></template>
        </a-table>
    </section>
</template>

<style scoped>
.viewer-table { min-width: 0; min-height: 420px; }
.viewer-toolbar { display: flex; min-height: 44px; align-items: center; justify-content: space-between; color: var(--color-text-2); font-size: 13px; }
.viewer-refresh { min-width: 72px; height: 32px; }
.viewer-error { display: flex; min-height: 360px; align-items: center; justify-content: center; gap: 12px; color: rgb(var(--danger-6)); }
.viewer-data-table { min-height: 360px; }
.viewer-channel { display: grid; gap: 3px; min-width: 0; }
.viewer-channel strong { overflow: hidden; color: var(--color-text-1); font-size: 13px; text-overflow: ellipsis; white-space: nowrap; }
.viewer-channel span { overflow: hidden; color: var(--color-text-3); font-family: var(--uvp-font-mono); font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }
.viewer-count { display: inline-flex; min-height: 32px; align-items: center; gap: 5px; padding: 4px 8px; border: 0; border-radius: 6px; background: transparent; color: rgb(var(--primary-6)); cursor: pointer; font: inherit; white-space: nowrap; }
.viewer-count:hover { background: var(--color-fill-2); }
.viewer-count:focus-visible { outline: 2px solid rgb(var(--primary-6)); outline-offset: 2px; }
.viewer-count svg:last-child { transition: transform 180ms ease; }
.viewer-count svg:last-child.expanded { transform: rotate(180deg); }
.viewer-online { display: inline-flex; align-items: center; gap: 6px; color: rgb(var(--success-6)); }
.viewer-online i { width: 7px; height: 7px; border-radius: 50%; background: currentColor; }
.viewer-detail { padding: 12px 16px 14px; background: var(--color-fill-1); }
.viewer-detail-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 10px; }
.viewer-detail-head strong { color: var(--color-text-1); font-size: 13px; }
.viewer-detail-head span { color: var(--color-text-3); font-size: 12px; }
.viewer-connections { overflow: hidden; border: 1px solid var(--color-border-2); border-radius: 8px; background: var(--color-bg-2); }
.viewer-connection { display: grid; grid-template-columns: minmax(220px, 1fr) 90px 90px; align-items: center; gap: 12px; min-height: 48px; padding: 7px 12px; border-bottom: 1px solid var(--color-border-1); }
.viewer-connection:last-child { border-bottom: 0; }
.viewer-connection > div { display: grid; gap: 2px; }
.viewer-connection > div span { color: var(--color-text-3); font-size: 11px; }
.viewer-connection > div strong { color: var(--color-text-1); font-family: var(--uvp-font-mono); font-size: 12px; font-weight: 500; }
.not-kickable { color: var(--color-text-3); font-size: 12px; }
@media (max-width: 768px) {
    .viewer-detail-head { align-items: flex-start; flex-direction: column; }
    .viewer-connection { grid-template-columns: 1fr auto; }
    .viewer-connection > div { grid-column: 1 / -1; }
}
@media (prefers-reduced-motion: reduce) { .viewer-table *, .viewer-table *::before, .viewer-table *::after { animation-duration: 0.01ms !important; transition-duration: 0.01ms !important; } }
</style>
