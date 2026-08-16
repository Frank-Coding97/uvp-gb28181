<template>
  <div class="sip-card" :class="{ 'sip-card--loading': loading }">
    <div class="sip-card__head">
      <div class="sip-card__title">
        <span class="sip-card__dot" :class="dotClass" />
        GB28181 SIP 协议监控
      </div>
      <div class="sip-card__sub" :class="{ 'sip-card__sub--live': connected }">{{ connectionLabel }}</div>
    </div>

    <div v-if="loading && !snapshot" class="sip-card__state sip-card__state--loading" role="status">
      <Activity :size="25" aria-hidden="true" />
      <strong>正在读取 SIP 实时状态</strong>
      <span>等待协议监控快照</span>
    </div>

    <div v-else-if="loadState !== 'ready' && !snapshot" class="sip-card__state" role="alert">
      <ShieldAlert :size="27" aria-hidden="true" />
      <strong>{{ failureCopy.title }}</strong>
      <span>{{ failureCopy.detail }}</span>
      <button v-if="loadState === 'error'" type="button" class="sip-card__retry" @click="retry">
        <RefreshCw :size="14" aria-hidden="true" />重新加载
      </button>
    </div>

    <template v-else>
      <SummaryBar
        :health="snapshot?.health ?? HEALTH_EMPTY"
        :today-total="snapshot?.todayTotal ?? 0"
        :today-abnormal="snapshot?.todayAbnormal ?? 0"
      />
      <PulseChart
        :samples="snapshot?.pulse?.samples ?? []"
        :abnormal-windows="snapshot?.pulse?.abnormalWindows ?? []"
      />
      <TransactionGrid :transactions="snapshot?.transactions ?? []" />
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { Activity, RefreshCw, ShieldAlert } from "lucide-vue-next";
import {
  fetchSipDashboardSnapshot,
  sipDashboardStreamUrl,
  HEALTH_EMPTY,
  type DashboardSnapshot
} from "@/api/gb28181";
import { classifyDashboardError, type DashboardLoadState } from "../../dashboardState";
import SummaryBar from "./SummaryBar.vue";
import TransactionGrid from "./TransactionGrid.vue";
import PulseChart from "./PulseChart.vue";

const snapshot = ref<DashboardSnapshot | null>(null);
const loadState = ref<DashboardLoadState>("loading");
const connected = ref(false);
let evtSource: EventSource | null = null;

const loading = computed(() => loadState.value === "loading");

const connectionLabel = computed(() => {
  if (loading.value) return "读取中";
  if (loadState.value === "forbidden") return "无权限";
  if (loadState.value === "disabled") return "未启用";
  if (connected.value) return "实时连接";
  if (snapshot.value) return "连接中断";
  return "加载失败";
});

const failureCopy = computed(() => {
  if (loadState.value === "forbidden") {
    return { title: "无权查看 SIP 协议监控", detail: "当前账号未授予协议监控访问权限" };
  }
  if (loadState.value === "disabled") {
    return { title: "SIP 协议监控未启用", detail: "当前服务未装配实时监控能力" };
  }
  return { title: "SIP 协议监控加载失败", detail: "未能读取实时信令，请检查服务状态后重试" };
});

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
      loadState.value = "ready";
    } else {
      loadState.value = "error";
    }
  } catch (error) {
    loadState.value = classifyDashboardError(error, true);
  }
}

function openStream(): void {
  try {
    // 同源(走 vite proxy / nginx 反代),不需要 withCredentials
    evtSource = new EventSource(sipDashboardStreamUrl());
    evtSource.addEventListener("snapshot", (ev: MessageEvent) => {
      try {
        snapshot.value = JSON.parse(ev.data) as DashboardSnapshot;
        loadState.value = "ready";
        connected.value = true;
      } catch {
        // 单帧解析失败不影响连接
      }
    });
    evtSource.onerror = () => {
      connected.value = false;
      if (!snapshot.value && loadState.value !== "forbidden" && loadState.value !== "disabled") {
        loadState.value = "error";
      }
    };
  } catch {
    connected.value = false;
    if (!snapshot.value) loadState.value = "error";
  }
}

function closeStream(): void {
  if (!evtSource) return;
  evtSource.close();
  evtSource = null;
}

function allowsStream(state: DashboardLoadState): boolean {
  return state !== "forbidden" && state !== "disabled";
}

async function retry(): Promise<void> {
  closeStream();
  connected.value = false;
  loadState.value = "loading";
  await loadInitial();
  if (allowsStream(loadState.value)) openStream();
}

onMounted(async () => {
  await loadInitial();
  if (allowsStream(loadState.value)) openStream();
});

onBeforeUnmount(closeStream);
</script>

<style scoped lang="scss">
.sip-card {
  display: flex;
  flex-direction: column;
  gap: 14px;
  padding: 16px 20px;
  font-family: -apple-system, "PingFang SC", "Microsoft YaHei", sans-serif;
  color: var(--uvp-text-primary);
  background: var(--uvp-panel-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: var(--uvp-panel-radius);
  box-shadow: var(--uvp-panel-shadow);
}

.sip-card--loading {
  opacity: 0.85;
}

.sip-card__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding-bottom: 4px;
  border-bottom: 1px solid var(--uvp-panel-border);
}

.sip-card__title {
  display: flex;
  gap: 10px;
  align-items: center;
  font-size: 15px;
  font-weight: 600;
  color: var(--uvp-text-primary);
}

.sip-card__dot {
  width: 8px;
  height: 8px;
  background: var(--uvp-text-tertiary);
  border-radius: 50%;
  transition: all 0.3s ease;
}

.sip-card__dot--ok {
  background: var(--uvp-brand-cyan);
  box-shadow: 0 0 6px rgb(15 170 166 / 48%);
  animation: sip-pulse 2s infinite;
}

.sip-card__dot--warn {
  background: var(--uvp-warning);
  box-shadow: 0 0 6px rgb(182 107 18 / 36%);
}

.sip-card__dot--danger {
  background: var(--uvp-danger);
  box-shadow: 0 0 6px rgb(209 67 67 / 38%);
}

.sip-card__dot--idle {
  background: var(--uvp-text-tertiary);
}

.sip-card__sub {
  font-size: 11px;
  color: var(--uvp-text-tertiary);
  letter-spacing: 0;
}

.sip-card__sub--live {
  color: var(--uvp-brand-cyan);
}

.sip-card__state {
  display: flex;
  flex: 1;
  flex-direction: column;
  gap: 7px;
  align-items: center;
  justify-content: center;
  min-height: 280px;
  padding: 24px;
  color: var(--uvp-danger);
  text-align: center;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 7px;

  strong {
    font-size: 14px;
    color: var(--uvp-text-primary);
  }

  span {
    font-size: 12px;
    color: var(--uvp-text-tertiary);
  }
}

.sip-card__state--loading {
  color: var(--uvp-brand);
}

.sip-card__retry {
  display: inline-flex;
  gap: 6px;
  align-items: center;
  justify-content: center;
  min-height: 44px;
  padding: 0 14px;
  margin-top: 5px;
  font: inherit;
  font-size: 12px;
  color: var(--uvp-brand);
  cursor: pointer;
  background: var(--uvp-panel-bg);
  border: 1px solid color-mix(in srgb, var(--uvp-brand) 30%, var(--uvp-panel-border));
  border-radius: 7px;
  transition:
    background-color 0.18s ease,
    border-color 0.18s ease,
    box-shadow 0.18s ease;

  &:hover,
  &:focus-visible {
    outline: none;
    background: var(--uvp-brand-soft);
    border-color: var(--uvp-brand);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--uvp-brand) 10%, transparent);
  }
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

@media (prefers-reduced-motion: reduce) {
  .sip-card__dot--ok {
    animation: none;
  }

  .sip-card__retry {
    transition: none;
  }
}
</style>
