<script setup lang="ts">
import LegacyIngressShell from "./workbench/ingress/LegacyIngressShell.vue";
import FFmpegPanel from "./workbench/ingress/FFmpegPanel.vue";
</script>

<template>
  <LegacyIngressShell>
    <template #default="{ active, scope, nodes, refreshKey }">
      <FFmpegPanel :active="active" :scope="scope" :nodes="nodes" :refresh-key="refreshKey" />
    </template>
  </LegacyIngressShell>
</template>
