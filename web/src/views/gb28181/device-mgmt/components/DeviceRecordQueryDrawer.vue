<script setup lang="ts">
import { computed, onUnmounted, reactive, ref, watch } from "vue";
import {
    AlertTriangle,
    CheckCircle2,
    ChevronDown,
    ChevronUp,
    Clock3,
    Database,
    FileVideo2,
    Loader2,
    RefreshCw,
    Search,
    Server,
    StopCircle,
    Video,
    WifiOff
} from "@lucide/vue";
import { getRecordQueryOptions, queryDeviceRecords, type ChannelVO, type RecordQueryOptions, type RecordQueryResult } from "../api";
import {
    createDefaultRecordQueryForm,
    mapRecordQueryError,
    paginateRecordQueryItems,
    recordQueryRequestTypes,
    recordQueryTypeText,
    serializeRecordQueryForm,
    sortRecordQueryItems,
    validateRecordQueryForm,
    type RecordQueryForm,
    type RecordQueryUiState,
    type RecordQueryValidationErrors
} from "../recordQueryState";

const props = defineProps<{
    visible: boolean;
    channel: ChannelVO | null;
}>();

const emit = defineEmits<{
    "update:visible": [value: boolean];
}>();

const options = ref<RecordQueryOptions | null>(null);
const optionsLoading = ref(false);
const state = ref<RecordQueryUiState>("idle");
const errorMessage = ref("");
const result = ref<RecordQueryResult | null>(null);
const advancedVisible = ref(false);
const page = ref(1);
const pageSize = ref(20);
const form = reactive<RecordQueryForm>({ startTime: "", endTime: "", type: "all", secrecy: 0, recorderId: "" });
const errors = reactive<RecordQueryValidationErrors>({});
let optionsToken = 0;
let queryToken = 0;
let queryController: AbortController | null = null;

const timeRange = computed<[string, string]>({
    get: (): [string, string] => [form.startTime, form.endTime],
    set: (value: [string, string]) => {
        form.startTime = value?.[0] || "";
        form.endTime = value?.[1] || "";
    }
});
const sortedItems = computed(() => sortRecordQueryItems(result.value?.list || []));
const visibleItems = computed(() => paginateRecordQueryItems(sortedItems.value, page.value, pageSize.value));
const hasResults = computed(() => sortedItems.value.length > 0);
const queryDisabled = computed(() => optionsLoading.value || !options.value || state.value === "querying");
const contextChannelName = computed(() => options.value?.channel.name || props.channel?.alias || props.channel?.name || "当前通道");
const stateMeta = computed(() => ({
    idle: { label: "等待查询", detail: "选择时间范围后查询设备本地录像目录", tone: "neutral", icon: Search },
    querying: { label: "正在查询设备", detail: `设备可能需要 ${options.value?.timeoutSeconds || 15} 秒返回录像目录`, tone: "info", icon: Loader2 },
    complete: { label: "查询完成", detail: `已收到 ${result.value?.receivedCount || 0} 条完整录像目录`, tone: "success", icon: CheckCircle2 },
    empty: { label: "未查询到录像", detail: "设备已明确返回当前条件下没有录像", tone: "neutral", icon: Database },
    partial: { label: "结果可能不完整", detail: `设备声明 ${result.value?.declaredTotal || 0} 条，当前收到 ${result.value?.receivedCount || 0} 条`, tone: "warning", icon: AlertTriangle },
    timeout: { label: "查询超时", detail: errorMessage.value || "设备未返回有效的录像目录，可保持条件重试", tone: "danger", icon: Clock3 },
    offline: { label: "设备离线", detail: errorMessage.value || "所属设备当前离线，无法查询录像目录", tone: "danger", icon: WifiOff },
    error: { label: "查询失败", detail: errorMessage.value || "录像查询未完成，请稍后重试", tone: "danger", icon: AlertTriangle }
})[state.value]);

watch(() => [props.visible, props.channel?.id] as const, ([visible, channelId], previous) => {
    if (!visible || !channelId) {
        if (previous?.[0]) cancelInFlight(false);
        return;
    }
    void loadContext(channelId);
}, { immediate: true });

onUnmounted(() => cancelInFlight(false));

async function loadContext(channelId: number) {
    cancelInFlight(false);
    const token = ++optionsToken;
    optionsLoading.value = true;
    options.value = null;
    result.value = null;
    state.value = "idle";
    errorMessage.value = "";
    page.value = 1;
    clearErrors();
    try {
        const response = await getRecordQueryOptions(channelId);
        if (token !== optionsToken || !props.visible || props.channel?.id !== channelId) return;
        if (response.code !== 0) throw new Error(response.message || "查询上下文加载失败");
        options.value = response.data;
        Object.assign(form, createDefaultRecordQueryForm(response.data));
    } catch (error) {
        if (token !== optionsToken) return;
        const mapped = mapRecordQueryError(error);
        state.value = mapped.state;
        errorMessage.value = mapped.message;
    } finally {
        if (token === optionsToken) optionsLoading.value = false;
    }
}

async function submitQuery() {
    if (!props.channel || !options.value || state.value === "querying") return;
    clearErrors();
    Object.assign(errors, validateRecordQueryForm(form, options.value));
    if (Object.keys(errors).length) return;

    queryController?.abort();
    const controller = new AbortController();
    queryController = controller;
    const token = ++queryToken;
    state.value = "querying";
    errorMessage.value = "";
    result.value = null;
    page.value = 1;
    try {
        const response = await queryDeviceRecords(props.channel.id, serializeRecordQueryForm(form), controller.signal);
        if (token !== queryToken || controller.signal.aborted || !props.visible) return;
        if (response.code !== 0) throw new Error(response.message || "查询失败");
        result.value = { ...response.data, list: sortRecordQueryItems(response.data.list || []) };
        state.value = response.data.status;
    } catch (error) {
        if (token !== queryToken || controller.signal.aborted || (error as { name?: string })?.name === "AbortError") return;
        const mapped = mapRecordQueryError(error);
        state.value = mapped.state;
        errorMessage.value = mapped.message;
    } finally {
        if (token === queryToken) queryController = null;
    }
}

function cancelInFlight(resetState = true) {
    optionsToken += 1;
    queryToken += 1;
    queryController?.abort();
    queryController = null;
    if (resetState && state.value === "querying") state.value = "idle";
}

function closeDrawer() {
    cancelInFlight(false);
    emit("update:visible", false);
}

function handleDrawerVisible(value: boolean) {
    if (!value) closeDrawer();
}

function handlePageChange(value: number) {
    page.value = value;
}

function clearErrors() {
    for (const key of Object.keys(errors) as Array<keyof RecordQueryValidationErrors>) delete errors[key];
}

function displayDateTime(value: string | null | undefined) {
    return value ? value.replace("T", " ").replace(/([+-]\d{2}:\d{2}|Z)$/, "") : "未知";
}

function durationText(start: string | null, end: string | null) {
    if (!start || !end) return "未知";
    const seconds = Math.max(0, Math.floor((Date.parse(end) - Date.parse(start)) / 1000));
    if (!Number.isFinite(seconds)) return "未知";
    const hours = Math.floor(seconds / 3600);
    const minutes = Math.floor((seconds % 3600) / 60);
    const remain = seconds % 60;
    return hours ? `${hours}小时${minutes}分` : minutes ? `${minutes}分${remain}秒` : `${remain}秒`;
}

function fileSizeText(value: number | null) {
    if (value == null || value < 0) return "未知";
    if (value >= 1024 ** 3) return `${(value / 1024 ** 3).toFixed(2)} GB`;
    if (value >= 1024 ** 2) return `${(value / 1024 ** 2).toFixed(1)} MB`;
    return `${Math.round(value / 1024)} KB`;
}
</script>

<template>
    <a-drawer
        :visible="visible"
        width="min(1120px, 94vw)"
        :footer="false"
        unmount-on-close
        class="record-query-drawer"
        @update:visible="handleDrawerVisible"
        @cancel="closeDrawer"
    >
        <template #title>
            <div class="drawer-title">
                <span class="drawer-title-icon"><FileVideo2 :size="18" /></span>
                <div>
                    <strong>设备录像</strong>
                    <span>{{ contextChannelName }}</span>
                </div>
            </div>
        </template>

        <a-spin :loading="optionsLoading" class="record-query-spin">
            <div class="record-query-layout" data-testid="record-query-drawer">
                <section v-if="options" class="context-strip" aria-label="当前查询对象">
                    <div class="context-main">
                        <span class="context-icon"><Server :size="17" /></span>
                        <div><span>所属设备</span><strong>{{ options.device.name }}</strong><code>{{ options.device.code }}</code></div>
                    </div>
                    <div class="context-main">
                        <span class="context-icon channel"><Video :size="17" /></span>
                        <div><span>查询通道</span><strong>{{ options.channel.name }}</strong><code>{{ options.channel.code }}</code></div>
                    </div>
                    <div class="context-meta"><span>平台时区</span><strong>{{ options.timezone }}</strong></div>
                    <div class="context-meta"><span>最长跨度</span><strong>{{ options.maxRangeHours }} 小时</strong></div>
                </section>

                <section class="query-section" aria-labelledby="record-query-condition-title">
                    <div class="section-heading">
                        <div><strong id="record-query-condition-title">查询条件</strong><span>时间按平台时区 {{ options?.timezone || '-' }} 发送</span></div>
                        <button type="button" class="advanced-toggle" @click="advancedVisible = !advancedVisible">
                            <ChevronUp v-if="advancedVisible" :size="15" />
                            <ChevronDown v-else :size="15" />
                            高级条件
                        </button>
                    </div>
                    <div class="query-form">
                        <label class="field range-field">
                            <span>录像时间</span>
                            <a-range-picker
                                v-model="timeRange"
                                show-time
                                value-format="YYYY-MM-DDTHH:mm:ss"
                                format="YYYY-MM-DD HH:mm:ss"
                                :allow-clear="false"
                                :disabled="state === 'querying'"
                            />
                            <small v-if="errors.startTime || errors.endTime" class="field-error">{{ errors.startTime || errors.endTime }}</small>
                            <small v-else class="field-value">{{ displayDateTime(form.startTime) }} 至 {{ displayDateTime(form.endTime) }}</small>
                        </label>
                        <label class="field type-field">
                            <span>录像类型</span>
                            <a-select v-model="form.type" :disabled="state === 'querying'">
                                <a-option v-for="item in recordQueryRequestTypes.filter(item => options?.supportedTypes.includes(item.value))" :key="item.value" :value="item.value">{{ item.label }}</a-option>
                            </a-select>
                            <small v-if="errors.type" class="field-error">{{ errors.type }}</small>
                        </label>
                        <div class="query-actions">
                            <a-button
                                v-if="state === 'querying'"
                                data-testid="record-query-cancel"
                                @click="cancelInFlight()"
                            >
                                <template #icon><StopCircle :size="15" /></template>取消查询
                            </a-button>
                            <a-button
                                type="primary"
                                :loading="state === 'querying'"
                                :disabled="queryDisabled"
                                data-testid="record-query-submit"
                                @click="submitQuery"
                            >
                                <template #icon><RefreshCw v-if="result || state === 'timeout' || state === 'error'" :size="15" /><Search v-else :size="15" /></template>
                                {{ result || ['timeout', 'offline', 'error'].includes(state) ? '重新查询' : '查询录像' }}
                            </a-button>
                        </div>
                    </div>
                    <div v-if="advancedVisible" class="advanced-fields">
                        <label class="field"><span>保密属性</span><a-input-number v-model="form.secrecy" :min="0" :precision="0" /><small v-if="errors.secrecy" class="field-error">{{ errors.secrecy }}</small></label>
                        <label class="field"><span>录像机标识</span><a-input v-model="form.recorderId" allow-clear placeholder="设备有明确要求时填写" /></label>
                    </div>
                </section>

                <section class="result-section" aria-labelledby="record-query-result-title">
                    <div class="section-heading result-heading">
                        <div><strong id="record-query-result-title">查询结果</strong><span v-if="result">耗时 {{ (result.elapsedMs / 1000).toFixed(1) }} 秒 · 本地分页</span></div>
                        <div v-if="result" class="result-counts"><span>设备声明 <b>{{ result.declaredTotal }}</b></span><span>平台收到 <b>{{ result.receivedCount }}</b></span></div>
                    </div>

                    <div class="status-band" :class="`tone-${stateMeta.tone}`" role="status">
                        <component :is="stateMeta.icon" :size="18" :class="{ spin: state === 'querying' }" />
                        <div><strong>{{ stateMeta.label }}</strong><span>{{ stateMeta.detail }}</span></div>
                    </div>

                    <div v-if="hasResults" class="result-table-wrap">
                        <a-table :data="visibleItems" :pagination="false" row-key="filePath" data-testid="result-table" class="record-result-table">
                            <template #columns>
                                <a-table-column title="录像名称" :width="190"><template #cell="{ record }"><a-tooltip :content="record.name || '未知'"><span class="ellipsis">{{ record.name || '未知' }}</span></a-tooltip></template></a-table-column>
                                <a-table-column title="开始时间" :width="170"><template #cell="{ record }"><span class="mono">{{ displayDateTime(record.startTime) }}</span></template></a-table-column>
                                <a-table-column title="结束时间" :width="170"><template #cell="{ record }"><span class="mono">{{ displayDateTime(record.endTime) }}</span></template></a-table-column>
                                <a-table-column title="时长" :width="100"><template #cell="{ record }">{{ durationText(record.startTime, record.endTime) }}</template></a-table-column>
                                <a-table-column title="类型" :width="100"><template #cell="{ record }"><span class="type-badge">{{ recordQueryTypeText(record.type) }}</span></template></a-table-column>
                                <a-table-column title="大小" :width="100"><template #cell="{ record }">{{ fileSizeText(record.fileSize) }}</template></a-table-column>
                                <a-table-column title="文件路径" :width="230"><template #cell="{ record }"><a-tooltip :content="record.filePath || '未知'"><code class="ellipsis">{{ record.filePath || '未知' }}</code></a-tooltip></template></a-table-column>
                                <a-table-column title="存储位置" :width="210"><template #cell="{ record }"><a-tooltip :content="record.recordLocation || '未知'"><code class="ellipsis">{{ record.recordLocation || '未知' }}</code></a-tooltip></template></a-table-column>
                            </template>
                        </a-table>
                    </div>
                    <div v-else-if="state !== 'querying'" class="result-placeholder">
                        <Database :size="28" />
                        <span>{{ state === 'empty' ? '当前条件下没有设备录像' : '查询后在这里查看设备返回的录像目录' }}</span>
                    </div>
                    <a-pagination
                        v-if="sortedItems.length > pageSize"
                        :current="page"
                        :page-size="pageSize"
                        :total="sortedItems.length"
                        show-total
                        data-testid="result-pagination"
                        class="result-pagination"
                        @change="handlePageChange"
                    />
                </section>
            </div>
        </a-spin>
    </a-drawer>
</template>

<style scoped>
.record-query-spin, .record-query-layout { min-height: 100%; }
.record-query-layout { display: flex; flex-direction: column; gap: 0; color: var(--uvp-text-primary); }
.drawer-title { display: flex; align-items: center; gap: 10px; min-width: 0; }
.drawer-title-icon { display: grid; width: 34px; height: 34px; place-items: center; color: var(--uvp-primary); background: color-mix(in srgb, var(--uvp-primary) 12%, transparent); border-radius: 6px; }
.drawer-title > div:last-child { display: flex; flex-direction: column; min-width: 0; }
.drawer-title strong { font-size: 15px; line-height: 20px; }
.drawer-title span:last-child { overflow: hidden; color: var(--uvp-text-tertiary); font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.context-strip { display: grid; grid-template-columns: minmax(210px, 1fr) minmax(210px, 1fr) 140px 110px; gap: 18px; align-items: center; padding: 14px 18px; background: var(--uvp-bg-secondary); border-bottom: 1px solid var(--uvp-border); }
.context-main { display: flex; gap: 10px; align-items: center; min-width: 0; }
.context-icon { display: grid; flex: 0 0 34px; width: 34px; height: 34px; place-items: center; color: var(--uvp-primary); background: color-mix(in srgb, var(--uvp-primary) 10%, transparent); border-radius: 6px; }
.context-icon.channel { color: var(--uvp-success); background: color-mix(in srgb, var(--uvp-success) 10%, transparent); }
.context-main > div, .context-meta { display: flex; flex-direction: column; min-width: 0; }
.context-main span, .context-meta span { color: var(--uvp-text-tertiary); font-size: 11px; line-height: 16px; }
.context-main strong, .context-meta strong { overflow: hidden; font-size: 13px; line-height: 20px; text-overflow: ellipsis; white-space: nowrap; }
.context-main code { overflow: hidden; color: var(--uvp-text-secondary); font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }
.query-section, .result-section { padding: 18px; }
.query-section { border-bottom: 1px solid var(--uvp-border); }
.section-heading { display: flex; justify-content: space-between; gap: 16px; align-items: center; margin-bottom: 14px; }
.section-heading > div:first-child { display: flex; gap: 10px; align-items: baseline; min-width: 0; }
.section-heading strong { font-size: 14px; }
.section-heading span { color: var(--uvp-text-tertiary); font-size: 12px; }
.advanced-toggle { display: inline-flex; min-height: 32px; gap: 5px; align-items: center; padding: 0 8px; color: var(--uvp-text-secondary); background: transparent; border: 0; border-radius: 4px; cursor: pointer; }
.advanced-toggle:hover { color: var(--uvp-primary); background: color-mix(in srgb, var(--uvp-primary) 8%, transparent); }
.advanced-toggle:focus-visible { outline: 2px solid var(--uvp-primary); outline-offset: 2px; }
.query-form { display: grid; grid-template-columns: minmax(390px, 1fr) 170px auto; gap: 14px; align-items: end; }
.advanced-fields { display: grid; grid-template-columns: 180px minmax(240px, 1fr); gap: 14px; max-width: 580px; padding-top: 14px; }
.field { display: flex; flex-direction: column; gap: 6px; min-width: 0; color: var(--uvp-text-secondary); font-size: 12px; }
.field-value { overflow: hidden; color: var(--uvp-text-tertiary); font-family: ui-monospace, SFMono-Regular, Menlo, monospace; text-overflow: ellipsis; white-space: nowrap; }
.field-error { color: var(--uvp-danger); }
.query-actions { display: flex; gap: 8px; justify-content: flex-end; padding-bottom: 22px; }
.result-section { min-height: 330px; }
.result-heading { margin-bottom: 12px; }
.result-counts { display: flex; gap: 16px; white-space: nowrap; }
.result-counts b { color: var(--uvp-text-primary); font-size: 14px; }
.status-band { display: flex; gap: 10px; align-items: center; min-height: 54px; padding: 10px 14px; margin-bottom: 12px; border: 1px solid var(--uvp-border); border-left-width: 3px; border-radius: 5px; }
.status-band > div { display: flex; flex-direction: column; gap: 2px; }
.status-band strong { font-size: 13px; }
.status-band span { color: var(--uvp-text-secondary); font-size: 12px; }
.tone-neutral { color: var(--uvp-text-secondary); background: var(--uvp-bg-secondary); }
.tone-info { color: var(--uvp-primary); background: color-mix(in srgb, var(--uvp-primary) 7%, var(--uvp-bg)); border-left-color: var(--uvp-primary); }
.tone-success { color: var(--uvp-success); background: color-mix(in srgb, var(--uvp-success) 7%, var(--uvp-bg)); border-left-color: var(--uvp-success); }
.tone-warning { color: var(--uvp-warning); background: color-mix(in srgb, var(--uvp-warning) 8%, var(--uvp-bg)); border-left-color: var(--uvp-warning); }
.tone-danger { color: var(--uvp-danger); background: color-mix(in srgb, var(--uvp-danger) 7%, var(--uvp-bg)); border-left-color: var(--uvp-danger); }
.result-table-wrap { overflow-x: auto; border: 1px solid var(--uvp-border); border-radius: 5px; }
.record-result-table { min-width: 1180px; }
.ellipsis { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.mono, code { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 11px; }
.type-badge { display: inline-flex; align-items: center; min-height: 24px; padding: 0 7px; color: var(--uvp-text-secondary); background: var(--uvp-bg-tertiary); border-radius: 4px; }
.result-placeholder { display: flex; min-height: 190px; flex-direction: column; gap: 10px; align-items: center; justify-content: center; color: var(--uvp-text-tertiary); border: 1px dashed var(--uvp-border); border-radius: 5px; }
.result-pagination { display: flex; justify-content: flex-end; margin-top: 14px; }
.spin { animation: record-query-spin 1s linear infinite; }
@keyframes record-query-spin { to { transform: rotate(360deg); } }
@media (max-width: 900px) {
    .context-strip { grid-template-columns: 1fr 1fr; }
    .query-form { grid-template-columns: 1fr 150px; }
    .query-actions { grid-column: 1 / -1; padding-bottom: 0; }
}
@media (max-width: 600px) {
    .context-strip { grid-template-columns: 1fr; gap: 10px; padding: 12px; }
    .context-meta { display: none; }
    .query-section, .result-section { padding: 14px 12px; }
    .section-heading, .section-heading > div:first-child { align-items: flex-start; }
    .section-heading > div:first-child { flex-direction: column; gap: 2px; }
    .query-form, .advanced-fields { grid-template-columns: 1fr; max-width: none; }
    .query-actions { justify-content: stretch; }
    .query-actions :deep(.arco-btn) { min-width: 0; min-height: 44px; flex: 1; }
    .advanced-toggle { min-height: 44px; }
    .result-heading { align-items: flex-start; }
    .result-counts { flex-direction: column; gap: 2px; text-align: right; }
}
@media (prefers-reduced-motion: reduce) { .spin { animation: none; } }
</style>
