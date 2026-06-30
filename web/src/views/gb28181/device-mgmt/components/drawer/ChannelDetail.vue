<script setup lang="ts">
/**
 * ChannelDetail — 通道详情(E2)
 *
 * 并行拉:GET /channel/:id + /channel/:id/mounts + /channel/:id/timeline
 *
 * 渲染分组(plan §9.8):
 *   1. 国标信息:20 位编码分段(行政区 6 / 行业 2 / 类型 3 / 序号 9)
 *   2. 能力(CapabilityChips)
 *   3. 多挂载位置(MountList)
 *   4. 24h 在线时序(OnlineTimeline)
 *   5. 设备信息 mini(parent device 关联)
 */
import { computed, onMounted, ref, watch } from "vue";
import { getChannel, getChannelMounts, getChannelTimeline, type ChannelVO, type ChannelMountVO, type TimelineSlot } from "../../api/device";
import CapabilityChips from "./CapabilityChips.vue";
import MountList from "./MountList.vue";
import OnlineTimeline from "./OnlineTimeline.vue";

interface Props {
  channelId: number;
}

const props = defineProps<Props>();

const channel = ref<ChannelVO | null>(null);
const mounts = ref<ChannelMountVO[]>([]);
const slots = ref<TimelineSlot[]>([]);
const phase1Simplified = ref(false);
const loading = ref(false);
const err = ref<string | null>(null);

async function load() {
  loading.value = true;
  err.value = null;
  channel.value = null;
  mounts.value = [];
  slots.value = [];

  try {
    const [chR, mR, tR] = await Promise.all([
      getChannel(props.channelId),
      getChannelMounts(props.channelId),
      getChannelTimeline(props.channelId, "24h"),
    ]);
    channel.value = chR?.data ?? null;
    mounts.value = mR?.data?.list ?? [];
    slots.value = tR?.data?.slots ?? [];
    phase1Simplified.value = !!tR?.data?.phase1Simplified;
  } catch (e) {
    err.value = e instanceof Error ? e.message : "加载失败";
  } finally {
    loading.value = false;
  }
}

onMounted(load);
watch(() => props.channelId, load);

// 国标 20 位编码分段(GB/T 28181 §10.2)
const codeSegments = computed(() => {
  const code = channel.value?.channelId ?? "";
  if (code.length !== 20) return null;
  return {
    civil: code.slice(0, 6), // 行政区码
    industry: code.slice(6, 8), // 行业 2 位
    type: code.slice(10, 13), // 类型 3 位(spec §10 — 位 11-13)
    typeArea: code.slice(8, 10), // 类型大类 2 位(位 9-10)
    serial: code.slice(13), // 序列号
  };
});
</script>

<template>
  <div class="channel-detail">
    <div v-if="err" class="banner banner-error">{{ err }}</div>
    <div v-if="loading && !channel" class="loading">加载中…</div>

    <template v-else-if="channel">
      <section class="group">
        <h4 class="group-title">通道</h4>
        <div class="field-grid">
          <div class="k">名称</div>
          <div class="v">{{ channel.name || "—" }}</div>
          <div class="k">国标编码</div>
          <div class="v dm-mono">
            <template v-if="codeSegments">
              <span class="seg seg-civil">{{ codeSegments.civil }}</span>
              <span class="seg seg-industry">{{ codeSegments.industry }}</span>
              <span class="seg seg-area">{{ codeSegments.typeArea }}</span>
              <span class="seg seg-type">{{ codeSegments.type }}</span>
              <span class="seg seg-serial">{{ codeSegments.serial }}</span>
            </template>
            <template v-else>{{ channel.channelId }}</template>
          </div>
          <div class="k">所属设备</div>
          <div class="v dm-mono">{{ channel.deviceId }}</div>
          <div class="k">厂商 / 型号</div>
          <div class="v">{{ channel.manufacturer || "—" }} / {{ channel.model || "—" }}</div>
          <div class="k">所有者</div>
          <div class="v">{{ channel.owner || "—" }}</div>
          <div class="k">坐标</div>
          <div class="v dm-mono">
            <template v-if="channel.latitude || channel.longitude">
              {{ channel.latitude.toFixed(5) }}, {{ channel.longitude.toFixed(5) }}
            </template>
            <template v-else>未上报</template>
          </div>
        </div>
      </section>

      <section class="group">
        <h4 class="group-title">能力</h4>
        <CapabilityChips :capabilities="channel.capabilities" :ptz-type="channel.ptzType" />
      </section>

      <section class="group">
        <h4 class="group-title">挂载位置({{ mounts.length }})</h4>
        <MountList :mounts="mounts" />
      </section>

      <section class="group">
        <h4 class="group-title">24 小时在线</h4>
        <OnlineTimeline :slots="slots" :phase1-simplified="phase1Simplified" />
      </section>
    </template>
  </div>
</template>

<style lang="scss" scoped>
.channel-detail {
  display: flex;
  flex-direction: column;
  gap: var(--space-6);
}

.banner-error {
  padding: var(--space-2) var(--space-3);
  background: var(--status-danger-fade);
  color: var(--status-danger);
  border-radius: var(--dm-radius-1);
  font-size: var(--font-12);
}

.loading {
  color: var(--color-text-4);
  text-align: center;
  padding: var(--space-8) 0;
}

.group {
  .group-title {
    font-size: var(--font-12);
    color: var(--color-text-4);
    font-weight: 500;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    margin-bottom: var(--space-3);
  }
}

.field-grid {
  display: grid;
  grid-template-columns: 80px 1fr;
  row-gap: var(--space-2);
  column-gap: var(--space-3);
  font-size: var(--font-13);

  .k {
    color: var(--color-text-4);
  }

  .v {
    color: var(--color-text-2);
    overflow-wrap: anywhere;
  }

  .seg {
    display: inline-block;
    padding: 0 4px;
    border-radius: 3px;
    margin-right: 2px;

    &.seg-civil { background: rgba(167, 139, 250, 0.12); color: var(--node-civil); }
    &.seg-industry { background: var(--color-bg-3); color: var(--color-text-3); }
    &.seg-area { background: var(--color-bg-3); color: var(--color-text-3); }
    &.seg-type { background: var(--primary-fade-08); color: var(--primary-5); }
    &.seg-serial { background: var(--color-bg-3); color: var(--color-text-2); }
  }
}
</style>
