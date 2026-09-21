<template>
  <div class="snow-fill">
    <div class="snow-fill-inner uvp-page-shell-flat snapshot-library-shell">
      <a-alert v-if="!canView" class="library-notice" type="warning">
        当前账号没有图像抓拍权限，无法查看图像库。请联系管理员分配「图像抓拍」（gb28181:device:snapshot）权限。
      </a-alert>

      <template v-else>
        <s-layout-search>
          <template #fields>
            <a-input
              v-model="filters.deviceCode"
              data-testid="library-device-code"
              allow-clear
              placeholder="设备国标编码（20 位）"
              style="width: 228px"
              @press-enter="query"
            />
            <a-input
              v-model="filters.channelCode"
              data-testid="library-channel-code"
              allow-clear
              placeholder="通道国标编码（20 位）"
              style="width: 228px"
              @press-enter="query"
            />
            <a-select
              v-model="filters.source"
              data-testid="library-source"
              allow-clear
              placeholder="全部来源"
              style="width: 140px"
            >
              <a-option v-for="option in SNAPSHOT_SOURCE_OPTIONS" :key="option.value" :value="option.value">
                {{ option.label }}
              </a-option>
            </a-select>
            <a-range-picker
              v-model="filters.range"
              data-testid="library-range"
              show-time
              value-format="YYYY-MM-DDTHH:mm:ssZ"
              allow-clear
              style="width: 330px"
            />
          </template>
          <template #actions>
            <a-button type="primary" data-testid="library-query" :loading="loading" @click="query">
              <template #icon><Search :size="15" /></template>
              查询
            </a-button>
            <a-button data-testid="library-reset" :disabled="loading" @click="reset">
              <template #icon><RotateCcw :size="15" /></template>
              重置
            </a-button>
          </template>
        </s-layout-search>

        <!-- 会话视图：设备详情里跑完一次抓拍跳过来看这一批图。
             会话本身只活在内存里，刷新即丢，而图已落库 —— 这是重新聚起一次抓拍的唯一线索。 -->
        <div v-if="filters.sessionId" class="library-session" data-testid="library-session">
          <a-tag color="arcoblue" closable @close="clearSessionFilter">本次抓拍会话 {{ filters.sessionId }}</a-tag>
          <span class="library-session-note">只显示这一次会话上传的图（会话在刷新后不再保留，图片仍然在）</span>
        </div>

        <a-alert v-if="errorMessage" class="library-notice" type="error" closable @close="errorMessage = ''">
          {{ errorMessage }}
        </a-alert>

        <div class="library-body">
          <div v-if="loading && !rows.length" class="library-state" data-testid="library-loading">正在加载抓拍图片…</div>
          <a-empty
            v-else-if="!rows.length"
            class="library-state"
            data-testid="library-empty"
            description="没有符合条件的抓拍图片"
          />
          <div v-else class="library-grid" data-testid="library-grid">
            <article v-for="item in rows" :key="item.id" class="library-card">
              <button
                type="button"
                class="library-thumb"
                :title="`查看大图 · ${item.fileName}`"
                :aria-label="`查看 ${item.channelName || item.channelCode} 的抓拍图`"
                @click="openPreview(item)"
              >
                <!-- ⛔ 取图接口在鉴权组内，src 必须经 imageUrlFor 补 ?token=，直连会整页 401 破图。 -->
                <img
                  v-if="!failedIds.has(item.id)"
                  :src="imageUrlFor(item)"
                  :alt="item.fileName"
                  loading="lazy"
                  decoding="async"
                  @error="markFailed(item.id)"
                />
                <span v-else class="library-thumb-failed"><ImageOff :size="18" />取图失败</span>
              </button>

              <div class="library-meta">
                <span class="library-title" :title="item.channelName || item.channelCode">
                  {{ item.channelName || item.channelCode || "未知通道" }}
                </span>
                <span class="library-sub" :title="item.deviceName || item.deviceCode">
                  {{ item.deviceName || item.deviceCode || "未知设备" }}
                </span>
                <span class="library-time" :title="`拍摄时刻 ${item.capturedAt}`">{{ formatCapturedAt(item.capturedAt) }}</span>
              </div>

              <div class="library-tags">
                <a-tag size="small">{{ snapshotSourceLabel(item.source) }}</a-tag>
                <span class="library-size">{{ formatSnapshotSize(item.size) }}</span>
              </div>
            </article>
          </div>
        </div>

        <footer class="library-footer">
          <span class="library-count">共 {{ pagination.total }} 张</span>
          <a-pagination
            data-testid="library-pagination"
            :current="pagination.page"
            :page-size="pagination.pageSize"
            :total="pagination.total"
            :page-size-options="SNAPSHOT_LIBRARY_PAGE_SIZE_OPTIONS"
            :disabled="loading"
            show-total
            show-page-size
            show-jumper
            @change="changePage"
            @page-size-change="changePageSize"
          />
        </footer>
      </template>
    </div>
  </div>

  <a-image-preview v-if="previewSrc" v-model:visible="previewVisible" :src="previewSrc" />
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { ImageOff, RotateCcw, Search } from "@lucide/vue";
import { getAccessToken } from "@/utils/auth";
import { getBaseUrl } from "@/api/utils";
import { useUserStoreHook } from "@/store/modules/user";
import { listSnapshotLibrary, type SnapshotLibraryItem } from "@/api/gb28181";
import {
  emptySnapshotFilters,
  formatCapturedAt,
  formatSnapshotSize,
  mayViewSnapshotLibrary,
  normalizeSnapshotQuery,
  snapshotContentImageUrl,
  snapshotFiltersFromRoute,
  snapshotSourceLabel,
  SNAPSHOT_LIBRARY_PAGE_SIZE,
  SNAPSHOT_LIBRARY_PAGE_SIZE_OPTIONS,
  SNAPSHOT_SOURCE_OPTIONS
} from "./snapshotLibraryState";

/**
 * 图像库：按设备/通道/来源/时间段翻历史抓拍图。
 *
 * 与「设备详情 → 设备控制 → 图像抓拍配置」的分工：
 * 那边是**发起一次抓拍**（下发 SnapshotConfig、轮询会话到 completed）；
 * 这边是**回看已经存下来的图** —— 会话一出页面就散了，图才是长期存在的那个东西。
 *
 * ⛔ 数据源是列表接口（只出元数据 + 取图地址），缩略图逐张按需拉：
 * 一个通道一年的图能上万张，列表内联图片字节会把响应打爆。
 * ⛔ 取图接口在鉴权组里，`<img>` 带不了 Authorization 头 ⇒ 地址必须补 `?token=`（imageUrlFor）。
 */
const route = useRoute();
const router = useRouter();
const userStore = useUserStoreHook();

// 权限码与菜单可见性、两个接口的 casbin 授权**同码**，三处必须一致。
const canView = computed(() => mayViewSnapshotLibrary(userStore.account.permissions));

const filters = reactive(snapshotFiltersFromRoute(route.query));
const rows = ref<SnapshotLibraryItem[]>([]);
const loading = ref(false);
const errorMessage = ref("");
const failedIds = ref(new Set<number>());
const accessToken = ref(getAccessToken()?.accessToken ?? "");
const pagination = reactive({ page: 1, pageSize: SNAPSHOT_LIBRARY_PAGE_SIZE, total: 0 });
const previewVisible = ref(false);
const previewSrc = ref("");

// 请求序号：筛选/翻页连点时只认最后一次响应，否则慢的旧响应会把新结果盖掉。
let requestToken = 0;

function imageUrlFor(item: SnapshotLibraryItem) {
  return snapshotContentImageUrl(item.url, accessToken.value, getBaseUrl());
}

function markFailed(id: number) {
  const next = new Set(failedIds.value);
  next.add(id);
  failedIds.value = next;
}

function openPreview(item: SnapshotLibraryItem) {
  const url = imageUrlFor(item);
  if (!url) return;
  previewSrc.value = url;
  previewVisible.value = true;
}

async function loadRows() {
  if (!canView.value) return;
  const token = ++requestToken;
  loading.value = true;
  errorMessage.value = "";
  // 每次都重读 token：access token 刷新之后旧值会让整页缩略图 401。
  accessToken.value = getAccessToken()?.accessToken ?? "";
  failedIds.value = new Set();
  try {
    const response = await listSnapshotLibrary(normalizeSnapshotQuery(filters, pagination.page, pagination.pageSize));
    if (token !== requestToken) return;
    rows.value = response.data?.list ?? [];
    pagination.total = response.data?.total ?? 0;
  } catch (error) {
    if (token !== requestToken) return;
    rows.value = [];
    pagination.total = 0;
    errorMessage.value = "查询图像库失败，请稍后重试";
    console.error(error);
  } finally {
    if (token === requestToken) loading.value = false;
  }
}

function query() {
  pagination.page = 1;
  void loadRows();
}

function clearRouteFilters() {
  // 深链带进来的条件(会话/编码)在重置后要一起清掉，否则"重置了但 URL 还写着"
  // ——再分享出去的人会看到被你重置掉的筛选。
  if (Object.keys(route.query).length) void router.replace({ query: {} });
}

function reset() {
  Object.assign(filters, emptySnapshotFilters());
  pagination.page = 1;
  clearRouteFilters();
  void loadRows();
}

function clearSessionFilter() {
  filters.sessionId = "";
  clearRouteFilters();
  query();
}

function changePage(nextPage: number) {
  pagination.page = nextPage;
  void loadRows();
}

function changePageSize(nextPageSize: number) {
  pagination.pageSize = nextPageSize;
  pagination.page = 1;
  void loadRows();
}

function applyRouteFilters() {
  Object.assign(filters, snapshotFiltersFromRoute(route.query));
  pagination.page = 1;
}

// 已经在图像库页面上时又点了一次"按会话查看"（例如从设备详情返回）——路由 query 变化即重载。
// ⛔ 只在 query 里确实带了定位条件时才动，否则 reset()/clearSessionFilter() 里那次 replace
// 会被当成一次新查询，白跑一遍（甚至把刚重置的筛选又灌回去）。
watch(
  () => route.query,
  next => {
    if (!next.sessionId && !next.channelCode && !next.deviceCode && !next.source) return;
    applyRouteFilters();
    void loadRows();
  }
);

onMounted(() => {
  if (canView.value) void loadRows();
});
</script>

<style scoped>
.snapshot-library-shell {
  display: flex;
  flex-direction: column;
  gap: 12px;
  height: 100%;
  min-height: 0;
}
.library-notice {
  margin: 0;
}
.library-session {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  align-items: center;
}
.library-session-note {
  font-size: 11.5px;
  color: var(--uvp-text-tertiary);
}
.library-body {
  flex: 1;
  min-height: 0;
  overflow: auto;
}
.library-state {
  padding: 48px 0;
  color: var(--uvp-text-tertiary);
  text-align: center;
}
.library-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(214px, 1fr));
  gap: 12px;
  padding: 2px;
}
.library-card {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 10px;
  background: var(--uvp-panel-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 12px;
}
.library-card:hover {
  border-color: var(--uvp-primary);
}
.library-thumb {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 100%;
  aspect-ratio: 16 / 9;
  padding: 0;
  overflow: hidden;
  cursor: pointer;
  background: var(--uvp-shell-muted);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 8px;
}
.library-thumb img {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.library-thumb-failed {
  display: inline-flex;
  gap: 6px;
  align-items: center;
  font-size: 11.5px;
  color: var(--uvp-danger);
}
.library-meta {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}
.library-title {
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 13px;
  font-weight: 600;
  color: var(--uvp-text-primary);
  white-space: nowrap;
}
.library-sub {
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 11.5px;
  color: var(--uvp-text-secondary);
  white-space: nowrap;
}
.library-time {
  font-size: 11.5px;
  color: var(--uvp-text-tertiary);
}
.library-tags {
  display: flex;
  gap: 8px;
  align-items: center;
  justify-content: space-between;
}
.library-size {
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}
.library-footer {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  align-items: center;
  justify-content: space-between;
  padding-top: 2px;
}
.library-count {
  font-size: 12px;
  color: var(--uvp-text-tertiary);
}

@media (width <= 900px) {
  .library-grid {
    grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
  }
  .library-footer {
    justify-content: center;
  }
}
</style>
