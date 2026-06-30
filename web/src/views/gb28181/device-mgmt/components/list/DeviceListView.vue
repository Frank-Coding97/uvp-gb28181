<script setup lang="ts">
/**
 * DeviceListView — 设备列表视图(C3)
 *
 * 责任:
 * - 拉取 /devices?status=&q=&sort= + 分页 + 排序参数 ↔ URL query
 * - 渲染 thead + DeviceListRow tbody
 * - 行 click → router.push ?node=deviceId,触发 DetailDrawer 打开
 *
 * 当前 active node 通过 URL ?node= 跨视图保留
 */
import { computed, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import DeviceListRow from "./DeviceListRow.vue";
import { listDevices, type DeviceVO, type DeviceStatus } from "../../api/device";

const route = useRoute();
const router = useRouter();

const list = ref<DeviceVO[]>([]);
const total = ref(0);
const loading = ref(false);
const err = ref<string | null>(null);

const selected = ref<Set<number>>(new Set());

const queryState = computed(() => {
  const page = Number(route.query.page) || 1;
  const pageSize = Number(route.query.pageSize) || 20;
  const sort = (route.query.sort as string) || "id:desc";
  const status = (route.query.status as DeviceStatus) || undefined;
  const q = (route.query.q as string) || undefined;
  return { page, pageSize, sort, status, q };
});

async function load() {
  loading.value = true;
  err.value = null;
  try {
    const r = await listDevices(queryState.value);
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
  () => [queryState.value.page, queryState.value.pageSize, queryState.value.sort, queryState.value.status, queryState.value.q],
  load
);

function toggleSelect(id: number) {
  const s = new Set(selected.value);
  if (s.has(id)) s.delete(id);
  else s.add(id);
  selected.value = s;
}

function openDrawer(deviceId: number) {
  // 用 device id 做 URL key,抽屉打开时按 deviceId 拉详情
  router.push({ query: { ...route.query, node: String(deviceId), drawerType: "device" } });
}

function gotoPage(p: number) {
  if (p < 1) return;
  router.replace({ query: { ...route.query, page: String(p) } });
}
</script>

<template>
  <div class="device-list-view">
    <div v-if="err" class="banner banner-error">{{ err }}</div>

    <div class="table-wrapper">
      <table class="device-table">
        <thead>
          <tr>
            <th class="col-check">
              <input type="checkbox" disabled />
            </th>
            <th class="col-thumb">缩略图</th>
            <th class="col-name">设备名 / 编码</th>
            <th class="col-vendor">厂商 / 型号</th>
            <th class="col-addr">IP : 端口</th>
            <th class="col-channels">通道</th>
            <th class="col-online">在线率</th>
            <th class="col-heart">最近心跳</th>
            <th class="col-action"></th>
          </tr>
        </thead>
        <tbody v-if="!loading">
          <DeviceListRow
            v-for="d in list"
            :key="d.id"
            :device="d"
            :selected="selected.has(d.id)"
            @toggle-select="toggleSelect"
            @open="openDrawer"
          />
        </tbody>
        <tbody v-else>
          <tr class="loading-row">
            <td colspan="9">加载中…</td>
          </tr>
        </tbody>
      </table>
    </div>

    <div v-if="!loading && list.length === 0 && !err" class="empty">
      <p>暂无设备</p>
      <p class="hint">等待 GB28181 设备注册或通道 Catalog NOTIFY 入库。</p>
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
.device-list-view {
  display: flex;
  flex-direction: column;
  min-height: 0;
  height: 100%;
}

.banner {
  margin: 0 0 var(--space-3);
  padding: var(--space-2) var(--space-3);
  border-radius: var(--dm-radius-1);
  font-size: var(--font-12);

  &-error {
    background: var(--status-danger-fade);
    color: var(--status-danger);
  }
}

.table-wrapper {
  flex: 1;
  min-height: 0;
  overflow: auto;
  background: var(--color-bg-2);
  border: 1px solid var(--color-border-2);
  border-radius: var(--dm-radius-2);
}

.device-table {
  width: 100%;
  border-collapse: collapse;
  font-size: var(--font-13);

  thead {
    position: sticky;
    top: 0;
    z-index: 1;
    background: var(--color-bg-2);

    th {
      padding: var(--space-3);
      text-align: left;
      color: var(--color-text-3);
      font-weight: 500;
      font-size: var(--font-12);
      border-bottom: 1px solid var(--color-border-2);
    }
  }
}

.loading-row td,
.empty {
  padding: var(--space-12) var(--space-4);
  text-align: center;
  color: var(--color-text-4);
}

.empty .hint {
  font-size: var(--font-12);
  margin-top: var(--space-2);
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
        border-color: var(--color-border-3);
      }
    }
  }
}
</style>
