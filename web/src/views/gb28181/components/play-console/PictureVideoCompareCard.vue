<script setup lang="ts">
defineProps<{
  verdict: { tone: string; text: string };
  verdictTitle: string;
  selectedStream: any;
  readRow: any;
  diffs: { codec?: boolean | null; resolution?: boolean | null; fps?: boolean | null };
  streamInfo: any;
  bitrate: number;
  videoFormatText: (value: any) => string;
  resolutionText: (value: any) => string;
  frameRateText: (value: any) => string;
}>();
</script>

<template>
  <div class="video-param-compare-card" data-testid="video-param-compare-card">
    <div class="vpc-status" data-testid="video-param-compare-status">
      <em class="vpc-verdict" :class="`is-${verdict.tone}`" data-testid="video-param-compare-verdict" :title="verdictTitle">
        {{ verdict.text }}
      </em>
    </div>
    <div v-if="selectedStream" class="vpc-grid" data-testid="video-param-compare">
      <div class="vpc-row" data-testid="video-param-compare-read">
        <span>设备回读</span
        ><strong
          >{{ videoFormatText(readRow?.videoFormat) }} · {{ resolutionText(readRow?.resolution) }} ·
          {{ frameRateText(readRow?.frameRate) }}</strong
        >
      </div>
      <div class="vpc-row" data-testid="video-param-compare-measured">
        <span>画面实测</span
        ><strong
          ><i :class="{ 'is-differ': diffs.codec === true }">{{ streamInfo.videoCodec }}</i> ·
          <i :class="{ 'is-differ': diffs.resolution === true }">{{ streamInfo.resolution }}</i> ·
          <i :class="{ 'is-differ': diffs.fps === true }">{{ streamInfo.videoFps ? `${streamInfo.videoFps} fps` : "—" }}</i
          ><em v-if="bitrate" class="vpc-bitrate">{{ bitrate }} kbps</em></strong
        >
      </div>
    </div>
    <p v-else class="vpc-empty" data-testid="video-param-compare-empty">
      还没有回读值 —— 点左侧「画面遮挡」卡上的「读取」，拿到设备参数后这里显示两行对照。
    </p>
  </div>
</template>
