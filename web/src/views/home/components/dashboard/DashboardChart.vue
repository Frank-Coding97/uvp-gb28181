<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import MediaVChart from "@/views/gb28181/zlm/workbench/components/MediaVChart.vue";
import type { MediaChartSpec } from "@/views/gb28181/zlm/workbench/chart/overviewChart";

const props = defineProps<{ spec: MediaChartSpec; title: string; summary?: string }>();
const host = ref<HTMLElement | null>(null);
const themeRevision = ref(0);
let themeObserver: MutationObserver | null = null;

// Canvas does not resolve CSS custom properties as SVG does.
const resolvedSpec = computed(() => {
  void themeRevision.value;
  if (!host.value) return props.spec;
  const styles = getComputedStyle(host.value);
  function resolve(value: unknown): unknown {
    if (typeof value === "string") {
      return value.replace(/var\((--[\w-]+)\)/g, (original, name: string) => styles.getPropertyValue(name).trim() || original);
    }
    if (Array.isArray(value)) return value.map(resolve);
    if (value && typeof value === "object") return Object.fromEntries(Object.entries(value).map(([key, item]) => [key, resolve(item)]));
    return value;
  }
  return resolve(props.spec) as MediaChartSpec;
});

onMounted(() => {
  themeObserver = new MutationObserver(() => { themeRevision.value += 1; });
  const options = { attributes: true, attributeFilter: ["class", "style", "arco-theme", "data-theme"] };
  themeObserver.observe(document.body, options);
  themeObserver.observe(document.documentElement, options);
});
onBeforeUnmount(() => themeObserver?.disconnect());
</script>

<template>
  <div ref="host" class="dashboard-chart">
    <MediaVChart v-if="host" :spec="resolvedSpec" :title="title" :summary="summary || title" :show-summary="false" />
  </div>
</template>

<style scoped>
.dashboard-chart{position:relative;width:100%;height:100%;min-width:0;min-height:0}
.dashboard-chart :deep(.media-vchart){position:absolute;inset:0;box-sizing:border-box;width:100%;height:100%;min-height:0;padding:0;background:transparent;border:0;border-radius:0;box-shadow:none}
.dashboard-chart :deep(.media-vchart__header){display:none}
.dashboard-chart :deep(.media-vchart__canvas){width:100%;height:100%;min-height:0}
</style>
