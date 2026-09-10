<script setup lang="ts">
import { computed } from "vue";
import type { WorkOrderCamera } from "@/api/gb28181-work-recording";
import { workOrderChannelRows, workOrderStateColor, workOrderStateLabel } from "./orderState";

const props = defineProps<{
    visible: boolean;
    /** 作业单号，显示在标题下方便核对。 */
    orderId?: string;
    projectName?: string;
    cameras?: WorkOrderCamera[];
}>();

const emit = defineEmits<{ (event: "close"): void }>();

const rows = computed(() => workOrderChannelRows(props.cameras || []));

const visible = computed({
    get: () => props.visible,
    set: value => {
        if (!value) emit("close");
    }
});
</script>

<template>
    <a-modal v-model:visible="visible" modal-class="uvp-system-dialog" title="录制通道明细" :width="760" :footer="false">
        <p class="work-order-channels-subtitle">
            <span>{{ projectName || "未填写项目名称" }}</span>
            <small v-if="orderId">作业单号 {{ orderId.slice(0, 8) }}</small>
        </p>
        <a-table class="uvp-data-table" :data="rows" row-key="key" :bordered="false" :pagination="false">
            <template #columns>
                <a-table-column title="设备名称" :width="170" :ellipsis="true" :tooltip="true">
                    <template #cell="{ record }">{{ record.deviceName }}</template>
                </a-table-column>
                <a-table-column title="设备ID" :width="200">
                    <template #cell="{ record }"><span class="work-order-channels-code">{{ record.deviceId }}</span></template>
                </a-table-column>
                <a-table-column title="通道名称" :width="170" :ellipsis="true" :tooltip="true">
                    <template #cell="{ record }">{{ record.channelName }}</template>
                </a-table-column>
                <a-table-column title="通道ID" :width="200">
                    <template #cell="{ record }"><span class="work-order-channels-code">{{ record.channelId }}</span></template>
                </a-table-column>
                <a-table-column title="状态" :width="96">
                    <template #cell="{ record }"><a-tag :color="workOrderStateColor(record.state)">{{ workOrderStateLabel(record.state) }}</a-tag></template>
                </a-table-column>
            </template>
            <template #empty><a-empty description="没有关联的录制通道" /></template>
        </a-table>
    </a-modal>
</template>

<style scoped>
.work-order-channels-subtitle { display: flex; align-items: baseline; gap: 10px; margin: 0 0 12px; font-size: 13px; }
.work-order-channels-subtitle small { color: var(--color-text-3); }
.work-order-channels-code { font-family: var(--font-mono, monospace); }
</style>
