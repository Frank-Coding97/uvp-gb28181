import { reactive, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { parseQuery, toQuery, type TraceFiltersState, type WorkbenchView, type Direction } from "./traceFilterTypes";

/**
 * useTraceFilters
 * - Single source of truth for SIP log page filter state.
 * - Initializes from route.query. Mutations flow back into URL via router.replace.
 * - Reverse binding (URL → state after mount) is intentionally NOT enabled — see spec.
 */
export function useTraceFilters() {
    const route = useRoute();
    const router = useRouter();
    const state: TraceFiltersState = reactive(parseQuery(route.query as Record<string, unknown>));

    watch(
        () => ({ ...state }),
        (next) => {
            const nextQuery = toQuery(next as TraceFiltersState);
            const same = JSON.stringify(nextQuery) === JSON.stringify(route.query);
            if (same) return;
            router.replace({ path: route.path, query: nextQuery });
        },
        { deep: true }
    );

    function setView(view: WorkbenchView) {
        state.view = view;
    }

    function setSearchKeyword(keyword: string) {
        state.searchKeyword = keyword;
    }

    function setDeviceIds(deviceIds: string[]) {
        state.deviceIds = deviceIds;
    }

    function setDirection(direction: string) {
        state.direction = direction as Direction;
    }

    function setMethod(method: string) {
        state.method = method;
    }

    function setStatusCodeRange(range: string) {
        state.statusCodeRange = range;
    }

    function setAnomalyOnly(anomalyOnly: boolean) {
        state.anomalyOnly = anomalyOnly;
    }

    function reset() {
        state.deviceId = "";
        state.deviceIds = [];
        state.direction = "";
        state.method = "";
        state.statusCode = "";
        state.statusCodeRange = "";
        state.callId = "";
        state.searchKeyword = "";
        state.anomalyOnly = false;
    }

    return { state, setView, setSearchKeyword, setDeviceIds, setDirection, setMethod, setStatusCodeRange, setAnomalyOnly, reset };
}
