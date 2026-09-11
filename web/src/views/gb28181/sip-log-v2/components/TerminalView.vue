<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { CircleDot, Download, Eraser, Pause, Play } from "lucide-vue-next";
import { buildTraceStreamUrl, type TraceMessageSummary } from "@/api/gb28181-trace";
import { formatFullTime, highlightSipPayload } from "../helpers";

// SSE 推送里的一条报文,payload 已在后端 RedactSIP 脱敏
interface StreamMessage extends TraceMessageSummary {
    payload: string;
    sensitive: boolean;
}

const props = defineProps<{
    deviceIds?: string[];
    callId?: string;
}>();

const paused = ref(false);
const autoScroll = ref(true);
const scrollRef = ref<HTMLElement | null>(null);
const stream = ref<StreamMessage[]>([]);
const connected = ref(false);
let eventSource: EventSource | null = null;

function closeStream() {
    if (eventSource) {
        eventSource.close();
        eventSource = null;
    }
    connected.value = false;
}

function openStream() {
    closeStream();
    // 后端 stream 支持单设备过滤;多设备暂由前端不做,或由后端后续扩展成 IN 语义
    const deviceId = props.deviceIds && props.deviceIds.length === 1 ? props.deviceIds[0] : undefined;
    const url = buildTraceStreamUrl({ deviceId, callId: props.callId });
    const es = new EventSource(url);
    eventSource = es;

    es.addEventListener("ready", () => {
        connected.value = true;
    });

    es.addEventListener("message", (e: MessageEvent) => {
        if (paused.value) return;
        try {
            const msg = JSON.parse(e.data) as StreamMessage;
            stream.value = [...stream.value, msg];
            if (stream.value.length > 500) {
                stream.value = stream.value.slice(-500);
            }
        } catch {
            // 忽略解析失败的行
        }
    });

    es.addEventListener("ping", () => {
        // 心跳,只用来保活,不做业务处理
    });

    es.onerror = () => {
        // 浏览器 EventSource 会自动重连,不需要手写。此处仅标记状态。
        connected.value = false;
    };
}

onMounted(() => {
    openStream();
});

onBeforeUnmount(() => {
    closeStream();
});

// 筛选变化重新订阅(前端仍持有筛选联动能力)
watch(
    () => [props.deviceIds, props.callId],
    () => {
        stream.value = [];
        openStream();
    },
    { deep: true }
);

// 自动滚到底部
watch(() => stream.value.length, async () => {
    if (paused.value || !autoScroll.value) return;
    await nextTick();
    if (scrollRef.value) scrollRef.value.scrollTop = scrollRef.value.scrollHeight;
});

function onScroll() {
    if (!scrollRef.value) return;
    const el = scrollRef.value;
    autoScroll.value = el.scrollHeight - el.scrollTop - el.clientHeight < 60;
}

function jumpToBottom() {
    if (!scrollRef.value) return;
    scrollRef.value.scrollTop = scrollRef.value.scrollHeight;
    autoScroll.value = true;
}

function clearBuffer() {
    stream.value = [];
}

function downloadLog() {
    const text = stream.value.map(m => renderPlain(m)).join("\n");
    const blob = new Blob([text], { type: "text/plain;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `sip-trace-${new Date().toISOString().replace(/[:.]/g, "-")}.log`;
    a.click();
    URL.revokeObjectURL(url);
}

function renderPlain(msg: StreamMessage): string {
    return `[${formatFullTime(msg.occurredAt)}] ${msg.direction === "inbound" ? "IN" : "OUT"} ${msg.transport} ${msg.localAddr} ${msg.direction === "inbound" ? "<-" : "->"} ${msg.remoteAddr}\n${msg.payload}`;
}

const stats = computed(() => ({
    total: stream.value.length,
    inbound: stream.value.filter(m => m.direction === "inbound").length,
    outbound: stream.value.filter(m => m.direction === "outbound").length
}));

function headerLine(msg: StreamMessage): string {
    const arrow = msg.direction === "inbound" ? "◀" : "▶";
    const dir = msg.direction === "inbound" ? "IN " : "OUT";
    return `${arrow} ${dir}  ${msg.transport}  ${msg.direction === "inbound" ? msg.remoteAddr : msg.localAddr} → ${msg.direction === "inbound" ? msg.localAddr : msg.remoteAddr}`;
}
</script>

<template>
    <div class="terminal-view">
        <div class="terminal-head">
            <div class="terminal-dot-group">
                <span class="term-dot red" />
                <span class="term-dot yellow" />
                <span class="term-dot green" />
            </div>
            <span class="terminal-title mono">sip-trace.log — {{ stats.total }} messages · IN {{ stats.inbound }} · OUT {{ stats.outbound }}</span>
            <div class="terminal-actions">
                <span class="live-indicator" :class="{ paused }">
                    <CircleDot :size="10" :class="{ live: !paused }" />
                    {{ paused ? "PAUSED" : "LIVE" }}
                </span>
                <button class="term-btn" @click="paused = !paused">
                    <component :is="paused ? Play : Pause" :size="12" />
                    {{ paused ? "恢复" : "暂停" }}
                </button>
                <button v-if="!autoScroll" class="term-btn primary" @click="jumpToBottom">
                    回到底部
                </button>
                <button class="term-btn" @click="clearBuffer" title="清空缓冲区">
                    <Eraser :size="12" />
                </button>
                <button class="term-btn" @click="downloadLog" title="下载日志">
                    <Download :size="12" />
                </button>
            </div>
        </div>
        <div ref="scrollRef" class="terminal-body" @scroll="onScroll">
            <div v-if="!stream.length" class="terminal-empty mono">
                $ tail -f sip-trace.log
                <br />
                <span class="dim"># 等待报文流入...</span>
            </div>
            <div
                v-for="msg in stream"
                :key="msg.eventId"
                :class="['msg-block', `dir-${msg.direction}`]"
            >
                <div class="msg-meta mono">
                    <span class="meta-time">{{ formatFullTime(msg.occurredAt) }}</span>
                    <span :class="['meta-dir', msg.direction]">{{ headerLine(msg) }}</span>
                </div>
                <pre class="msg-payload mono" v-html="highlightSipPayload(msg.payload)" />
            </div>
        </div>
        <div class="terminal-footer">
            <span class="mono">$ sip-trace-daemon --filter port=5060 --format=raw</span>
            <span class="footer-tip">滚动到底部恢复自动跟随 · 缓冲区保留最近 500 条</span>
        </div>
    </div>
</template>

<style scoped>
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, "JetBrains Mono", Consolas, monospace; }

.terminal-view {
    box-sizing: border-box;
    display: flex;
    flex-direction: column;
    width: 100%;
    height: 100%;
    min-height: 0;
    color: #d4d4d8;
    background: #0d1117;
    border: 1px solid #21262d;
    border-radius: 10px;
    overflow: hidden;
}

/* 顶部标题栏 */
.terminal-head {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    gap: 12px;
    height: 40px;
    padding: 0 14px;
    background: linear-gradient(180deg, #1a1f27 0%, #131820 100%);
    border-bottom: 1px solid #21262d;
}
.terminal-dot-group { display: inline-flex; gap: 6px; }
.term-dot { width: 11px; height: 11px; border-radius: 50%; opacity: 0.85; }
.term-dot.red { background: #ff5f56; }
.term-dot.yellow { background: #ffbd2e; }
.term-dot.green { background: #27c93f; }
.terminal-title {
    flex: 1;
    color: #9ba3b0;
    font-size: 12px;
    text-align: center;
    letter-spacing: 0.3px;
}
.terminal-actions { display: inline-flex; align-items: center; gap: 6px; }
.live-indicator {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    color: #7ee787;
    font-size: 11px;
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-weight: 600;
    letter-spacing: 0.5px;
}
.live-indicator.paused { color: #ffbd2e; }
.live-indicator :deep(svg.live) { animation: pulse 1.4s ease-in-out infinite; }
@keyframes pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.3; } }

.term-btn {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 4px 9px;
    color: #c9d1d9;
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 11px;
    background: #21262d;
    border: 1px solid #30363d;
    border-radius: 4px;
    cursor: pointer;
    transition: background 120ms;
}
.term-btn:hover { background: #30363d; }
.term-btn.primary { color: #79c0ff; border-color: #388bfd; }

/* 主体:滚动的报文流 */
.terminal-body {
    flex: 1;
    min-height: 0;
    padding: 12px 20px 20px;
    overflow: auto;
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 12.5px;
    line-height: 1.55;
    scrollbar-color: #30363d transparent;
    scrollbar-width: thin;
}
.terminal-body::-webkit-scrollbar { width: 8px; }
.terminal-body::-webkit-scrollbar-thumb { background: #30363d; border-radius: 4px; }
.terminal-body::-webkit-scrollbar-track { background: transparent; }

.terminal-empty { padding: 12px 0; color: #6e7681; font-size: 12.5px; }
.terminal-empty .dim { color: #484f58; }

/* 每段报文 */
.msg-block {
    padding: 8px 0 12px;
    border-bottom: 1px dashed #21262d;
}
.msg-block:last-child { border-bottom: 0; }

.msg-meta {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 2px 0 6px;
    font-size: 11.5px;
}
.meta-time { color: #7d8590; }
.meta-dir { font-weight: 600; letter-spacing: 0.3px; }
.meta-dir.inbound { color: #7ee787; }
.meta-dir.outbound { color: #79c0ff; }

.msg-payload {
    margin: 0;
    padding: 0;
    color: #d4d4d8;
    font-size: 12.5px;
    line-height: 1.6;
    white-space: pre-wrap;
    word-break: break-all;
    background: transparent;
    border: 0;
}

/* 语法高亮 - 深色模式适配(比 DetailPanel 更亮的对比) */
.msg-payload :deep(.tok-method) { color: #ff7b72; font-weight: 700; }
.msg-payload :deep(.tok-version) { color: #6e7681; }
.msg-payload :deep(.tok-uri), .msg-payload :deep(.tok-uri-line) { color: #79c0ff; }
.msg-payload :deep(.tok-header-key) { color: #7ee787; }
.msg-payload :deep(.tok-callid) { color: #ff7b72; font-weight: 600; }
.msg-payload :deep(.tok-number) { color: #ffa657; font-weight: 600; }
.msg-payload :deep(.tok-ip) { color: #d2a8ff; }
.msg-payload :deep(.tok-port) { color: #ffa657; }
.msg-payload :deep(.tok-status-success) { color: #7ee787; font-weight: 700; }
.msg-payload :deep(.tok-status-warning) { color: #ffa657; font-weight: 700; }
.msg-payload :deep(.tok-status-danger) { color: #ff7b72; font-weight: 700; }
.msg-payload :deep(.tok-status-info) { color: #79c0ff; font-weight: 700; }
.msg-payload :deep(.tok-xml-tag) { color: #7ee787; font-weight: 600; }
.msg-payload :deep(.tok-xml-attr) { color: #d2a8ff; }
.msg-payload :deep(.tok-xml-string) { color: #a5d6ff; }
.msg-payload :deep(.tok-sdp-key) { color: #7ee787; font-weight: 600; }

/* 底部提示栏 */
.terminal-footer {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    justify-content: space-between;
    height: 30px;
    padding: 0 14px;
    color: #6e7681;
    font-size: 11px;
    background: #0a0e13;
    border-top: 1px solid #21262d;
}
.footer-tip { font-size: 11px; }
</style>
