<script setup lang="ts">
import { onMounted } from "vue";
import { useRoute, useRouter } from "vue-router";

const route = useRoute();
const router = useRouter();

onMounted(() => {
  const nodeId = typeof route.query.nodeId === "string" && /^\d+$/.test(route.query.nodeId) && Number(route.query.nodeId) > 0
    ? route.query.nodeId
    : undefined;
  void router.replace({ path: "/gb28181/zlm/overview", query: nodeId ? { nodeId } : {} });
});
</script>

<template>
  <div class="retired-runtime-overview" role="status" aria-live="polite">正在进入总览…</div>
</template>

<style scoped>
.retired-runtime-overview { display: grid; min-height: 240px; place-items: center; color: var(--zlm-text-3); }
</style>
