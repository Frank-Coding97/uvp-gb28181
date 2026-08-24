<script setup lang="ts">
import { computed, ref } from "vue";
import { Message } from "@arco-design/web-vue";
import { ArrowDown, ArrowUp, Code2, Copy, FileText, List } from "lucide-vue-next";
import type { TraceMessageDetail } from "@/api/gb28181-trace";
import { highlightSdp, highlightSipHeaderValue, highlightSipPayload, highlightXml, isKeyHeader, parsePayload } from "../helpers";

type DetailTab = "structured" | "raw" | "sdp" | "xml";

const props = defineProps<{
    message: TraceMessageDetail | null;
    loading?: boolean;
}>();

const emit = defineEmits<{
    (e: "step", delta: 1 | -1): void;
}>();

const detailTab = ref<DetailTab>("structured");

const parsedPayload = computed(() => props.message ? parsePayload(props.message.payload) : null);

const availableTabs = computed<DetailTab[]>(() => {
    const tabs: DetailTab[] = ["structured", "raw"];
    if (parsedPayload.value?.bodyType === "sdp") tabs.push("sdp");
    if (parsedPayload.value?.bodyType === "xml") tabs.push("xml");
    return tabs;
});

const tabLabel: Record<DetailTab, string> = { structured: "概要", raw: "原文", sdp: "SDP", xml: "XML" };

const rawHtml = computed(() => {
    if (!props.message) return "";
    return highlightSipPayload(props.message.payload);
});

const sdpHtml = computed(() => {
    if (!parsedPayload.value) return "";
    return highlightSdp(parsedPayload.value.body);
});

const xmlHtml = computed(() => {
    if (!parsedPayload.value) return "";
    return highlightXml(parsedPayload.value.body);
});

const startLineHtml = computed(() => {
    if (!parsedPayload.value) return "";
    // 复用高亮里的 startLine 高亮逻辑:直接高亮完整 payload,取第一行
    const highlighted = highlightSipPayload(parsedPayload.value.startLine);
    return highlighted.split("\n")[0] || "";
});

const structuredBodyHtml = computed(() => {
    if (!parsedPayload.value) return "";
    const type = parsedPayload.value.bodyType;
    if (type === "xml") return highlightXml(parsedPayload.value.body);
    if (type === "sdp") return highlightSdp(parsedPayload.value.body);
    return "";
});

async function copyPayload() {
    if (!props.message) return;
    try {
        await navigator.clipboard.writeText(props.message.payload);
        Message.success("已复制到剪贴板");
    } catch {
        Message.warning("复制失败,请手动选中");
    }
}
</script>

<template>
    <aside class="detail-pane" aria-label="报文详情">
        <div class="pane-head">
            <span class="pane-title">
                报文详情
                <span v-if="message" class="head-inline-meta">
                    · CSeq {{ message.cseq }} {{ message.cseqMethod }}
                </span>
            </span>
            <div class="pane-actions">
                <a-tooltip content="上一条"><button class="step-btn" :disabled="!message" @click="emit('step', -1)"><ArrowUp :size="13" /></button></a-tooltip>
                <a-tooltip content="下一条"><button class="step-btn" :disabled="!message" @click="emit('step', 1)"><ArrowDown :size="13" /></button></a-tooltip>
                <a-tooltip content="复制原文"><button class="step-btn" :disabled="!message" @click="copyPayload"><Copy :size="13" /></button></a-tooltip>
            </div>
        </div>
        <div v-if="message" class="detail-tabs">
            <button
                v-for="tab in availableTabs"
                :key="tab"
                type="button"
                :class="['detail-tab', { active: detailTab === tab }]"
                @click="detailTab = tab"
            >{{ tabLabel[tab] }}</button>
        </div>
        <div class="pane-body">
            <a-spin :loading="loading" class="pane-spin">
                <div v-if="message && parsedPayload" class="detail-content">
                    <div v-if="detailTab === 'structured'" class="structured-view">
                        <section class="structured-section section-start-line">
                            <div class="block-heading">
                                <FileText :size="14" aria-hidden="true" />
                                <span>起始行</span>
                            </div>
                            <code class="start-line-text mono" v-html="startLineHtml" />
                        </section>
                        <section class="structured-section section-headers">
                            <div class="block-heading">
                                <List :size="14" aria-hidden="true" />
                                <span>头部字段</span>
                                <span class="section-count">{{ parsedPayload.headers.length }}</span>
                            </div>
                            <dl class="header-list">
                                <div
                                    v-for="(h, idx) in parsedPayload.headers"
                                    :key="idx"
                                    :class="['header-row', { 'key-header-row': isKeyHeader(h.name) }]"
                                >
                                    <dt :class="{ 'key-header': isKeyHeader(h.name) }">{{ h.name }}</dt>
                                    <dd :class="['mono', { 'key-header-val': isKeyHeader(h.name) }]" v-html="highlightSipHeaderValue(h.name, h.value)" />
                                </div>
                            </dl>
                        </section>
                        <section v-if="parsedPayload.bodyType !== 'empty'" class="structured-section section-body">
                            <div class="block-heading">
                                <Code2 :size="14" aria-hidden="true" />
                                <span>正文</span>
                                <span class="body-type">{{ parsedPayload.bodyType.toUpperCase() }}</span>
                            </div>
                            <pre v-if="structuredBodyHtml" class="body-code mono" v-html="structuredBodyHtml" />
                            <pre v-else class="body-code mono">{{ parsedPayload.body }}</pre>
                        </section>
                    </div>
                    <pre v-else-if="detailTab === 'raw'" class="raw-code mono" v-html="rawHtml" />
                    <pre v-else-if="detailTab === 'sdp'" class="raw-code mono" v-html="sdpHtml" />
                    <pre v-else class="raw-code mono" v-html="xmlHtml" />
                </div>
                <a-empty v-else description="选中报文后查看详情" />
            </a-spin>
        </div>
    </aside>
</template>

<style scoped>
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; }
.detail-pane {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    background: var(--uvp-panel-bg);
    border: 1px solid var(--uvp-panel-border);
    border-radius: 10px;
    overflow: hidden;
}
.pane-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    min-height: 42px;
    padding: 0 14px;
    background: var(--uvp-list-toolbar-bg);
    border-bottom: 1px solid var(--uvp-panel-border);
}
.pane-title { color: var(--uvp-text-primary); font-size: 13px; font-weight: 600; }
.head-inline-meta { color: var(--uvp-text-tertiary); font-size: 12px; font-weight: normal; }
.pane-actions { display: inline-flex; align-items: center; gap: 4px; }
.step-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 26px;
    height: 26px;
    padding: 0;
    color: var(--uvp-text-secondary);
    background: transparent;
    border: 1px solid var(--uvp-panel-border);
    border-radius: 6px;
    cursor: pointer;
}
.step-btn:hover:not(:disabled) { color: var(--uvp-brand); border-color: var(--uvp-brand); }
.step-btn:disabled { opacity: 0.4; cursor: not-allowed; }
.detail-tabs {
    display: flex;
    gap: 4px;
    padding: 8px 12px 0;
    border-bottom: 1px solid var(--uvp-panel-border);
}
.detail-tab {
    padding: 6px 12px 8px;
    color: var(--uvp-text-tertiary);
    background: transparent;
    border: 0;
    border-bottom: 2px solid transparent;
    font-size: 12px;
    font-weight: 500;
    cursor: pointer;
}
.detail-tab:hover { color: var(--uvp-text-secondary); }
.detail-tab.active { color: var(--uvp-brand); border-bottom-color: var(--uvp-brand); }

.pane-body { display: flex; flex: 1; min-height: 0; padding: 0 14px 18px; overflow: auto; }
.pane-spin { display: flex; flex: 1; min-width: 0; min-height: 0; width: 100%; }
.pane-spin :deep(.arco-spin-children) { display: flex; flex: 1; flex-direction: column; min-width: 0; }
.pane-spin :deep(.arco-empty) { margin: auto; }

.detail-content { display: flex; flex-direction: column; width: 100%; }
.structured-view { display: flex; flex-direction: column; }
.structured-section {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 12px 0 14px;
    border-bottom: 1px solid color-mix(in srgb, var(--uvp-panel-border) 78%, transparent);
}
.structured-section:last-child { border-bottom: 0; }
.block-heading {
    display: flex;
    align-items: center;
    gap: 6px;
    color: var(--uvp-text-secondary);
    font-size: 12px;
    font-weight: 600;
}
.block-heading > svg { color: var(--uvp-brand); }
.section-count,
.body-type {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 20px;
    height: 18px;
    padding: 0 5px;
    color: var(--uvp-text-tertiary);
    background: var(--uvp-list-toolbar-bg);
    border-radius: 4px;
    font-size: 10px;
    font-weight: 500;
}
.start-line-text {
    display: block;
    padding: 9px 10px;
    color: var(--uvp-text-primary);
    background: color-mix(in srgb, var(--uvp-brand) 4%, var(--uvp-list-toolbar-bg));
    border-left: 3px solid var(--uvp-brand);
    border-radius: 0 4px 4px 0;
    font-size: 12px;
    line-height: 1.55;
    overflow-wrap: anywhere;
}
.header-list {
    margin: 0;
    border-top: 1px solid var(--uvp-panel-border);
    border-bottom: 1px solid var(--uvp-panel-border);
}
.header-row {
    display: grid;
    grid-template-columns: 118px minmax(0, 1fr);
    min-width: 0;
    border-bottom: 1px solid color-mix(in srgb, var(--uvp-panel-border) 68%, transparent);
}
.header-row:last-child { border-bottom: 0; }
.header-row:hover { background: color-mix(in srgb, var(--uvp-brand) 3%, transparent); }
.header-list dt,
.header-list dd {
    min-width: 0;
    padding: 6px 10px;
}
.header-list dt {
    color: var(--uvp-text-tertiary);
    font-size: 12px;
    line-height: 1.55;
    background: color-mix(in srgb, var(--uvp-text-tertiary) 5%, transparent);
    border-right: 1px solid color-mix(in srgb, var(--uvp-panel-border) 68%, transparent);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}
.header-list dd {
    margin: 0;
    color: var(--uvp-text-secondary);
    font-size: 12px;
    line-height: 1.55;
    overflow-wrap: anywhere;
}
.header-list dt.key-header { color: var(--uvp-text-primary); font-weight: 600; }
.header-list dd.key-header-val { color: var(--uvp-text-primary); }
.body-code, .raw-code {
    margin: 0;
    padding: 10px 12px;
    color: var(--uvp-text-primary);
    background: var(--uvp-list-toolbar-bg);
    border: 1px solid var(--uvp-panel-border);
    border-radius: 6px;
    font-size: 12px;
    line-height: 1.65;
    white-space: pre-wrap;
    word-break: break-all;
    max-height: 100%;
    overflow: auto;
}
.raw-code { white-space: pre; }
.detail-content > .raw-code { margin-top: 12px; }

/* ============ 语法高亮 tokens(sngrep 风格) ============ */
.body-code :deep(.tok-xml-tag), .raw-code :deep(.tok-xml-tag) { color: #2dd4bf; font-weight: 600; }
.body-code :deep(.tok-xml-attr), .raw-code :deep(.tok-xml-attr) { color: #a78bfa; }
.body-code :deep(.tok-xml-string), .raw-code :deep(.tok-xml-string) { color: #10b981; }
.body-code :deep(.tok-sdp-key), .raw-code :deep(.tok-sdp-key) { color: #2dd4bf; font-weight: 600; }

.raw-code :deep(.tok-method) { color: #ec4899; font-weight: 700; }
.raw-code :deep(.tok-version) { color: var(--uvp-text-tertiary); }
.raw-code :deep(.tok-uri), .raw-code :deep(.tok-uri-line) { color: #2563eb; }
.raw-code :deep(.tok-header-key) { color: #06b6d4; }
.raw-code :deep(.tok-callid) { color: #ec4899; font-weight: 600; }
.raw-code :deep(.tok-number) { color: #f59e0b; font-weight: 600; }
.raw-code :deep(.tok-ip) { color: #a78bfa; }
.raw-code :deep(.tok-port) { color: #a78bfa; }
.raw-code :deep(.tok-status-success) { color: #10b981; font-weight: 700; }
.raw-code :deep(.tok-status-warning) { color: #f59e0b; font-weight: 700; }
.raw-code :deep(.tok-status-danger) { color: var(--uvp-danger); font-weight: 700; }
.raw-code :deep(.tok-status-info) { color: #2563eb; font-weight: 700; }

/* 结构化视图的起始行也用同一套 token */
.start-line-text :deep(.tok-method) { color: #ec4899; font-weight: 700; }
.start-line-text :deep(.tok-uri-line) { color: #2563eb; }
.start-line-text :deep(.tok-version) { color: var(--uvp-text-tertiary); }
.start-line-text :deep(.tok-ip) { color: #a78bfa; }
.start-line-text :deep(.tok-port) { color: #a78bfa; }
.start-line-text :deep(.tok-status-success) { color: #10b981; font-weight: 700; }
.start-line-text :deep(.tok-status-warning) { color: #f59e0b; font-weight: 700; }
.start-line-text :deep(.tok-status-danger) { color: var(--uvp-danger); font-weight: 700; }
.start-line-text :deep(.tok-status-info) { color: #2563eb; font-weight: 700; }

/* Header 值高亮 */
.header-list dd :deep(.tok-uri) { color: #2563eb; }
.header-list dd :deep(.tok-ip) { color: #a78bfa; }
.header-list dd :deep(.tok-port) { color: #a78bfa; }
.header-list dd :deep(.tok-callid) { color: #ec4899; font-weight: 600; }
.header-list dd :deep(.tok-number) { color: #f59e0b; font-weight: 600; }
.header-list dd :deep(.tok-method) { color: #ec4899; font-weight: 700; }
</style>
