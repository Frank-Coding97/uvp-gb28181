<script setup lang="ts">
import LegacyIngressShell from "./workbench/ingress/LegacyIngressShell.vue";
import RTPPanel from "./workbench/ingress/RTPPanel.vue";
</script>

<template>
  <LegacyIngressShell>
    <template #default="{ active, scope, nodes, refreshKey }">
      <RTPPanel :active="active" :scope="scope" :nodes="nodes" :refresh-key="refreshKey" />
    </template>
  </LegacyIngressShell>
</template>
