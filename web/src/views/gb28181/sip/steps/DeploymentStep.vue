<script setup lang="ts">
import { Building2, Globe2 } from "lucide-vue-next";
import type { SipDeploymentMode } from "@/api/gb28181";

defineProps<{ modelValue: SipDeploymentMode | "" }>();
const emit = defineEmits<{ "update:modelValue": [value: SipDeploymentMode] }>();
</script>

<template>
    <div class="deployment-options">
        <button
            type="button"
            class="deployment-option"
            :class="{ selected: modelValue === 'lan' }"
            @click="emit('update:modelValue', 'lan')"
        >
            <Building2 :size="22" aria-hidden="true" />
            <span class="option-copy">
                <strong>局域网部署</strong>
                <small>设备与平台位于同一网络</small>
            </span>
        </button>
        <button
            type="button"
            class="deployment-option"
            :class="{ selected: modelValue === 'public' }"
            @click="emit('update:modelValue', 'public')"
        >
            <Globe2 :size="22" aria-hidden="true" />
            <span class="option-copy">
                <strong>公网部署</strong>
                <small>设备通过公网地址注册</small>
            </span>
        </button>
    </div>
</template>

<style scoped>
.deployment-options {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 12px;
}

.deployment-option {
    display: flex;
    align-items: flex-start;
    gap: 12px;
    min-height: 92px;
    padding: 16px;
    color: var(--uvp-text-secondary);
    text-align: left;
    background: var(--uvp-list-toolbar-bg);
    border: 1px solid var(--uvp-panel-border);
    border-radius: 8px;
    cursor: pointer;
}

.deployment-option:hover,
.deployment-option.selected {
    color: var(--uvp-brand-strong);
    border-color: var(--uvp-brand);
    box-shadow: inset 0 0 0 1px var(--uvp-brand-soft);
}

.option-copy {
    display: grid;
    gap: 6px;
}

.option-copy strong {
    color: var(--uvp-text-primary);
    font-size: 14px;
    line-height: 20px;
}

.option-copy small {
    color: var(--uvp-text-tertiary);
    font-size: 12px;
    line-height: 18px;
}

@media (max-width: 640px) {
    .deployment-options {
        grid-template-columns: 1fr;
    }
}
</style>
