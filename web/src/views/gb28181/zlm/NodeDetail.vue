<script setup lang="ts">
import NodeDetailPanel from "./workbench/nodes/NodeDetail.vue";
</script>

<template>
  <!-- Legacy route shell: reuse the canonical node service configuration page. -->
  <NodeDetailPanel :canonical="false" />
</template>
