<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useTraceFilters } from "./composables/useTraceFilters";
import type { Direction } from "./composables/traceFilterTypes";

const { state, setDeviceIds, setDirection, setMethod, setStatusCodeRange, setAnomalyOnly } = useTraceFilters();

// 设备列表(从 API 获取,这里先 mock)
const deviceOptions = ref<Array<{ value: string; label: string }>>([
    { value: "34020000001320000001", label: "IPC-001" },
    { value: "34020000001320000002", label: "IPC-002" },
    { value: "34020000001320000003", label: "NVR-001" }
]);

// 方向选项
const directionOptions = [
    { value: "", label: "全部" },
    { value: "inbound", label: "入站" },
    { value: "outbound", label: "出站" }
];

// 方法选项(常见 SIP 方法)
const methodOptions = [
    { value: "", label: "全部" },
    { value: "REGISTER", label: "REGISTER" },
    { value: "INVITE", label: "INVITE" },
    { value: "MESSAGE", label: "MESSAGE" },
    { value: "SUBSCRIBE", label: "SUBSCRIBE" },
    { value: "NOTIFY", label: "NOTIFY" },
    { value: "BYE", label: "BYE" },
    { value: "ACK", label: "ACK" },
    { value: "CANCEL", label: "CANCEL" }
];

// 状态码范围选项
const statusCodeRangeOptions = [
    { value: "", label: "全部" },
    { value: "1xx", label: "1xx 临时响应" },
    { value: "2xx", label: "2xx 成功" },
    { value: "3xx", label: "3xx 重定向" },
    { value: "4xx", label: "4xx 客户端错误" },
    { value: "5xx", label: "5xx 服务器错误" },
    { value: "6xx", label: "6xx 全局失败" }
];

// 本地状态(双向绑定)
const localDeviceIds = ref<string[]>(state.deviceIds || []);
const localDirection = ref<string>(state.direction || "");
const localMethod = ref<string>(state.method || "");
const localStatusCodeRange = ref<string>(state.statusCodeRange || "");
const localAnomalyOnly = ref<boolean>(state.anomalyOnly || false);

// 监听本地变化,同步到 filter state
watch(localDeviceIds, (val) => setDeviceIds(val));
watch(localDirection, (val) => setDirection(val as Direction | ""));
watch(localMethod, (val) => setMethod(val));
watch(localStatusCodeRange, (val) => setStatusCodeRange(val));
watch(localAnomalyOnly, (val) => setAnomalyOnly(val));

// 监听 filter state 变化,同步到本地(URL 变化时)
watch(
    () => state.deviceIds,
    (val) => {
        localDeviceIds.value = val || [];
    }
);
watch(
    () => state.direction,
    (val) => {
        localDirection.value = val || "";
    }
);
watch(
    () => state.method,
    (val) => {
        localMethod.value = val || "";
    }
);
watch(
    () => state.statusCodeRange,
    (val) => {
        localStatusCodeRange.value = val || "";
    }
);
watch(
    () => state.anomalyOnly,
    (val) => {
        localAnomalyOnly.value = val || false;
    }
);

const hasActiveFilters = computed(() => {
    return (
        localDeviceIds.value.length > 0 ||
        localDirection.value !== "" ||
        localMethod.value !== "" ||
        localStatusCodeRange.value !== "" ||
        localAnomalyOnly.value
    );
});

function clearAllFilters() {
    localDeviceIds.value = [];
    localDirection.value = "";
    localMethod.value = "";
    localStatusCodeRange.value = "";
    localAnomalyOnly.value = false;
}
</script>

<template>
    <div class="trace-filters-bar">
        <div class="filters-row">
            <a-select
                v-model="localDeviceIds"
                placeholder="选择设备"
                allow-clear
                allow-search
                multiple
                :max-tag-count="2"
                style="width: 240px"
            >
                <a-option v-for="opt in deviceOptions" :key="opt.value" :value="opt.value">
                    {{ opt.label }}
                </a-option>
            </a-select>

            <a-select v-model="localDirection" placeholder="方向" allow-clear style="width: 140px">
                <a-option v-for="opt in directionOptions" :key="opt.value" :value="opt.value">
                    {{ opt.label }}
                </a-option>
            </a-select>

            <a-select v-model="localMethod" placeholder="方法" allow-clear allow-search style="width: 160px">
                <a-option v-for="opt in methodOptions" :key="opt.value" :value="opt.value">
                    {{ opt.label }}
                </a-option>
            </a-select>

            <a-select v-model="localStatusCodeRange" placeholder="状态码" allow-clear style="width: 180px">
                <a-option v-for="opt in statusCodeRangeOptions" :key="opt.value" :value="opt.value">
                    {{ opt.label }}
                </a-option>
            </a-select>

            <a-checkbox v-model="localAnomalyOnly">仅显示异常会话</a-checkbox>

            <a-button v-if="hasActiveFilters" type="text" size="small" @click="clearAllFilters">清空筛选</a-button>
        </div>
    </div>
</template>

<style scoped>
.trace-filters-bar {
    padding: 8px 12px;
    background: #f7f8fa;
    border-radius: 6px;
}
.filters-row {
    display: flex;
    align-items: center;
    gap: 12px;
    flex-wrap: wrap;
}
</style>
