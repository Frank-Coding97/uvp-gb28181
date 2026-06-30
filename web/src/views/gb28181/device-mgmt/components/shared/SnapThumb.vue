<script setup lang="ts">
/**
 * SnapThumb — 通道快照缩略图(C3 / D1 / E1 通用)
 *
 * 显示策略(plan §5.6):
 * - 有 src:img + cache-bust 时间戳
 * - 无 src:渐变占位 + 厂商简称(HK/DH/UVP...)
 * - 离线:grayscale + 暗淡 + "已离线" overlay
 *
 * 列表行 48×27 / 卡片 16:9(D1 通过 size=card 切大尺寸)
 */
import { computed } from "vue";

interface Props {
  src?: string | null;
  vendorHint?: string;
  online?: boolean;
  size?: "row" | "card";
  alt?: string;
}

const props = withDefaults(defineProps<Props>(), {
  src: null,
  vendorHint: "",
  online: true,
  size: "row",
  alt: "",
});

const initials = computed(() => {
  const v = (props.vendorHint || "").trim().toUpperCase();
  if (!v) return "—";
  if (v.includes("HIK") || v.includes("HAIK") || v.startsWith("H")) return "HK";
  if (v.includes("DAHU") || v.includes("DH")) return "DH";
  if (v.startsWith("UV") || v.startsWith("U")) return "UV";
  return v.slice(0, 2);
});

const placeholderClass = computed(() => {
  if (initials.value === "HK") return "p-hk";
  if (initials.value === "DH") return "p-dh";
  if (initials.value === "UV") return "p-uv";
  return "p-generic";
});
</script>

<template>
  <div :class="['snap-thumb', `sz-${size}`, { offline: !online }]">
    <img v-if="src" :src="src" :alt="alt" loading="lazy" />
    <div v-else :class="['placeholder', placeholderClass]">
      {{ initials }}
    </div>
    <div v-if="!online" class="offline-overlay">已离线</div>
  </div>
</template>

<style lang="scss" scoped>
.snap-thumb {
  position: relative;
  background: var(--color-bg-3);
  border-radius: var(--dm-radius-1);
  overflow: hidden;
  flex-shrink: 0;

  &.sz-row {
    width: 48px;
    height: 27px;
  }

  &.sz-card {
    aspect-ratio: 16 / 9;
    width: 100%;
    border-radius: var(--dm-radius-3);
  }

  img,
  .placeholder {
    width: 100%;
    height: 100%;
    object-fit: cover;
    transition: filter var(--duration-fast) var(--ease-out);
  }

  .placeholder {
    display: flex;
    align-items: center;
    justify-content: center;
    color: rgba(255, 255, 255, 0.78);
    font-weight: 600;
    font-size: 11px;
    letter-spacing: 0.04em;

    &.p-hk {
      background: linear-gradient(135deg, #1e3a8a 0%, #312e81 100%);
    }
    &.p-dh {
      background: linear-gradient(135deg, #166534 0%, #14532d 100%);
    }
    &.p-uv {
      background: linear-gradient(135deg, #3730a3 0%, #1e1b4b 100%);
    }
    &.p-generic {
      background: linear-gradient(135deg, var(--color-bg-3) 0%, var(--color-bg-1) 100%);
    }
  }

  &.offline {
    img,
    .placeholder {
      filter: grayscale(1) brightness(0.5);
    }
  }

  .offline-overlay {
    position: absolute;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--color-bg-overlay);
    color: var(--color-text-3);
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.06em;
  }

  &.sz-card .placeholder {
    font-size: 24px;
  }
}
</style>
