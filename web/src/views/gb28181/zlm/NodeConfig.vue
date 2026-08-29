<script setup lang="ts">
import NodeConfigPanel from "./components/NodeConfigPanel.vue";

defineProps<{ nodeId: number }>();
</script>

<template>
  <NodeConfigPanel :node-id="nodeId" />
</template>
