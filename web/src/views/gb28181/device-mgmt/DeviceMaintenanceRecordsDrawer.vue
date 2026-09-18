<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from "vue";
import { AlertTriangle, ArrowLeft, CircleCheck, CircleX, Clock3, History, Loader2, RefreshCcw } from "@lucide/vue";
import {
    listFirmwareUpgrades,
    listMaintenanceOperations,
    type DeviceVO,
    type MaintenanceOperation,
    type UpgradeOperation
} from "./api";

type RecordType = "reboot" | "upgrade";
type RecordItem = MaintenanceOperation | UpgradeOperation;

interface RebootState {
    list: MaintenanceOperation[];
    total: number;
    page: number;
    loading: boolean;
    error: string;
    loaded: boolean;
}

interface UpgradeState {
    list: UpgradeOperation[];
    total: number;
    page: number;
    loading: boolean;
    error: string;
    loaded: boolean;
}

type SelectedRecord =
    | { type: "reboot"; item: MaintenanceOperation }
    | { type: "upgrade"; item: UpgradeOperation };

const props = withDefaults(defineProps<{
    visible: boolean;
    device: DeviceVO | null;
    initialType?: RecordType;
    operationId?: string;
}>(), {
    initialType: "reboot"
});

const emit = defineEmits<{
    "update:visible": [value: boolean];
}>();

const PAGE_SIZE = 10;
const rebootState = reactive<RebootState>({ list: [], total: 0, page: 1, loading: false, error: "", loaded: false });
const upgradeState = reactive<UpgradeState>({ list: [], total: 0, page: 1, loading: false, error: "", loaded: false });
const activeType = ref<RecordType>(props.initialType);
const selectedRecord = ref<SelectedRecord | null>(null);
const deepLinkMessage = ref("");
const deepLinkPending = ref(Boolean(props.operationId));
const requestVersions: Record<RecordType, number> = { reboot: 0, upgrade: 0 };
let contextVersion = 0;

const activeState = computed(() => activeType.value === "reboot" ? rebootState : upgradeState);
const deviceName = computed(() => props.device?.alias?.trim() || props.device?.name?.trim() || "未命名设备");
const deviceVendor = computed(() => [props.device?.manufacturer, props.device?.model].filter(Boolean).join(" / ") || "未上报");
const selectedTimeline = computed(() => {
    const selected = selectedRecord.value;
    if (!selected) return [];
    const timeline = [
        { label: "创建时间", value: selected.item.createdAt },
        { label: "平台发送时间", value: selected.item.sentAt }
    ];
    if (selected.type === "upgrade") timeline.push({ label: "设备受理时间", value: selected.item.acceptedAt });
    timeline.push({ label: "完成 / 结果回报时间", value: selected.item.completedAt });
    return timeline;
});
const selectedTechnical = computed(() => {
    const selected = selectedRecord.value;
    if (!selected) return [];
    const item = selected.item;
    const entries = [
        { label: "操作编号", value: item.operationId },
        { label: "SIP 状态", value: item.sipStatus == null ? "未记录" : String(item.sipStatus) },
        { label: "目标编码", value: selected.type === "reboot" ? selected.item.targetCode || "未记录" : "未记录" }
    ];
    if (selected.type === "upgrade") {
        entries.push({ label: "会话编号", value: selected.item.sessionId || "未记录" });
        entries.push({ label: "错误码", value: selected.item.errorCode || "未记录" });
    }
    return entries;
});

function stateFor(type: RecordType): RebootState | UpgradeState {
    return type === "reboot" ? rebootState : upgradeState;
}

function resetState(state: RebootState | UpgradeState) {
    state.list = [];
    state.total = 0;
    state.page = 1;
    state.loading = false;
    state.error = "";
    state.loaded = false;
}

function invalidateRequests() {
    contextVersion += 1;
    requestVersions.reboot += 1;
    requestVersions.upgrade += 1;
}

function isCurrent(type: RecordType, token: number, version: number, deviceId: number) {
    return token === contextVersion
        && requestVersions[type] === version
        && props.visible
        && props.device?.id === deviceId;
}

function formatDateTime(value?: string | null) {
    if (!value || value.startsWith("0001-01-01")) return "未记录";
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return "未记录";
    const formatter = new Intl.DateTimeFormat("zh-CN", {
        year: "numeric",
        month: "2-digit",
        day: "2-digit",
        hour: "2-digit",
        minute: "2-digit",
        second: "2-digit",
        hourCycle: "h23"
    });
    return formatter.format(date).replace(/\//g, "-");
}

function statusKey(value?: string | null) {
    return String(value || "unknown").toLowerCase();
}

function statusText(type: RecordType, value?: string | null) {
    const key = statusKey(value);
    if (type === "upgrade" && key === "succeeded") return "升级成功";
    return ({
        queued: "排队中",
        pending: "处理中",
        sent: "已发送",
        accepted: "已受理",
        rejected: "已拒绝",
        cancelled: "已取消",
        failed: "失败",
        timeout: "结果未知",
        unknown: "结果未知"
    } as Record<string, string>)[key] || String(value || "结果未知");
}

function statusTone(value?: string | null) {
    const key = statusKey(value);
    if (key === "succeeded") return "success";
    if (["failed", "rejected", "cancelled"].includes(key)) return "danger";
    if (["timeout", "unknown"].includes(key)) return "warning";
    return "pending";
}

function operationLabel(type: RecordType) {
    return type === "reboot" ? "重启设备" : "固件升级";
}

function recordTypeTitle(type: RecordType) {
    return type === "reboot" ? "重启记录" : "升级记录";
}

function countText(state: RebootState | UpgradeState) {
    return state.loaded ? String(state.total) : "—";
}

function actorText(actorId?: number | null) {
    return actorId && actorId > 0 ? `账号 #${actorId}` : "未记录";
}

function failedReasonText(value?: string | null) {
    return ({
        "01": "软件下载超时",
        "02": "升级包损坏",
        "03": "系统异常",
        "99": "其他原因"
    } as Record<string, string>)[String(value || "")] || String(value || "");
}

function rebootSummary(operation: MaintenanceOperation) {
    if (operation.errorMessage?.trim()) return operation.errorMessage;
    const key = statusKey(operation.status);
    if (key === "sent") return "平台已发送请求，设备执行结果未回传";
    if (key === "accepted") return "设备已受理，执行结果待回报";
    if (key === "timeout" || key === "unknown") return "设备执行结果未知，请根据设备状态确认";
    if (operation.sipStatus != null && operation.sipStatus !== 200) return `SIP ${operation.sipStatus}`;
    if (["failed", "rejected", "cancelled"].includes(key)) return "设备未完成此次操作";
    return "-";
}

function upgradeSummary(operation: UpgradeOperation) {
    const reason = failedReasonText(operation.failedReason);
    if (reason && operation.errorMessage?.trim()) return `${reason}：${operation.errorMessage}`;
    if (reason) return reason;
    if (operation.errorMessage?.trim()) return operation.errorMessage;
    const key = statusKey(operation.status);
    if (key === "sent") return "请求已发送，等待设备回报升级结果";
    if (key === "accepted") return "设备已受理，等待升级完成";
    if (key === "succeeded") return operation.currentFirmware ? `设备回报版本 ${operation.currentFirmware}` : "设备已回报升级完成";
    if (key === "unknown" || key === "timeout") return "设备执行结果未知，请根据设备状态确认";
    if (["failed", "rejected"].includes(key)) return "升级未完成";
    return "-";
}

function summaryText(type: RecordType, item: RecordItem) {
    return type === "reboot" ? rebootSummary(item as MaintenanceOperation) : upgradeSummary(item as UpgradeOperation);
}

function failureReason(selected: SelectedRecord) {
    if (selected.type === "upgrade") {
        const reason = failedReasonText(selected.item.failedReason);
        if (reason && selected.item.errorMessage?.trim()) return `${reason}：${selected.item.errorMessage}`;
        return reason || selected.item.errorMessage || "未记录";
    }
    return selected.item.errorMessage?.trim() || "未记录";
}

function selectedVersion(selected: SelectedRecord) {
    return selected.type === "upgrade" ? selected.item.firmware || "未记录" : "不适用";
}

function selectedReportedVersion(selected: SelectedRecord) {
    return selected.type === "upgrade" ? selected.item.currentFirmware || "未回报" : "不适用";
}

function selectedSummary(selected: SelectedRecord) {
    return summaryText(selected.type, selected.item);
}

function selectedStatusText(selected: SelectedRecord) {
    return statusText(selected.type, selected.item.status);
}

function selectedStatusTone(selected: SelectedRecord) {
    return statusTone(selected.item.status);
}

function selectRecord(type: RecordType, item: RecordItem) {
    selectedRecord.value = type === "reboot"
        ? { type: "reboot", item: item as MaintenanceOperation }
        : { type: "upgrade", item: item as UpgradeOperation };
}

function errorText(error: unknown, fallback: string) {
    if (error instanceof Error && error.message) return error.message;
    if (typeof error === "object" && error && "message" in error && typeof error.message === "string") return error.message;
    return fallback;
}

function tryResolveDeepLink(type: RecordType) {
    if (!deepLinkPending.value || !props.operationId || type !== props.initialType || activeType.value !== type) return;
    const state = stateFor(type);
    const match = state.list.find(item => item.operationId === props.operationId);
    if (match) {
        selectRecord(type, match);
        deepLinkMessage.value = "";
        deepLinkPending.value = false;
        return;
    }
    deepLinkMessage.value = state.page === 1
        ? `未在当前页找到操作 ${props.operationId}，请使用分页继续查找。`
        : `当前页未找到操作 ${props.operationId}。`;
}

async function loadPage(type: RecordType, page: number, token = contextVersion, deviceId = props.device?.id) {
    if (typeof deviceId !== "number" || !props.visible) return;
    const state = stateFor(type);
    const version = ++requestVersions[type];
    state.loading = true;
    state.error = "";
    try {
        if (type === "reboot") {
            const result = await listMaintenanceOperations(deviceId, { page, pageSize: PAGE_SIZE });
            if (!isCurrent(type, token, version, deviceId)) return;
            if (result.code !== 0) throw new Error(result.message || "重启记录加载失败");
            rebootState.list = result.data?.list || [];
            rebootState.total = result.data?.total || 0;
            rebootState.page = result.data?.page || page;
            rebootState.loaded = true;
        } else {
            const result = await listFirmwareUpgrades(deviceId, { page, pageSize: PAGE_SIZE });
            if (!isCurrent(type, token, version, deviceId)) return;
            if (result.code !== 0) throw new Error(result.message || "升级记录加载失败");
            upgradeState.list = result.data?.list || [];
            upgradeState.total = result.data?.total || 0;
            upgradeState.page = result.data?.page || page;
            upgradeState.loaded = true;
        }
        const selected = selectedRecord.value;
        if (selected?.type === type) {
            const updated = state.list.find(item => item.operationId === selected.item.operationId);
            if (updated) selectRecord(type, updated);
            else state.error = "当前页未包含这条记录，详情仍为上次读取的结果。";
        }
        tryResolveDeepLink(type);
    } catch (error) {
        if (isCurrent(type, token, version, deviceId)) state.error = errorText(error, type === "reboot" ? "重启记录加载失败" : "升级记录加载失败");
    } finally {
        if (isCurrent(type, token, version, deviceId)) state.loading = false;
    }
}

function setActiveType(type: RecordType) {
    if (activeType.value === type) return;
    activeType.value = type;
    selectedRecord.value = null;
    deepLinkMessage.value = "";
    const state = stateFor(type);
    if (!state.loaded && !state.loading && props.device?.id != null) void loadPage(type, 1);
    else tryResolveDeepLink(type);
}

function refreshCurrent() {
    deepLinkMessage.value = "";
    deepLinkPending.value = false;
    if (props.device?.id != null) void loadPage(activeType.value, activeState.value.page);
}

function retryCurrent() {
    const state = stateFor(activeType.value);
    if (props.device?.id != null) void loadPage(activeType.value, state.page || 1);
}

function changePage(page: number) {
    selectedRecord.value = null;
    if (props.device?.id != null) void loadPage(activeType.value, page);
}

function showDetails(type: RecordType, item: RecordItem) {
    selectRecord(type, item);
}

function closeDetails() {
    selectedRecord.value = null;
}

function handleDrawerVisible(value: boolean) {
    if (!value) {
        invalidateRequests();
        selectedRecord.value = null;
    }
    emit("update:visible", value);
}

function closeDrawer() {
    invalidateRequests();
    selectedRecord.value = null;
    emit("update:visible", false);
}

watch(
    [() => props.visible, () => props.device?.id, () => props.initialType, () => props.operationId],
    ([visible, deviceId, initialType, operationId]) => {
        invalidateRequests();
        activeType.value = initialType === "upgrade" ? "upgrade" : "reboot";
        selectedRecord.value = null;
        deepLinkMessage.value = "";
        deepLinkPending.value = Boolean(operationId);
        resetState(rebootState);
        resetState(upgradeState);
        if (visible && typeof deviceId === "number") void loadPage(activeType.value, 1);
    },
    { immediate: true }
);

onBeforeUnmount(() => invalidateRequests());
</script>

<template>
    <a-drawer
        :visible="visible"
        :width="'min(760px, 100vw)'"
        :footer="false"
        unmount-on-close
        class="maintenance-records-drawer"
        data-testid="maintenance-records-drawer"
        @update:visible="handleDrawerVisible"
        @cancel="closeDrawer"
    >
        <template #title>
            <div class="records-title">
                <span class="records-title-icon"><History :size="18" /></span>
                <div><strong>维护记录</strong><span>{{ deviceName }}</span></div>
            </div>
        </template>

        <div class="records-layout" data-testid="maintenance-records-content">
            <div v-if="!device" class="records-empty" role="status">未选择设备</div>
            <template v-else>
                <section class="records-device-summary" aria-label="当前设备">
                    <div class="device-identity">
                        <strong>{{ deviceName }}</strong>
                        <code>{{ device.deviceId }}</code>
                    </div>
                    <span class="device-status" :class="{ online: device.online }"><i></i>{{ device.online ? '在线' : '离线' }}</span>
                    <dl>
                        <div><dt>厂商 / 型号</dt><dd>{{ deviceVendor }}</dd></div>
                        <div><dt>当前固件</dt><dd class="mono">{{ device.firmware || '未上报' }}</dd></div>
                        <div><dt>通道在线</dt><dd>{{ device.channelOnlineCount }} / {{ device.channelCount }}</dd></div>
                    </dl>
                </section>

                <div class="records-toolbar">
                    <span>只读查看设备操作结果，离线设备也可查看历史。</span>
                    <a-button data-testid="maintenance-records-refresh" class="uvp-refresh-btn" size="small" :loading="activeState.loading" @click="refreshCurrent"><RefreshCcw :size="14" />刷新</a-button>
                </div>

                <div v-if="selectedRecord" class="record-detail" data-testid="maintenance-record-detail">
                    <p v-if="activeState.error" class="records-error" role="alert">刷新未完成，当前显示上次读取的结果。{{ activeState.error }}</p>
                    <button type="button" class="detail-back" data-testid="maintenance-record-detail-back" @click="closeDetails"><ArrowLeft :size="15" />返回记录</button>
                    <header class="detail-heading">
                        <div><h2>{{ operationLabel(selectedRecord.type) }}</h2></div>
                        <span class="record-status" :class="`tone-${selectedStatusTone(selectedRecord)}`"><CircleCheck v-if="selectedStatusTone(selectedRecord) === 'success'" :size="14" /><Clock3 v-else-if="selectedStatusTone(selectedRecord) === 'pending'" :size="14" /><CircleX v-else :size="14" />{{ selectedStatusText(selectedRecord) }}</span>
                    </header>

                    <dl class="detail-facts">
                        <div><dt>操作人</dt><dd>{{ actorText(selectedRecord.item.actorId) }}</dd></div>
                        <div><dt>摘要</dt><dd>{{ selectedSummary(selectedRecord) }}</dd></div>
                        <div v-if="selectedRecord.type === 'upgrade'"><dt>目标版本</dt><dd class="mono">{{ selectedVersion(selectedRecord) }}</dd></div>
                        <div v-if="selectedRecord.type === 'upgrade'"><dt>设备回报版本</dt><dd class="mono">{{ selectedReportedVersion(selectedRecord) }}</dd></div>
                        <div v-if="['failed', 'rejected', 'cancelled'].includes(selectedRecord.item.status)"><dt>失败原因</dt><dd>{{ failureReason(selectedRecord) }}</dd></div>
                    </dl>

                    <section class="detail-section">
                        <div class="detail-section-heading"><strong>操作时间线</strong></div>
                        <ol class="detail-timeline">
                            <li v-for="node in selectedTimeline" :key="node.label"><span class="timeline-dot"></span><div><strong>{{ node.label }}</strong><time>{{ formatDateTime(node.value) }}</time></div></li>
                        </ol>
                    </section>

                    <details class="technical-details" data-testid="maintenance-record-technical">
                        <summary>技术信息</summary>
                        <dl>
                            <div v-for="entry in selectedTechnical" :key="entry.label"><dt>{{ entry.label }}</dt><dd class="mono">{{ entry.value }}</dd></div>
                        </dl>
                    </details>
                </div>

                <template v-else>
                    <div class="record-tabs" role="tablist" aria-label="维护记录类型">
                        <button type="button" role="tab" :aria-selected="activeType === 'reboot'" :class="{ active: activeType === 'reboot' }" data-testid="maintenance-records-tab-reboot" @click="setActiveType('reboot')">{{ recordTypeTitle('reboot') }} <span>{{ countText(rebootState) }}</span></button>
                        <button type="button" role="tab" :aria-selected="activeType === 'upgrade'" :class="{ active: activeType === 'upgrade' }" data-testid="maintenance-records-tab-upgrade" @click="setActiveType('upgrade')">{{ recordTypeTitle('upgrade') }} <span>{{ countText(upgradeState) }}</span></button>
                    </div>

                    <section class="record-list-panel" :aria-label="recordTypeTitle(activeType)">
                        <div class="list-heading"><div><strong>{{ recordTypeTitle(activeType) }}</strong><span>共 {{ countText(activeState) }} 条</span></div><span v-if="activeState.loading && activeState.list.length" class="loading-inline"><Loader2 :size="14" class="spin" />正在刷新</span></div>
                        <div v-if="deepLinkMessage" class="deep-link-note" role="status"><AlertTriangle :size="15" />{{ deepLinkMessage }}</div>
                        <div v-if="activeState.error" class="records-error" role="alert"><CircleX :size="16" /><div><strong>记录加载失败</strong><span>{{ activeState.error }}</span><button type="button" data-testid="maintenance-records-retry" @click="retryCurrent">重试</button></div></div>
                        <div v-else-if="activeState.loading && !activeState.list.length" class="records-state" role="status"><Loader2 :size="17" class="spin" />正在加载{{ recordTypeTitle(activeType) }}</div>
                        <div v-else-if="!activeState.list.length" class="records-state" role="status">暂无{{ recordTypeTitle(activeType) }}</div>
                        <div v-else class="record-table" role="table">
                            <div class="record-row record-row-head" role="row"><span>时间</span><span>操作</span><span>操作人</span><span>结果</span><span>摘要</span></div>
                            <button v-for="item in activeState.list" :key="item.operationId" type="button" class="record-row record-row-item" role="row" :data-testid="`maintenance-record-${activeType}-row`" @click="showDetails(activeType, item)">
                                <time :title="formatDateTime(item.createdAt)">{{ formatDateTime(item.createdAt) }}</time>
                                <strong>{{ operationLabel(activeType) }}</strong>
                                <span>{{ actorText(item.actorId) }}</span>
                                <span class="record-status" :class="`tone-${statusTone(item.status)}`">{{ statusText(activeType, item.status) }}</span>
                                <span class="record-summary" :title="summaryText(activeType, item)">{{ summaryText(activeType, item) }}</span>
                            </button>
                        </div>
                        <footer v-if="activeState.total > 0" class="records-pagination"><a-pagination :current="activeState.page" :page-size="PAGE_SIZE" :total="activeState.total" :loading="activeState.loading" show-jumper @change="changePage" /></footer>
                    </section>
                </template>
            </template>
        </div>
    </a-drawer>
</template>

<style scoped>
.records-layout { display: flex; min-height: 100%; flex-direction: column; gap: 12px; color: var(--uvp-text-primary); }
.records-title { display: flex; min-width: 0; gap: 9px; align-items: center; }
.records-title-icon { display: grid; width: 32px; height: 32px; place-items: center; color: var(--uvp-brand); background: var(--uvp-brand-soft); border-radius: 8px; }
.records-title > div:last-child { display: grid; min-width: 0; gap: 1px; }
.records-title strong { font-size: 15px; }
.records-title span:last-child { overflow: hidden; color: var(--uvp-text-tertiary); font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.records-device-summary, .record-list-panel, .record-detail { padding: 14px; border: 1px solid var(--uvp-panel-border); border-radius: 12px; background: var(--uvp-panel-bg); }
.records-device-summary { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 10px; }
.device-identity { display: grid; min-width: 0; gap: 3px; }
.device-identity strong, .device-identity code { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.device-identity code, .mono { color: var(--uvp-text-tertiary); font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 11px; }
.device-status { display: inline-flex; height: fit-content; gap: 5px; align-items: center; padding: 4px 9px; color: var(--uvp-danger); background: var(--uvp-danger-soft); border: 1px solid var(--uvp-danger-border); border-radius: 999px; font-size: 12px; white-space: nowrap; }
.device-status.online { color: var(--uvp-brand-cyan); background: color-mix(in srgb, var(--uvp-brand-cyan) 10%, transparent); border-color: color-mix(in srgb, var(--uvp-brand-cyan) 26%, transparent); }
.device-status i { width: 6px; height: 6px; border-radius: 50%; background: currentColor; }
.records-device-summary dl { display: grid; grid-column: 1 / -1; grid-template-columns: 1.4fr 1fr .7fr; gap: 10px; padding-top: 11px; margin: 0; border-top: 1px solid var(--uvp-panel-border); }
.records-device-summary dl div, .detail-facts div, .technical-details dl div { display: grid; min-width: 0; gap: 3px; }
dt { color: var(--uvp-text-tertiary); font-size: 11px; }
dd { overflow: hidden; margin: 0; text-overflow: ellipsis; white-space: nowrap; }
.records-device-summary dd { font-size: 13px; }
.records-toolbar { display: flex; min-height: 32px; align-items: center; justify-content: space-between; gap: 10px; color: var(--uvp-text-tertiary); font-size: 12px; }
.records-toolbar :deep(.arco-btn) { display: inline-flex; gap: 5px; align-items: center; }
.record-tabs { display: flex; gap: 4px; border-bottom: 1px solid var(--uvp-panel-border); }
.record-tabs button { position: relative; display: inline-flex; min-height: 38px; gap: 6px; align-items: center; padding: 0 13px; color: var(--uvp-text-secondary); background: transparent; border: 0; cursor: pointer; font: inherit; }
.record-tabs button:hover { color: var(--uvp-brand); }
.record-tabs button.active { color: var(--uvp-brand); font-weight: 650; }
.record-tabs button.active::after { position: absolute; right: 10px; bottom: -1px; left: 10px; height: 2px; background: var(--uvp-brand); border-radius: 2px; content: ""; }
.record-tabs span { min-width: 19px; padding: 1px 5px; color: var(--uvp-text-tertiary); background: var(--uvp-bg-secondary); border-radius: 999px; font-size: 11px; }
.record-list-panel { min-height: 320px; }
.list-heading, .detail-section-heading { display: flex; align-items: center; justify-content: space-between; gap: 10px; margin-bottom: 12px; }
.list-heading > div { display: flex; gap: 10px; align-items: baseline; }
.list-heading span, .detail-section-heading span { color: var(--uvp-text-tertiary); font-size: 11px; }
.loading-inline { display: inline-flex; gap: 5px; align-items: center; }
.record-table { overflow: hidden; border: 1px solid var(--uvp-panel-border); border-radius: 8px; }
.record-row { display: grid; width: 100%; grid-template-columns: minmax(125px, 1.1fr) minmax(85px, .8fr) minmax(76px, .7fr) minmax(66px, .6fr) minmax(130px, 1.4fr); gap: 10px; align-items: center; padding: 11px 12px; text-align: left; }
.record-row-head { color: var(--uvp-text-tertiary); background: var(--uvp-bg-secondary); font-size: 11px; }
.record-row-item { color: var(--uvp-text-secondary); background: transparent; border: 0; border-top: 1px solid var(--uvp-panel-border); cursor: pointer; font: inherit; }
.record-row-item:hover { background: var(--uvp-brand-soft); }
.record-row-item time { color: var(--uvp-text-tertiary); font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 11px; }
.record-row-item strong { overflow: hidden; color: var(--uvp-text-primary); text-overflow: ellipsis; white-space: nowrap; }
.record-summary { overflow: hidden; color: var(--uvp-text-tertiary); text-overflow: ellipsis; white-space: nowrap; }
.record-status { display: inline-flex; width: fit-content; gap: 4px; align-items: center; font-size: 12px; white-space: nowrap; }
.record-status.tone-success { color: var(--uvp-success); }
.record-status.tone-pending { color: var(--uvp-warning); }
.record-status.tone-warning { color: var(--uvp-warning); }
.record-status.tone-danger { color: var(--uvp-danger); }
.records-state, .records-empty { display: flex; min-height: 220px; gap: 8px; align-items: center; justify-content: center; color: var(--uvp-text-tertiary); }
.records-error, .deep-link-note { display: flex; gap: 8px; align-items: flex-start; padding: 11px 12px; border-radius: 8px; font-size: 12px; }
.records-error { color: var(--uvp-danger); background: var(--uvp-danger-soft); border: 1px solid var(--uvp-danger-border); }
.records-error > div { display: grid; gap: 4px; }
.records-error span { color: var(--uvp-text-secondary); }
.records-error button { width: fit-content; padding: 0; color: var(--uvp-danger); background: transparent; border: 0; cursor: pointer; font: inherit; text-decoration: underline; }
.deep-link-note { margin-bottom: 10px; color: var(--uvp-warning); background: var(--uvp-warning-soft); border: 1px solid var(--uvp-warning-border); }
.records-pagination { display: flex; justify-content: flex-end; margin-top: 13px; }
.detail-back { display: inline-flex; gap: 5px; align-items: center; padding: 0; margin-bottom: 14px; color: var(--uvp-text-secondary); background: transparent; border: 0; cursor: pointer; font: inherit; font-size: 12px; }
.detail-back:hover { color: var(--uvp-brand); }
.detail-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 10px; padding-bottom: 13px; border-bottom: 1px solid var(--uvp-panel-border); }
.detail-heading > div { display: grid; min-width: 0; gap: 4px; }
.detail-heading h2 { margin: 0; font-size: 17px; }
.detail-heading code { overflow: hidden; color: var(--uvp-text-tertiary); text-overflow: ellipsis; white-space: nowrap; }
.record-type-label { color: var(--uvp-brand); font-size: 11px; font-weight: 650; }
.detail-facts { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; padding: 14px 0; margin: 0; border-bottom: 1px solid var(--uvp-panel-border); }
.detail-facts dd { color: var(--uvp-text-secondary); font-size: 13px; white-space: normal; }
.detail-section { padding-top: 15px; }
.detail-section-heading { margin-bottom: 6px; }
.detail-timeline { display: grid; gap: 0; padding: 0; margin: 0; list-style: none; }
.detail-timeline li { position: relative; display: flex; gap: 9px; min-height: 44px; }
.detail-timeline li:not(:last-child)::before { position: absolute; top: 14px; bottom: 0; left: 4px; width: 1px; background: var(--uvp-panel-border); content: ""; }
.timeline-dot { z-index: 1; flex: 0 0 auto; width: 9px; height: 9px; margin-top: 4px; background: var(--uvp-brand); border: 2px solid var(--uvp-panel-bg); border-radius: 50%; box-shadow: 0 0 0 1px var(--uvp-brand); }
.detail-timeline li > div { display: grid; gap: 2px; }
.detail-timeline strong { font-size: 12px; }
.detail-timeline time { color: var(--uvp-text-secondary); font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 12px; }
.technical-details { padding-top: 13px; margin-top: 8px; border-top: 1px solid var(--uvp-panel-border); }
.technical-details summary { color: var(--uvp-text-secondary); cursor: pointer; font-size: 12px; }
.technical-details dl { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; padding-top: 12px; margin: 0; }
.technical-details dd { color: var(--uvp-text-secondary); }
.spin { animation: maintenance-record-spin 1s linear infinite; }
@keyframes maintenance-record-spin { to { transform: rotate(360deg); } }
@media (max-width: 600px) {
    .records-device-summary dl { grid-template-columns: 1fr 1fr; }
    .records-device-summary dl div:last-child { grid-column: 1 / -1; }
    .records-toolbar { align-items: flex-start; }
    .record-row { grid-template-columns: 1fr 1fr; gap: 5px 9px; }
    .record-row-head { display: none; }
    .record-row-item > :first-child { grid-column: 1 / -1; }
    .record-row-item > :last-child { grid-column: 1 / -1; }
    .detail-facts, .technical-details dl { grid-template-columns: 1fr; }
}
@media (prefers-reduced-motion: reduce) { .spin { animation: none; } }
</style>
