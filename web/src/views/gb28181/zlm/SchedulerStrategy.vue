<script setup lang="ts">
import SchedulerStrategyPanel from "./workbench/scheduling/SchedulerStrategyPanel.vue";
</script>

<template>
  <div class="snow-fill">
    <div class="snow-fill-inner uvp-page-shell-flat scheduler-shell">
      <SchedulerStrategyPanel />
    </div>
  </div>
</template>

<style scoped>.scheduler-shell { padding: 0; overflow: hidden; }</style>
