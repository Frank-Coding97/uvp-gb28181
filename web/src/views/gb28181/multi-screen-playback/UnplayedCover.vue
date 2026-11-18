<script setup lang="ts">
import { computed } from "vue";

const props = defineProps<{ index: number }>();
const slotNumber = computed(() => String(props.index + 1).padStart(2, "0"));
</script>

<template>
  <div class="unplayed-cover" :aria-label="`${index + 1}号预览窗口，待接入`">
    <div class="cover-meta" aria-hidden="true">
      <span class="slot-number">窗口 {{ slotNumber }}</span>
      <span class="idle-state"><span class="idle-dot" />待接入</span>
    </div>
    <div class="brand-lockup" aria-hidden="true">
      <strong class="cover-brand">UVP</strong>
      <span class="brand-caption">统一视频接入平台</span>
    </div>
  </div>
</template>

<style scoped>
.unplayed-cover {
  position: relative;
  display: grid;
  flex: 1;
  place-items: center;
  width: 100%;
  min-height: 0;
  overflow: hidden;
  color: #f8fafc;
  background: #090b0f;
  container-type: inline-size;
}
.cover-meta {
  position: absolute;
  top: 0;
  right: 0;
  left: 0;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 14px;
  font-size: 10px;
  color: #9ca3af;
}
.slot-number {
  font-family: var(--zlm-font-mono);
  font-variant-numeric: tabular-nums;
  color: #64748b; /* 槽号是定位信息，弱一档 */
}
.idle-state {
  display: inline-flex;
  gap: 6px;
  align-items: center;
  font-size: 11px;
  font-weight: 500;
  color: #cbd5e1; /* 待接入才是这块真正要读的信息，提到主位 */
}
.idle-dot {
  width: 6px;
  height: 6px;
  background: #94a3b8;
  border-radius: 50%;
}
.brand-lockup {
  display: flex;
  flex-direction: column;
  gap: 10px;
  align-items: center;
}
.cover-brand {
  font-family: var(--zlm-font-display);
  font-size: 31px;
  font-weight: 700;
  line-height: 1;
  color: transparent;
  letter-spacing: 0;

  /* 品牌字标只作水印：暗端收进背景，亮端不过白，避免四块黑砖里冒出四个广告牌 */
  background: linear-gradient(110deg, #6d86ad 6%, #5c789f 52%, #4a6c93 100%);
  background-clip: text;
  opacity: 0.5;
  -webkit-text-fill-color: transparent;
}
.brand-caption {
  font-size: 10px;
  font-weight: 500;
  line-height: 1.4;
  color: #5b6b80;
  letter-spacing: 0;
  opacity: 0.85;
}

@container (max-width: 220px) {
  .cover-brand {
    font-size: 24px;
  }
  .brand-caption {
    font-size: 9px;
  }
}

@container (min-width: 520px) {
  .cover-brand {
    font-size: 40px;
  }
  .brand-caption {
    font-size: 11px;
  }
}
</style>
