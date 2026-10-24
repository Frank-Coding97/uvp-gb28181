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

const props = withDefaults(
  defineProps<{
    samples: MediaRateSample[];
    active?: boolean;
  }>(),
  {
    active: true
  }
);

const hasData = computed(() => props.samples.some(sample => sample.upstream !== null || sample.downstream !== null));
const summary = computed(() => (props.samples.length ? `最近 5 分钟已记录 ${props.samples.length} 个采样点` : "暂无采样"));
const chartSpec = computed(() => createMediaRateChartSpec(props.samples));
</script>

<style scoped>
.media-rate-area {
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 150px;
  overflow: hidden;
}
.media-rate-area__plot {
  display: grid;
  flex: 1;
  grid-template-rows: minmax(0, 1fr);
  min-height: 0;
  overflow: hidden;
}
.media-rate-area__plot :deep(.media-vchart) {
  position: relative;
  box-sizing: border-box;
  width: 100%;
  height: 100%;
  min-height: 0;
  padding: 0;
  background: transparent;
  border: 0;
  border-radius: 0;
  box-shadow: none;
}
.media-rate-area__plot :deep(.media-vchart__header) {
  display: none;
}

/* VChart 的 _setCanvasStyle 会给宿主写内联 `display:block; position:relative`，
   内联样式优先级高于这里的 :deep 规则 ⇒ 画布一度脱离 absolute 约束进入正常流。
   用 `!important` 把 absolute 钉死，保证画布永远只是覆盖层、不参与父级高度计算。 */
.media-rate-area__plot :deep(.media-vchart__canvas) {
  position: absolute !important;
  inset: 0;
  width: 100%;
  height: 100%;
  min-height: 0;
}
.media-rate-area__empty {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 150px;
  font-size: 12px;
  color: var(--uvp-text-tertiary);
}
</style>
