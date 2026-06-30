<script setup lang="ts">
/**
 * DeviceCardView — 卡片视图(D1)
 *
 * grid 自适应:auto-fill minmax(280px, 1fr)
 * 拉 /channels(B2),按当前 URL ?node= / ?status= 过滤
 * 行 click → 抽屉打开;dblclick → 点播浮窗(本期占位)
 *
 * IntersectionObserver lazy snapshot refresh 留 Phase 2(B5 真接入后再开),
 * 本期卡片快照走渐变占位,与列表视图行为一致
 */
import { computed, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import DeviceCard from "./DeviceCard.vue";
import { listChannels, type ChannelVO, type DeviceStatus } from "../../api/device";

const route = useRoute();
const router = useRouter();

const list = ref<ChannelVO[]>([]);
const total = ref(0);
const loading = ref(false);
const err = ref<string | null>(null);

const queryState = computed(() => {
  const page = Number(route.query.page) || 1;
  const pageSize = Number(route.query.pageSize) || 24;
  const nodeId = route.query.node ? Number(route.query.node) : undefined;
  const status = (route.query.status as DeviceStatus) || undefined;
  const ptz: 0 | 1 | undefined = route.query.ptz === "1" ? 1 : undefined;
  const q = (route.query.q as string) || undefined;
  return { page, pageSize, nodeId, status, ptz, q };
});

async function load() {
  loading.value = true;
  err.value = null;
  try {
    const r = await listChannels(queryState.value);
    list.value = r?.data?.list ?? [];
    total.value = r?.data?.total ?? 0;
  } catch (e) {
    err.value = e instanceof Error ? e.message : "加载失败";
    list.value = [];
    total.value = 0;
  } finally {
    loading.value = false;
  }
}

onMounted(load);

watch(
  () => [
    queryState.value.page,
    queryState.value.pageSize,
    queryState.value.nodeId,
    queryState.value.status,
    queryState.value.ptz,
    queryState.value.q,
  ],
  load
);

function openDrawer(channelId: number) {
  router.push({
    query: { ...route.query, node: String(channelId), drawerType: "channel" },
  });
}

function startPlay(channelId: number) {
  // Phase 2 接 PlayPopover + EasyPlayer.js Pro / mpegts;本期 console 占位
  // eslint-disable-next-line no-console
  console.log("[device-mgmt] play channel", channelId);
}

function gotoPage(p: number) {
  if (p < 1) return;
  router.replace({ query: { ...route.query, page: String(p) } });
}
</script>

<template>
  <div class="device-card-view">
    <div v-if="err" class="banner banner-error">{{ err }}</div>

    <div v-if="loading" class="empty">加载中…</div>

    <div v-else-if="list.length === 0" class="empty">
      <p>暂无通道</p>
      <p class="hint">选择左侧目录节点过滤,或等待 Catalog NOTIFY 入库。</p>
    </div>

    <div v-else class="card-grid">
      <DeviceCard
        v-for="ch in list"
        :key="ch.id"
        :channel="ch"
        :mount-count="1"
        @open="openDrawer"
        @play="startPlay"
      />
    </div>

    <footer v-if="total > queryState.pageSize" class="pagination">
      <span class="info">
        共 <strong>{{ total }}</strong> 条 / 第 {{ queryState.page }} 页
      </span>
      <div class="page-btns">
        <button
          type="button"
          :disabled="queryState.page <= 1"
          @click="gotoPage(queryState.page - 1)"
        >
          上一页
        </button>
        <button
          type="button"
          :disabled="queryState.page * queryState.pageSize >= total"
          @click="gotoPage(queryState.page + 1)"
        >
          下一页
        </button>
      </div>
    </footer>
  </div>
</template>

<style lang="scss" scoped>
.device-card-view {
  display: flex;
  flex-direction: column;
  min-height: 0;
  height: 100%;
}

.banner-error {
  margin-bottom: var(--space-3);
  padding: var(--space-2) var(--space-3);
  border-radius: var(--dm-radius-1);
  background: var(--status-danger-fade);
  color: var(--status-danger);
  font-size: var(--font-12);
}

.empty {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  color: var(--color-text-4);
  font-size: var(--font-16);

  .hint {
    margin-top: var(--space-2);
    font-size: var(--font-12);
  }
}

.card-grid {
  flex: 1;
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: var(--space-4);
  min-height: 0;
  overflow-y: auto;
  padding-bottom: var(--space-4);
}

.pagination {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: var(--space-3) 0 0;
  font-size: var(--font-12);
  color: var(--color-text-3);

  .info strong {
    color: var(--color-text-1);
    font-family: var(--font-mono);
  }

  .page-btns {
    display: flex;
    gap: var(--space-2);

    button {
      padding: 4px 12px;
      background: var(--color-bg-3);
      border: 1px solid var(--color-border-2);
      border-radius: var(--dm-radius-1);
      color: var(--color-text-2);
      font-size: var(--font-12);
      cursor: pointer;

      &:disabled {
        opacity: 0.4;
        cursor: not-allowed;
      }

      &:not(:disabled):hover {
        background: var(--color-bg-2);
        color: var(--color-text-1);
      }
    }
  }
}
</style>
