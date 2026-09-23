<script setup lang="ts">
import { computed } from "vue";
import { Activity, AlertTriangle, CheckCircle2, Loader2, Play, Signal, Video } from "@lucide/vue";

interface ProbeDurationOption {
  value: number;
  label: string;
}

const props = defineProps<{
  phase: string;
  streamInfo: Record<string, any>;
  playResult: Record<string, any> | null;
  readerCount: number;
  totalReaderCount: number;
  monitorBytesSpeedText: string;
  monitorTotalBytesText: string;
  monitorCollectedAtText: string;
  monitorAudioTrack?: Record<string, any> | null;
  liveMetrics: Record<string, any>;
  probeState: string;
  probeStatusText: string;
  canDiagnosePlayback: boolean;
  probeButtonText: string;
  probeDurationMs: number;
  probeDurations: readonly ProbeDurationOption[];
  probeResult: Record<string, any> | null;
  probeFinishedAt: string;
  formatLoss: (value: number | null) => string;
}>();

const emit = defineEmits<{
  (event: "startProbe"): void;
  (event: "update:probeDuration", value: number): void;
}>();

const durationModel = computed({
  get: () => props.probeDurationMs,
  set: value => emit("update:probeDuration", Number(value))
});
</script>

<template>
  <div class="panel probe-panel" data-testid="linked-side-probe">
    <section class="stream-brief probe-card" data-testid="stream-brief">
      <div class="section-hd first">
        <span class="section-title"><Signal :size="13" />概览</span>
        <span class="section-meta">2 秒刷新 · {{ monitorCollectedAtText }}</span>
      </div>
      <div class="stream-brief-overview">
        <div>
          <span>当前观看</span>
          <strong>{{ readerCount }}</strong>
          <small>累计 {{ totalReaderCount }}</small>
        </div>
        <div>
          <span>数据速率</span>
          <strong>{{ monitorBytesSpeedText }}</strong>
          <small>累计 {{ monitorTotalBytesText }}</small>
        </div>
        <div>
          <span>媒体节点</span>
          <strong :title="streamInfo.nodeName">{{ streamInfo.nodeName }}</strong>
          <small :title="streamInfo.nodeHost">{{ streamInfo.nodeHost }}</small>
        </div>
        <div>
          <span>流 ID</span>
          <strong :title="streamInfo.streamId || '—'">{{ streamInfo.streamId || "—" }}</strong>
          <small :title="`SSRC ${streamInfo.ssrc || '—'} · APP ${playResult?.app || '—'}`">
            SSRC {{ streamInfo.ssrc || "—" }} · APP {{ playResult?.app || "—" }}
          </small>
        </div>
      </div>

      <div class="stream-brief-split">
        <section class="stream-brief-kind video">
          <header><Video :size="12" />视频</header>
          <div class="stream-brief-rows">
            <div>
              <span>编码</span><strong>{{ streamInfo.videoCodec }}</strong>
            </div>
            <div>
              <span>分辨率</span><strong>{{ streamInfo.resolution }}</strong>
            </div>
            <div>
              <span>帧率</span><strong>{{ streamInfo.videoFps || "—" }}</strong>
            </div>
            <div>
              <span>丢包</span>
              <strong :class="{ warn: (liveMetrics.videoLoss ?? 0) > 0.005, err: (liveMetrics.videoLoss ?? 0) > 0.02 }">
                {{ formatLoss(liveMetrics.videoLoss) }}
              </strong>
            </div>
          </div>
        </section>
        <section class="stream-brief-kind audio">
          <header><Activity :size="12" />音频</header>
          <div class="stream-brief-rows">
            <div>
              <span>编码</span><strong>{{ streamInfo.audioCodec }}</strong>
            </div>
            <div>
              <span>采样率</span><strong>{{ streamInfo.audioSampleRate ? `${streamInfo.audioSampleRate} Hz` : "—" }}</strong>
            </div>
            <div>
              <span>声道</span><strong>{{ monitorAudioTrack?.channels || "—" }}</strong>
            </div>
            <div>
              <span>丢包</span>
              <strong :class="{ warn: (liveMetrics.audioLoss ?? 0) > 0.005, err: (liveMetrics.audioLoss ?? 0) > 0.02 }">
                {{ formatLoss(liveMetrics.audioLoss) }}
              </strong>
            </div>
          </div>
        </section>
      </div>
    </section>

    <section class="probe-card" data-testid="probe-check">
      <div class="section-hd first">
        <span class="section-title"><Activity :size="13" />逐帧健康检测</span>
        <span class="probe-status" :class="probeState"><span class="dot"></span>{{ probeStatusText }}</span>
      </div>

      <div v-if="canDiagnosePlayback" class="probe-action-row">
        <button
          class="probe-action"
          data-testid="probe-start"
          :disabled="phase !== 'playing' || probeState === 'sampling'"
          @click="emit('startProbe')"
        >
          <Loader2 v-if="probeState === 'sampling'" :size="14" class="spin" />
          <Play v-else :size="14" />
          <span>{{ probeButtonText }}</span>
        </button>
        <a-select
          v-model="durationModel"
          class="probe-duration"
          data-testid="probe-duration"
          aria-label="采样时长"
          :disabled="probeState === 'sampling'"
        >
          <a-option v-for="duration in probeDurations" :key="duration.value" :value="duration.value">
            {{ duration.label }}
          </a-option>
        </a-select>
      </div>

      <div class="probe-summary" :class="{ muted: probeState !== 'complete' }">
        <div>
          <span>采样时长</span
          ><strong>{{ probeResult ? (probeResult.summary.sampleDurationMs / 1000).toFixed(2) : "—" }}<em>s</em></strong>
        </div>
        <div>
          <span>采集帧数</span><strong>{{ probeResult?.summary.frameCount ?? "—" }}<em>帧</em></strong>
        </div>
        <div>
          <span>采样流量</span
          ><strong>{{ probeResult ? Math.round(probeResult.summary.totalBytes / 1024) : "—" }}<em>KB</em></strong>
        </div>
      </div>

      <div
        v-if="probeState === 'complete'"
        class="probe-verdict"
        :class="{ warning: probeResult?.health.status === 'warning', error: probeResult?.health.status === 'error' }"
        :title="`完成于 ${probeFinishedAt} · ${probeResult?.health.issues?.[0]?.message || '未发现异常帧间隔'}`"
      >
        <CheckCircle2 v-if="probeResult?.health.status === 'ok'" :size="14" />
        <AlertTriangle v-else :size="14" />
        <strong>{{ probeResult?.health.status === "ok" ? "流健康，帧序与时间戳连续" : "检测发现需要关注的问题" }}</strong>
      </div>
    </section>
  </div>
</template>

<style scoped lang="scss">
.panel {
  display: grid;
  gap: 10px;
}
.probe-panel {
  gap: 8px;
}
.section-hd {
  display: flex;
  gap: 8px;
  align-items: center;
  justify-content: space-between;
  margin-top: 10px;
}
.section-hd.first {
  margin-top: 0;
}
.section-title {
  display: inline-flex;
  gap: 6px;
  align-items: center;
  font-size: 11.5px;
  font-weight: 600;
  color: var(--uvp-text-secondary);
}
.section-meta {
  display: inline-flex;
  gap: 4px;
  align-items: center;
  font-size: 9.5px;
  color: var(--uvp-text-tertiary);
}
.probe-card {
  display: grid;
  gap: 7px;
  padding: 9px 11px;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 10px;
}
.probe-card.stream-brief {
  grid-template-rows: auto auto minmax(0, 1fr);
  min-height: 0;
}
.stream-brief-overview {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 8px;
}
.stream-brief-overview > div {
  display: grid;
  gap: 2px;
  min-width: 0;
  padding: 7px 9px;
}
.stream-brief-overview > div:nth-child(2n) {
  border-left: 1px solid var(--uvp-panel-border);
}
.stream-brief-overview > div:nth-child(n + 3) {
  border-top: 1px solid var(--uvp-panel-border);
}
.stream-brief-overview span {
  font-size: 9.5px;
  color: var(--uvp-text-tertiary);
}
.stream-brief-overview strong {
  overflow: hidden;
  text-overflow: ellipsis;
  font-family: ui-monospace, Menlo, monospace;
  font-size: 14px;
  font-weight: 650;
  color: var(--uvp-text-primary);
  white-space: nowrap;
}
.stream-brief-overview small {
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 9px;
  color: var(--uvp-text-tertiary);
  white-space: nowrap;
}
.stream-brief-split {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 6px;
  align-items: stretch;
  min-height: 0;
}
.stream-brief-kind {
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
  padding: 7px 9px;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 8px;
}
.stream-brief-kind.audio {
  border-left: 2px solid var(--uvp-brand-cyan);
}
.stream-brief-kind.video {
  border-left: 2px solid var(--uvp-brand);
}
.stream-brief-kind header {
  display: inline-flex;
  gap: 4px;
  align-items: center;
  margin-bottom: 5px;
  font-size: 10px;
  font-weight: 600;
  color: var(--uvp-text-secondary);
}
.stream-brief-rows {
  display: grid;
  gap: 3px;
}
.stream-brief-rows > div {
  display: flex;
  gap: 6px;
  align-items: baseline;
  justify-content: space-between;
  min-width: 0;
}
.stream-brief-rows span {
  flex-shrink: 0;
  font-size: 9.5px;
  color: var(--uvp-text-tertiary);
}
.stream-brief-rows strong {
  overflow: hidden;
  text-overflow: ellipsis;
  font-family: ui-monospace, Menlo, monospace;
  font-size: 10.5px;
  font-weight: 600;
  color: var(--uvp-text-primary);
  white-space: nowrap;
}
.stream-brief-rows strong.warn {
  color: var(--uvp-warning);
}
.stream-brief-rows strong.err {
  color: var(--uvp-danger);
}
.probe-status {
  display: inline-flex;
  flex-shrink: 0;
  gap: 5px;
  align-items: center;
  padding: 3px 7px;
  font-size: 9.5px;
  color: var(--uvp-text-tertiary);
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 999px;
}
.probe-status .dot {
  width: 5px;
  height: 5px;
  background: currentcolor;
  border-radius: 50%;
}
.probe-status.sampling {
  color: var(--uvp-warning);
  border-color: var(--uvp-warning-border);
}
.probe-status.complete {
  color: var(--uvp-brand-cyan);
  border-color: color-mix(in srgb, var(--uvp-brand-cyan) 30%, var(--uvp-panel-border));
}
.probe-action-row {
  display: flex;
  gap: 6px;
  align-items: stretch;
}
.probe-action {
  display: inline-flex;
  flex: 7 1 0;
  gap: 6px;
  align-items: center;
  justify-content: center;
  width: auto;
  min-width: 0;
  min-height: 30px;
  padding: 0 12px;
  font-size: 11.5px;
  font-weight: 600;
  color: #ffffff;
  cursor: pointer;
  background: var(--uvp-brand);
  border: 0;
  border-radius: 7px;
}
.probe-action:hover:not(:disabled) {
  background: var(--uvp-brand-strong);
}
.probe-action:disabled {
  cursor: not-allowed;
  opacity: 0.56;
}
:deep(.probe-duration) {
  flex: 3 1 0;
  min-width: 0;
}
.probe-summary {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 8px;
}
.probe-summary > div {
  display: grid;
  gap: 3px;
  min-width: 0;
  padding: 5px 8px;
}
.probe-summary > div + div {
  border-left: 1px solid var(--uvp-panel-border);
}
.probe-summary span {
  font-size: 9.5px;
  color: var(--uvp-text-tertiary);
}
.probe-summary strong {
  font-family: ui-monospace, Menlo, monospace;
  font-size: 13px;
  font-weight: 650;
  color: var(--uvp-text-primary);
  white-space: nowrap;
}
.probe-summary em {
  margin-left: 2px;
  font-size: 9px;
  font-style: normal;
  font-weight: 400;
  color: var(--uvp-text-tertiary);
}
.probe-summary.muted {
  opacity: 0.56;
}
.probe-verdict {
  display: flex;
  gap: 6px;
  align-items: center;
  padding: 7px 10px;
  color: var(--uvp-brand-cyan);
  background: color-mix(in srgb, var(--uvp-brand-cyan) 7%, transparent);
  border: 1px solid color-mix(in srgb, var(--uvp-brand-cyan) 24%, var(--uvp-panel-border));
  border-radius: 8px;
}
.probe-verdict > svg {
  flex-shrink: 0;
}
.probe-verdict strong {
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 10.5px;
  font-weight: 600;
  color: var(--uvp-text-primary);
  white-space: nowrap;
}
</style>
