import { ref, onMounted, onBeforeUnmount } from "vue";
import { fetchTraceHealth, type TraceHealth } from "@/api/gb28181-trace";

const DEFAULT_INTERVAL_MS = 30_000;

export function useTraceHealth(options: { intervalMs?: number; autoStart?: boolean } = {}) {
    const intervalMs = options.intervalMs ?? DEFAULT_INTERVAL_MS;
    const autoStart = options.autoStart ?? true;

    const health = ref<TraceHealth>({ state: "disabled", queueDepth: 0, queueCapacity: 0, dropped: 0 });
    const loading = ref(false);
    let timer: ReturnType<typeof setInterval> | null = null;

    async function refresh() {
        loading.value = true;
        try {
            const response = await fetchTraceHealth();
            if (response.code === 0 && response.data) {
                health.value = response.data;
            }
        } catch {
            // network errors do not flip health state; server-side degraded/disabled is authoritative
        } finally {
            loading.value = false;
        }
    }

    function start() {
        stop();
        timer = setInterval(refresh, intervalMs);
    }

    function stop() {
        if (timer) {
            clearInterval(timer);
            timer = null;
        }
    }

    if (autoStart) {
        onMounted(() => {
            refresh();
            start();
        });
        onBeforeUnmount(stop);
    }

    return { health, loading, refresh, start, stop };
}
