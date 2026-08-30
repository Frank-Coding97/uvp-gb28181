<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";

import RecordingPlansPanel from "./RecordingPlansPanel.vue";

const route = useRoute();
const stream = computed(() => Array.isArray(route.query.stream) ? route.query.stream[0] : route.query.stream);
const nodeId = computed(() => Array.isArray(route.query.nodeId) ? route.query.nodeId[0] : route.query.nodeId);
</script>

<template>
  <div class="snow-fill recording-schedules-route">
    <div class="snow-fill-inner uvp-page-shell-flat recording-schedules-route__inner">
      <RecordingPlansPanel :active="true" :stream="stream" :node-id="nodeId" />
    </div>
  </div>
</template>

<style scoped>
.recording-schedules-route,
.recording-schedules-route__inner { box-sizing: border-box; width: 100%; height: 100%; min-height: 0; overflow: hidden; }
</style>
