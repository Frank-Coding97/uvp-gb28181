<script setup lang="ts">
import { History, RotateCcw, Upload } from "@lucide/vue";
defineProps<{ canUpgrade: boolean; canReboot: boolean }>();
const emit = defineEmits<{ upgrade: []; records: []; reboot: [] }>();
</script>
<template>
    <a-doption class="device-action-menu-item" :disabled="!canUpgrade" :title="canUpgrade ? '' : '暂无设备升级权限'" @click="canUpgrade && emit('upgrade')">
        <Upload :size="14" /><span>固件升级</span>
    </a-doption>
    <a-doption class="device-action-menu-item" @click="emit('records')">
        <History :size="14" /><span>维护记录</span>
    </a-doption>
    <a-doption class="device-action-menu-item maintenance-reboot-menu" :disabled="!canReboot" :title="canReboot ? '' : '暂无设备重启权限'" @click="canReboot && emit('reboot')">
        <RotateCcw :size="14" /><span>重启设备</span>
    </a-doption>
</template>
<style scoped>
.maintenance-reboot-menu { margin-top: 4px; border-top: 1px solid var(--uvp-panel-border); color: var(--uvp-warning); }
</style>
