<script setup lang="ts">
import NodeListPanel from "./workbench/nodes/NodeListPanel.vue";
</script>

<template>
  <!-- Legacy route shell: NodeListPanel keeps routine actions behind gb28181:zlm:node:manage;
       deletion still uses ZLMNodeActionDialog impact fingerprint confirmation. -->
  <NodeListPanel :canonical="false" />
</template>
