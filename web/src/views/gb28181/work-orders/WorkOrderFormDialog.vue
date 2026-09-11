<script setup lang="ts">
import { computed, ref, watch, type Component } from "vue";
import { Message } from "@arco-design/web-vue";
import { AlertTriangle, CheckCircle2, FileText, Info, SlidersHorizontal, UserRound, Video } from "lucide-vue-next";
import { createWorkOrder, listWorkOrderFormHistory, type WorkOrderFormHistoryEntry, type WorkOrderSnapshot, type WorkRecordingForm } from "@/api/gb28181-work-recording";
import {
    missingWorkOrderFields,
    normalizeWorkOrderForm,
    workOrderFieldLabels,
    workOrderFormSections,
    workOrderLongFields,
    workOrderPersonnelMax,
    workOrderRequiredFields
} from "./orderState";

export interface WorkOrderChannelOption {
    id: number;
    label: string;
}

const props = defineProps<{
    visible: boolean;
    /** 本次将录制的通道（正在播放的画面），由调用方派生。 */
    channels: WorkOrderChannelOption[];
    canCreate: boolean;
}>();

const emit = defineEmits<{ (event: "close"): void; (event: "created", snapshot: WorkOrderSnapshot): void }>();

const sections = workOrderFormSections;
const longFields = new Set<string>(workOrderLongFields as string[]);
const requiredFields = new Set<string>(workOrderRequiredFields as string[]);
const allFields = sections.flatMap(section => section.fields);

/**
 * 支持「录入后寄存」的单值字段：项目名称 / 站区 / 作业负责人。
 * 作业人员是多值（顿号分隔），另走 a-textarea + 旁挂的历史补齐框。
 */
const historySingleFields: Array<keyof WorkRecordingForm> = ["projectName", "stationArea", "workLeader"];

/** 按字段缓存历史值数组。a-auto-complete 直接拿它当下拉 data。 */
const historyByField = ref<Record<string, WorkOrderFormHistoryEntry[]>>({});

/** a-auto-complete 的 filter-option：Vue 模板里不能写 TS 类型注解，
 *  抽到 script 里以函数形式引用，类型可声明在形参上。 */
function filterHistoryOption(value: string, option: { value: string; label: string }) {
    return option.label.includes(value);
}

/**
 * 展示顺序。分组归属仍以 `orderState` 为准（这里是排序，不是重新分组），
 * 目的是把 5 个必填项集中到前三行：项目名称 → 作业负责人/作业人员 → 站区/锚段号，
 * 现场把前三行填完就能提交，不必先扫过 11 个选填项。
 */
const fieldOrder: Array<keyof WorkRecordingForm> = [
    "projectName",
    "workLeader",
    "workPersonnel",
    "stationArea",
    "anchorSectionNo",
    "major",
    "mileage",
    "startAnchorPillarNo",
    "endAnchorPillarNo",
    "tensionWireCarModel",
    "tensionWireCarNo",
    "setTension",
    "straightenerStatus",
    "straightenerInspector",
    "wireLayingProcess",
    "remark"
];

/** 需要整行宽度的字段：标识字段与长文本，半宽会把内容压成两三行。 */
const fullWidthFields = new Set<string>(["projectName", "wireLayingProcess", "remark"]);

/** 分组图标，让三段式分组有可扫视的锚点。 */
const sectionIcons: Record<string, Component> = {
    基本信息: UserRound,
    技术参数: SlidersHorizontal,
    作业情况: FileText
};

const LABEL_COL_WIDTH = 112;
const labelColStyle = { width: `${LABEL_COL_WIDTH}px`, flex: `0 0 ${LABEL_COL_WIDTH}px` };
const wrapperColStyle = { flex: "1 1 auto", minWidth: "0" };

const orderedSections = computed(() =>
    sections.map(section => ({
        title: section.title,
        icon: sectionIcons[section.title] || FileText,
        fields: [...section.fields].sort((left, right) => orderIndex(String(left[0])) - orderIndex(String(right[0])))
    }))
);

function orderIndex(key: string) {
    const index = fieldOrder.indexOf(key as keyof WorkRecordingForm);
    // 未登记的字段（例如后续新增）排在分组末尾，而不是被静默丢掉。
    return index === -1 ? fieldOrder.length : index;
}

/** 编辑态统一用字符串，提交时再规整成后端形状。作业人员是多值，单独用数组维护。 */
const fields = ref<Record<string, string>>({});
/** 已选作业人员列表（下方以可删除 tag 展示）。 */
const personnelList = ref<string[]>([]);
const touched = ref(false);
/** 单字段失焦后即校验，符合项目表单规则「校验走 blur」。 */
const blurred = ref<Set<string>>(new Set());
const submitting = ref(false);
const error = ref("");
const requestId = ref("");

const CHIP_LIMIT = 6;

const channelIds = computed(() => props.channels.map(channel => channel.id));
const visibleChannels = computed(() => props.channels.slice(0, CHIP_LIMIT));
const hiddenChannels = computed(() => props.channels.slice(CHIP_LIMIT));
const hiddenChannelTitle = computed(() => hiddenChannels.value.map(channel => channel.label).join("、"));

function newRequestId() {
    return typeof globalThis.crypto?.randomUUID === "function" ? globalThis.crypto.randomUUID() : `work-order-${Date.now()}-${Math.random().toString(36).slice(2)}`;
}

function reset() {
    fields.value = Object.fromEntries(allFields.map(([key]) => [String(key), ""]));
    personnelList.value = [];
    touched.value = false;
    blurred.value = new Set();
    error.value = "";
    submitting.value = false;
    // 同一次打开复用同一个 requestId，重复提交由后端幂等吸收。
    requestId.value = newRequestId();
}

function currentForm(): WorkRecordingForm {
    return normalizeWorkOrderForm({ ...fields.value, workPersonnel: personnelList.value });
}

const missing = computed(() => missingWorkOrderFields(currentForm()));
/** 与后端 formPersonnelMax 对齐的人数，超限时前端先拦。 */
const personnelCount = computed(() => personnelList.value.length);
const personnelOverflow = computed(() => personnelCount.value > workOrderPersonnelMax);
const submitDisabled = computed(() => !props.canCreate || submitting.value || channelIds.value.length === 0);

/** 未触摸、也未失焦过的不报错：用户还没输入就提示"必填"是最差的表单体验。 */
function fieldError(field: string): string {
    if (field === "workPersonnel" && personnelOverflow.value) return `作业人员最多 ${workOrderPersonnelMax} 人`;
    if (!requiredFields.has(field)) return "";
    if (!touched.value && !blurred.value.has(field)) return "";
    const label = workOrderFieldLabels[field] || field;
    return missing.value.includes(label) ? `请填写${label}` : "";
}


function markBlurred(field: string) {
    if (blurred.value.has(field)) return;
    const next = new Set(blurred.value);
    next.add(field);
    blurred.value = next;
}

function isRequired(field: string) {
    return requiredFields.has(field);
}

/**
 * 底部反馈区：始终可见，替灰掉的提交按钮解释原因。
 * 提交失败时优先展示失败原因——它比"还差几项必填"更紧急。
 */
const footerFeedback = computed<{ tone: "error" | "hint" | "ready"; text: string }>(() => {
    if (error.value) return { tone: "error", text: error.value };
    if (!channelIds.value.length) return { tone: "hint", text: "没有正在播放的画面，无法开始录制" };
    if (personnelOverflow.value) return { tone: "hint", text: `作业人员最多 ${workOrderPersonnelMax} 人` };
    if (missing.value.length) {
        const preview = missing.value.slice(0, 3).join("、");
        const suffix = missing.value.length > 3 ? " 等" : "";
        return { tone: "hint", text: `还差 ${missing.value.length} 项必填：${preview}${suffix}` };
    }
    return { tone: "ready", text: "必填项已完整，提交后立即开始录制" };
});

async function submit() {
    if (submitting.value) return;
    touched.value = true;
    if (!channelIds.value.length) {
        error.value = "没有正在播放的画面，无法开始录制";
        return;
    }
    if (personnelOverflow.value) {
        error.value = `作业人员最多 ${workOrderPersonnelMax} 人，请精简后再提交`;
        return;
    }
    if (missing.value.length) {
        error.value = `请先填写：${missing.value.join("、")}`;
        return;
    }
    submitting.value = true;
    error.value = "";
    try {
        const response = await createWorkOrder({
            requestId: requestId.value,
            channelIds: channelIds.value,
            form: currentForm()
        });
        if (response.code !== 0) throw new Error(response.message || "作业单创建失败");
        Message.success(`已开始录制 ${channelIds.value.length} 路画面`);
        emit("created", response.data);
        emit("close");
    } catch (reason: any) {
        error.value = reason?.response?.data?.message || reason?.message || "作业单创建失败，请重试";
    } finally {
        submitting.value = false;
    }
}

function close() {
    if (submitting.value) return;
    emit("close");
}

async function loadFormHistory() {
    historyByField.value = {};
    // 4 个字段各拉一次。任一字段失败不影响其它字段（catch 内吞），
    // 历史值是辅助能力，拉不到就退化成纯手输，不应阻断弹窗。
    const targets: Array<keyof WorkRecordingForm> = [...historySingleFields, "workPersonnel"];
    await Promise.all(targets.map(async field => {
        try {
            const response = await listWorkOrderFormHistory(String(field), 50);
            if (response.code === 0) {
                historyByField.value = {
                    ...historyByField.value,
                    [String(field)]: response.data?.items || []
                };
            }
        } catch {
            // ignore: 退化路径
        }
    }));
}

/**
 * 作业人员下拉的候选来自历史值（录入后寄存），但历史为空时不能因此锁死录入：
 * `a-select` 开启了 `allow-create`，用户可直接输入新人名回车添加，与「从历史选用」并行。
 * 已选人员从候选里排除，避免重复选到同一个人。
 */
const personnelOptions = computed(() =>
    (historyByField.value["workPersonnel"] || [])
        .map(entry => ({ value: entry.value, label: entry.value }))
        .filter(option => !personnelList.value.includes(option.value))
);

watch(
    () => props.visible,
    visible => {
        if (!visible) return;
        reset();
        void loadFormHistory();
        // 不再自动 focus 第一个字段：a-modal 还在过渡时 focus 会让 a-auto-complete
        // 读到中间态的 boundingClientRect，popup 位置被算到相邻字段下方。
        // 键盘用户可 Tab 跳过说明区，与 a11y 推荐一致。
    },
    { immediate: true }
);
</script>

<template>
    <a-modal
        :visible="visible"
        modal-class="uvp-system-dialog"
        title="新建作业单并开始录制"
        width="min(960px, calc(100vw - 24px))"
        :mask-closable="false"
        @cancel="close"
    >
        <div class="work-order-form">
            <!-- 只读上下文：本次录制范围。用信息块而非禁用输入框，避免"看起来能改"。 -->
            <div v-if="channels.length" class="record-scope" data-test="record-scope" role="status">
                <span class="record-scope-icon" aria-hidden="true"><Video :size="16" /></span>
                <div class="record-scope-body" :style="{ marginLeft: `${LABEL_COL_WIDTH - 36}px` }">
                    <p class="record-scope-title">本次录制 <strong>{{ channelIds.length }}</strong> 路画面</p>
                    <div class="record-scope-channels">
                        <a-tag v-for="channel in visibleChannels" :key="channel.id" size="small" class="record-scope-chip" data-test="channel-chip">{{ channel.label }}</a-tag>
                        <a-tooltip v-if="hiddenChannels.length" :content="hiddenChannelTitle">
                            <a-tag size="small" class="record-scope-chip record-scope-chip--more" data-test="channel-chip-more">+{{ hiddenChannels.length }}</a-tag>
                        </a-tooltip>
                    </div>
                </div>
            </div>
            <a-alert v-else type="warning" data-test="no-channel-alert">请先播放需要录制的画面，再新建作业单。</a-alert>

            <section v-for="section in orderedSections" :key="section.title" class="form-section" :data-section="section.title">
                <header>
                    <span class="form-section-icon" aria-hidden="true"><component :is="section.icon" :size="14" /></span>
                    <h3>{{ section.title }}</h3>
                </header>
                <div class="form-grid">
                    <a-form-item
                        v-for="[key, label] in section.fields"
                        :key="key"
                        :data-field="String(key)"
                        :class="{ 'form-grid-wide': fullWidthFields.has(String(key)) }"
                        :label-col-style="labelColStyle"
                        :wrapper-col-style="wrapperColStyle"
                        :label="label"
                        :required="isRequired(String(key))"
                        :validate-status="fieldError(String(key)) ? 'error' : ''"
                        :help="fieldError(String(key))"
                    >
                        <a-select
                            v-if="key === 'workPersonnel'"
                            v-model="personnelList"
                            multiple
                            allow-search
                            allow-create
                            :options="personnelOptions"
                            :trigger-props="{ contentStyle: { maxHeight: '180px' }, updateAtScroll: true }"
                            placeholder="选择或输入作业人员，回车添加"
                            data-test="work-personnel-input"
                            @blur="markBlurred(String(key))"
                        />
                        <a-auto-complete
                            v-else-if="historySingleFields.includes(String(key) as keyof WorkRecordingForm)"
                            v-model="fields[String(key)]"
                            :data="(historyByField[String(key)] || []).map(entry => ({ value: entry.value, label: entry.value }))"
                            :trigger-props="{ contentStyle: { maxHeight: '240px' }, updateAtScroll: true }"
                            allow-clear
                            :max-length="256"
                            :placeholder="`请输入或选择${label}`"
                            :filter-option="filterHistoryOption"
                            data-test="form-history-auto-complete"
                            @blur="markBlurred(String(key))"
                        />
                        <a-textarea
                            v-else-if="longFields.has(String(key))"
                            v-model="fields[String(key)]"
                            allow-clear
                            :max-length="4000"
                            :auto-size="{ minRows: 2, maxRows: 4 }"
                            :placeholder="`请输入${label}`"
                            @blur="markBlurred(String(key))"
                        />
                        <a-input
                            v-else
                            v-model="fields[String(key)]"
                            allow-clear
                            :max-length="256"
                            :placeholder="`请输入${label}`"
                            @blur="markBlurred(String(key))"
                        />
                    </a-form-item>
                </div>
            </section>
        </div>

        <template #footer>
            <div class="work-order-dialog-footer">
                <p
                    v-if="footerFeedback.tone === 'error'"
                    class="footer-feedback footer-feedback--error"
                    role="alert"
                    data-test="form-error"
                >
                    <AlertTriangle :size="14" aria-hidden="true" /><span>{{ footerFeedback.text }}</span>
                </p>
                <p
                    v-else
                    class="footer-feedback"
                    :class="`footer-feedback--${footerFeedback.tone}`"
                    aria-live="polite"
                    data-test="footer-hint"
                >
                    <CheckCircle2 v-if="footerFeedback.tone === 'ready'" :size="14" aria-hidden="true" />
                    <Info v-else :size="14" aria-hidden="true" />
                    <span>{{ footerFeedback.text }}</span>
                </p>
                <div class="footer-actions">
                    <a-button :disabled="submitting" @click="close">取消</a-button>
                    <a-button type="primary" :loading="submitting" :disabled="submitDisabled" @click="submit">提交并开始作业</a-button>
                </div>
            </div>
        </template>
    </a-modal>
</template>

<style scoped>
.work-order-form {
    max-height: calc(100vh - 300px);
    overflow-y: auto;
    overscroll-behavior: contain;
    color: var(--uvp-text-primary);
}

/* ── 录制范围（只读上下文）───────────────────────────── */
.record-scope {
    display: flex;
    gap: 10px;
    align-items: flex-start;
    padding: 12px 14px;
    margin-bottom: 18px;
    background: var(--uvp-brand-soft);
    border: 1px solid rgb(37 99 235 / 12%);
    border-radius: 10px;
}
.record-scope-icon {
    display: inline-flex;
    flex: 0 0 auto;
    align-items: center;
    justify-content: center;
    width: 26px;
    height: 26px;
    color: var(--uvp-brand);
    background: var(--uvp-dialog-bg);
    border-radius: 8px;
}
.record-scope-body { min-width: 0; }
.record-scope-title { margin: 0; color: var(--uvp-text-secondary); font-size: 13px; line-height: 16px; }
.record-scope-title strong { margin: 0 2px; color: var(--uvp-brand); font-size: 15px; font-weight: 680; }
.record-scope-channels { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 8px; }
.record-scope-chip { max-width: 180px; overflow: hidden; text-overflow: ellipsis; }
.record-scope-chip--more { color: var(--uvp-brand); }

/* ── 分组区块 ─────────────────────────────────────── */
.form-section { padding-bottom: 16px; }
.form-section + .form-section { padding-top: 16px; border-top: 1px solid var(--uvp-dialog-border); }
.form-section > header { display: flex; gap: 8px; align-items: center; margin-bottom: 14px; }
.form-section-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 26px;
    height: 26px;
    color: var(--uvp-brand);
    background: var(--uvp-brand-soft);
    border-radius: 8px;
}
.form-section h3 { margin: 0; color: var(--uvp-text-primary); font-size: 15px; font-weight: 620; letter-spacing: 0; }

/* 2 列：产品既有表单弹窗的默认节奏；窄屏回落单列，断点与设计系统一致。 */
.form-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 2px 20px; }

/*
 * 表单历史值下拉（4 个字段都套 a-auto-complete）的滚动条样式。
 * 项目里 uvp-data-table 已自定义滚动条，但 a-auto-complete 的弹出层走 Arco 默认，
 * 在 macOS / Chrome 上会显示原生细灰条，与产品设计不一致。这里给它复用项目风格：
 * 6px 宽、圆角 thumb、透明 track；同时在窄屏上 240px maxHeight 配合 4-5 条/屏的滚动节奏。
 */
.work-order-form :deep(.arco-trigger-popup .arco-trigger-popup-content) {
  overscroll-behavior: contain;
}
.work-order-form :deep(.arco-trigger-popup .arco-trigger-popup-content::-webkit-scrollbar) {
  width: 6px;
}
.work-order-form :deep(.arco-trigger-popup .arco-trigger-popup-content::-webkit-scrollbar-thumb) {
  background: var(--color-fill-3);
  border-radius: 3px;
}
.work-order-form :deep(.arco-trigger-popup .arco-trigger-popup-content::-webkit-scrollbar-thumb:hover) {
  background: var(--color-fill-4);
}
.work-order-form :deep(.arco-trigger-popup .arco-trigger-popup-content::-webkit-scrollbar-track) {
  background: transparent;
}
.work-order-form :deep(.arco-trigger-popup .arco-trigger-popup-content) {
  scrollbar-width: thin;
  scrollbar-color: var(--color-fill-3) transparent;
}

/* workPersonnel 用 a-select multiple：tag + 删除 Arco 自带，scoped 样式只需对齐 placeholder 颜色。 */
.work-order-form .arco-select-view { min-height: 32px; }
.work-order-personnel-tag {
    margin: 0;
    background: var(--uvp-brand-soft);
    border: 1px solid rgb(37 99 235 / 14%);
    color: var(--uvp-brand);
}
.work-order-personnel-empty {
    color: var(--color-text-3);
    font-size: 12px;
    line-height: 24px;
}
.form-grid-wide { grid-column: 1 / -1; }

/* ── 底部反馈区（始终可见）─────────────────────────── */
.work-order-dialog-footer { display: flex; gap: 16px; align-items: center; width: 100%; }
.footer-feedback {
    display: flex;
    flex: 1;
    gap: 6px;
    align-items: center;
    min-width: 0;
    margin: 0;
    font-size: 12px;
    line-height: 18px;
}
.footer-feedback > svg { flex: 0 0 auto; }
.footer-feedback > span { overflow: hidden; text-overflow: ellipsis; }
.footer-feedback--error { color: var(--uvp-danger); }
.footer-feedback--error > span { overflow: visible; white-space: normal; }
.footer-feedback--hint { color: var(--uvp-warning); }
.footer-feedback--ready { color: var(--uvp-brand-cyan); }
.footer-actions { display: flex; flex: 0 0 auto; gap: 10px; }

@media (max-width: 768px) {
    .work-order-form { max-height: calc(100vh - 260px); }
    .form-grid { grid-template-columns: minmax(0, 1fr); }
    .work-order-dialog-footer { flex-direction: column; align-items: stretch; }
    .footer-actions { justify-content: flex-end; }
}
</style>
