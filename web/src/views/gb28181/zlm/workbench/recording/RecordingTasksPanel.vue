<script setup lang="ts">
import { ref } from "vue";
import RecordingWorkspacePanel from "@/views/gb28181/cloud-recordings/RecordingWorkspacePanel.vue";

withDefaults(defineProps<{
  active?: boolean;
  autoRefresh?: boolean;
  nodeId?: unknown;
}>(), { active: false, autoRefresh: true });

const emit = defineEmits<{
  stats: [value: { filesTotal?: number | null; activeTotal?: number | null }];
}>();
const panel = ref<{ refresh: () => void } | null>(null);

defineExpose({ refresh: () => panel.value?.refresh() });
</script>

<template>
  <RecordingWorkspacePanel
    ref="panel"
    :active="active"
    :auto-refresh="autoRefresh"
    :node-id="nodeId"
    mode="tasks"
    @stats="emit('stats', $event)"
  />
</template>
