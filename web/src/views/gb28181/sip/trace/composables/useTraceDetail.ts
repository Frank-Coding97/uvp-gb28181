import { ref } from "vue";
import { getTraceMessage, type TraceMessageDetail } from "@/api/gb28181-trace";

export function useTraceDetail() {
    const detail = ref<TraceMessageDetail | null>(null);
    const loading = ref(false);
    const error = ref("");

    async function load(eventId: string, options: { sensitive?: boolean; purpose?: string } = {}) {
        loading.value = true;
        error.value = "";
        try {
            const response = await getTraceMessage(eventId, options);
            if (response.code !== 0) throw new Error(response.message || "报文详情读取失败");
            detail.value = response.data;
            return response.data;
        } catch (reason) {
            error.value = reason instanceof Error ? reason.message : String(reason);
            detail.value = null;
            return null;
        } finally {
            loading.value = false;
        }
    }

    function clear() {
        detail.value = null;
        error.value = "";
    }

    return { detail, loading, error, load, clear };
}
