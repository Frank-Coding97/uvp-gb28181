<script setup lang="ts">
import { computed, onActivated, onBeforeUnmount, onDeactivated, onMounted, reactive, ref, watch } from "vue";
import { Modal, Message } from "@arco-design/web-vue";
import { useRouter } from "vue-router";
import { Eye, KeyRound, MoreHorizontal, Plus, RefreshCw, RotateCcw, ScrollText, Search, ShieldCheck } from "lucide-vue-next";
import { useUserStoreHook } from "@/store/modules/user";
import {
  OPENAPI_CLIENT_DATA_SCOPE_OPTIONS,
  createOpenAPIClient,
  disableOpenAPIClient,
  enableOpenAPIClient,
  isOpenAPISuccess,
  listOpenAPIClients,
  revokeOpenAPIClient,
  rotateOpenAPIClientSecret,
  type OpenAPIClientCreateInput,
  type OpenAPIClientDataScope,
  type OpenAPIClientListParams,
  type OpenAPIClientStatus,
  type OpenAPIClientView,
  type OpenAPIManagedDepartment
} from "@/api/gb28181-openapi";
import OpenAPIClientCreateDialog from "./OpenAPIClientCreateDialog.vue";
import OpenAPISecretDialog from "./OpenAPISecretDialog.vue";

type StatusAction = "enable" | "disable" | "revoke";

const userStore = useUserStoreHook();
const router = useRouter();
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
const loading = ref(false);
const error = ref("");
const form = reactive({ ownerDeptId: undefined as number | undefined });
const pagination = reactive({ current: 1, pageSize: 10, total: 0, showTotal: true, showJumper: true, showPageSize: true });
const tableScroll = computed(() => ({ x: "100%", minWidth: 1120 }));

const createVisible = ref(false);
const createLoading = ref(false);
const createError = ref("");

const secretPayload = ref<{ clientId: number; accessKey: string; secretKey: string } | null>(null);
const secretOperation = ref<"create" | "rotate">("create");
const secretVisible = computed(() => secretPayload.value !== null);

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
  clearCreateState();
  clients.value = [];
  ownerDepartments.value = [];
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

function departmentName(id: number) {
  return ownerDepartments.value.find(item => item.id === id)?.name || `部门 #${id}`;
}

// ⛔ 数据范围是**每个客户端自己的配置**（1=本部门 / 4=本部门及以下），不能像以前那样
//    在列里硬编码"不含下级 / 共享设备" —— 那句话只对"本部门"成立，给"本部门及以下"
//    的客户端贴上就是错的。列的生命力就用标签把范围喊出来。
// ⚠️ 老数据可能没带这个字段（后端 migration 上线前建的客户端）→ 返回空串，由模板跳过，
//    绝不回落成"本部门"（那等于凭空收紧/放宽可见范围，是安全相关的推断）。
function dataScopeLabel(scope?: OpenAPIClientDataScope) {
  if (scope === undefined || scope === null) return "";
  return OPENAPI_CLIENT_DATA_SCOPE_OPTIONS.find(option => option.value === scope)?.label || `数据范围 #${scope}`;
}

function statusLabel(status: OpenAPIClientStatus) {
  return status === "active" ? "启用" : status === "disabled" ? "停用" : "已撤销";
}

function statusColor(status: OpenAPIClientStatus) {
  return status === "active" ? "green" : status === "disabled" ? "orange" : "red";
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

function openCreate() {
  if (!canCreate.value) return;
  nextGeneration();
  createError.value = "";
  createVisible.value = true;
}

function openWorkspace(record: OpenAPIClientView, tab: "overview" | "capabilities" | "security" | "logs" = "overview") {
  if (!canRead.value) return;
  void router.push({ path: `/gb28181/openapi-client/${record.id}`, query: { tab } });
}

function clearCreateState() {
  createVisible.value = false;
  createLoading.value = false;
  createError.value = "";
}

function closeCreate() {
  nextGeneration();
  clearCreateState();
}

async function performCreate(input: OpenAPIClientCreateInput) {
  if (!mounted || !canCreate.value || createLoading.value) return;
  const generation = lifecycleGeneration;
  createLoading.value = true;
  createError.value = "";
  try {
    const result = await createOpenAPIClient(input);
    if (!isCurrentGeneration(generation)) return;
    const failure = responseError(result, "创建 OpenAPI 客户端失败");
    if (failure || !result.data?.client || !result.data.secretKey) throw failure || new Error("创建响应缺少一次性 SK");
    secretOperation.value = "create";
    secretPayload.value = { clientId: result.data.client.id, accessKey: result.data.client.ak, secretKey: result.data.secretKey };
    createLoading.value = false;
    closeCreate();
    Message.success("客户端创建成功，请安全保存一次性 SK");
    await load();
  } catch (cause: unknown) {
    if (!isCurrentGeneration(generation)) return;
    if (handleAccessDenied(cause)) return;
    createError.value = errorMessage(cause, "创建 OpenAPI 客户端失败");
  } finally {
    if (isCurrentGeneration(generation)) createLoading.value = false;
  }
}

async function performRotate(record: OpenAPIClientView) {
  if (!mounted || !canRotate.value || !record || createLoading.value) return;
  const generation = lifecycleGeneration;
  createLoading.value = true;
  createError.value = "";
  try {
    const result = await rotateOpenAPIClientSecret(record.id, record.rowVersion);
    if (!isCurrentGeneration(generation)) return;
    const failure = responseError(result, "轮换 SK 失败");
    if (failure || !result.data?.client || !result.data.secretKey) throw failure || new Error("轮换响应缺少一次性 SK");
    secretOperation.value = "rotate";
    secretPayload.value = { clientId: result.data.client.id, accessKey: result.data.client.ak, secretKey: result.data.secretKey };
    updateListClient(result.data.client);
    Message.success("SK 已轮换，请安全保存新的密钥");
  } catch (cause: unknown) {
    if (!isCurrentGeneration(generation)) return;
    if (handleAccessDenied(cause)) return;
    if (httpStatus(cause) === 409) {
      await load();
      if (isCurrentGeneration(generation)) Message.error("客户端状态已变化，列表已刷新，请重新确认后操作。");
    } else {
      Message.error(errorMessage(cause, "轮换 SK 失败"));
    }
  } finally {
    if (isCurrentGeneration(generation)) createLoading.value = false;
  }
}

async function performStatus(action: StatusAction, record: OpenAPIClientView) {
  if (!mounted || !canStatus.value || !record || createLoading.value) return;
  const generation = lifecycleGeneration;
  createLoading.value = true;
  createError.value = "";
  try {
    const request = action === "enable" ? enableOpenAPIClient : action === "disable" ? disableOpenAPIClient : revokeOpenAPIClient;
    const result = await request(record.id, record.rowVersion);
    if (!isCurrentGeneration(generation)) return;
    const failure = responseError(result, `${action === "enable" ? "启用" : action === "disable" ? "停用" : "撤销"}客户端失败`);
    if (failure || !result.data?.client) throw failure || new Error("状态变更响应缺少客户端");
    updateListClient(result.data.client);
    if (!isCurrentGeneration(generation)) return;
    Message.success(
      action === "enable"
        ? "客户端已启用"
        : action === "disable"
          ? "客户端认证已停用，清退进度另行确认"
          : "客户端已撤销，清退进度另行确认"
    );
  } catch (cause: unknown) {
    if (!isCurrentGeneration(generation)) return;
    if (handleAccessDenied(cause)) return;
    if (httpStatus(cause) === 409) {
      await load();
      if (isCurrentGeneration(generation)) Message.error("客户端状态已变化，列表已刷新，请重新确认后操作。");
    } else {
      Message.error(errorMessage(cause, "客户端状态变更失败"));
    }
  } finally {
    if (isCurrentGeneration(generation)) createLoading.value = false;
  }
}

function requestRotate(record: OpenAPIClientView) {
  if (!mounted || !canRotate.value || createLoading.value) return;
  const generation = lifecycleGeneration;
  Modal.confirm({
    title: "轮换 OpenAPI 客户端 SK",
    content: "轮换会立即使旧 SK 失效，新的 SK 只展示一次。确认继续吗？",
    okText: "确认轮换 SK",
    cancelText: "取消",
    okButtonProps: { status: "warning" },
    onOk: () => (isCurrentGeneration(generation) ? performRotate(record) : undefined)
  });
}

function requestStatus(action: StatusAction, record: OpenAPIClientView) {
  if (!mounted || !canStatus.value || createLoading.value) return;
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
    onOk: () => (isCurrentGeneration(generation) ? performStatus(action, record) : undefined)
  });
}

function closeSecret() {
  secretPayload.value = null;
  secretOperation.value = "create";
}

function configureCreatedClient() {
  const id = secretPayload.value?.clientId;
  closeSecret();
  if (id) void router.push({ path: `/gb28181/openapi-client/${id}`, query: { tab: "capabilities" } });
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
    if (mounted && canRead.value) void load();
  },
  { flush: "sync" }
);

onMounted(() => {
  mounted = true;
  if (canRead.value) void load();
});

onActivated(() => {
  if (mounted) return;
  mounted = true;
  if (canRead.value) void load();
});

onDeactivated(deactivatePage);
onBeforeUnmount(deactivatePage);

defineExpose({
  clients,
  ownerDepartments,
  form,
  pagination,
  error,
  createError,
  createVisible,
  secretPayload,
  load,
  search,
  reset,
  handlePageChange,
  handlePageSizeChange,
  openWorkspace,
  openCreate,
  closeCreate,
  performCreate,
  requestRotate,
  performRotate,
  performStatus,
  closeSecret,
  configureCreatedClient
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
                <a-option v-for="department in ownerDepartments" :key="department.id" :value="department.id">{{
                  department.name
                }}</a-option>
              </a-select>
            </div>
          </template>
          <template #actions>
            <a-button type="primary" @click="search"
              ><template #icon><Search :size="16" /></template>查询</a-button
            >
            <a-button @click="reset"
              ><template #icon><RotateCcw :size="16" /></template>重置</a-button
            >
          </template>
          <template #extra>
            <a-tooltip content="刷新 OpenAPI 客户端" position="top">
              <a-button class="uvp-refresh-btn" :loading="loading" aria-label="刷新 OpenAPI 客户端" @click="load">
                <template #icon><RefreshCw :size="16" /></template>刷新
              </a-button>
            </a-tooltip>
            <a-button v-if="canCreate" type="primary" @click="openCreate"
              ><template #icon><Plus :size="16" /></template>新建客户端</a-button
            >
          </template>
        </s-layout-search>

        <div v-if="error" class="openapi-client-page__error" role="alert">
          <span>{{ error }}</span>
          <a-button size="small" @click="load">重试</a-button>
        </div>
        <div v-if="!error" class="openapi-client-page__boundary-note">
          每个客户端按归属部门和数据范围访问设备；认证状态与观看连接清退状态分别确认。
        </div>

        <a-table
          v-if="!error"
          class="uvp-data-table openapi-client-table"
          row-key="id"
          :data="clients"
          :loading="loading"
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
            <a-table-column title="归属部门与数据范围" :width="210">
              <template #cell="{ record }">
                <span>{{ departmentName(record.ownerDeptId) }}</span>
                <a-tag v-if="dataScopeLabel(record.dataScope)" size="small" class="openapi-client-table__scope">{{
                  dataScopeLabel(record.dataScope)
                }}</a-tag>
              </template>
            </a-table-column>
            <a-table-column title="认证状态" :width="110">
              <template #cell="{ record }"
                ><a-tag :color="statusColor(record.status)">{{ statusLabel(record.status) }}</a-tag></template
              >
            </a-table-column>
            <a-table-column title="密钥版本" data-index="secretVersion" :width="100" />
            <a-table-column title="操作" :width="410" fixed="right">
              <template #cell="{ record }">
                <div class="openapi-client-table__actions">
                  <a-button
                    v-if="canGrant"
                    type="text"
                    class="uvp-table-action uvp-table-action--permission"
                    @click="openWorkspace(record, 'capabilities')"
                    ><template #icon><ShieldCheck :size="15" /></template>配置能力</a-button
                  >
                  <a-button v-if="canRead" type="text" class="uvp-table-action" @click="openWorkspace(record, 'overview')"
                    ><template #icon><Eye :size="15" /></template>详情</a-button
                  >
                  <a-button v-if="canAudit" type="text" class="uvp-table-action" @click="openWorkspace(record, 'logs')"
                    ><template #icon><ScrollText :size="15" /></template>调用记录</a-button
                  >
                  <a-dropdown v-if="(canRotate || canStatus) && record.status !== 'revoked'" trigger="click">
                    <a-button type="text" class="uvp-table-action" aria-label="更多客户端操作">
                      <template #icon><MoreHorizontal :size="16" /></template>更多
                    </a-button>
                    <template #content>
                      <a-doption v-if="canRotate" @click="requestRotate(record)"><KeyRound :size="14" />轮换 SK</a-doption>
                      <a-doption v-if="canStatus && record.status === 'active'" @click="requestStatus('disable', record)"
                        >停用</a-doption
                      >
                      <a-doption v-if="canStatus && record.status === 'disabled'" @click="requestStatus('enable', record)"
                        >启用</a-doption
                      >
                      <a-doption v-if="canStatus" class="openapi-client-table__danger" @click="requestStatus('revoke', record)"
                        >撤销</a-doption
                      >
                    </template>
                  </a-dropdown>
                </div>
              </template>
            </a-table-column>
          </template>
        </a-table>
      </template>
      <a-empty v-else description="没有 OpenAPI 客户端查看权限" />
    </div>

    <OpenAPIClientCreateDialog
      :visible="createVisible"
      :departments="ownerDepartments"
      :submitting="createLoading"
      :error="createError"
      @close="closeCreate"
      @create="performCreate"
    />
    <OpenAPISecretDialog
      :visible="secretVisible"
      :access-key="secretPayload?.accessKey || ''"
      :secret-key="secretPayload?.secretKey || ''"
      :operation="secretOperation"
      @close="closeSecret"
      @configure="configureCreatedClient"
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
  gap: 12px;
  align-items: center;
  padding: 10px 14px;
  margin: 12px 0;
  font-size: 13px;
  border-radius: 4px;
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
  font-weight: 600;
  color: var(--color-text-1);
}

.openapi-client-table__ak {
  display: block;
  margin-top: 4px;
  font-size: 11px;
  color: var(--color-text-3);
  overflow-wrap: anywhere;
}

.openapi-client-table__scope {
  margin-left: 6px;
}

.openapi-client-table__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 2px 4px;
}

.openapi-client-table :deep(.arco-btn) {
  min-height: 34px;
}

.openapi-client-table__danger {
  color: rgb(var(--danger-6));
}

@media (width <= 768px) {
  .openapi-client-filter {
    width: 100%;
  }

  .openapi-client-page__boundary-note {
    align-items: flex-start;
  }
}
</style>
