<script setup lang="ts">
/**
 * DeviceDetail — 设备详情(E3)
 *
 * 来源:GET /device/:id(B2)+ GET /channels?deviceId=xxx mini 列表
 *
 * 渲染:
 *   1. 注册信息(IP/端口/transport/firmware/register_time/keepalive)
 *   2. 订阅能力(subscribe_capability 三态 chip)
 *   3. 派生统计:channelCount / channelOnlineCount / onlineRate
 *   4. 该设备所有通道 mini 列表(每行 36px,点击切到 channel 抽屉)
 */
import { computed, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { getDevice, listChannels, type DeviceVO, type ChannelVO } from "../../api/device";
import StatusDot from "../shared/StatusDot.vue";

interface Props {
  deviceId: number;
}

const props = defineProps<Props>();

const route = useRoute();
const router = useRouter();

const device = ref<DeviceVO | null>(null);
const channels = ref<ChannelVO[]>([]);
const loading = ref(false);
const err = ref<string | null>(null);

async function load() {
  loading.value = true;
  err.value = null;
  device.value = null;
  channels.value = [];
  try {
    const dR = await getDevice(props.deviceId);
    device.value = dR?.data ?? null;
    // 拉该设备的通道(后端 ListChannels 支持 deviceId 过滤 — 这里走基本过滤,Phase 2 加专用 query)
    if (device.value) {
      const chR = await listChannels({ pageSize: 50, q: device.value.deviceId });
      channels.value = chR?.data?.list ?? [];
    }
  } catch (e) {
    err.value = e instanceof Error ? e.message : "加载失败";
  } finally {
    loading.value = false;
  }
}

onMounted(load);
watch(() => props.deviceId, load);

const subscribeLabel = computed(() => {
  const v = device.value?.subscribeCapability ?? "unknown";
  const map = { unknown: "未试探", subscribed: "已订阅 ✓", fallback: "降级 (主动 Query)" } as const;
  return map[v];
});

const subscribeClass = computed(() => {
  const v = device.value?.subscribeCapability ?? "unknown";
  return `sub-${v}`;
});

function fmtTs(ts: string | null): string {
  if (!ts) return "—";
  try {
    return new Date(ts).toLocaleString();
  } catch {
    return ts;
  }
}

function gotoChannel(id: number) {
  router.push({ query: { ...route.query, node: String(id), drawerType: "channel" } });
}
</script>

<template>
  <div class="device-detail">
    <div v-if="err" class="banner banner-error">{{ err }}</div>
    <div v-if="loading && !device" class="loading">加载中…</div>

    <template v-else-if="device">
      <section class="group">
        <h4 class="group-title">设备</h4>
        <div class="field-grid">
          <div class="k">名称</div>
          <div class="v">{{ device.name || "—" }}</div>
          <div class="k">国标编码</div>
          <div class="v dm-mono">{{ device.deviceId }}</div>
          <div class="k">在线</div>
          <div class="v">
            <StatusDot :status="device.online ? 'online' : 'offline'" />
            {{ device.online ? "在线" : "离线" }}
          </div>
          <div class="k">订阅能力</div>
          <div class="v">
            <span :class="['sub-chip', subscribeClass]">{{ subscribeLabel }}</span>
          </div>
          <div class="k">IP : 端口</div>
          <div class="v dm-mono">{{ device.ip || "—" }}:{{ device.port || "—" }}</div>
          <div class="k">厂商 / 型号</div>
          <div class="v">{{ device.manufacturer || "—" }} / {{ device.model || "—" }}</div>
          <div class="k">固件</div>
          <div class="v">{{ device.firmware || "—" }}</div>
          <div class="k">注册时间</div>
          <div class="v">{{ fmtTs(device.registerTime) }}</div>
          <div class="k">最后心跳</div>
          <div class="v">{{ fmtTs(device.keepaliveTime) }}</div>
        </div>
      </section>

      <section class="group">
        <h4 class="group-title">通道统计</h4>
        <div class="stat-row">
          <div class="stat">
            <div class="stat-v">{{ device.channelCount }}</div>
            <div class="stat-l">通道总数</div>
          </div>
          <div class="stat">
            <div class="stat-v stat-online">{{ device.channelOnlineCount }}</div>
            <div class="stat-l">在线</div>
          </div>
          <div class="stat">
            <div class="stat-v">{{ Math.round((device.onlineRate || 0) * 100) }}%</div>
            <div class="stat-l">在线率</div>
          </div>
        </div>
      </section>

      <section class="group">
        <h4 class="group-title">通道列表({{ channels.length }})</h4>
        <ul v-if="channels.length" class="ch-list">
          <li
            v-for="ch in channels"
            :key="ch.id"
            class="ch-item"
            @click="gotoChannel(ch.id)"
          >
            <StatusDot :status="ch.status === 1 ? 'online' : 'offline'" />
            <span class="ch-name">{{ ch.name || ch.channelId }}</span>
            <span class="ch-code dm-mono">{{ ch.channelId }}</span>
          </li>
        </ul>
        <p v-else class="empty">无关联通道</p>
      </section>
    </template>
  </div>
</template>

<style lang="scss" scoped>
.device-detail {
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

.group-title {
  font-size: var(--font-12);
  color: var(--color-text-4);
  font-weight: 500;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  margin-bottom: var(--space-3);
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
    display: flex;
    align-items: center;
    gap: var(--space-2);
    overflow-wrap: anywhere;
  }
}

.sub-chip {
  display: inline-flex;
  align-items: center;
  padding: 2px 8px;
  border-radius: var(--dm-radius-pill);
  font-size: 11px;
  font-weight: 500;

  &.sub-unknown {
    background: var(--color-bg-3);
    color: var(--color-text-4);
  }
  &.sub-subscribed {
    background: var(--status-online-fade);
    color: var(--status-online);
  }
  &.sub-fallback {
    background: var(--status-warning-fade);
    color: var(--status-warning);
  }
}

.stat-row {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: var(--space-2);
}

.stat {
  background: var(--color-bg-3);
  padding: var(--space-3);
  border-radius: var(--dm-radius-2);
  text-align: center;

  .stat-v {
    font-size: var(--font-20);
    color: var(--color-text-1);
    font-family: var(--font-mono);
    font-weight: 600;

    &.stat-online {
      color: var(--status-online);
    }
  }

  .stat-l {
    margin-top: 4px;
    font-size: var(--font-12);
    color: var(--color-text-4);
  }
}

.ch-list {
  list-style: none;
  padding: 0;
  margin: 0;
}

.ch-item {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  height: 36px;
  padding: 0 var(--space-3);
  border-radius: var(--dm-radius-1);
  cursor: pointer;
  font-size: var(--font-13);
  transition: background var(--duration-fast) var(--ease-out);

  &:hover {
    background: var(--color-bg-3);
  }

  .ch-name {
    flex: 1;
    color: var(--color-text-2);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .ch-code {
    color: var(--color-text-4);
    font-size: 11px;
  }
}

.empty {
  color: var(--color-text-4);
  font-size: var(--font-12);
}
</style>
