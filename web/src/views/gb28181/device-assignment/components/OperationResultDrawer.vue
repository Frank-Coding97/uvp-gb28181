<script setup lang="ts">
import { computed } from "vue";
import { AlertTriangle, CheckCircle2, CircleDashed, ExternalLink, RotateCcw } from "lucide-vue-next";
import type { AssignmentResult, GrantApplyResult } from "../api";

const props = defineProps<{
    visible: boolean;
    kind: "assignment" | "grant";
    result: AssignmentResult | GrantApplyResult | null;
}>();

const emit = defineEmits<{
    (event: "update:visible", visible: boolean): void;
    (event: "retry"): void;
    (event: "viewLog"): void;
}>();

const visible = computed({ get: () => props.visible, set: (value) => emit("update:visible", value) });
const summary = computed(() => props.result?.summary ?? { requested: 0, changed: 0, skipped: 0, failed: 0 });
const results = computed(() => props.result?.results ?? []);
const failed = computed(() => results.value.filter((item) => item.status === "failed"));
const title = computed(() => (props.kind === "assignment" ? "归属调整结果" : "共享授权结果"));
</script>

<template>
    <a-drawer v-model:visible="visible" :width="560" :title="title" unmount-on-close>
        <div class="operation-result">
            <div class="operation-result__summary">
                <div><strong>{{ summary.requested }}</strong><span>请求</span></div>
                <div class="success"><strong>{{ summary.changed }}</strong><span>已变更</span></div>
                <div class="skipped"><strong>{{ summary.skipped }}</strong><span>已跳过</span></div>
                <div class="failed"><strong>{{ summary.failed }}</strong><span>失败</span></div>
            </div>
            <a-alert v-if="summary.failed" type="warning">
                有 {{ summary.failed }} 台设备未完成操作，可只重试失败项。
            </a-alert>
            <div class="operation-result__list">
                <div v-for="item in results" :key="item.deviceId" class="operation-result__item" :class="item.status">
                    <CheckCircle2 v-if="item.status === 'changed'" :size="16" />
                    <CircleDashed v-else-if="item.status === 'skipped'" :size="16" />
                    <AlertTriangle v-else :size="16" />
                    <div class="operation-result__item-main">
                        <strong>{{ item.name || item.deviceCode || `设备 #${item.deviceId}` }}</strong>
                        <span>{{ item.deviceCode || `设备 #${item.deviceId}` }} · {{ item.message || item.status }}</span>
                    </div>
                    <span class="operation-result__status">{{ item.status === "changed" ? "成功" : item.status === "skipped" ? "跳过" : "失败" }}</span>
                </div>
                <div v-if="!results.length" class="operation-result__empty">没有可展示的操作明细</div>
            </div>
        </div>
        <template #footer>
            <a-space>
                <a-button @click="emit('viewLog')"><ExternalLink :size="14" /> 查看操作日志</a-button>
                <a-button v-if="failed.length" type="primary" @click="emit('retry')"><RotateCcw :size="14" /> 只重试失败项</a-button>
                <a-button v-else type="primary" @click="visible = false">完成</a-button>
            </a-space>
        </template>
    </a-drawer>
</template>

<style scoped lang="less">
.operation-result { display: flex; flex-direction: column; gap: 14px; }
.operation-result__summary { display: grid; grid-template-columns: repeat(4, 1fr); gap: 8px; }
.operation-result__summary > div { display: flex; flex-direction: column; gap: 2px; padding: 10px; border: 1px solid var(--uvp-panel-border); border-radius: 8px; }
.operation-result__summary strong { color: var(--uvp-text-primary); font-size: 20px; line-height: 24px; }
.operation-result__summary span { color: var(--uvp-text-tertiary); font-size: 11px; }
.operation-result__summary .success strong { color: var(--uvp-success); }
.operation-result__summary .skipped strong { color: var(--uvp-warning); }
.operation-result__summary .failed strong { color: var(--uvp-danger); }
.operation-result__list { display: flex; max-height: 390px; flex-direction: column; gap: 7px; overflow: auto; }
.operation-result__item { display: flex; align-items: center; gap: 8px; padding: 9px 10px; color: var(--uvp-text-secondary); border: 1px solid var(--uvp-panel-border); border-radius: 8px; }
.operation-result__item.changed { color: var(--uvp-success); }
.operation-result__item.skipped { color: var(--uvp-warning); }
.operation-result__item.failed { color: var(--uvp-danger); }
.operation-result__item-main { min-width: 0; flex: 1; }
.operation-result__item-main strong, .operation-result__item-main span { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.operation-result__item-main strong { color: var(--uvp-text-primary); font-size: 13px; }
.operation-result__item-main span { margin-top: 2px; color: var(--uvp-text-tertiary); font-size: 11px; }
.operation-result__status { flex: 0 0 auto; font-size: 11px; }
.operation-result__empty { padding: 32px; color: var(--uvp-text-tertiary); text-align: center; font-size: 12px; }
</style>
