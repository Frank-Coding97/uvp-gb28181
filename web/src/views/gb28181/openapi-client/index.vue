<script setup lang="ts">
import { computed, onActivated, onBeforeUnmount, onDeactivated, onMounted, reactive, ref, watch } from "vue";
import { Modal, Message } from "@arco-design/web-vue";
import { Ban, Eye, KeyRound, Plus, RefreshCw, RotateCcw, ScrollText, Search, ShieldCheck, ShieldOff } from "lucide-vue-next";
import { useUserStoreHook } from "@/store/modules/user";
import {
  createOpenAPIClient,
  disableOpenAPIClient,
  enableOpenAPIClient,
  getOpenAPIClient,
  getOpenAPIClientCapabilities,
  getOpenAPIClientRevocationStatus,
  isOpenAPISuccess,
  listOpenAPIClientAudits,
  listOpenAPIClients,
  revokeOpenAPIClient,
  rotateOpenAPIClientSecret,
  updateOpenAPIClientScopes,
  type OpenAPIClientAudit,
  type OpenAPIClientCreateInput,
  type OpenAPIClientListParams,
  type OpenAPIClientStatus,
  type OpenAPIClientView,
  type OpenAPIManagedDepartment,
  type OpenAPIRevocationStatus,
  type OpenAPIScopeView
} from "@/api/gb28181-openapi";
import OpenAPIClientDrawer from "./OpenAPIClientDrawer.vue";
import OpenAPISecretDialog from "./OpenAPISecretDialog.vue";

type StatusAction = "enable" | "disable" | "revoke";

const userStore = useUserStoreHook();
const permissions = computed(() => userStore.account?.permissions || []);
const hasPermission = (permission: string) => permissions.value.includes("*:*:*") || permissions.value.includes(permission);
const canRead = computed(() => hasPermission("gb28181:openapi:client:read"));
const canCreate = computed(() => hasPermission("gb28181:openapi:client:create"));
const canGrant = computed(() => hasPermission("gb28181:openapi:client:grant"));
const canRotate = computed(() => hasPermission("gb28181:openapi:client:rotate"));
const canStatus = computed(() => hasPermission("gb28181:openapi:client:status"));
const canAudit = computed(() => hasPermission("gb28181:openapi:client:audit"));

const clients = ref<OpenAPIClientView[]>([]);
const ownerDepartments = ref<OpenAPIManagedDepartment[]>([]);
const capabilities = ref<string[]>([]);
const loading = ref(false);
const capabilitiesLoading = ref(false);
const capabilitiesReady = ref(false);
const error = ref("");
const capabilitiesError = ref("");
const form = reactive({ ownerDeptId: undefined as number | undefined });
const pagination = reactive({ current: 1, pageSize: 20, total: 0, showTotal: true, showJumper: true, showPageSize: true });
const tableScroll = computed(() => ({ x: "100%", minWidth: 1120 }));

const drawerVisible = ref(false);
const drawerMode = ref<"create" | "detail">("detail");
const drawerLoading = ref(false);
const drawerError = ref("");
const currentClient = ref<OpenAPIClientView | null>(null);
const currentScopes = ref<OpenAPIScopeView[]>([]);
const detailReady = ref(false);
const detailClientId = ref<number | null>(null);
const detailRowVersion = ref<number | null>(null);
const revocationStatus = ref<OpenAPIRevocationStatus | null>(null);
const revocationError = ref("");
const revocationLoading = ref(false);
const auditItems = ref<OpenAPIClientAudit[]>([]);
const auditLoading = ref(false);

const secretPayload = ref<{ accessKey: string; secretKey: string } | null>(null);
const secretOperation = ref<"create" | "rotate">("create");
const secretVisible = computed(() => secretPayload.value !== null);
const authStatus = computed(() => currentClient.value?.status || null);
const canSaveScopes = computed(
  () =>
    canGrant.value &&
    !drawerLoading.value &&
    capabilitiesReady.value &&
    detailReady.value &&
    !!currentClient.value &&
    currentClient.value.id === detailClientId.value &&
    currentClient.value.rowVersion === detailRowVersion.value
);

let requestVersion = 0;
let lifecycleGeneration = 0;
let pageGeneration = 0;
let mounted = true;

function nextGeneration() {
  lifecycleGeneration += 1;
  return lifecycleGeneration;
}

function isCurrentGeneration(generation: number) {
  return mounted && generation === lifecycleGeneration;
}

function isCurrentPage(page: number) {
  return mounted && page === pageGeneration;
}

function isCurrentClient(generation: number, id: number) {
  return isCurrentGeneration(generation) && currentClient.value?.id === id;
}

function buildParams(): OpenAPIClientListParams {
  const params: OpenAPIClientListParams = { page: pagination.current, pageSize: pagination.pageSize };
  if (form.ownerDeptId) params.ownerDeptId = form.ownerDeptId;
  return params;
}

function errorMessage(cause: unknown, fallback: string) {
  if (cause && typeof cause === "object" && "response" in cause) {
    const response = (cause as { response?: { data?: { message?: string } } }).response;
    const message = response?.data?.message;
    if (message) return String(message);
  }
  if (cause instanceof Error && cause.message) return cause.message;
  if (typeof cause === "string") return cause;
  return fallback;
}

function responseError<T>(response: { code: string; message: string; data: T }, fallback: string) {
  return isOpenAPISuccess(response) ? null : new Error(response.message || fallback);
}

function httpStatus(cause: unknown) {
  if (!cause || typeof cause !== "object" || !("response" in cause)) return 0;
  return Number((cause as { response?: { status?: number } }).response?.status || 0);
}

function clearAccessState() {
  nextGeneration();
  pageGeneration += 1;
  requestVersion += 1;
  closeSecret();
  clearDrawerState();
  clients.value = [];
  ownerDepartments.value = [];
  capabilities.value = [];
  capabilitiesReady.value = false;
  capabilitiesLoading.value = false;
  capabilitiesError.value = "";
  loading.value = false;
  pagination.total = 0;
}

function handleAccessDenied(cause: unknown) {
  if (![401, 403, 404].includes(httpStatus(cause))) return false;
  // A masked 404 also means this client is no longer in the operator's scope.
  clearAccessState();
  error.value = "当前访问已被拒绝或对象已不可访问，已清空缓存，请刷新后重试。";
  return true;
}

function conflictMessage(rowVersion: number, refreshed = true) {
  return refreshed
    ? `版本冲突：页面提交的 rowVersion=${rowVersion} 已失效，已刷新当前详情，请确认最新状态后重试。`
    : `版本冲突：页面提交的 rowVersion=${rowVersion} 已失效，但当前详情刷新失败，请重试。`;
}

function departmentName(id: number) {
  return ownerDepartments.value.find(item => item.id === id)?.name || `部门 #${id}`;
}

function statusLabel(status: OpenAPIClientStatus) {
  return status === "active" ? "启用" : status === "disabled" ? "停用" : "已撤销";
}

function statusColor(status: OpenAPIClientStatus) {
  return status === "active" ? "green" : status === "disabled" ? "orange" : "red";
}

async function loadCapabilities() {
  if (!canRead.value) return;
  const page = pageGeneration;
  capabilitiesLoading.value = true;
  capabilitiesReady.value = false;
  capabilitiesError.value = "";
  try {
    const result = await getOpenAPIClientCapabilities();
    if (!isCurrentPage(page)) return;
    const failure = responseError(result, "能力目录加载失败");
    if (failure) throw failure;
    capabilities.value = Array.isArray(result.data) ? result.data : [];
    capabilitiesReady.value = true;
  } catch (cause: unknown) {
    if (!isCurrentPage(page)) return;
    if (handleAccessDenied(cause)) return;
    capabilities.value = [];
    capabilitiesReady.value = false;
    capabilitiesError.value = errorMessage(cause, "能力目录加载失败");
  } finally {
    if (isCurrentPage(page)) capabilitiesLoading.value = false;
  }
}

async function load() {
  if (!canRead.value) {
    clients.value = [];
    ownerDepartments.value = [];
    return;
  }
  const version = ++requestVersion;
  const page = pageGeneration;
  loading.value = true;
  error.value = "";
  try {
    const result = await listOpenAPIClients(buildParams());
    if (version !== requestVersion || !isCurrentPage(page)) return;
    const failure = responseError(result, "OpenAPI 客户端加载失败");
    if (failure) throw failure;
    clients.value = result.data?.items || [];
    ownerDepartments.value = result.data?.ownerDepartments || [];
    pagination.total = result.data?.total || 0;
  } catch (cause: unknown) {
    if (version !== requestVersion || !isCurrentPage(page)) return;
    if (handleAccessDenied(cause)) return;
    clients.value = [];
    ownerDepartments.value = [];
    pagination.total = 0;
    error.value = errorMessage(cause, "OpenAPI 客户端加载失败");
  } finally {
    if (version === requestVersion && isCurrentPage(page)) loading.value = false;
  }
}

async function search() {
  pagination.current = 1;
  await load();
}

async function reset() {
  form.ownerDeptId = undefined;
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

function updateListClient(updated: OpenAPIClientView) {
  const index = clients.value.findIndex(item => item.id === updated.id);
  if (index >= 0) clients.value.splice(index, 1, updated);
}

async function loadRevocationStatus(id = currentClient.value?.id, generation = lifecycleGeneration) {
  if (!canStatus.value || !id || !isCurrentClient(generation, id)) return;
  revocationLoading.value = true;
  revocationError.value = "";
  try {
    const result = await getOpenAPIClientRevocationStatus(id);
    if (!isCurrentClient(generation, id)) return;
    const failure = responseError(result, "撤销清退进度暂不可用");
    if (failure) throw failure;
    revocationStatus.value = result.data;
  } catch (cause: unknown) {
    if (!isCurrentClient(generation, id)) return;
    if (handleAccessDenied(cause)) return;
    revocationStatus.value = null;
    revocationError.value = `撤销清退进度暂不可用：${errorMessage(cause, "服务未就绪")}`;
  } finally {
    if (isCurrentClient(generation, id)) revocationLoading.value = false;
  }
}

async function refreshDetail(id: number, generation = lifecycleGeneration) {
  if (!isCurrentClient(generation, id)) return false;
  detailReady.value = false;
  detailClientId.value = null;
  detailRowVersion.value = null;
  drawerLoading.value = true;
  try {
    const result = await getOpenAPIClient(id);
    if (!isCurrentClient(generation, id)) return false;
    const failure = responseError(result, "客户端详情加载失败");
    if (failure || !result.data?.client || !Array.isArray(result.data.scopes)) throw failure || new Error("客户端详情响应不完整");
    currentClient.value = result.data.client;
    currentScopes.value = result.data.scopes || [];
    detailClientId.value = result.data.client.id;
    detailRowVersion.value = result.data.client.rowVersion;
    detailReady.value = true;
    updateListClient(result.data.client);
    await loadRevocationStatus(id, generation);
    return isCurrentClient(generation, id);
  } catch (cause: unknown) {
    if (!isCurrentClient(generation, id)) return false;
    if (handleAccessDenied(cause)) return false;
    throw cause;
  } finally {
    if (isCurrentClient(generation, id)) drawerLoading.value = false;
  }
}

function openDetailContext(record: OpenAPIClientView, generation: number) {
  if (!isCurrentGeneration(generation)) return false;
  drawerMode.value = "detail";
  currentClient.value = record;
  currentScopes.value = [];
  detailReady.value = false;
  detailClientId.value = null;
  detailRowVersion.value = null;
  revocationStatus.value = null;
  revocationError.value = "";
  auditItems.value = [];
  drawerError.value = "";
  drawerVisible.value = true;
  return true;
}

async function refreshDetailContext(record: OpenAPIClientView, generation: number) {
  if (!openDetailContext(record, generation)) return false;
  try {
    return await refreshDetail(record.id, generation);
  } catch {
    return false;
  }
}

async function openDetail(record: OpenAPIClientView) {
  if (!canRead.value || drawerLoading.value) return;
  const generation = nextGeneration();
  openDetailContext(record, generation);
  try {
    await refreshDetail(record.id, generation);
  } catch (cause: unknown) {
    if (isCurrentClient(generation, record.id)) drawerError.value = errorMessage(cause, "客户端详情加载失败");
  }
}

function openCreate() {
  if (!canCreate.value) return;
  nextGeneration();
  drawerMode.value = "create";
  currentClient.value = null;
  currentScopes.value = [];
  detailReady.value = false;
  detailClientId.value = null;
  detailRowVersion.value = null;
  drawerError.value = "";
  drawerVisible.value = true;
}

function clearDrawerState() {
  drawerVisible.value = false;
  drawerMode.value = "detail";
  drawerLoading.value = false;
  auditLoading.value = false;
  revocationLoading.value = false;
  drawerError.value = "";
  currentClient.value = null;
  currentScopes.value = [];
  detailReady.value = false;
  detailClientId.value = null;
  detailRowVersion.value = null;
  revocationStatus.value = null;
  revocationError.value = "";
  auditItems.value = [];
}

function closeDrawer() {
  nextGeneration();
  clearDrawerState();
}

async function performCreate(input: OpenAPIClientCreateInput) {
  if (!mounted || !canCreate.value || drawerLoading.value) return;
  const generation = lifecycleGeneration;
  drawerLoading.value = true;
  drawerError.value = "";
  try {
    const result = await createOpenAPIClient(input);
    if (!isCurrentGeneration(generation)) return;
    const failure = responseError(result, "创建 OpenAPI 客户端失败");
    if (failure || !result.data?.client || !result.data.secretKey) throw failure || new Error("创建响应缺少一次性 SK");
    secretOperation.value = "create";
    secretPayload.value = { accessKey: result.data.client.ak, secretKey: result.data.secretKey };
    drawerLoading.value = false;
    closeDrawer();
    Message.success("客户端创建成功，请安全保存一次性 SK");
    await load();
  } catch (cause: unknown) {
    if (!isCurrentGeneration(generation)) return;
    if (handleAccessDenied(cause)) return;
    drawerError.value = errorMessage(cause, "创建 OpenAPI 客户端失败");
  } finally {
    if (isCurrentGeneration(generation)) drawerLoading.value = false;
  }
}

async function saveScopes(id: number, scopes: string[], rowVersion: number) {
  if (
    !mounted ||
    !canSaveScopes.value ||
    !currentClient.value ||
    currentClient.value.id !== id ||
    currentClient.value.rowVersion !== rowVersion
  ) return;
  const generation = lifecycleGeneration;
  drawerLoading.value = true;
  drawerError.value = "";
  try {
    const result = await updateOpenAPIClientScopes(id, { rowVersion, scopes });
    if (!isCurrentClient(generation, id)) return;
    const failure = responseError(result, "保存客户端能力失败");
    if (failure) throw failure;
    if (result.data) updateListClient(result.data);
    await refreshDetail(id, generation);
    if (!isCurrentClient(generation, id)) return;
    Message.success("客户端能力已更新");
  } catch (cause: unknown) {
    if (!isCurrentClient(generation, id)) return;
    if (handleAccessDenied(cause)) return;
    if (httpStatus(cause) === 409) {
      drawerError.value = conflictMessage(rowVersion);
      try { await refreshDetail(id, generation); } catch { /* keep the explicit conflict message */ }
    } else {
      drawerError.value = errorMessage(cause, "保存客户端能力失败");
    }
  } finally {
    if (isCurrentClient(generation, id)) drawerLoading.value = false;
  }
}

async function performRotate(record: OpenAPIClientView = currentClient.value as OpenAPIClientView) {
  if (!mounted || !canRotate.value || !record || drawerLoading.value) return;
  const generation = lifecycleGeneration;
  drawerLoading.value = true;
  drawerError.value = "";
  try {
    const result = await rotateOpenAPIClientSecret(record.id, record.rowVersion);
    if (!isCurrentGeneration(generation)) return;
    const failure = responseError(result, "轮换 SK 失败");
    if (failure || !result.data?.client || !result.data.secretKey) throw failure || new Error("轮换响应缺少一次性 SK");
    secretOperation.value = "rotate";
    secretPayload.value = { accessKey: result.data.client.ak, secretKey: result.data.secretKey };
    currentClient.value = result.data.client;
    updateListClient(result.data.client);
    Message.success("SK 已轮换，请安全保存新的密钥");
  } catch (cause: unknown) {
    if (!isCurrentGeneration(generation)) return;
    if (handleAccessDenied(cause)) return;
    if (httpStatus(cause) === 409) {
      const refreshed = await refreshDetailContext(record, generation);
      if (isCurrentGeneration(generation)) drawerError.value = conflictMessage(record.rowVersion, refreshed);
    } else {
      drawerError.value = errorMessage(cause, "轮换 SK 失败");
    }
  } finally {
    if (isCurrentGeneration(generation)) drawerLoading.value = false;
  }
}

async function performStatus(action: StatusAction, record: OpenAPIClientView = currentClient.value as OpenAPIClientView) {
  if (!mounted || !canStatus.value || !record || drawerLoading.value) return;
  const generation = lifecycleGeneration;
  drawerLoading.value = true;
  drawerError.value = "";
  try {
    const request = action === "enable" ? enableOpenAPIClient : action === "disable" ? disableOpenAPIClient : revokeOpenAPIClient;
    const result = await request(record.id, record.rowVersion);
    if (!isCurrentGeneration(generation)) return;
    const failure = responseError(result, `${action === "enable" ? "启用" : action === "disable" ? "停用" : "撤销"}客户端失败`);
    if (failure || !result.data?.client) throw failure || new Error("状态变更响应缺少客户端");
    const shouldShowDrawer = action !== "enable" || drawerVisible.value;
    openDetailContext(result.data.client, generation);
    if (action === "enable" && !shouldShowDrawer) drawerVisible.value = false;
    updateListClient(result.data.client);
    if (action !== "enable") {
      revocationStatus.value = { status: result.data.revocationStatus || "pending", pending: 0, closed: 0 };
      revocationError.value = "";
      try {
        const refreshed = await refreshDetail(result.data.client.id, generation);
        if (!refreshed && isCurrentGeneration(generation)) drawerError.value = "状态已更新，但当前详情刷新失败，请重试。";
      } catch (detailCause: unknown) {
        if (isCurrentGeneration(generation)) drawerError.value = `状态已更新，但当前详情刷新失败：${errorMessage(detailCause, "请重试")}`;
      }
    } else {
      revocationStatus.value = null;
      revocationError.value = "";
    }
    if (!isCurrentGeneration(generation)) return;
    Message.success(action === "enable" ? "客户端已启用" : action === "disable" ? "客户端认证已停用，清退进度另行确认" : "客户端已撤销，清退进度另行确认");
    if (action !== "enable") drawerVisible.value = true;
  } catch (cause: unknown) {
    if (!isCurrentGeneration(generation)) return;
    if (handleAccessDenied(cause)) return;
    if (httpStatus(cause) === 409) {
      const refreshed = await refreshDetailContext(record, generation);
      if (isCurrentGeneration(generation)) drawerError.value = conflictMessage(record.rowVersion, refreshed);
    } else {
      drawerError.value = errorMessage(cause, "客户端状态变更失败");
    }
  } finally {
    if (isCurrentGeneration(generation)) drawerLoading.value = false;
  }
}

function requestRotate(record: OpenAPIClientView) {
  if (!mounted || !canRotate.value || drawerLoading.value) return;
  const generation = lifecycleGeneration;
  Modal.confirm({
    title: "轮换 OpenAPI 客户端 SK",
    content: "轮换会立即使旧 SK 失效，新的 SK 只展示一次。确认继续吗？",
    okText: "确认轮换 SK",
    cancelText: "取消",
    okButtonProps: { status: "warning" },
    onOk: () => isCurrentGeneration(generation) ? performRotate(record) : undefined
  });
}

function requestStatus(action: StatusAction, record: OpenAPIClientView) {
  if (!mounted || !canStatus.value || drawerLoading.value) return;
  const generation = lifecycleGeneration;
  const isRevoke = action === "revoke";
  Modal.confirm({
    title: isRevoke ? "撤销 OpenAPI 客户端" : action === "disable" ? "停用 OpenAPI 客户端" : "启用 OpenAPI 客户端",
    content: isRevoke
      ? "撤销不可恢复，认证会立即失败；观看连接清退需以服务端进度确认，不会自动误伤其他客户端。"
      : action === "disable"
        ? "停用会立即阻止新请求；观看连接不会被前端虚报为已清退，请在详情中查看服务端进度。"
        : "启用不会恢复旧的播放授权，请确认客户端仍属于有效部门。",
    okText: isRevoke ? "确认撤销" : action === "disable" ? "确认停用" : "确认启用",
    cancelText: "取消",
    okButtonProps: isRevoke || action === "disable" ? { status: "danger" } : undefined,
    onOk: () => isCurrentGeneration(generation) ? performStatus(action, record) : undefined
  });
}

async function refreshRevocation() {
  await loadRevocationStatus(currentClient.value?.id, lifecycleGeneration);
}

async function loadAudits() {
  if (!canAudit.value || !currentClient.value) return;
  const generation = lifecycleGeneration;
  const clientId = currentClient.value.id;
  auditLoading.value = true;
  try {
    const result = await listOpenAPIClientAudits(clientId);
    if (!isCurrentClient(generation, clientId)) return;
    const failure = responseError(result, "审计加载失败");
    if (failure) throw failure;
    auditItems.value = result.data?.items || [];
  } catch (cause: unknown) {
    if (!isCurrentClient(generation, clientId)) return;
    if (handleAccessDenied(cause)) return;
    auditItems.value = [];
    drawerError.value = errorMessage(cause, "审计加载失败");
  } finally {
    if (isCurrentClient(generation, clientId)) auditLoading.value = false;
  }
}

function closeSecret() {
  secretPayload.value = null;
  secretOperation.value = "create";
}

function deactivatePage() {
  mounted = false;
  clearAccessState();
}

watch(
  () => JSON.stringify([userStore.account?.id, [...(userStore.account?.roles || [])].sort(), [...permissions.value].sort()]),
  () => {
    clearAccessState();
    form.ownerDeptId = undefined;
    pagination.current = 1;
    if (mounted && canRead.value) {
      void load();
      void loadCapabilities();
    }
  },
  { flush: "sync" }
);

onMounted(() => {
  mounted = true;
  if (canRead.value) {
    void load();
    void loadCapabilities();
  }
});

onActivated(() => {
  if (mounted) return;
  mounted = true;
  if (canRead.value) {
    void load();
    void loadCapabilities();
  }
});

onDeactivated(deactivatePage);
onBeforeUnmount(deactivatePage);

defineExpose({
  clients,
  ownerDepartments,
  capabilities,
  capabilitiesReady,
  form,
  pagination,
  error,
  drawerError,
  currentClient,
  currentScopes,
  detailReady,
  detailClientId,
  detailRowVersion,
  drawerMode,
  drawerVisible,
  canSaveScopes,
  auditItems,
  authStatus,
  revocationStatus,
  revocationError,
  secretPayload,
  load,
  search,
  reset,
  handlePageChange,
  handlePageSizeChange,
  openDetail,
  openCreate,
  closeDrawer,
  performCreate,
  saveScopes,
  requestRotate,
  performRotate,
  performStatus,
  refreshRevocation,
  loadAudits,
  closeSecret
});
</script>

<template>
  <div class="snow-page openapi-client-page">
    <div class="snow-inner uvp-page-shell-flat openapi-client-page__inner">
      <template v-if="canRead">
        <s-layout-search>
          <template #fields>
            <div class="openapi-client-filter">
              <a-select v-model="form.ownerDeptId" placeholder="归属部门" allow-clear allow-search>
                <a-option v-for="department in ownerDepartments" :key="department.id" :value="department.id">{{ department.name }}</a-option>
              </a-select>
            </div>
          </template>
          <template #actions>
            <a-button type="primary" @click="search"><template #icon><Search :size="16" /></template>查询</a-button>
            <a-button @click="reset"><template #icon><RotateCcw :size="16" /></template>重置</a-button>
          </template>
          <template #extra>
            <a-tooltip content="刷新 OpenAPI 客户端" position="top">
              <a-button class="uvp-refresh-btn" :loading="loading" aria-label="刷新 OpenAPI 客户端" @click="load">
                <template #icon><RefreshCw :size="16" /></template>刷新
              </a-button>
            </a-tooltip>
            <a-button v-if="canCreate" type="primary" @click="openCreate"><template #icon><Plus :size="16" /></template>新建客户端</a-button>
          </template>
        </s-layout-search>

        <div v-if="error" class="openapi-client-page__error" role="alert">
          <span>{{ error }}</span>
          <a-button size="small" @click="load">重试</a-button>
        </div>
        <div v-if="capabilitiesError" class="openapi-client-page__notice" role="status">{{ capabilitiesError }}；能力授权暂不可用。</div>
        <div v-if="!error" class="openapi-client-page__boundary-note">
          所有客户端只允许访问归属部门的设备，不包含下级部门或共享设备；认证状态与观看连接清退状态分别确认。
        </div>

        <a-table
          v-if="!error"
          class="uvp-data-table openapi-client-table"
          row-key="id"
          :data="clients"
          :loading="loading || capabilitiesLoading"
          :pagination="pagination"
          :scroll="tableScroll"
          :bordered="false"
          @page-change="handlePageChange"
          @page-size-change="handlePageSizeChange"
        >
          <template #empty><a-empty description="暂无 OpenAPI 客户端" /></template>
          <template #columns>
            <a-table-column title="客户端" :width="220">
              <template #cell="{ record }">
                <div class="openapi-client-table__name">{{ record.name }}</div>
                <code class="openapi-client-table__ak">{{ record.ak }}</code>
              </template>
            </a-table-column>
            <a-table-column title="归属部门（精确）" :width="190">
              <template #cell="{ record }">
                <span>{{ departmentName(record.ownerDeptId) }}</span>
                <small class="openapi-client-table__hint">不含下级 / 共享设备</small>
              </template>
            </a-table-column>
            <a-table-column title="认证状态" :width="110">
              <template #cell="{ record }"><a-tag :color="statusColor(record.status)">{{ statusLabel(record.status) }}</a-tag></template>
            </a-table-column>
            <a-table-column title="密钥版本" data-index="secretVersion" :width="100" />
            <a-table-column title="rowVersion" data-index="rowVersion" :width="100" />
            <a-table-column title="操作" :width="520" fixed="right">
              <template #cell="{ record }">
                <div class="openapi-client-table__actions">
                  <a-button v-if="canRead" type="text" class="uvp-table-action" @click="openDetail(record)"><template #icon><Eye :size="15" /></template>详情</a-button>
                  <a-button v-if="canGrant" type="text" class="uvp-table-action uvp-table-action--permission" @click="openDetail(record)"><template #icon><ShieldCheck :size="15" /></template>能力</a-button>
                  <a-button v-if="canRotate && record.status !== 'revoked'" type="text" class="uvp-table-action" @click="requestRotate(record)"><template #icon><KeyRound :size="15" /></template>轮换 SK</a-button>
                  <a-button v-if="canStatus && record.status === 'active'" type="text" status="warning" @click="requestStatus('disable', record)"><template #icon><ShieldOff :size="15" /></template>停用</a-button>
                  <a-button v-if="canStatus && record.status === 'disabled'" type="text" @click="requestStatus('enable', record)"><template #icon><ShieldCheck :size="15" /></template>启用</a-button>
                  <a-button v-if="canStatus && record.status !== 'revoked'" type="text" status="danger" @click="requestStatus('revoke', record)"><template #icon><Ban :size="15" /></template>撤销</a-button>
                  <a-button v-if="canAudit" type="text" class="uvp-table-action" @click="openDetail(record); loadAudits()"><template #icon><ScrollText :size="15" /></template>审计</a-button>
                </div>
              </template>
            </a-table-column>
          </template>
        </a-table>
      </template>
      <a-empty v-else description="没有 OpenAPI 客户端查看权限" />
    </div>

    <OpenAPIClientDrawer
      :visible="drawerVisible"
      :mode="drawerMode"
      :client="currentClient"
      :scopes="currentScopes"
      :capabilities="capabilities"
      :capabilities-ready="capabilitiesReady"
      :detail-ready="detailReady"
      :detail-client-id="detailClientId"
      :detail-row-version="detailRowVersion"
      :departments="ownerDepartments"
      :submitting="drawerLoading"
      :error="drawerError"
      :can-grant="canGrant"
      :can-status="canStatus"
      :can-audit="canAudit"
      :revocation-status="revocationStatus"
      :revocation-error="revocationError"
      :revocation-loading="revocationLoading"
      :audit-items="auditItems"
      :audit-loading="auditLoading"
      @close="closeDrawer"
      @create="performCreate"
      @save-scopes="scopes => currentClient && saveScopes(currentClient.id, scopes, currentClient.rowVersion)"
      @refresh-revocation="refreshRevocation"
      @load-audits="loadAudits"
    />
    <OpenAPISecretDialog
      :visible="secretVisible"
      :access-key="secretPayload?.accessKey || ''"
      :secret-key="secretPayload?.secretKey || ''"
      :operation="secretOperation"
      @close="closeSecret"
    />
  </div>
</template>

<style scoped>
.openapi-client-page__inner {
  min-width: 0;
}

.openapi-client-filter {
  width: 220px;
}

.openapi-client-page__error,
.openapi-client-page__notice,
.openapi-client-page__boundary-note {
  display: flex;
  align-items: center;
  gap: 12px;
  margin: 12px 0;
  padding: 10px 14px;
  border-radius: 4px;
  font-size: 13px;
}

.openapi-client-page__error {
  justify-content: space-between;
  color: rgb(var(--danger-6));
  background: rgb(var(--danger-1));
}

.openapi-client-page__notice {
  color: rgb(var(--warning-7));
  background: rgb(var(--warning-1));
}

.openapi-client-page__boundary-note {
  color: var(--color-text-3);
  background: var(--color-fill-1);
}

.openapi-client-table__name {
  color: var(--color-text-1);
  font-weight: 600;
}

.openapi-client-table__ak {
  display: block;
  margin-top: 4px;
  color: var(--color-text-3);
  font-size: 11px;
  overflow-wrap: anywhere;
}

.openapi-client-table__hint {
  display: block;
  margin-top: 3px;
  color: var(--color-text-3);
  font-size: 11px;
}

.openapi-client-table__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 2px 4px;
}

.openapi-client-table :deep(.arco-btn) {
  min-height: 34px;
}

@media (max-width: 768px) {
  .openapi-client-filter {
    width: 100%;
  }

  .openapi-client-page__boundary-note {
    align-items: flex-start;
  }
}
</style>
