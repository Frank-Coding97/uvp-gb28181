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
}
.idle-state {
  display: inline-flex;
  gap: 6px;
  align-items: center;
}
.idle-dot {
  width: 6px;
  height: 6px;
  background: #6b7280;
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
  font-size: 52px;
  font-weight: 700;
  line-height: 1;
  color: transparent;
  letter-spacing: 0;
  background: linear-gradient(110deg, #f8fafc 8%, #c7d7f2 54%, #72a2f5 100%);
  background-clip: text;
  filter: drop-shadow(0 8px 18px rgb(37 99 235 / 16%));
  -webkit-text-fill-color: transparent;
}
.brand-caption {
  font-size: 10px;
  font-weight: 500;
  line-height: 1.4;
  color: #778397;
  letter-spacing: 0;
}

@container (max-width: 220px) {
  .cover-brand {
    font-size: 38px;
  }
  .brand-caption {
    font-size: 9px;
  }
}

@container (min-width: 520px) {
  .cover-brand {
    font-size: 68px;
  }
  .brand-caption {
    font-size: 11px;
  }
}
</style>
