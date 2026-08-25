<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from "vue";
import { RefreshCw, RotateCcw, Search } from "lucide-vue-next";
import { getDivisionAPI, type DivisionItem } from "@/api/department";
import { getOnlineUsersAPI, type OnlineUserListParams, type OnlineUserSession, type OnlineUserStatus } from "@/api/online-user";
import { formatTime } from "@/globals";
import { useUserStoreHook } from "@/store/modules/user";
import OnlineUserAction from "./OnlineUserAction.vue";

interface DepartmentOption {
  key: number;
  title: string;
  children?: DepartmentOption[];
}

const userStore = useUserStoreHook();
const sessions = ref<OnlineUserSession[]>([]);
const currentSid = ref("");
const loading = ref(false);
const error = ref("");
const departmentOptions = ref<DepartmentOption[]>([]);
const form = reactive({
  username: "",
  departmentId: undefined as number | undefined,
  clientIp: "",
  status: "" as OnlineUserStatus | ""
});
const pagination = reactive({
  current: 1,
  pageSize: 20,
  total: 0,
  showTotal: true,
  showJumper: true,
  showPageSize: true
});

const canForce = computed(() => {
  const permissions = userStore.account.permissions || [];
  return permissions.includes("*:*:*") || permissions.includes("system:online-user:force-logout");
});
const tableScroll = computed(() => ({
  x: "100%",
  minWidth: canForce.value ? 1280 : 1160
}));

let requestVersion = 0;
let refreshTimer: ReturnType<typeof setInterval> | undefined;

function buildParams(): OnlineUserListParams {
  const params: OnlineUserListParams = { pageNum: pagination.current, pageSize: pagination.pageSize };
  const username = form.username.trim();
  const clientIp = form.clientIp.trim();
  if (username) params.username = username;
  if (form.departmentId) params.departmentId = form.departmentId;
  if (clientIp) params.clientIp = clientIp;
  if (form.status) params.status = form.status;
  return params;
}

function errorMessage(cause: unknown) {
  if (cause instanceof Error) return cause.message;
  if (typeof cause === "string") return cause;
  if (cause && typeof cause === "object" && "message" in cause) return String(cause.message);
  return "在线用户加载失败";
}

async function load(silent = false) {
  const version = ++requestVersion;
  if (!silent) loading.value = true;
  error.value = "";
  try {
    const result = await getOnlineUsersAPI(buildParams());
    if (version !== requestVersion) return;
    if (result.code !== 0) throw new Error(result.message || "在线用户加载失败");
    sessions.value = result.data?.list || [];
    pagination.total = result.data?.total || 0;
    currentSid.value = result.data?.currentSid || "";
  } catch (cause: unknown) {
    if (version !== requestVersion) return;
    error.value = errorMessage(cause);
  } finally {
    if (version === requestVersion) loading.value = false;
  }
}

async function search() {
  pagination.current = 1;
  await load();
}

async function reset() {
  form.username = "";
  form.departmentId = undefined;
  form.clientIp = "";
  form.status = "";
  pagination.current = 1;
  await load();
}

async function handlePageChange(current: number) {
  pagination.current = current;
  await load();
}

async function handlePageSizeChange(pageSize: number) {
  pagination.pageSize = pageSize;
  pagination.current = 1;
  await load();
}

function removeSession(sid: string) {
  sessions.value = sessions.value.filter(item => item.sid !== sid);
  pagination.total = Math.max(0, pagination.total - 1);
}

function mapDepartments(items: DivisionItem[]): DepartmentOption[] {
  return items.map(item => ({
    key: item.id,
    title: item.name,
    children: item.children?.length ? mapDepartments(item.children) : undefined
  }));
}

async function loadDepartments() {
  try {
    const result = await getDivisionAPI();
    if (result.code === 0) departmentOptions.value = mapDepartments(result.data?.list || []);
  } catch {
    departmentOptions.value = [];
  }
}

function stopRefresh() {
  if (refreshTimer) clearInterval(refreshTimer);
  refreshTimer = undefined;
}

function startRefresh() {
  stopRefresh();
  if (document.visibilityState !== "visible") return;
  refreshTimer = setInterval(() => { void load(true); }, 30_000);
}

function handleVisibilityChange() {
  if (document.visibilityState === "visible") {
    void load(true);
    startRefresh();
  } else {
    stopRefresh();
  }
}

onMounted(() => {
  void load();
  void loadDepartments();
  startRefresh();
  document.addEventListener("visibilitychange", handleVisibilityChange);
});

onBeforeUnmount(() => {
  requestVersion += 1;
  stopRefresh();
  document.removeEventListener("visibilitychange", handleVisibilityChange);
});

defineExpose({ form, sessions, currentSid, pagination, load, search, reset, handlePageChange, handlePageSizeChange });
</script>

<template>
  <div class="snow-page online-user-page">
    <div class="snow-inner uvp-page-shell-flat online-user-page__inner">
      <s-layout-search>
        <template #fields>
          <div class="online-user-filter"><a-input v-model="form.username" placeholder="用户名" allow-clear @press-enter="search" /></div>
          <div class="online-user-filter">
            <a-tree-select v-model="form.departmentId" :data="departmentOptions" placeholder="部门" allow-clear allow-search />
          </div>
          <div class="online-user-filter"><a-input v-model="form.clientIp" placeholder="IP 地址" allow-clear @press-enter="search" /></div>
          <div class="online-user-filter online-user-filter--status">
            <a-select v-model="form.status" placeholder="状态" allow-clear>
              <a-option value="active">活跃</a-option>
              <a-option value="idle">空闲</a-option>
            </a-select>
          </div>
        </template>
        <template #actions>
          <a-button type="primary" @click="search">
            <template #icon><Search :size="16" /></template>
            查询
          </a-button>
          <a-button @click="reset">
            <template #icon><RotateCcw :size="16" /></template>
            重置
          </a-button>
        </template>
        <template #extra>
          <a-tooltip content="刷新在线用户" position="top">
            <a-button class="online-user-refresh" :loading="loading" aria-label="刷新在线用户" @click="load()">
              <template #icon><RefreshCw :size="16" /></template>
              刷新
            </a-button>
          </a-tooltip>
        </template>
      </s-layout-search>

      <div v-if="error" class="online-user-error" role="alert">
        <span>{{ error }}</span>
        <a-button size="small" @click="load()">重试</a-button>
      </div>

      <a-table
        v-else
        class="uvp-data-table online-user-table"
        row-key="sid"
        :data="sessions"
        :loading="loading"
        :pagination="pagination"
        :scroll="tableScroll"
        :bordered="false"
        @page-change="handlePageChange"
        @page-size-change="handlePageSizeChange"
      >
        <template #columns>
          <a-table-column title="用户" :width="160">
            <template #cell="{ record }">
              <div class="online-user-primary"><strong>{{ record.username }}</strong><span class="online-user-secondary">{{ record.nickName || '-' }}</span></div>
            </template>
          </a-table-column>
          <a-table-column title="部门" data-index="departmentName" :width="150" :ellipsis="true" :tooltip="true" />
          <a-table-column title="IP / 地点" :width="180">
            <template #cell="{ record }">
              <div class="online-user-primary"><span class="online-user-primary-text">{{ record.clientIp }}</span><span class="online-user-secondary">{{ record.loginLocation || '-' }}</span></div>
            </template>
          </a-table-column>
          <a-table-column title="终端" :width="180">
            <template #cell="{ record }">
              <div class="online-user-primary"><span class="online-user-primary-text">{{ record.browser || '未知浏览器' }}</span><span class="online-user-secondary">{{ record.os || '未知系统' }}</span></div>
            </template>
          </a-table-column>
          <a-table-column title="登录时间" :width="180"><template #cell="{ record }">{{ formatTime(record.loginAt) }}</template></a-table-column>
          <a-table-column title="最后活跃" :width="180"><template #cell="{ record }">{{ formatTime(record.lastActiveAt) }}</template></a-table-column>
          <a-table-column title="状态" :width="100" align="center">
            <template #cell="{ record }">
              <span class="online-user-status" :class="`online-user-status--${record.status}`"><i />{{ record.status === 'active' ? '活跃' : '空闲' }}</span>
            </template>
          </a-table-column>
          <a-table-column v-if="canForce" title="操作" :width="144" align="center" fixed="right">
            <template #cell="{ record }">
              <OnlineUserAction :session="record" :current-sid="currentSid" :can-force="canForce" @success="removeSession" />
            </template>
          </a-table-column>
        </template>
        <template #empty><a-empty description="暂无符合条件的在线用户" /></template>
      </a-table>
    </div>
  </div>
</template>

<style scoped>
.online-user-page,
.online-user-page__inner {
  min-width: 0;
  max-width: 100%;
  overflow-x: hidden;
}

.online-user-filter {
  flex: 0 1 176px;
  width: 176px;
  max-width: 100%;
}

.online-user-filter--status {
  flex-basis: 128px;
  width: 128px;
}

.online-user-filter :deep(.arco-input-wrapper),
.online-user-filter :deep(.arco-select-view) {
  box-sizing: border-box;
  width: 100%;
  height: 44px;
  min-height: 44px;
  background: var(--uvp-search-control-bg) !important;
  border: 1px solid var(--uvp-search-secondary-btn-border) !important;
  border-radius: 10px !important;
  box-shadow: var(--uvp-search-control-shadow) !important;
}

.online-user-filter :deep(.arco-input-wrapper:focus-within),
.online-user-filter :deep(.arco-select-view.arco-select-view-focus),
.online-user-filter :deep(.arco-select-view:focus-within) {
  border-color: var(--uvp-brand) !important;
  box-shadow: var(--uvp-search-control-focus-shadow) !important;
}

.online-user-filter :deep(.arco-input::placeholder),
.online-user-filter :deep(.arco-select-view-input::placeholder) {
  color: var(--uvp-text-tertiary);
  opacity: 1;
}

.online-user-refresh {
  min-width: 88px;
  min-height: 44px;
}

.online-user-page :deep(.uvp-search-panel .arco-btn),
.online-user-error :deep(.arco-btn) {
  box-sizing: border-box;
  height: 44px;
  min-height: 44px;
  border-radius: 10px;
}

.online-user-page :deep(.uvp-search-panel__extra .arco-btn) {
  color: var(--uvp-secondary-action-text) !important;
  background: var(--uvp-secondary-action-bg) !important;
  border-color: var(--uvp-secondary-action-border) !important;
  box-shadow: none !important;
}

.online-user-page :deep(.uvp-search-panel__extra .arco-btn:hover) {
  color: var(--uvp-brand-strong) !important;
  background: var(--uvp-secondary-action-hover-bg) !important;
  border-color: var(--uvp-brand) !important;
}

.online-user-page :deep(.arco-pagination-item),
.online-user-page :deep(.arco-pagination-jumper-input),
.online-user-page :deep(.arco-pagination-options .arco-select-view) {
  min-width: 32px;
  min-height: 32px;
}

.online-user-error {
  display: flex;
  min-height: 280px;
  align-items: center;
  justify-content: center;
  gap: 12px;
  color: rgb(var(--danger-6));
  text-align: center;
}

.online-user-table {
  min-width: 0;
}

.online-user-page :deep(.uvp-data-table .arco-table-cell) {
  font-size: 14px;
  line-height: 22px;
}

.online-user-primary {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 2px;
  line-height: 18px;
}

.online-user-primary > * {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.online-user-primary-text {
  color: var(--uvp-text-primary);
  font-size: 13px;
}

.online-user-primary strong {
  color: var(--uvp-text-primary);
  font-size: 14px;
  font-weight: 650;
  line-height: 20px;
}

.online-user-secondary {
  color: var(--uvp-text-secondary);
  font-size: 13px;
  line-height: 18px;
}

.online-user-status {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  white-space: nowrap;
}

.online-user-status i {
  width: 7px;
  height: 7px;
  flex: 0 0 auto;
  border-radius: 50%;
  background: currentColor;
}

.online-user-status--active { color: rgb(var(--success-6)); }
.online-user-status--idle { color: var(--uvp-text-secondary); }

.online-user-page :deep(.arco-btn:focus-visible),
.online-user-page :deep(.arco-input-wrapper:focus-within),
.online-user-page :deep(.arco-select-view:focus-within) {
  outline: 2px solid rgb(var(--primary-6));
  outline-offset: 2px;
}

@media (max-width: 768px) {
  .online-user-filter,
  .online-user-filter--status {
    flex-basis: min(100%, 280px);
    width: min(100%, 280px);
  }
}

@media (prefers-reduced-motion: reduce) {
  .online-user-page,
  .online-user-page *,
  .online-user-page *::before,
  .online-user-page *::after {
    scroll-behavior: auto !important;
    animation-duration: 0.01ms !important;
    transition-duration: 0.01ms !important;
  }
}
</style>
