<script setup lang="ts">
import NodeDetailPanel from "./workbench/nodes/NodeDetail.vue";
</script>

<template>
  <!-- Legacy route shell: NodeDetailPanel owns ZLMNodeActionDialog and keeps gb28181:zlm:restart permission gating. -->
  <NodeDetailPanel :canonical="false" />
</template>
