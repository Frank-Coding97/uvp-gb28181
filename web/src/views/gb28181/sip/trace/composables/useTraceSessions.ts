import { ref } from "vue";
import { listTraceSessionMessages, listTraceSessions, type TraceMessageSummary, type TraceSessionQuery, type TraceSessionSummary } from "@/api/gb28181-trace";

export function useTraceSessions() {
    const items = ref<TraceSessionSummary[]>([]);
    const messages = ref<TraceMessageSummary[]>([]);
    const selected = ref<TraceSessionSummary | null>(null);
    const loading = ref(false);
    const messageLoading = ref(false);
    const error = ref("");

    async function load(params: TraceSessionQuery) {
        loading.value = true;
        error.value = "";
        try {
            const response = await listTraceSessions(params);
            if (response.code !== 0) throw new Error(response.message || "会话查询失败");
            items.value = response.data.items || [];
            selected.value = null;
            messages.value = [];
        } catch (reason) {
            error.value = reason instanceof Error ? reason.message : String(reason);
        } finally {
            loading.value = false;
        }
    }

    async function loadMessages(session: TraceSessionSummary) {
        selected.value = session;
        messages.value = [];
        if (!session.originalAvailable) return;
        messageLoading.value = true;
        try {
            const response = await listTraceSessionMessages(session.callId, {
                from: new Date(Date.parse(session.firstAt) - 1000).toISOString(),
                to: new Date(Date.parse(session.lastAt) + 1000).toISOString(),
                deviceId: session.deviceId || undefined,
                limit: 500
            });
            if (response.code !== 0) throw new Error(response.message || "会话报文读取失败");
            messages.value = [...response.data.items].sort((a, b) => Date.parse(a.occurredAt) - Date.parse(b.occurredAt));
        } catch (reason) {
            error.value = reason instanceof Error ? reason.message : String(reason);
        } finally {
            messageLoading.value = false;
        }
    }

    return { items, messages, selected, loading, messageLoading, error, load, loadMessages };
}
