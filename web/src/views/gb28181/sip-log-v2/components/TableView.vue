<script setup lang="ts">
import type { TraceBusinessCode, TraceSessionSummary as TraceSession } from "@/api/gb28181-trace";
import { formatDuration, formatFullTime, sessionStateLabel } from "../helpers";

defineProps<{
    sessions: TraceSession[];
    selectedCallId: string;
    loading?: boolean;
}>();

const emit = defineEmits<{
    (e: "select-session", session: TraceSession): void;
}>();

const columns = [
    { title: "#", slotName: "idx", width: 52, align: "center" as const },
    { title: "业务类型", slotName: "business", width: 118 },
    { title: "起始方法", slotName: "method", width: 92 },
    { title: "From 国标 ID", slotName: "from", width: 164 },
    { title: "To 国标 ID", slotName: "to", width: 164 },
    { title: "Source", slotName: "source", ellipsis: true, tooltip: true, width: 174 },
    { title: "Destination", slotName: "destination", ellipsis: true, tooltip: true, width: 174 },
    { title: "报文数", slotName: "msgs", width: 76, align: "center" as const },
    { title: "起止时间", slotName: "time", width: 176 },
    { title: "时长", slotName: "duration", width: 84 },
    { title: "状态", slotName: "state", width: 100, align: "center" as const }
];

function firstMethod(session: TraceSession): string {
    return session.firstMethod || (session.methods || [])[0] || "?";
}
function fromUri(session: TraceSession): string {
    return session.fromId || session.deviceId || "-";
}
function toUri(session: TraceSession): string {
    return session.toId || "-";
}

function businessTone(code: TraceBusinessCode): string {
    if (["realtime_play", "playback", "download", "talk", "broadcast"].includes(code)) return "media";
    if (["keepalive", "register", "subscription", "ack"].includes(code)) return "connection";
    if (["alarm", "hangup"].includes(code)) return "warning";
    if (["catalog", "device_info", "device_status", "record_query", "mobile_position"].includes(code)) return "query";
    if (["device_control", "ptz", "playback_control"].includes(code)) return "control";
    return "unknown";
}
function sourceAddr(session: TraceSession): string {
    return session.sourceAddr || "-";
}
function destAddr(session: TraceSession): string {
    return session.destinationAddr || "-";
}
function computeDurationMs(session: TraceSession): number {
    return new Date(session.lastAt).getTime() - new Date(session.firstAt).getTime();
}

function methodTone(method: string): string {
    if (method === "REGISTER" || method === "SUBSCRIBE" || method === "NOTIFY") return "info";
    if (method === "INVITE" || method === "ACK") return "success";
    if (method === "BYE" || method === "CANCEL") return "warning";
    if (method === "MESSAGE") return "accent";
    return "neutral";
}
</script>

<template>
    <div class="table-view">
        <a-table
            class="uvp-data-table sip-log-table"
            row-key="callId"
            :data="sessions"
            :columns="columns"
            :loading="loading"
            :bordered="false"
            :pagination="false"
            :scroll="{ x: 1540, y: '100%' }"
            :row-class="(record: TraceSession) => record.callId === selectedCallId ? 'row-active' : ''"
            @row-click="(record: TraceSession) => emit('select-session', record)"
        >
            <template #idx="{ rowIndex }">
                <span class="mono idx-cell">{{ rowIndex + 1 }}</span>
            </template>
            <template #method="{ record }">
                <span :class="['method-chip', `chip-${methodTone(firstMethod(record))}`]">
                    {{ firstMethod(record) }}
                </span>
            </template>
            <template #business="{ record }">
                <span :class="['business-chip', `business-${businessTone(record.businessCode || 'unknown')}`]">
                    <span class="business-dot" aria-hidden="true" />
                    {{ record.businessType || "未知业务" }}
                </span>
            </template>
            <template #source="{ record }">
                <span class="mono addr-cell">{{ sourceAddr(record) }}</span>
            </template>
            <template #destination="{ record }">
                <span class="mono addr-cell">{{ destAddr(record) }}</span>
            </template>
            <template #from="{ record }">
                <span class="mono uri-cell">{{ fromUri(record) }}</span>
            </template>
            <template #to="{ record }">
                <span class="mono uri-cell">{{ toUri(record) }}</span>
            </template>
            <template #msgs="{ record }">
                <span class="msg-count">{{ record.messageCount }}</span>
            </template>
            <template #time="{ record }">
                <span class="mono time-cell">{{ formatFullTime(record.firstAt) }}</span>
            </template>
            <template #duration="{ record }">
                <span class="mono">{{ formatDuration(computeDurationMs(record)) }}</span>
            </template>
            <template #state="{ record }">
                <span :class="['state-chip', `state-${sessionStateLabel(record).tone}`]">
                    {{ sessionStateLabel(record).label }}
                </span>
            </template>
        </a-table>
    </div>
</template>

<style scoped>
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, monospace; }
.table-view {
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
.sip-log-table { flex: 1; min-height: 0; }
.sip-log-table :deep(.arco-table-tr) { cursor: pointer; }
.sip-log-table :deep(.arco-table-tr:hover .arco-table-td) { background: var(--uvp-table-row-hover-bg); }
.sip-log-table :deep(.row-active .arco-table-td) { background: var(--uvp-brand-soft) !important; box-shadow: inset 3px 0 0 var(--uvp-brand); }

.idx-cell { color: var(--uvp-text-tertiary); font-size: 12px; }
.time-cell { color: var(--uvp-text-secondary); font-size: 12px; }
.uri-cell { color: var(--uvp-text-secondary); font-size: 12px; }
.addr-cell { color: var(--uvp-text-primary); font-size: 12px; }
.msg-count { color: var(--uvp-text-primary); font-weight: 500; }

.business-chip {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: 24px;
    padding: 0 9px;
    color: var(--business-color);
    background: color-mix(in srgb, var(--business-color) 9%, transparent);
    border: 1px solid color-mix(in srgb, var(--business-color) 20%, transparent);
    border-radius: 5px;
    font-size: 12px;
    font-weight: 600;
    white-space: nowrap;
}
.business-dot { width: 5px; height: 5px; background: currentColor; border-radius: 50%; }
.business-media { --business-color: #0f766e; }
.business-connection { --business-color: #2563eb; }
.business-warning { --business-color: #dc2626; }
.business-query { --business-color: #7c3aed; }
.business-control { --business-color: #c2410c; }
.business-unknown { --business-color: var(--uvp-text-tertiary); }

.method-chip {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    height: 22px;
    padding: 0 8px;
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 12px;
    font-weight: 600;
    border-radius: 4px;
}
.chip-success { color: #059669; background: rgb(5 150 105 / 10%); }
.chip-warning { color: var(--uvp-warning); background: var(--uvp-warning-soft); }
.chip-danger { color: var(--uvp-danger); background: var(--uvp-danger-soft); }
.chip-info { color: var(--uvp-brand); background: var(--uvp-brand-soft); }
.chip-neutral { color: var(--uvp-text-tertiary); background: color-mix(in srgb, var(--uvp-text-tertiary) 12%, transparent); }
.chip-accent { color: #7c3aed; background: rgb(124 58 237 / 10%); }

.state-chip {
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
</style>
