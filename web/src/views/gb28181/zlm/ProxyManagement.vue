<script setup lang="ts">
import LegacyIngressShell from "./workbench/ingress/LegacyIngressShell.vue";
import ProxyPanel from "./workbench/ingress/ProxyPanel.vue";
</script>

<template>
  <LegacyIngressShell>
    <template #default="{ active, scope, nodes, refreshKey }">
      <ProxyPanel :active="active" :scope="scope" :nodes="nodes" :refresh-key="refreshKey" show-tabs />
    </template>
  </LegacyIngressShell>
</template>
