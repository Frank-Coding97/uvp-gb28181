<script setup lang="ts">
import SchedulerLogPanel from "./workbench/scheduling/SchedulerLogPanel.vue";
</script>

<template>
  <div class="snow-fill">
    <div class="snow-fill-inner uvp-page-shell-flat scheduler-log-shell">
      <SchedulerLogPanel />
    </div>
  </div>
</template>

<style scoped>.scheduler-log-shell { padding: 0; overflow: hidden; }</style>
