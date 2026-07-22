import { ref } from "vue";
import { listTraceMessages, type TraceMessagePage, type TraceMessageQuery, type TraceMessageSummary } from "@/api/gb28181-trace";

export function useTraceMessages() {
    const items = ref<TraceMessageSummary[]>([]);
    const nextCursor = ref("");
    const loading = ref(false);
    const error = ref("");
    let generation = 0;

    async function load(params: TraceMessageQuery, append = false): Promise<TraceMessagePage | null> {
        const current = ++generation;
        loading.value = true;
        error.value = "";
        try {
            const response = await listTraceMessages({ ...params, cursor: append ? nextCursor.value || undefined : undefined });
            if (current !== generation) return null;
            if (response.code !== 0) throw new Error(response.message || "SIP 日志查询失败");
            const page = response.data;
            items.value = append ? [...items.value, ...page.items] : page.items;
            nextCursor.value = page.nextCursor || "";
            return page;
        } catch (reason) {
            if (current === generation) error.value = reason instanceof Error ? reason.message : String(reason);
            return null;
        } finally {
            if (current === generation) loading.value = false;
        }
    }

    function reset() {
        generation += 1;
        items.value = [];
        nextCursor.value = "";
        error.value = "";
    }

    return { items, nextCursor, loading, error, load, reset };
}
