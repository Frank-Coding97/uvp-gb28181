<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";
import SchedulerLogPanel from "./workbench/scheduling/SchedulerLogPanel.vue";
import { schedulerLogResultFromQuery } from "./schedulerLogState";

const route = useRoute();
const initialLogResult = computed(() => schedulerLogResultFromQuery(route.query.result));
</script>

<template>
  <div class="snow-fill scheduler-log-page">
    <div class="snow-fill-inner uvp-page-shell-flat scheduler-log-shell">
      <SchedulerLogPanel :initial-result="initialLogResult" />
    </div>
  </div>
</template>

<style scoped>
.scheduler-log-page {
  height: 100%;
  min-height: 0;
  overflow: hidden;
}
.scheduler-log-shell {
  display: flex;
  height: 100%;
  min-height: 0;
  padding: 0;
  overflow: hidden;
}
</style>
