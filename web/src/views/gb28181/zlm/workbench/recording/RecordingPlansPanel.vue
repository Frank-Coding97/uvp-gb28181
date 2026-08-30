<script setup lang="ts">
import RecordingPlansPanel from "@/views/gb28181/recording-schedules/RecordingPlansPanel.vue";

withDefaults(defineProps<{
  active?: boolean;
  stream?: unknown;
  nodeId?: unknown;
}>(), {
  active: false
});
</script>

<template>
  <RecordingPlansPanel :active="active" :stream="stream" :node-id="nodeId" />
</template>
