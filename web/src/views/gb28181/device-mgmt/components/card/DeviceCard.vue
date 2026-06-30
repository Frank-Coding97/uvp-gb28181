<script setup lang="ts">
/**
 * DeviceCard — 通道卡片(D1)
 *
 * 视觉(mockup card.html .device-card):
 *  - card-snap:16:9 快照(SnapThumb)+ 类型 chip + PTZ 角标 + 底部状态条
 *  - card-info:标题(含 MountBadge)+ 国标编码 + 厂商型号 meta
 *  - 离线卡片:整卡灰度
 *  - anomaly:橙色描边
 *  - hover:抬升 + 阴影 + 点播 CTA 显现
 *
 * 数据源是 ChannelVO(B2 /channels 返回)— 卡片视图按通道渲染,
 * 跟列表视图按设备渲染互补
 */
import { computed } from "vue";
import SnapThumb from "../shared/SnapThumb.vue";
import StatusDot from "../shared/StatusDot.vue";
import MountBadge from "../shared/MountBadge.vue";
import AnomalyFlag from "../shared/AnomalyFlag.vue";
import type { ChannelVO } from "../../api/device";

interface Props {
  channel: ChannelVO;
  /** 多挂载数(由父级 /channel/:id/mounts 拉,或后端列表 join 提供) */
  mountCount?: number;
  /** anomaly 标记 */
  anomaly?: boolean;
}

const props = withDefaults(defineProps<Props>(), { mountCount: 1, anomaly: false });
const emit = defineEmits<{
  (e: "open", id: number): void;
  (e: "play", id: number): void;
}>();

const isOnline = computed(() => props.channel.status === 1);

const heartLabel = computed(() => {
  const ts = props.channel.updatedAt;
  if (!ts) return "—";
  const t = new Date(ts);
  const diff = Math.floor((Date.now() - t.getTime()) / 1000);
  if (diff < 60) return `${diff}s 前`;
  if (diff < 3600) return `${Math.floor(diff / 60)}m 前`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}h 前`;
  return t.toLocaleDateString();
});

const isPTZ = computed(() => (props.channel.ptzType || 0) > 0);
const cameraType = computed(() => {
  if (isPTZ.value) return "球机";
  return "枪机";
});

function onOpen() {
  emit("open", props.channel.id);
}

function onPlay(e: MouseEvent) {
  e.stopPropagation();
  emit("play", props.channel.id);
}
</script>

<template>
  <article
    :class="['device-card', { offline: !isOnline, anomaly }]"
    @click="onOpen"
    @dblclick="onPlay"
  >
    <div class="card-snap">
      <SnapThumb
        :vendor-hint="channel.manufacturer"
        :online="isOnline"
        size="card"
        :alt="channel.name"
      />
      <div class="snap-type">{{ cameraType }}</div>
      <div v-if="isPTZ" class="snap-corner" title="支持云台">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <path d="M12 2v20M2 12h20" />
        </svg>
      </div>
      <div v-if="anomaly" class="snap-anomaly">
        <AnomalyFlag size="sm" />
      </div>
      <div class="snap-overlay">
        <StatusDot :status="isOnline ? 'online' : 'offline'" />
        <span class="status-text">{{ isOnline ? "在线" : "离线" }}</span>
        <span class="heart-text">{{ heartLabel }}</span>
        <button v-if="isOnline" class="play-cta" type="button" @click="onPlay">
          <svg viewBox="0 0 24 24" fill="currentColor"><polygon points="6 4 20 12 6 20 6 4" /></svg>
          点播
        </button>
      </div>
    </div>
    <div class="card-info">
      <div class="title">
        {{ channel.name || "通道" }}
        <MountBadge :count="mountCount" />
      </div>
      <div class="code dm-mono">{{ channel.channelId }}</div>
      <div class="meta">
        <span v-if="channel.manufacturer">{{ channel.manufacturer }}</span>
        <span v-if="channel.manufacturer && channel.model" class="dot-sep">·</span>
        <span v-if="channel.model">{{ channel.model }}</span>
      </div>
    </div>
  </article>
</template>

<style lang="scss" scoped>
.device-card {
  background: var(--color-bg-2);
  border: 1px solid var(--color-border-2);
  border-radius: var(--dm-radius-3);
  overflow: hidden;
  cursor: pointer;
  transition: transform var(--duration-fast) var(--ease-out),
    box-shadow var(--duration-fast) var(--ease-out),
    border-color var(--duration-fast) var(--ease-out);

  &:hover {
    transform: translateY(-2px);
    box-shadow: var(--shadow-card-hover);
    border-color: var(--color-border-3);

    .play-cta {
      opacity: 1;
      transform: translateY(0);
    }
  }

  &.anomaly {
    border-color: var(--status-warning);
  }

  &.offline {
    .card-info .title,
    .card-info .meta {
      color: var(--color-text-3);
    }
  }
}

.card-snap {
  position: relative;
  background: var(--color-bg-3);
  aspect-ratio: 16 / 9;
  overflow: hidden;

  :deep(.snap-thumb) {
    width: 100%;
    height: 100%;
    border-radius: 0;
  }

  .snap-type {
    position: absolute;
    top: 8px;
    left: 8px;
    display: inline-flex;
    align-items: center;
    gap: 4px;
    height: 20px;
    padding: 0 8px;
    background: var(--color-bg-overlay);
    color: var(--color-text-1);
    border-radius: var(--dm-radius-1);
    font-size: 11px;
    backdrop-filter: blur(4px);
  }

  .snap-corner {
    position: absolute;
    top: 8px;
    right: 8px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 22px;
    height: 22px;
    background: var(--primary-fade-24);
    color: var(--primary-5);
    border-radius: 50%;

    svg {
      width: 12px;
      height: 12px;
    }
  }

  .snap-anomaly {
    position: absolute;
    bottom: 32px;
    right: 8px;
  }

  .snap-overlay {
    position: absolute;
    left: 0;
    right: 0;
    bottom: 0;
    display: flex;
    align-items: center;
    gap: var(--space-2);
    height: 28px;
    padding: 0 var(--space-2);
    background: linear-gradient(180deg, transparent 0%, rgba(0, 0, 0, 0.7) 100%);
    color: var(--color-text-1);
    font-size: 11px;

    .status-text {
      font-weight: 500;
    }

    .heart-text {
      flex: 1;
      color: var(--color-text-3);
    }

    .play-cta {
      display: inline-flex;
      align-items: center;
      gap: 4px;
      padding: 4px 10px;
      background: var(--primary-6);
      color: white;
      border: 0;
      border-radius: var(--dm-radius-1);
      font-size: 11px;
      font-weight: 500;
      cursor: pointer;
      opacity: 0;
      transform: translateY(4px);
      transition: opacity var(--duration-fast) var(--ease-out),
        transform var(--duration-fast) var(--ease-out);

      svg {
        width: 10px;
        height: 10px;
      }

      &:hover {
        background: var(--primary-7);
      }
    }
  }
}

.card-info {
  padding: var(--space-3);

  .title {
    display: flex;
    align-items: center;
    color: var(--color-text-1);
    font-size: var(--font-13);
    font-weight: 500;
    margin-bottom: 4px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .code {
    color: var(--color-text-4);
    font-size: 11px;
    margin-bottom: 6px;
  }

  .meta {
    display: flex;
    align-items: center;
    gap: 6px;
    color: var(--color-text-3);
    font-size: 11px;

    .dot-sep {
      color: var(--color-text-4);
    }
  }
}
</style>
