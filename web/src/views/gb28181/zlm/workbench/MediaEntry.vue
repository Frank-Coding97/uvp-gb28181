<script setup lang="ts">
import { onMounted, ref } from "vue";
import { storeToRefs } from "pinia";
import { useRouter } from "vue-router";

import { useRouteConfigStore } from "@/store/modules/route-config";
import { resolveFirstMediaWorkspace } from "./mediaAccess";

const router = useRouter();
const routeStore = useRouteConfigStore();
const { routeList } = storeToRefs(routeStore);
const denied = ref(false);

onMounted(() => {
  const destination = resolveFirstMediaWorkspace(
    routeList.value.map((route: Menu.MenuOptions) => route.path)
  );
  if (destination) {
    void router.replace(destination);
    return;
  }
  denied.value = true;
});
</script>

<template>
  <section class="media-entry" :class="{ 'media-entry--denied': denied }">
    <div v-if="denied" class="media-entry__state" role="alert">
      <span class="media-entry__code">403</span>
      <h1>没有流媒体管理权限</h1>
      <p>当前账号没有可进入的流媒体工作台，请联系管理员分配对应菜单和业务权限。</p>
    </div>
    <div v-else class="media-entry__state" role="status" aria-live="polite">
      <span class="media-entry__pulse" aria-hidden="true" />
      <p>正在进入可访问的流媒体工作台…</p>
    </div>
  </section>
</template>

<style scoped>
.media-entry {
  display: grid;
  min-height: 100%;
  padding: var(--zlm-space-6, 24px);
  place-items: center;
  color: var(--zlm-text-2, var(--color-text-2));
  background: var(--zlm-page-bg, var(--color-fill-1));
}

.media-entry__state {
  display: grid;
  max-width: 520px;
  justify-items: center;
  gap: 10px;
  padding: 30px;
  text-align: center;
  background: var(--zlm-card, var(--color-bg-2));
  border: 1px solid var(--zlm-border, var(--color-border-2));
  border-radius: var(--zlm-radius-xl, 14px);
}

.media-entry__state h1,
.media-entry__state p {
  margin: 0;
}

.media-entry__code {
  color: var(--zlm-warn-600, rgb(var(--warning-6)));
  font-family: var(--zlm-font-mono, monospace);
  font-size: 28px;
  font-weight: 700;
}

.media-entry__pulse {
  width: 12px;
  height: 12px;
  background: var(--zlm-brand-500, rgb(var(--primary-6)));
  border-radius: 50%;
}
</style>
