<script setup lang="ts">
import NodeListPanel from "./workbench/nodes/NodeListPanel.vue";
</script>

<template>
  <!-- Legacy route shell: NodeListPanel retains testZLMNodeConnection, ZLMNodeActionDialog,
       gb28181:zlm:node:manage and gb28181:zlm:node:kick permission gates, impact fingerprint and danger confirmation. -->
  <NodeListPanel :canonical="false" />
</template>
