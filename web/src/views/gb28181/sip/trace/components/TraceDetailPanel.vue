<template>
    <div class="trace-detail-panel">
        <div v-if="!record" class="empty-state">
            <span>选择一条消息查看详情</span>
        </div>
        <div v-else class="detail-content">
            <div class="header-section">
                <div class="header-row">
                    <span class="label">方向:</span>
                    <span :class="['value', record.direction]">
                        {{ record.direction === 'inbound' ? '⬇️ 接收' : '⬆️ 发送' }}
                    </span>
                </div>
                <div class="header-row">
                    <span class="label">方法:</span>
                    <span class="value method">{{ record.method }}</span>
                    <span v-if="record.statusCode" class="status-code">{{ record.statusCode }}</span>
                </div>
                <div class="header-row">
                    <span class="label">时间:</span>
                    <span class="value">{{ formatTimestamp(record.ts) }}</span>
                </div>
                <div class="header-row">
                    <span class="label">From:</span>
                    <span class="value">{{ record.from }}</span>
                </div>
                <div class="header-row">
                    <span class="label">To:</span>
                    <span class="value">{{ record.to }}</span>
                </div>
                <div class="header-row">
                    <span class="label">Call-ID:</span>
                    <span class="value call-id">{{ record.callId }}</span>
                </div>
            </div>

            <div v-if="record.body" class="body-section">
                <div class="section-header">
                    <span>消息体</span>
                    <a-button size="small" @click="copyBody">复制</a-button>
                </div>
                <pre class="body-content" :class="bodyType">{{ record.body }}</pre>
            </div>

            <div v-if="parsedContent" class="parsed-section">
                <div class="section-header">
                    <span>解析结果</span>
                </div>
                <div class="parsed-content">
                    <div v-for="(value, key) in parsedContent" :key="key" class="parsed-row">
                        <span class="parsed-key">{{ key }}:</span>
                        <span class="parsed-value">{{ value }}</span>
                    </div>
                </div>
            </div>
        </div>
    </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { Message } from "@arco-design/web-vue";
import type { TraceRecord } from "../api/traceApi";

const props = defineProps<{
    record: TraceRecord | null;
}>();

const bodyType = computed(() => {
    if (!props.record?.body) return "";
    const body = props.record.body;
    if (body.includes("<?xml")) return "xml";
    if (body.includes("v=0") || body.includes("m=video")) return "sdp";
    return "plain";
});

const parsedContent = computed(() => {
    if (!props.record?.body) return null;
    const body = props.record.body;

    if (bodyType.value === "xml") {
        const cmdMatch = body.match(/<CmdType>([^<]+)<\/CmdType>/);
        const snMatch = body.match(/<SN>([^<]+)<\/SN>/);
        const deviceMatch = body.match(/<DeviceID>([^<]+)<\/DeviceID>/);
        const statusMatch = body.match(/<Status>([^<]+)<\/Status>/);
        return {
            命令类型: cmdMatch?.[1] || "-",
            序列号: snMatch?.[1] || "-",
            设备ID: deviceMatch?.[1] || "-",
            状态: statusMatch?.[1] || "-"
        };
    }

    if (bodyType.value === "sdp") {
        const lines = body.split("\r\n");
        const result: Record<string, string> = {};
        lines.forEach((line) => {
            if (line.startsWith("v=")) result["版本"] = line.substring(2);
            if (line.startsWith("s=")) result["会话名"] = line.substring(2);
            if (line.startsWith("m=")) result["媒体"] = line.substring(2);
        });
        return result;
    }

    return null;
});

function formatTimestamp(ts: number): string {
    return new Date(ts).toLocaleString("zh-CN", {
        year: "numeric",
        month: "2-digit",
        day: "2-digit",
        hour: "2-digit",
        minute: "2-digit",
        second: "2-digit",
        hour12: false
    });
}

function copyBody() {
    if (props.record?.body) {
        navigator.clipboard.writeText(props.record.body);
        Message.success("已复制消息体");
    }
}
</script>

<style scoped lang="less">
.trace-detail-panel {
    height: 100%;
    overflow-y: auto;
    background: #fff;
}

.empty-state {
    height: 100%;
    display: flex;
    align-items: center;
    justify-content: center;
    color: #86909c;
    font-size: 14px;
}

.detail-content {
    padding: 16px;
}

.header-section {
    margin-bottom: 16px;
}

.header-row {
    display: flex;
    align-items: center;
    margin-bottom: 8px;
    font-size: 13px;

    .label {
        width: 80px;
        color: #86909c;
        flex-shrink: 0;
    }

    .value {
        color: #1d2129;
        word-break: break-all;

        &.inbound {
            color: #059669;
            font-weight: 500;
        }

        &.outbound {
            color: #0d6efd;
            font-weight: 500;
        }

        &.method {
            font-weight: 600;
        }

        &.call-id {
            font-family: "SF Mono", Monaco, monospace;
            font-size: 12px;
        }
    }

    .status-code {
        margin-left: 8px;
        padding: 2px 6px;
        border-radius: 4px;
        background: #059669;
        color: #fff;
        font-size: 11px;
        font-weight: 600;
    }
}

.body-section,
.parsed-section {
    margin-top: 16px;
    border-top: 1px solid #e5e6eb;
    padding-top: 16px;
}

.section-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 8px;
    font-weight: 600;
    font-size: 13px;
    color: #1d2129;
}

.body-content {
    margin: 0;
    padding: 12px;
    background: #f7f8fa;
    border-radius: 6px;
    font-family: "SF Mono", Monaco, monospace;
    font-size: 12px;
    line-height: 1.6;
    color: #1d2129;
    overflow-x: auto;
    white-space: pre-wrap;
    word-break: break-all;

    &.xml {
        color: #0d6efd;
    }

    &.sdp {
        color: #059669;
    }
}

.parsed-content {
    padding: 12px;
    background: #f7f8fa;
    border-radius: 6px;
}

.parsed-row {
    display: flex;
    margin-bottom: 6px;
    font-size: 13px;

    &:last-child {
        margin-bottom: 0;
    }

    .parsed-key {
        width: 80px;
        color: #86909c;
        flex-shrink: 0;
    }

    .parsed-value {
        color: #1d2129;
        word-break: break-all;
    }
}

/* 深色模式适配 */
body[arco-theme='dark'] {
    .trace-detail-panel {
        background: #17171a;
    }

    .empty-state {
        color: #9ca3af;
    }

    .header-row {
        .label {
            color: #9ca3af;
        }

        .value {
            color: #e5e7eb;
        }
    }

    .body-section,
    .parsed-section {
        border-top-color: #2e2e30;
    }

    .section-header {
        color: #e5e7eb;
    }

    .body-content {
        background: #1e1e20;
        color: #e5e7eb;

        &.xml {
            color: #60a5fa;
        }

        &.sdp {
            color: #34d399;
        }
    }

    .parsed-content {
        background: #1e1e20;
    }

    .parsed-row {
        .parsed-key {
            color: #9ca3af;
        }

        .parsed-value {
            color: #e5e7eb;
        }
    }
}
</style>
