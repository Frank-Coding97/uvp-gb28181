<script setup lang="ts">
import { onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";

import { readStoredZLMNodeID } from "@/store/modules/zlm-context";
import { resolveLegacyMediaRoute } from "./mediaRoutes";

const route = useRoute();
const router = useRouter();
const invalid = ref(false);

onMounted(() => {
  const destination = resolveLegacyMediaRoute(
    route.path,
    route.query as Record<string, unknown>,
    { recentNodeId: readStoredZLMNodeID() }
  );
  if (!destination) {
    invalid.value = true;
    return;
  }
  void router.replace({ path: destination.path, query: destination.query });
});
</script>

<template>
  <section class="legacy-media-route">
    <div v-if="invalid" role="alert">
      <h1>旧地址无法识别</h1>
      <p>该流媒体地址已失效，请从“流媒体管理”菜单重新进入。</p>
    </div>
    <div v-else role="status" aria-live="polite">正在迁移到新的流媒体工作台…</div>
  </section>
</template>

<style scoped>
.legacy-media-route {
  display: grid;
  min-height: 240px;
  padding: var(--zlm-space-6, 24px);
  place-items: center;
  color: var(--zlm-text-2, var(--color-text-2));
  text-align: center;
}

.legacy-media-route h1,
.legacy-media-route p {
  margin: 0 0 8px;
}
</style>
