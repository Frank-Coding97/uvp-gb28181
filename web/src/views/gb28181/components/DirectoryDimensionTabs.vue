<script setup lang="ts">
import { computed } from "vue";
import type { DirectoryDimension } from "@/api/gb28181";

/**
 * 三维度目录 tab 容器
 *
 * 顶部横向 Segmented(D-2 决策)切换三个维度:
 *  - native      国标自动注册
 *  - biz_group   业务分组
 *  - civil_code  行政区划
 *
 * 每个 tab 通过 slot 分发子组件(DirectoryTreeNative / BizGroup / CivilCode)
 * 未分配桶徽章通过 unassigned-count prop 传入(civil_code tab 显示红点数字)
 */

interface Props {
    /** 当前激活的维度(默认 native) */
    modelValue?: DirectoryDimension;
    /** 未分配行政区通道数(>0 时 civil_code tab 显示红点数字) */
    unassignedCount?: number;
}

const props = withDefaults(defineProps<Props>(), {
    modelValue: "native",
    unassignedCount: 0
});

const emit = defineEmits<{
    "update:modelValue": [value: DirectoryDimension];
    change: [value: DirectoryDimension];
}>();

const currentDimension = computed({
    get: () => props.modelValue,
    set: (v) => {
        emit("update:modelValue", v);
        emit("change", v);
    }
});

interface TabOption {
    label: string;
    value: DirectoryDimension;
}

const tabOptions: TabOption[] = [
    { label: "国标目录", value: "native" },
    { label: "业务分组", value: "biz_group" },
    { label: "行政区划", value: "civil_code" }
];
</script>

<template>
    <div class="directory-dimension-tabs">
        <div class="tab-bar">
            <a-radio-group
                v-model="currentDimension"
                type="button"
                size="small"
                class="tab-segmented"
            >
                <a-radio v-for="opt in tabOptions" :key="opt.value" :value="opt.value">
                    <span class="tab-label">
                        {{ opt.label }}
                        <a-badge
                            v-if="opt.value === 'civil_code' && unassignedCount > 0"
                            :count="unassignedCount"
                            :max-count="99"
                            class="tab-badge"
                        />
                    </span>
                </a-radio>
            </a-radio-group>
        </div>

        <div class="tab-content">
            <div v-show="currentDimension === 'native'">
                <slot name="native" />
            </div>
            <div v-show="currentDimension === 'biz_group'">
                <slot name="biz_group" />
            </div>
            <div v-show="currentDimension === 'civil_code'">
                <slot name="civil_code" />
            </div>
        </div>
    </div>
</template>

<style scoped>
.directory-dimension-tabs {
    display: flex;
    flex-direction: column;
    height: 100%;
}

.tab-bar {
    padding: 8px 4px;
    border-bottom: 1px solid var(--color-border-1);
}

.tab-segmented {
    width: 100%;
    display: flex;
}

.tab-segmented :deep(.arco-radio-button) {
    flex: 1;
    text-align: center;
}

.tab-label {
    display: inline-flex;
    align-items: center;
    gap: 4px;
}

.tab-badge :deep(.arco-badge-number) {
    background: var(--color-danger-6, #f53f3f);
    font-size: 10px;
    height: 14px;
    line-height: 14px;
    min-width: 14px;
    padding: 0 4px;
}

.tab-content {
    flex: 1;
    overflow: auto;
    padding: 8px 4px;
}
</style>
