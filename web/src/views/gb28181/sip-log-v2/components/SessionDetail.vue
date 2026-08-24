<script setup lang="ts">
import { computed } from "vue";
import { AlertTriangle, ArrowLeft, ArrowRight, Copy, Server, Video } from "lucide-vue-next";
import { Message } from "@arco-design/web-vue";
import type {
    TraceMessageSummary as TraceMessage,
    TraceSessionDiagnosis,
    TraceSessionSummary as TraceSession
} from "@/api/gb28181-trace";
import { diagnosisLabel, formatDuration, formatFullTime, methodLabel, sessionStateLabel, statusTone } from "../helpers";

const props = defineProps<{
    session: TraceSession;
    messages: TraceMessage[];
    selectedEventId: string;
}>();

const emit = defineEmits<{
    (e: "back"): void;
    (e: "select-message", message: TraceMessage): void;
}>();

// 局域网场景固定 2 个泳道:设备端 vs 平台端
// 使用第一条消息推断:outbound 时 localAddr=平台/remoteAddr=设备;inbound 时反之
const lanes = computed<string[]>(() => {
    const first = props.messages[0];
    if (!first) return [];
    if (first.direction === "outbound") {
        // 平台发出:local=平台, remote=设备
        return [first.remoteAddr, first.localAddr]; // 左侧设备,右侧平台
    }
    // inbound: local=平台, remote=设备
    return [first.remoteAddr, first.localAddr];
});

// 相邻报文的时间差(秒),格式 +0.123
const relativeDeltas = computed<string[]>(() => {
    if (!props.messages.length) return [];
    const first = new Date(props.messages[0].occurredAt).getTime();
    return props.messages.map(m => {
        const delta = (new Date(m.occurredAt).getTime() - first) / 1000;
        return `+${delta.toFixed(3)}`;
    });
});

function toneForMessage(msg: TraceMessage): string {
    if (msg.statusCode) return statusTone(msg.statusCode);
    if (msg.method === "INVITE" || msg.method === "ACK") return "success";
    if (msg.method === "REGISTER" || msg.method === "SUBSCRIBE") return "info";
    if (msg.method === "MESSAGE") return "accent";
    if (msg.method === "BYE" || msg.method === "CANCEL") return "warning";
    return "neutral";
}

function laneIndex(addr: string): number {
    return lanes.value.indexOf(addr);
}

function arrowGoesRight(msg: TraceMessage): boolean {
    const fromIdx = laneIndex(msg.direction === "outbound" ? msg.localAddr : msg.remoteAddr);
    const toIdx = laneIndex(msg.direction === "outbound" ? msg.remoteAddr : msg.localAddr);
    return fromIdx < toIdx;
}

// 拆 IP 和 port,port 用不同色
function splitAddr(addr: string): { ip: string; port: string } {
    const idx = addr.lastIndexOf(":");
    if (idx < 0) return { ip: addr, port: "" };
    return { ip: addr.slice(0, idx), port: addr.slice(idx) };
}

function laneRole(index: number): "设备端" | "平台端" {
    return index === 0 ? "设备端" : "平台端";
}

function deriveScenario(session: TraceSession): string {
    if (session.diagnosis?.category === "register_failure") return "register-fail";
    if (session.diagnosis?.category === "play_stuck") return "invite-stuck";
    return session.businessCode || "unknown";
}

function scenarioEmoji(session: TraceSession): string {
    const scenario = deriveScenario(session);
    if (scenario.startsWith("register")) return "📱";
    if (["invite-stuck", "realtime_play", "playback", "download", "talk", "broadcast"].includes(scenario)) return "📞";
    if (scenario === "keepalive") return "💓";
    if (scenario === "subscription") return "🔔";
    return "📋";
}

function scenarioLabel(session: TraceSession): string {
    const scenario = deriveScenario(session);
    const map: Record<string, string> = {
        "register": "设备注册",
        "register-fail": "注册失败",
        "invite-stuck": "点播卡住",
    };
    return map[scenario] || session.businessType || "未知业务";
}

function diagnosisStageLabel(stage: TraceSessionDiagnosis["stage"]): string {
    return ({ register: "注册", signaling: "信令", ack: "ACK", media: "媒体" } as Record<string, string>)[String(stage)] || String(stage);
}

function computeDuration(session: TraceSession): number {
    return new Date(session.lastAt).getTime() - new Date(session.firstAt).getTime();
}

async function copyCallId() {
    try {
        await navigator.clipboard.writeText(props.session.callId);
        Message.success("Call-ID 已复制");
    } catch {
        Message.warning("复制失败");
    }
}
</script>

<template>
    <div class="session-detail">
        <!-- 顶部返回栏 -->
        <div class="detail-topbar">
            <button class="back-btn action-neutral" @click="emit('back')">
                <ArrowLeft :size="14" />
                返回列表
            </button>
            <div class="topbar-title">
                <span class="scenario-emoji">{{ scenarioEmoji(session) }}</span>
                <span class="scenario-name">{{ scenarioLabel(session) }}</span>
                <span class="call-hash mono">#{{ session.callId.replace(/[^a-zA-Z0-9]/g, '').slice(0, 8) }}</span>
                <span :class="['state-tag', `state-${sessionStateLabel(session).tone}`]">
                    {{ sessionStateLabel(session).label }}
                </span>
            </div>
            <button class="copy-btn action-brand" @click="copyCallId">
                <Copy :size="13" />
                复制 Call-ID
            </button>
        </div>

        <div v-if="session.diagnosis" class="diagnosis-summary">
            <AlertTriangle :size="16" aria-hidden="true" />
            <strong>{{ diagnosisLabel(session.diagnosis.code) }}</strong>
            <span>{{ diagnosisStageLabel(session.diagnosis.stage) }}阶段</span>
            <span>{{ formatFullTime(session.diagnosis.observedAt) }}</span>
            <span>{{ session.diagnosis.source === 'runtime' ? '实时判定' : '历史证据保守回算' }}</span>
            <span v-if="session.diagnosis.cseq" class="mono">CSeq {{ session.diagnosis.cseq }}</span>
        </div>

        <!-- 会话元信息 -->
        <div class="detail-meta">
            <div class="meta-identity-row">
                <div class="meta-item">
                    <span class="meta-key">Call-ID</span>
                    <span class="meta-val mono call-id-val">{{ session.callId }}</span>
                </div>
                <div class="meta-item">
                    <span class="meta-key">关联设备</span>
                    <span class="meta-val mono">{{ session.deviceId || "-" }}</span>
                </div>
            </div>

            <div class="meta-metrics">
                <div class="meta-item meta-time-range">
                    <span class="meta-key">时间范围</span>
                    <div class="time-range-values">
                        <span class="time-point">
                            <small>开始</small>
                            <span class="meta-val mono">{{ formatFullTime(session.firstAt) }}</span>
                        </span>
                        <ArrowRight :size="14" class="time-range-arrow" aria-hidden="true" />
                        <span class="time-point">
                            <small>结束</small>
                            <span class="meta-val mono">{{ formatFullTime(session.lastAt) }}</span>
                        </span>
                    </div>
                </div>
                <div class="meta-item">
                    <span class="meta-key">持续时间</span>
                    <span class="meta-val mono metric-value">{{ formatDuration(computeDuration(session)) }}</span>
                </div>
                <div class="meta-item">
                    <span class="meta-key">报文统计</span>
                    <span class="meta-val mono metric-value">{{ session.messageCount }} 条</span>
                    <span class="metric-note">入 {{ session.inboundCount }} · 出 {{ session.outboundCount }}</span>
                </div>
                <div class="meta-item">
                    <span class="meta-key">业务类型</span>
                    <span class="meta-val metric-value">{{ session.businessType || "未知业务" }}</span>
                </div>
            </div>

            <div class="meta-route-row">
                <div class="route-endpoint">
                    <span class="meta-key">From</span>
                    <span class="meta-val mono">{{ session.fromUri || "-" }}</span>
                </div>
                <span class="route-arrow" aria-hidden="true"><ArrowRight :size="14" /></span>
                <div class="route-endpoint">
                    <span class="meta-key">To</span>
                    <span class="meta-val mono">{{ session.toUri || "-" }}</span>
                </div>
            </div>
        </div>

        <!-- sngrep 风格时序图 -->
        <div class="ladder-wrap">
            <div class="ladder">
                <!-- 泳道头部(粘性)- sngrep 风格:IP 上方带短横线托盘 -->
                <div class="lane-header-row" :style="{ '--lane-count': lanes.length }">
                    <div class="lane-header-timecol" />
                    <div
                        v-for="(lane, idx) in lanes"
                        :key="lane"
                        class="lane-header-cell mono"
                        :style="{ gridColumn: `${idx + 2}` }"
                    >
                        <span :class="['lane-role', idx === 0 ? 'role-device' : 'role-platform']">
                            <Video v-if="idx === 0" class="lane-role-icon" :size="14" :stroke-width="2" aria-hidden="true" />
                            <Server v-else class="lane-role-icon" :size="14" :stroke-width="2" aria-hidden="true" />
                            {{ laneRole(idx) }}
                        </span>
                        <span class="lane-header-label">
                            <span class="lane-ip">{{ splitAddr(lane).ip }}</span><span class="lane-port">{{ splitAddr(lane).port }}</span>
                        </span>
                        <span class="lane-header-tray" />
                    </div>
                </div>
                <!-- 时间栏右侧分隔线,贯穿整个 ladder -->
                <div class="time-col-divider" />
                <!-- 泳道竖线容器,竖线位于每个泳道格子中心 -->
                <div class="lane-lines">
                    <span
                        v-for="idx in lanes.length"
                        :key="idx"
                        class="lane-line"
                        :style="{ left: `calc(${(idx - 0.5) * (100 / lanes.length)}%)` }"
                    />
                </div>
                <!-- 每条报文 -->
                <button
                    v-for="(msg, msgIdx) in messages"
                    :key="msg.eventId"
                    type="button"
                    :class="['ladder-row', `tone-${toneForMessage(msg)}`, { active: selectedEventId === msg.eventId }]"
                    :style="{ '--lane-count': lanes.length }"
                    @click="emit('select-message', msg)"
                >
                    <div class="row-time-col">
                        <span class="row-time mono">{{ formatFullTime(msg.occurredAt) }}</span>
                        <span class="row-delta mono">{{ relativeDeltas[msgIdx] }}</span>
                    </div>
                    <div
                        :class="['row-arrow', arrowGoesRight(msg) ? 'to-right' : 'to-left']"
                        :style="{
                            gridColumnStart: Math.min(laneIndex(msg.localAddr), laneIndex(msg.remoteAddr)) + 2,
                            gridColumnEnd: Math.max(laneIndex(msg.localAddr), laneIndex(msg.remoteAddr)) + 3,
                            '--span-count': Math.abs(laneIndex(msg.localAddr) - laneIndex(msg.remoteAddr)) + 1,
                        }"
                    >
                        <span class="arrow-label mono">{{ methodLabel(msg) }} · CSeq {{ msg.cseq }} {{ msg.cseqMethod }}</span>
                        <div class="arrow-track">
                            <span class="arrow-line" />
                            <span class="arrow-head mono">{{ arrowGoesRight(msg) ? '>' : '<' }}</span>
                        </div>
                    </div>
                </button>
            </div>
        </div>
    </div>
</template>

<style scoped>
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, "JetBrains Mono", monospace; }
.session-detail {
    box-sizing: border-box;
    display: flex;
    flex-direction: column;
    width: 100%;
    height: 100%;
    min-height: 0;
    background: var(--uvp-panel-bg);
    border: 1px solid var(--uvp-panel-border);
    border-radius: 10px;
    overflow: hidden;
}

/* 顶部返回栏 */
.detail-topbar {
    display: flex;
    align-items: center;
    gap: 12px;
    height: 48px;
    padding: 0 14px;
    background: var(--uvp-list-toolbar-bg);
    border-bottom: 1px solid var(--uvp-panel-border);
    flex: 0 0 auto;
}
.back-btn,
.copy-btn {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    height: 28px;
    padding: 0 10px;
    border: 1px solid transparent;
    border-radius: 6px;
    font-size: 12px;
    font-weight: 500;
    cursor: pointer;
    transition: color 120ms, background 120ms, border-color 120ms;
}
.action-neutral {
    color: var(--uvp-text-secondary);
    background: color-mix(in srgb, var(--uvp-text-tertiary) 9%, var(--uvp-panel-bg));
    border-color: color-mix(in srgb, var(--uvp-text-tertiary) 18%, var(--uvp-panel-border));
}
.action-neutral:hover {
    color: var(--uvp-text-primary);
    background: color-mix(in srgb, var(--uvp-text-tertiary) 15%, var(--uvp-panel-bg));
}
.topbar-title { display: flex; align-items: center; gap: 8px; flex: 1; min-width: 0; }
.scenario-emoji { font-size: 18px; }
.scenario-name { color: var(--uvp-text-primary); font-size: 14px; font-weight: 600; }
.call-hash { color: var(--uvp-text-tertiary); font-size: 13px; }
.state-tag {
    display: inline-flex;
    align-items: center;
    height: 22px;
    padding: 0 10px;
    font-size: 11px;
    font-weight: 500;
    border-radius: 11px;
}
.state-success { color: #059669; background: rgb(5 150 105 / 10%); }
.state-warning { color: var(--uvp-warning); background: var(--uvp-warning-soft); }
.state-danger { color: var(--uvp-danger); background: var(--uvp-danger-soft); }
.state-info { color: var(--uvp-brand); background: var(--uvp-brand-soft); }
.state-neutral { color: var(--uvp-text-tertiary); background: color-mix(in srgb, var(--uvp-text-tertiary) 12%, transparent); }
.action-brand {
    color: var(--uvp-brand);
    background: var(--uvp-brand-soft);
    border-color: color-mix(in srgb, var(--uvp-brand) 28%, var(--uvp-panel-border));
}
.action-brand:hover {
    color: #fff;
    background: var(--uvp-brand);
    border-color: var(--uvp-brand);
}

/* 会话元信息 */
.detail-meta {
    display: flex;
    flex-direction: column;
    background: var(--uvp-list-toolbar-bg);
    border-bottom: 1px solid var(--uvp-panel-border);
}
.meta-item { display: flex; flex-direction: column; gap: 2px; min-width: 0; max-width: 100%; }
.meta-key { color: var(--uvp-text-tertiary); font-size: 10px; font-weight: 600; text-transform: uppercase; }
.meta-val { color: var(--uvp-text-primary); font-size: 12px; overflow-wrap: anywhere; }
.call-id-val { color: var(--uvp-brand); font-weight: 500; }
.meta-identity-row {
    display: grid;
    grid-template-columns: minmax(0, 1.8fr) minmax(180px, 1fr);
    gap: 24px;
    padding: 10px 16px;
    border-bottom: 1px solid color-mix(in srgb, var(--uvp-panel-border) 75%, transparent);
}
.meta-metrics {
    display: grid;
    grid-template-columns: minmax(280px, 2.4fr) repeat(3, minmax(86px, 1fr));
    border-bottom: 1px solid color-mix(in srgb, var(--uvp-panel-border) 75%, transparent);
}
.meta-metrics > .meta-item {
    justify-content: center;
    min-height: 58px;
    padding: 8px 14px;
    border-right: 1px solid color-mix(in srgb, var(--uvp-panel-border) 75%, transparent);
}
.meta-metrics > .meta-item:last-child { border-right: 0; }
.time-range-values {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 18px minmax(0, 1fr);
    align-items: center;
    gap: 6px;
}
.time-point { display: flex; flex-direction: column; gap: 1px; min-width: 0; }
.time-point small,
.metric-note { color: var(--uvp-text-tertiary); font-size: 10px; }
.time-range-arrow { color: var(--uvp-text-tertiary); }
.metric-value { font-size: 13px; font-weight: 600; }
.meta-route-row {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 28px minmax(0, 1fr);
    align-items: center;
    gap: 8px;
    padding: 9px 16px 11px;
}
.route-endpoint { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
.route-arrow {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 24px;
    height: 24px;
    color: var(--uvp-brand);
    background: var(--uvp-brand-soft);
    border-radius: 50%;
}
.diagnosis-summary {
    display: flex;
    align-items: center;
    gap: 10px;
    min-height: 36px;
    padding: 0 16px;
    color: var(--uvp-warning);
    background: var(--uvp-warning-soft);
    border-bottom: 1px solid color-mix(in srgb, var(--uvp-warning) 24%, var(--uvp-panel-border));
    font-size: 12px;
    flex-wrap: wrap;
}
.diagnosis-summary strong { color: var(--uvp-text-primary); }

/* ============ sngrep 时序图核心 ============ */
.ladder-wrap {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: 0;
    background: var(--uvp-panel-bg);
}
.ladder {
    display: flex;
    flex-direction: column;
    min-width: 100%;
    min-height: 100%;   /* 让内容撑满,让竖线贯穿 */
    position: relative;
    padding-bottom: 24px;
}

/* 泳道头 */
.lane-header-row {
    position: sticky;
    top: 0;
    z-index: 3;
    display: grid;
    grid-template-columns: 220px repeat(var(--lane-count), 1fr);
    align-items: end;
    min-height: 72px;
    padding: 12px 16px 0;
    background: var(--uvp-panel-bg);
}
.lane-header-timecol { grid-column: 1; }
.lane-header-cell {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 4px;
    padding: 4px 8px 0;
    font-size: 13px;
    text-align: center;
    letter-spacing: 0.2px;
    word-break: break-all;
}
.lane-role {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    color: var(--uvp-text-secondary);
    font-family: inherit;
    font-size: 12px;
    font-weight: 600;
}
.lane-role-icon { flex: 0 0 auto; }
.lane-role.role-device { color: #0f766e; }
.lane-role.role-platform { color: var(--uvp-brand); }
.lane-header-label { display: inline-flex; align-items: baseline; }
.lane-ip { color: #ec4899; font-weight: 700; }
.lane-port { color: var(--uvp-text-primary); font-weight: 700; }
.lane-header-tray {
    display: block;
    width: 60%;
    max-width: 180px;
    height: 2px;
    background: color-mix(in srgb, var(--uvp-text-tertiary) 42%, transparent);
    border-radius: 1px;
}

/* 泳道竖线(绝对定位,从托盘下沿贯穿到底) — 位于每个泳道栏位中心 */
.lane-lines {
    position: absolute;
    top: 72px;
    bottom: 0;
    left: 236px;
    right: 16px;
    pointer-events: none;
    z-index: 2;
}
.lane-line {
    position: absolute;
    top: 0;
    bottom: 0;
    width: 1px;
    background: color-mix(in srgb, var(--uvp-text-tertiary) 50%, transparent);
}

/* 时间栏右侧的分隔竖线,同样贯穿到底 */
.time-col-divider {
    position: absolute;
    top: 72px;
    bottom: 0;
    left: 220px;
    width: 1px;
    background: color-mix(in srgb, var(--uvp-text-tertiary) 25%, transparent);
    pointer-events: none;
    z-index: 2;
}

/* 每一行(一条报文) */
.ladder-row {
    position: relative;
    z-index: 1;
    display: grid;
    grid-template-columns: 220px repeat(var(--lane-count), 1fr);
    align-items: center;
    gap: 0;
    width: 100%;
    min-height: 56px;
    padding: 8px 16px;
    background: transparent;
    border: 0;
    cursor: pointer;
    text-align: left;
    transition: background 120ms;
}
.ladder-row:hover { background: color-mix(in srgb, var(--uvp-brand) 5%, transparent); }
.ladder-row.active { background: var(--uvp-brand-soft); }

/* 箭头本身 z-index 提到竖线之上,避免箭头被竖线穿过看着别扭 */
.row-arrow { position: relative; z-index: 3; }

.row-time-col { display: flex; flex-direction: column; gap: 2px; grid-column: 1; padding-right: 12px; }
.row-time { color: var(--uvp-text-primary); font-size: 12px; font-weight: 500; }
.row-delta { color: var(--uvp-brand); font-size: 11px; }

/* 箭头单元:横跨相邻泳道,起点和终点对齐两根竖线的中心 */
.row-arrow {
    display: flex;
    flex-direction: column;
    align-items: stretch;
    gap: 4px;
    /* 内缩到每个泳道栏位的中心:每个栏位宽度是 (100% / 2) = 50%,
       中心距离外沿是 25%,所以左右各 padding 25%*(1/spanCount) */
    padding-left: calc(100% / (var(--span-count, 2) * 2));
    padding-right: calc(100% / (var(--span-count, 2) * 2));
}
.arrow-label {
    color: var(--tone-color, var(--uvp-text-primary));
    font-size: 13px;
    font-weight: 700;
    text-align: center;
    letter-spacing: 0.3px;
}
.arrow-track {
    display: flex;
    align-items: center;
    height: 12px;
    gap: 0;
}
.arrow-line {
    flex: 1;
    height: 2px;
    background: var(--tone-color, var(--uvp-brand));
    border-radius: 1px;
}
.arrow-head {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 14px;
    font-size: 15px;
    font-weight: 700;
    color: var(--tone-color, var(--uvp-brand));
    line-height: 1;
}
.row-arrow.to-right .arrow-head { order: 2; }
.row-arrow.to-left .arrow-track { flex-direction: row-reverse; }
.row-arrow.to-left .arrow-head { order: 2; }

/* Tone 变量 */
.ladder-row.tone-success { --tone-color: #10b981; }
.ladder-row.tone-warning { --tone-color: var(--uvp-warning); }
.ladder-row.tone-danger  { --tone-color: var(--uvp-danger); }
.ladder-row.tone-info    { --tone-color: var(--uvp-brand); }
.ladder-row.tone-accent  { --tone-color: #7c3aed; }
.ladder-row.tone-neutral { --tone-color: var(--uvp-text-tertiary); }

@media (max-width: 768px) {
    .detail-topbar { flex-wrap: wrap; height: auto; min-height: 48px; padding: 8px 10px; }
    .topbar-title { order: -1; flex-basis: 100%; }
    .meta-identity-row { grid-template-columns: 1fr; gap: 8px; }
    .meta-metrics { grid-template-columns: repeat(2, minmax(0, 1fr)); }
    .meta-time-range { grid-column: 1 / -1; }
    .meta-metrics > .meta-item:nth-child(2) { border-top: 1px solid color-mix(in srgb, var(--uvp-panel-border) 75%, transparent); }
    .meta-route-row { grid-template-columns: 1fr; }
    .route-arrow { transform: rotate(90deg); }
}
</style>
