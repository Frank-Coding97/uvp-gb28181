<template>
  <div class="sip-card" :class="{ 'sip-card--loading': loading }">
    <!-- 卡片标题 -->
    <div class="sip-card__head">
      <div class="sip-card__title">
        <span class="sip-card__dot" :class="dotClass" />
        GB28181 SIP 协议监控
      </div>
      <div class="sip-card__sub">
        SIGNALING HEALTH · {{ connected ? "实时" : "离线" }}
      </div>
    </div>

    <!-- ① 健康度汇总条 -->
    <SummaryBar
      :health="snapshot?.health ?? HEALTH_EMPTY"
      :today-total="snapshot?.todayTotal ?? 0"
      :today-abnormal="snapshot?.todayAbnormal ?? 0"
      :pending="snapshot?.pending ?? 0"
    />

    <!-- ② 协议事务矩阵 -->
    <TransactionGrid :transactions="snapshot?.transactions ?? []" />

    <!-- ③ 信令脉搏图 -->
    <PulseChart
      :samples="snapshot?.pulse?.samples ?? []"
      :abnormal-windows="snapshot?.pulse?.abnormalWindows ?? []"
    />
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, computed } from "vue";
import {
  fetchSipDashboardSnapshot,
  sipDashboardStreamUrl,
  HEALTH_EMPTY,
  type DashboardSnapshot
} from "@/api/gb28181";
import SummaryBar from "./SummaryBar.vue";
import TransactionGrid from "./TransactionGrid.vue";
import PulseChart from "./PulseChart.vue";

const snapshot = ref<DashboardSnapshot | null>(null);
const loading = ref(true);
const connected = ref(false);
let evtSource: EventSource | null = null;

const dotClass = computed(() => {
  if (!connected.value) return "sip-card__dot--idle";
  const h = snapshot.value?.health ?? HEALTH_EMPTY;
  if (h === HEALTH_EMPTY) return "sip-card__dot--idle";
  if (h < 90) return "sip-card__dot--danger";
  if (h < 95) return "sip-card__dot--warn";
  return "sip-card__dot--ok";
});

async function loadInitial(): Promise<void> {
  try {
    const res = await fetchSipDashboardSnapshot();
    if (res && res.data) {
      snapshot.value = res.data;
    }
  } catch {
    // 首屏失败不阻塞,SSE 还会推
  } finally {
    loading.value = false;
  }
}

function openStream(): void {
  try {
    // 同源(走 vite proxy / nginx 反代),不需要 withCredentials
    evtSource = new EventSource(sipDashboardStreamUrl());
    evtSource.addEventListener("snapshot", (ev: MessageEvent) => {
      try {
        snapshot.value = JSON.parse(ev.data) as DashboardSnapshot;
        connected.value = true;
      } catch {
        // 单帧解析失败不影响连接
      }
    });
    evtSource.onerror = () => {
      connected.value = false;
    };
  } catch {
    connected.value = false;
  }
}

onMounted(async () => {
  await loadInitial();
  openStream();
});

onBeforeUnmount(() => {
  if (evtSource) {
    evtSource.close();
    evtSource = null;
  }
});
</script>

<style scoped lang="scss">
.sip-card {
  background: #fff;
  border-radius: 8px;
  padding: 16px 20px;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.04);
  display: flex;
  flex-direction: column;
  gap: 14px;
  color: #333;
  font-family: -apple-system, "PingFang SC", "Microsoft YaHei", sans-serif;
}

.sip-card--loading {
  opacity: 0.85;
}

.sip-card__head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding-bottom: 4px;
  border-bottom: 1px solid #f0f0f0;
}

.sip-card__title {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 15px;
  font-weight: 600;
  color: #333;
}

.sip-card__dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #d9d9d9;
  transition: all 0.3s ease;
}

.sip-card__dot--ok {
  background: #52c41a;
  box-shadow: 0 0 6px rgba(82, 196, 26, 0.5);
  animation: sip-pulse 2s infinite;
}

.sip-card__dot--warn {
  background: #fa8c16;
  box-shadow: 0 0 6px rgba(250, 140, 22, 0.5);
}

.sip-card__dot--danger {
  background: #ff4d4f;
  box-shadow: 0 0 6px rgba(255, 77, 79, 0.5);
}

.sip-card__dot--idle {
  background: #d9d9d9;
}

.sip-card__sub {
  color: #999;
  font-size: 11px;
  letter-spacing: 0.5px;
}

@keyframes sip-pulse {
  0%,
  100% {
    opacity: 1;
  }
  50% {
    opacity: 0.4;
  }
}
</style>
