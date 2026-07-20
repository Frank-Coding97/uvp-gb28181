import { reactive, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { parseQuery, toQuery, type TraceFiltersState, type WorkbenchView } from "./traceFilterTypes";

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

    function reset() {
        state.deviceId = "";
        state.direction = "";
        state.method = "";
        state.statusCode = "";
        state.callId = "";
        state.searchKeyword = "";
    }

    return { state, setView, setSearchKeyword, reset };
}
