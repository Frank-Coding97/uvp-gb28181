<template>
  <div class="media-rate-area">
    <div class="media-rate-area__plot">
      <MediaVChart
        v-if="hasData"
        :spec="chartSpec"
        title="媒体实时速率"
        :summary="summary"
        :show-summary="false"
        :active="active"
      />
      <div v-else class="media-rate-area__empty" role="status">暂无采样</div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import MediaVChart from "../zlm/workbench/components/MediaVChart.vue";
import { createMediaRateChartSpec, type MediaRateSample } from "./mediaRateChart";

const props = withDefaults(defineProps<{
  samples: MediaRateSample[];
  active?: boolean;
}>(), {
  active: true
});

const hasData = computed(() => props.samples.some(sample => sample.upstream !== null || sample.downstream !== null));
const summary = computed(() => props.samples.length ? `最近 5 分钟已记录 ${props.samples.length} 个采样点` : "暂无采样");
const chartSpec = computed(() => createMediaRateChartSpec(props.samples));
</script>

<style scoped>
.media-rate-area{display:flex;flex-direction:column;height:calc(100% - 74px);min-height:150px;min-width:0;overflow:hidden}.media-rate-area__plot{display:grid;flex:1;grid-template-rows:minmax(0,1fr);min-height:0;overflow:hidden}.media-rate-area__plot :deep(.media-vchart){position:relative;box-sizing:border-box;width:100%;height:100%;min-height:0;padding:0;background:transparent;border:0;border-radius:0;box-shadow:none}.media-rate-area__plot :deep(.media-vchart__header){display:none}.media-rate-area__plot :deep(.media-vchart__canvas){position:absolute;inset:0;width:100%;height:100%;min-height:0}.media-rate-area__empty{display:flex;align-items:center;justify-content:center;min-height:150px;color:var(--uvp-text-tertiary);font-size:12px}
</style>
