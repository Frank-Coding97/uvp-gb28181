<script setup lang="ts">
import RecordingWorkspacePanel from "./RecordingWorkspacePanel.vue";
</script>

<template>
  <div class="snow-fill cloud-recordings-route">
    <div class="snow-fill-inner uvp-page-shell-flat cloud-recordings-route__inner">
      <RecordingWorkspacePanel :active="true" mode="all" />
    </div>
  </div>
</template>

<style scoped>
.cloud-recordings-route,
.cloud-recordings-route__inner { box-sizing: border-box; width: 100%; height: 100%; min-height: 0; overflow: hidden; }
</style>
