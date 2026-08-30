<script setup lang="ts">
import { ref } from "vue";
import RecordingPlansPanel from "@/views/gb28181/recording-schedules/RecordingPlansPanel.vue";

withDefaults(defineProps<{
  active?: boolean;
  stream?: unknown;
  nodeId?: unknown;
}>(), {
  active: false
});

const emit = defineEmits<{
  stats: [value: { enabledPlanTotal?: number | null; abnormalChannelTotal?: number | null }];
}>();
const panel = ref<{ refresh: () => void } | null>(null);

defineExpose({ refresh: () => panel.value?.refresh() });
</script>

<template>
  <RecordingPlansPanel ref="panel" :active="active" :stream="stream" :node-id="nodeId" @stats="emit('stats', $event)" />
</template>
