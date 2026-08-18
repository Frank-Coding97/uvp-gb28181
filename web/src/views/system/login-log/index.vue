<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { Eraser, Eye, KeyRound, RotateCcw, Search, Trash2 } from "lucide-vue-next";
import { Modal } from "@arco-design/web-vue";
import { formatTime } from "@/globals";
import { useUserStoreHook } from "@/store/modules/user";
import {
  clearLoginLogsAPI,
  deleteLoginLogsAPI,
  getLoginLogDetailAPI,
  getLoginLogsAPI,
  unlockLoginLogAccountAPI,
  type LoginLogDetail,
  type LoginLogFailureReason,
  type LoginLogItem,
  type LoginLogListParams,
  type LoginLogResult
} from "@/api/login-log";

const reasonLabels: Record<string, string> = {
  captcha_invalid: "验证码错误",
  user_not_found: "用户不存在",
  user_disabled: "用户未启用",
  account_locked: "账户已锁定",
  password_incorrect: "密码错误",
  session_create_failed: "会话创建失败",
  server_error: "服务器错误"
};

const form = reactive({
  username: "",
  result: "" as LoginLogResult | "",
  failureReason: "" as LoginLogFailureReason | "",
  ip: ""
});
const dateRange = ref<string[]>([]);
const logs = ref<LoginLogItem[]>([]);
const loading = ref(false);
const error = ref("");
const pagination = reactive({ current: 1, pageSize: 20, total: 0, showTotal: true, showJumper: true, showPageSize: true });
const detailVisible = ref(false);
const detailLoading = ref(false);
const detailError = ref("");
const currentDetail = ref<LoginLogDetail | null>(null);
const selectedRowKeys = ref<number[]>([]);
const deleting = ref(false);
const clearing = ref(false);
const unlockingId = ref<number | null>(null);
const permissions = computed(() => useUserStoreHook().account.permissions || []);
const can = (permission: string) => permissions.value.includes("*:*:*") || permissions.value.includes(permission);
const canDelete = computed(() => can("system:login-log:delete"));
const canClear = computed(() => can("system:login-log:clear"));
const canUnlock = computed(() => can("system:login-log:unlock"));
const rowSelection = computed(() => (canDelete.value ? { type: "checkbox" as const, showCheckedAll: true } : undefined));

const tableScroll = computed(() => ({ x: "100%", minWidth: 1060 }));

function buildParams(): LoginLogListParams {
  const params: LoginLogListParams = { pageNum: pagination.current, pageSize: pagination.pageSize };
  if (form.username.trim()) params.username = form.username.trim();
  if (form.result) params.result = form.result;
  if (form.failureReason) params.failureReason = form.failureReason;
  if (form.ip.trim()) params.ip = form.ip.trim();
  if (dateRange.value.length === 2) {
    params.startTime = dateRange.value[0];
    params.endTime = dateRange.value[1];
  }
  return params;
}

function errorMessage(cause: unknown) {
  if (cause instanceof Error) return cause.message;
  if (cause && typeof cause === "object" && "message" in cause) return String(cause.message);
  return "登录日志加载失败";
}

async function load() {
  loading.value = true;
  error.value = "";
  try {
    const result = await getLoginLogsAPI(buildParams());
    if (result.code !== 0) throw new Error(result.message || "登录日志加载失败");
    logs.value = result.data?.list || [];
    selectedRowKeys.value = [];
    pagination.total = result.data?.total || 0;
  } catch (cause: unknown) {
    error.value = errorMessage(cause);
  } finally {
    loading.value = false;
  }
}

function requestDelete() {
  if (!canDelete.value || !selectedRowKeys.value.length || deleting.value) return;
  Modal.warning({
    title: "删除登录日志",
    content: `将删除选中的 ${selectedRowKeys.value.length} 条登录日志，删除后不可恢复。`,
    okText: "删除", cancelText: "取消", hideCancel: false, escToClose: true,
    okButtonProps: { status: "danger" }, onOk: () => performDelete()
  });
}

async function performDelete() {
  deleting.value = true;
  const ids = [...selectedRowKeys.value];
  try {
    const result = await deleteLoginLogsAPI(ids);
    if (result.code !== 0) throw new Error(result.message || "删除登录日志失败");
    selectedRowKeys.value = [];
    await load();
  } catch (cause) {
    error.value = errorMessage(cause);
  } finally { deleting.value = false; }
}

function requestClear() {
  if (!canClear.value || clearing.value) return;
  Modal.warning({
    title: "清空登录日志",
    content: "将清空当前数据库中已有的全部登录日志，删除后不可恢复。",
    okText: "清空", cancelText: "取消", hideCancel: false, escToClose: true,
    okButtonProps: { status: "danger" }, onOk: () => performClear()
  });
}

async function performClear() {
  clearing.value = true;
  try {
    const result = await clearLoginLogsAPI();
    if (result.code !== 0) throw new Error(result.message || "清空登录日志失败");
    pagination.current = 1;
    selectedRowKeys.value = [];
    await load();
  } catch (cause) {
    error.value = errorMessage(cause);
  } finally { clearing.value = false; }
}

function requestUnlock(record: LoginLogItem) {
  if (!canUnlock.value || unlockingId.value !== null) return;
  Modal.warning({
    title: "解锁账号",
    content: `确认解锁账号“${record.username}”吗？`,
    okText: "解锁", cancelText: "取消", hideCancel: false, escToClose: true,
    onOk: () => performUnlock(record.id)
  });
}

async function performUnlock(id: number) {
  unlockingId.value = id;
  try {
    const result = await unlockLoginLogAccountAPI(id);
    if (result.code !== 0) throw new Error(result.message || "解锁账号失败");
    await load();
  } catch (cause) {
    error.value = errorMessage(cause);
  } finally { unlockingId.value = null; }
}

async function search() {
  pagination.current = 1;
  await load();
}

async function reset() {
  form.username = "";
  form.result = "";
  form.failureReason = "";
  form.ip = "";
  dateRange.value = [];
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

async function viewDetail(record: LoginLogItem) {
  detailVisible.value = true;
  detailLoading.value = true;
  detailError.value = "";
  currentDetail.value = null;
  try {
    const result = await getLoginLogDetailAPI(record.id);
    if (result.code !== 0) throw new Error(result.message || "登录日志详情加载失败");
    currentDetail.value = result.data;
  } catch (cause: unknown) {
    detailError.value = errorMessage(cause);
  } finally {
    detailLoading.value = false;
  }
}

function resultLabel(result: string) {
  return result === "success" ? "成功" : "失败";
}

function failureLabel(reason?: string) {
  return reasonLabels[reason || ""] || (reason ? "其他失败" : "-");
}

onMounted(() => void load());

defineExpose({ form, dateRange, logs, pagination, loading, error, currentDetail, detailLoading, detailError, selectedRowKeys, canDelete, canClear, canUnlock, load, search, reset, handlePageChange, handlePageSizeChange, viewDetail, requestDelete, requestClear, requestUnlock, performDelete, performClear, performUnlock, failureLabel });
</script>

<template>
  <div class="snow-page login-log-page">
    <div class="snow-inner uvp-page-shell-flat login-log-page__inner">
      <s-layout-search>
        <template #fields>
          <div class="login-log-filter"><a-input v-model="form.username" placeholder="用户名" allow-clear @press-enter="search" /></div>
          <div class="login-log-filter login-log-filter--compact">
            <a-select v-model="form.result" placeholder="结果" allow-clear>
              <a-option value="success">成功</a-option>
              <a-option value="failure">失败</a-option>
            </a-select>
          </div>
          <div class="login-log-filter login-log-filter--reason">
            <a-select v-model="form.failureReason" placeholder="失败原因" allow-clear>
              <a-option v-for="(label, reason) in reasonLabels" :key="reason" :value="reason">{{ label }}</a-option>
            </a-select>
          </div>
          <div class="login-log-filter"><a-input v-model="form.ip" placeholder="IP 地址" allow-clear @press-enter="search" /></div>
          <a-range-picker v-model="dateRange" show-time value-format="YYYY-MM-DD HH:mm:ss" allow-clear />
        </template>
        <template #actions>
          <a-button type="primary" @click="search"><template #icon><Search :size="16" /></template>查询</a-button>
          <a-button @click="reset"><template #icon><RotateCcw :size="16" /></template>重置</a-button>
          <a-button v-if="canClear" class="login-log-clear-button" status="danger" :loading="clearing" @click="requestClear"><template #icon><Eraser :size="16" /></template>清空日志</a-button>
        </template>
      </s-layout-search>

      <div v-if="error" class="login-log-error" role="alert">
        <span>{{ error }}</span>
        <a-button size="small" @click="load">重试</a-button>
      </div>

      <div v-if="canDelete && selectedRowKeys.length" class="login-log-batch-bar"><span>已选 <strong>{{ selectedRowKeys.length }}</strong> 条</span><a-button status="danger" :loading="deleting" @click="requestDelete"><template #icon><Trash2 :size="14" /></template>删除</a-button></div>
      <a-table v-if="!error" class="uvp-data-table login-log-table" row-key="id" :data="logs" :loading="loading" :pagination="pagination" :scroll="tableScroll" :bordered="false" v-model:selected-keys="selectedRowKeys" :row-selection="rowSelection" @page-change="handlePageChange" @page-size-change="handlePageSizeChange">
        <template #columns>
          <a-table-column title="用户名" data-index="username" :width="140" />
          <a-table-column title="结果" :width="90" align="center">
            <template #cell="{ record }"><a-tag :color="record.result === 'success' ? 'green' : 'red'">{{ resultLabel(record.result) }}</a-tag></template>
          </a-table-column>
          <a-table-column title="失败原因" :width="150"><template #cell="{ record }">{{ failureLabel(record.failureReason) }}</template></a-table-column>
          <a-table-column title="IP 地址" data-index="ip" :width="140" />
          <a-table-column title="地点" data-index="location" :width="120" />
          <a-table-column title="浏览器 / OS" :width="200"><template #cell="{ record }">{{ record.browser || '未知浏览器' }} / {{ record.os || '未知系统' }}</template></a-table-column>
          <a-table-column title="登录时间" :width="180"><template #cell="{ record }">{{ formatTime(record.createdAt) }}</template></a-table-column>
          <a-table-column title="操作" :width="canUnlock ? 160 : 90" align="center" fixed="right"><template #cell="{ record }"><div class="uvp-table-actions"><a-link class="uvp-table-action uvp-table-action--detail" @click="viewDetail(record)"><template #icon><Eye :size="13" /></template><span>详情</span></a-link><a-link v-if="canUnlock && record.result === 'failure' && record.failureReason === 'account_locked' && record.userId" class="uvp-table-action uvp-table-action--unlock" :loading="unlockingId === record.id" @click="requestUnlock(record)"><template #icon><KeyRound :size="13" /></template><span>解锁</span></a-link></div></template></a-table-column>
        </template>
        <template #empty><a-empty description="暂无登录日志" /></template>
      </a-table>
    </div>

    <a-modal modal-class="uvp-system-dialog login-log-detail-modal" v-model:visible="detailVisible" width="min(95vw, 680px)" :footer="false">
      <template #title>登录日志详情</template>
      <a-spin :loading="detailLoading" class="login-log-detail-loading">
        <div v-if="detailError" class="login-log-error" role="alert">{{ detailError }}</div>
        <a-empty v-else-if="!currentDetail && !detailLoading" description="暂无详情" />
        <a-descriptions v-else-if="currentDetail" class="uvp-system-description uvp-system-description--compact" :column="1" bordered>
          <a-descriptions-item label="用户名">{{ currentDetail.username }}</a-descriptions-item>
          <a-descriptions-item label="结果"><a-tag :color="currentDetail.result === 'success' ? 'green' : 'red'">{{ resultLabel(currentDetail.result) }}</a-tag></a-descriptions-item>
          <a-descriptions-item label="失败原因">{{ failureLabel(currentDetail.failureReason) }}</a-descriptions-item>
          <a-descriptions-item label="IP 地址">{{ currentDetail.ip }}</a-descriptions-item>
          <a-descriptions-item label="地点">{{ currentDetail.location }}</a-descriptions-item>
          <a-descriptions-item label="浏览器 / OS">{{ currentDetail.browser || '未知浏览器' }} / {{ currentDetail.os || '未知系统' }}</a-descriptions-item>
          <a-descriptions-item label="用户代理"><pre class="login-log-user-agent">{{ currentDetail.userAgent || '未知' }}</pre></a-descriptions-item>
          <a-descriptions-item label="登录时间">{{ formatTime(currentDetail.createdAt) }}</a-descriptions-item>
        </a-descriptions>
      </a-spin>
    </a-modal>
  </div>
</template>

<style scoped>
.login-log-page,
.login-log-page__inner {
  min-width: 0;
  max-width: 100%;
  overflow-x: hidden;
}

.login-log-filter {
  flex: 0 1 176px;
  width: 176px;
  max-width: 100%;
}

.login-log-filter--compact { width: 120px; flex-basis: 120px; }
.login-log-filter--reason { width: 160px; flex-basis: 160px; }
.login-log-filter :deep(.arco-input-wrapper),
.login-log-filter :deep(.arco-select-view) { width: 100%; min-height: 44px; }
.login-log-page :deep(.uvp-search-panel .arco-btn) { min-height: 44px; }
.login-log-page :deep(.uvp-search-panel__actions .login-log-clear-button) {
  color: var(--uvp-danger);
  background: var(--uvp-danger-soft);
  border-color: var(--uvp-danger-border);
  box-shadow: none;
}
.login-log-page :deep(.uvp-search-panel__actions .login-log-clear-button:hover) {
  color: #c53030;
  background: #ffe8e8;
  border-color: #efb7b7;
}
.login-log-error { display: flex; align-items: center; justify-content: space-between; gap: 16px; margin: 12px 0; padding: 12px 14px; color: var(--uvp-danger); background: var(--uvp-danger-soft); border: 1px solid var(--uvp-danger-border); border-radius: 6px; }
.login-log-batch-bar { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin: 12px 0; padding: 10px 14px; background: var(--uvp-surface-muted); border: 1px solid var(--uvp-border); border-radius: 6px; }
.login-log-detail-loading { display: block; min-height: 180px; }
.login-log-user-agent { max-height: 180px; margin: 0; overflow: auto; white-space: pre-wrap; overflow-wrap: anywhere; word-break: break-word; color: var(--uvp-text-secondary); }

@media (max-width: 768px) {
  .login-log-filter,
  .login-log-filter--compact,
  .login-log-filter--reason { flex: 1 1 100%; width: 100%; }
  .login-log-page :deep(.arco-picker) { width: 100%; min-height: 44px; }
  .login-log-error { align-items: flex-start; flex-direction: column; }
}
</style>
