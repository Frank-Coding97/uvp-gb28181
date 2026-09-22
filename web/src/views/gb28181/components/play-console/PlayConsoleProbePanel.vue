<script setup lang="ts">
import { Activity, Gauge, Loader2, Maximize2, Signal, Video } from "@lucide/vue";

type ProbeValue = any;

const props = defineProps<{
  visible: boolean;
  active: boolean;
  probeResult: ProbeValue;
  probeOverview: ProbeValue;
  probeState: string;
  frameOverviewMeta: string;
  frameOverviewAriaLabel: string;
  probeBucketHeight: (bucket: ProbeValue) => number;
}>();

const emit = defineEmits<{ (event: "openTimeline"): void }>();
</script>

<template>
  <div v-if="visible" v-show="active" class="linked-detail linked-detail-probe" data-testid="linked-detail-probe">
    <div class="linked-probe-layout">
      <section class="linked-section probe-detail-card">
        <div class="section-hd first">
          <span class="section-title"><Video :size="13" />轨道明细</span>
          <span class="section-meta">{{
            props.probeResult ? `${[props.probeResult.video, props.probeResult.audio].filter(Boolean).length} 条轨道` : "待采样"
          }}</span>
        </div>
        <div class="probe-track-merged">
          <div class="probe-track-row video">
            <span class="probe-track-kind"><Video :size="12" />视频</span>
            <div class="probe-track-cells">
              <div>
                <span>精确 FPS</span
                ><strong>{{ props.probeResult?.video?.fps == null ? "—" : props.probeResult.video.fps.toFixed(1) }}</strong>
              </div>
              <div>
                <span>采样帧</span><strong>{{ props.probeResult?.video?.frameCount ?? "—" }}</strong>
              </div>
              <div>
                <span>关键帧</span><strong>{{ props.probeResult?.video?.keyFrameCount ?? "—" }}</strong>
              </div>
              <div>
                <span>GOP</span
                ><strong>{{
                  props.probeResult?.video?.gop == null ? "—" : `${props.probeResult.video.gop.toFixed(1)} 帧`
                }}</strong>
              </div>
            </div>
          </div>
          <div class="probe-track-row audio">
            <span class="probe-track-kind"><Activity :size="12" />音频</span>
            <div class="probe-track-cells">
              <div>
                <span>采样帧</span><strong>{{ props.probeResult?.audio?.frameCount ?? "—" }}</strong>
              </div>
              <div>
                <span>帧间隔</span
                ><strong>{{
                  props.probeResult?.audio?.averageIntervalMs == null
                    ? "—"
                    : `${props.probeResult.audio.averageIntervalMs.toFixed(1)} ms`
                }}</strong>
              </div>
            </div>
          </div>
        </div>
      </section>

      <section class="linked-section probe-detail-card">
        <div class="section-hd first">
          <span class="section-title"><Gauge :size="13" />时间戳监控</span>
          <span class="section-meta" :class="{ good: props.probeResult?.health.status === 'ok' }">{{
            props.probeResult ? (props.probeResult.health.status === "ok" ? "平稳" : "需关注") : "待检测"
          }}</span>
        </div>
        <div class="probe-health-grid">
          <div>
            <span>视频 DTS 间隔</span
            ><strong>{{
              props.probeResult?.timestamps.videoDtsIntervalMeanMs == null
                ? "—"
                : `${props.probeResult.timestamps.videoDtsIntervalMeanMs.toFixed(1)} ms`
            }}</strong
            ><em>均值</em>
          </div>
          <div>
            <span>帧到达抖动</span
            ><strong>{{
              props.probeResult?.timestamps.arrivalJitterMs == null
                ? "—"
                : `${props.probeResult.timestamps.arrivalJitterMs.toFixed(1)} ms`
            }}</strong
            ><em>标准差</em>
          </div>
          <div>
            <span>PTS-DTS</span
            ><strong>{{
              props.probeResult?.timestamps.ptsDtsMaxMs == null
                ? "—"
                : `${props.probeResult.timestamps.ptsDtsMaxMs.toFixed(1)} ms`
            }}</strong
            ><em>最大值</em>
          </div>
          <div>
            <span>音视频交织</span
            ><strong>{{
              props.probeResult?.timestamps.avArrivalSkewMaxMs == null
                ? "—"
                : `${props.probeResult.timestamps.avArrivalSkewMaxMs.toFixed(1)} ms`
            }}</strong
            ><em>最大偏差</em>
          </div>
        </div>
      </section>

      <section class="linked-section probe-detail-card">
        <div class="section-hd first">
          <span class="section-title"><Signal :size="13" />帧到达时间线</span>
          <span class="section-meta" :class="{ good: props.probeOverview && props.probeOverview.stallCount === 0 }">{{
            props.frameOverviewMeta
          }}</span>
        </div>
        <button
          type="button"
          class="frame-overview"
          :class="{ muted: !props.probeOverview }"
          :disabled="!props.probeOverview"
          data-testid="probe-timeline-open"
          :aria-label="props.frameOverviewAriaLabel"
          @click="emit('openTimeline')"
        >
          <template v-if="props.probeOverview">
            <div class="frame-overview-bars">
              <span
                v-for="(bucket, index) in props.probeOverview.buckets"
                :key="index"
                class="frame-overview-bar"
                :class="{ stalled: bucket.stalled, empty: bucket.count === 0 }"
                :style="{ height: `${props.probeBucketHeight(bucket)}%` }"
              ></span>
            </div>
            <div class="frame-overview-foot">
              <span
                >{{ props.probeOverview.totalFrames }} 帧<template v-if="props.probeOverview.truncated">
                  · 明细含末尾 {{ props.probeOverview.sampledFrames }} 帧</template
                ></span
              ><span class="frame-overview-cta">查看逐帧详情<Maximize2 :size="11" /></span>
            </div>
          </template>
          <div v-else class="frame-overview-empty">
            <template v-if="props.probeState === 'sampling'"
              ><Loader2 :size="18" class="spin" /><span>正在采集帧到达数据</span></template
            >
            <template v-else><Activity :size="18" /><span>启动检测后展示全量帧到达概览</span></template>
          </div>
        </button>
      </section>
    </div>
  </div>
</template>

<style lang="scss">
.linked-detail-probe {
  box-sizing: border-box;
  display: flex;
  flex-direction: column;
  height: var(--play-console-detail-height, 180px);
  min-height: 0;
  overflow: auto;
}
.linked-probe-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) minmax(0, 1.4fr);
  gap: 12px;
  align-items: stretch;
  height: 100%;
  min-height: 0;
}
.linked-probe-layout > .linked-section {
  display: flex;
  flex-direction: column;
  min-height: 0;
}
.linked-probe-layout > .linked-section > .section-hd {
  flex: 0 0 auto;
}
.linked-probe-layout > .linked-section > .probe-track-merged,
.linked-probe-layout > .linked-section > .probe-health-grid,
.linked-probe-layout > .linked-section > .frame-overview {
  flex: 1 1 0;
  min-height: 0;
}

@media (width <= 720px) {
  .linked-probe-layout {
    grid-template-columns: 1fr;
    height: auto;
  }
}
</style>
