<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter, onBeforeRouteLeave } from "vue-router";
import { Message, Modal } from "@arco-design/web-vue";
import { ArrowLeft, RefreshCw } from "lucide-vue-next";
import { useUserStoreHook } from "@/store/modules/user";
import {
  OPENAPI_CLIENT_DATA_SCOPE_OPTIONS,
  getOpenAPIClient,
  getOpenAPIClientCapabilities,
  getOpenAPIClientCapabilityCatalog,
  isOpenAPISuccess,
  listOpenAPIClientAudits,
  updateOpenAPIClientScopes,
  type OpenAPIClientAudit,
  type OpenAPIClientAuditListParams,
  type OpenAPIClientView,
  type OpenAPICapabilityGroup,
  type OpenAPIScopeView
} from "@/api/gb28181-openapi";
import OpenAPICapabilityWorkbench from "./OpenAPICapabilityWorkbench.vue";

type DetailTab = "capabilities" | "logs";
type AuditResultFilter = "all" | "success" | "failure";

const tabs: { name: DetailTab; label: string }[] = [{ name: "capabilities", label: "能力授权" }];
const route = useRoute();
const router = useRouter();
const userStore = useUserStoreHook();
const permissions = computed(() => userStore.account?.permissions || []);
const permitted = (key: string) =>
  permissions.value.includes("*:*:*") || permissions.value.includes(`gb28181:openapi:client:${key}`);
const canRead = computed(() => permitted("read"));
const canGrant = computed(() => permitted("grant"));
const canAudit = computed(() => permitted("audit"));

const clientId = computed(() => Number(route.params.id));
const activeTab = ref<DetailTab>("capabilities");
if (route.query.tab === "logs") activeTab.value = "logs";
const client = ref<OpenAPIClientView | null>(null);
const scopes = ref<OpenAPIScopeView[]>([]);
const groups = ref<OpenAPICapabilityGroup[]>([]);
const availableScopes = ref<string[]>([]);
const detailReady = ref(false);
const catalogReady = ref(false);
const loading = ref(false);
const saving = ref(false);
const error = ref("");
const scopeError = ref("");
const auditError = ref("");
const audits = ref<OpenAPIClientAudit[]>([]);
const auditLoading = ref(false);
const auditLoaded = ref(false);
const auditPagination = reactive({
  current: 1,
  pageSize: 10,
  total: 0,
  showTotal: true,
  showJumper: true,
  showPageSize: true,
  pageSizeOptions: [10, 20, 50, 100]
});
const dirty = ref(false);
const workbench = ref<InstanceType<typeof OpenAPICapabilityWorkbench> | null>(null);
const auditFilter = ref<AuditResultFilter>("all");
let generation = 0;
let allowLeave = false;

const enabledScopes = computed(() => scopes.value.filter(scope => scope.enabled).map(scope => scope.scope));
const grantedCount = computed(() => enabledScopes.value.filter(scope => availableScopes.value.includes(scope)).length);
const editable = computed(() => canGrant.value && detailReady.value && catalogReady.value && client.value?.status === "active");
function errorMessage(cause: unknown, fallback: string) {
  if (cause && typeof cause === "object" && "response" in cause) {
    const message = (cause as { response?: { data?: { message?: string } } }).response?.data?.message;
    if (message) return message;
  }
  return cause instanceof Error && cause.message ? cause.message : fallback;
}

function httpStatus(cause: unknown) {
  if (!cause || typeof cause !== "object" || !("response" in cause)) return 0;
  return Number((cause as { response?: { status?: number } }).response?.status || 0);
}

function clearSensitiveState() {
  generation++;
  client.value = null;
  scopes.value = [];
  groups.value = [];
  availableScopes.value = [];
  audits.value = [];
  auditPagination.current = 1;
  auditPagination.total = 0;
  auditLoaded.value = false;
  detailReady.value = false;
  catalogReady.value = false;
  dirty.value = false;
}

function handleAccessDenied(cause: unknown) {
  if (![401, 403, 404].includes(httpStatus(cause))) return false;
  clearSensitiveState();
  error.value = "当前账号无权查看此客户端，请返回列表重新选择。";
  return true;
}

async function load() {
  if (!canRead.value || !Number.isSafeInteger(clientId.value) || clientId.value <= 0) return;
  const token = ++generation;
  const id = clientId.value;
  detailReady.value = false;
  catalogReady.value = false;
  loading.value = true;
  error.value = "";
  try {
    const [detailResult, capabilitiesResult, catalogResult] = await Promise.allSettled([
      getOpenAPIClient(id),
      getOpenAPIClientCapabilities(),
      getOpenAPIClientCapabilityCatalog()
    ]);
    if (token !== generation || !canRead.value) return;
    if (detailResult.status === "rejected") throw detailResult.reason;
    const detail = detailResult.value;
    if (!isOpenAPISuccess(detail) || !detail.data?.client || !Array.isArray(detail.data.scopes)) {
      throw new Error(detail.message || "客户端详情加载失败");
    }
    client.value = detail.data.client;
    scopes.value = detail.data.scopes;
    detailReady.value = true;
    if (activeTab.value === "logs") void loadAudits();
    if (capabilitiesResult.status === "rejected") {
      if (!handleAccessDenied(capabilitiesResult.reason)) {
        scopeError.value = errorMessage(capabilitiesResult.reason, "能力目录加载失败");
      }
      return;
    }
    const capabilities = capabilitiesResult.value;
    if (!isOpenAPISuccess(capabilities) || !Array.isArray(capabilities.data)) {
      scopeError.value = capabilities.message || "能力目录加载失败";
      return;
    }
    if (catalogResult.status === "rejected") {
      if (!handleAccessDenied(catalogResult.reason)) {
        scopeError.value = errorMessage(catalogResult.reason, "能力分组目录加载失败");
      }
      return;
    }
    const catalog = catalogResult.value;
    if (!isOpenAPISuccess(catalog) || !Array.isArray(catalog.data?.groups)) {
      scopeError.value = catalog.message || "能力分组目录加载失败";
      return;
    }
    availableScopes.value = capabilities.data;
    groups.value = catalog.data.groups;
    catalogReady.value = true;
    scopeError.value = "";
  } catch (cause) {
    if (token !== generation) return;
    if (!handleAccessDenied(cause)) error.value = errorMessage(cause, "客户端详情加载失败");
  } finally {
    if (token === generation) loading.value = false;
  }
}

async function refreshDetail() {
  if (!client.value || !canRead.value) return false;
  const token = generation;
  const id = client.value.id;
  const result = await getOpenAPIClient(id);
  if (token !== generation || !canRead.value) return false;
  if (!isOpenAPISuccess(result) || !result.data?.client || !Array.isArray(result.data.scopes)) {
    throw new Error(result.message || "客户端详情刷新失败");
  }
  client.value = result.data.client;
  scopes.value = result.data.scopes;
  detailReady.value = true;
  return true;
}

async function saveScopes(nextScopes: string[]) {
  if (!editable.value || !client.value || saving.value) return;
  const id = client.value.id;
  const version = client.value.rowVersion;
  const token = generation;
  saving.value = true;
  scopeError.value = "";
  try {
    const result = await updateOpenAPIClientScopes(id, { rowVersion: version, scopes: nextScopes });
    if (token !== generation || client.value?.id !== id) return;
    if (!isOpenAPISuccess(result)) throw new Error(result.message || "能力授权保存失败");
    await refreshDetail();
    if (token === generation) Message.success("能力授权已更新");
  } catch (cause) {
    if (token !== generation) return;
    if (handleAccessDenied(cause)) return;
    if (httpStatus(cause) === 409) {
      scopeError.value = "授权已被其他管理员更新。已重新加载最新版本，请核对本次变更后再保存。";
      try {
        await refreshDetail();
        await nextTick();
        workbench.value?.setDraft(nextScopes);
      } catch {
        scopeError.value = "授权版本冲突且刷新失败，请稍后重试。";
      }
    } else scopeError.value = errorMessage(cause, "能力授权保存失败");
  } finally {
    if (token === generation) saving.value = false;
  }
}

async function loadAudits() {
  if (!canAudit.value || !client.value || auditLoading.value) return;
  const token = generation;
  const id = client.value.id;
  auditLoading.value = true;
  auditError.value = "";
  try {
    const params: OpenAPIClientAuditListParams = {
      page: auditPagination.current,
      pageSize: auditPagination.pageSize,
      ...(auditFilter.value === "all" ? {} : { result: auditFilter.value })
    };
    const result = await listOpenAPIClientAudits(id, params);
    if (token !== generation || client.value?.id !== id) return;
    if (!isOpenAPISuccess(result)) throw new Error(result.message || "调用记录加载失败");
    audits.value = result.data?.items || [];
    auditPagination.current = result.data?.page || auditPagination.current;
    auditPagination.pageSize = result.data?.pageSize || auditPagination.pageSize;
    auditPagination.total = result.data?.total || 0;
    auditLoaded.value = true;
  } catch (cause) {
    if (token !== generation) return;
    if (!handleAccessDenied(cause)) auditError.value = errorMessage(cause, "调用记录加载失败");
  } finally {
    if (token === generation) auditLoading.value = false;
  }
}

function handleAuditPageChange(page: number) {
  auditPagination.current = page;
  void loadAudits();
}

function handleAuditPageSizeChange(pageSize: number) {
  auditPagination.pageSize = pageSize;
  auditPagination.current = 1;
  void loadAudits();
}

function handleAuditFilterChange(value: string | number) {
  const filter = String(value);
  if (filter !== "all" && filter !== "success" && filter !== "failure") return;
  auditFilter.value = filter;
  auditPagination.current = 1;
  void loadAudits();
}

function activateTab(tab: DetailTab) {
  activeTab.value = tab;
  void router.replace({ query: { ...route.query, tab } });
  if (tab === "logs" && !auditLoaded.value) void loadAudits();
}

function switchTab(tab: DetailTab) {
  if (tab === activeTab.value) return;
  if (!dirty.value) {
    activateTab(tab);
    return;
  }
  Modal.confirm({
    title: "放弃未保存修改",
    content: "能力授权尚未保存，放弃本次修改并切换页面？",
    okText: "放弃修改",
    cancelText: "继续编辑",
    okButtonProps: { status: "warning" },
    onOk: () => activateTab(tab)
  });
}

function backToList() {
  void router.push("/gb28181/openapi-client");
}

function beforeUnload(event: BeforeUnloadEvent) {
  if (!dirty.value) return;
  event.preventDefault();
  event.returnValue = "";
}

onBeforeRouteLeave(() => {
  if (!dirty.value || allowLeave) return true;
  return new Promise<boolean>(resolve => {
    Modal.confirm({
      title: "放弃未保存修改",
      content: "能力授权尚未保存，放弃本次修改并离开页面？",
      okText: "放弃修改",
      cancelText: "留在当前页",
      okButtonProps: { status: "warning" },
      onOk: () => {
        allowLeave = true;
        resolve(true);
      },
      onCancel: () => resolve(false)
    });
  });
});
watch(
  () => route.query.tab,
  value => {
    const tab = tabs.find(item => item.name === value)?.name;
    if (tab && tab !== activeTab.value) switchTab(tab);
  }
);
watch(
  () => [clientId.value, canRead.value] as const,
  () => {
    clearSensitiveState();
    if (canRead.value) void load();
  }
);
watch(
  () => [canGrant.value, canAudit.value],
  ([grant, audit]) => {
    if (!grant) dirty.value = false;
    if (!audit) {
      audits.value = [];
      auditLoaded.value = false;
      auditPagination.total = 0;
    }
  }
);
onMounted(() => {
  if (canRead.value) void load();
  window.addEventListener("beforeunload", beforeUnload);
  if (activeTab.value === "logs") void loadAudits();
});
onBeforeUnmount(() => {
  window.removeEventListener("beforeunload", beforeUnload);
  clearSensitiveState();
});

function scopeLabel(scope: string) {
  return groups.value.flatMap(group => group.capabilities).find(item => item.scope === scope)?.name || scope;
}

function statusLabel(status: OpenAPIClientView["status"]) {
  return status === "active" ? "启用" : status === "disabled" ? "停用" : "已撤销";
}

function dataScopeLabel() {
  return OPENAPI_CLIENT_DATA_SCOPE_OPTIONS.find(option => option.value === client.value?.dataScope)?.label || "-";
}

function formatTime(value?: string | null) {
  if (!value) return "-";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  const pad = (part: number) => String(part).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(
    date.getMinutes()
  )}:${pad(date.getSeconds())}`;
}
</script>

<template>
  <div class="snow-page openapi-detail">
    <div class="snow-inner uvp-page-shell-flat openapi-detail__inner">
      <a-empty v-if="!canRead" description="没有 OpenAPI 客户端查看权限" />
      <a-empty v-else-if="!Number.isSafeInteger(clientId) || clientId <= 0" description="无效的客户端 ID" />
      <template v-else>
        <a-alert v-if="error" type="error" class="openapi-detail__alert" role="alert">
          {{ error }} <a-button size="small" @click="load">重试</a-button>
        </a-alert>
        <a-spin v-if="loading && !client" class="openapi-detail__loading" />
        <template v-if="client && detailReady">
          <a-card class="uvp-system-panel openapi-detail__summary" :bordered="false">
            <div class="openapi-detail__summary-content">
              <a-button class="openapi-detail__summary-back" type="text" @click="backToList">
                <template #icon><ArrowLeft :size="16" /></template>返回客户端列表
              </a-button>
              <div class="openapi-detail__header">
                <div class="openapi-detail__identity">
                  <div class="openapi-detail__title">
                    <h1>{{ client.name }}</h1>
                    <a-tag :color="client.status === 'active' ? 'green' : client.status === 'disabled' ? 'orange' : 'red'">
                      {{ statusLabel(client.status) }}
                    </a-tag>
                  </div>
                  <div class="openapi-detail__meta">
                    <code>{{ client.ak }}</code>
                    <span>{{ dataScopeLabel() }}</span>
                    <span>已授权 {{ grantedCount }} / {{ availableScopes.length }} 项</span>
                  </div>
                </div>
                <a-button :loading="loading" @click="load"
                  ><template #icon><RefreshCw :size="15" /></template>刷新</a-button
                >
              </div>
            </div>
          </a-card>

          <div v-if="activeTab === 'capabilities'" class="openapi-detail__content">
            <a-alert v-if="scopeError" type="error" class="openapi-detail__alert" role="alert">{{ scopeError }}</a-alert>
            <OpenAPICapabilityWorkbench
              v-if="catalogReady"
              ref="workbench"
              :groups="groups"
              :enabled-scopes="enabledScopes"
              :editable="editable"
              :saving="saving"
              @dirty-change="(value: boolean) => (dirty = value)"
              @save="saveScopes"
            />
            <a-card v-else class="uvp-system-panel openapi-detail__panel" :bordered="false">
              <a-empty description="能力目录暂不可用，授权已锁定" />
            </a-card>
          </div>

          <div v-else-if="activeTab === 'logs'" class="openapi-detail__content">
            <template v-if="canAudit">
              <s-layout-search class="openapi-detail__logs-filter-bar">
                <template #fields>
                  <a-select
                    v-model="auditFilter"
                    class="openapi-detail__logs-filter"
                    aria-label="调用结果筛选"
                    @change="handleAuditFilterChange"
                  >
                    <a-option value="all">全部结果</a-option>
                    <a-option value="success">成功</a-option>
                    <a-option value="failure">失败</a-option>
                  </a-select>
                </template>
                <template #actions>
                  <a-button :loading="auditLoading" @click="loadAudits"
                    ><template #icon><RefreshCw :size="15" /></template>刷新</a-button
                  >
                </template>
              </s-layout-search>
              <a-alert v-if="auditError" type="error" class="openapi-detail__alert" role="alert">{{ auditError }}</a-alert>
              <a-table
                class="uvp-data-table"
                :data="audits"
                :loading="auditLoading"
                :pagination="auditPagination"
                row-key="requestId"
                :scroll="{ x: '100%', minWidth: 980 }"
                @page-change="handleAuditPageChange"
                @page-size-change="handleAuditPageSizeChange"
              >
                <template #empty><a-empty :description="auditLoaded ? '暂无调用记录' : '尚未加载调用记录'" /></template>
                <template #columns>
                  <a-table-column title="时间" :width="190"
                    ><template #cell="{ record }"
                      ><time :datetime="record.createdAt">{{ formatTime(record.createdAt) }}</time></template
                    ></a-table-column
                  >
                  <a-table-column title="能力 / 操作" :width="220"
                    ><template #cell="{ record }"
                      ><strong>{{ scopeLabel(record.scope) || "管理操作" }}</strong>
                      <div>
                        <code>{{ record.scope || "-" }}</code>
                      </div></template
                    ></a-table-column
                  >
                  <a-table-column title="资源" :width="165"
                    ><template #cell="{ record }"
                      >{{ record.resourceType || "-" }} · {{ record.resourceId || "-" }}</template
                    ></a-table-column
                  >
                  <a-table-column title="结果" :width="100"
                    ><template #cell="{ record }"
                      ><a-tag :color="record.result === 'success' ? 'green' : 'orange'">{{ record.result }}</a-tag></template
                    ></a-table-column
                  >
                  <a-table-column title="原因" data-index="reasonClass" :width="150" />
                  <a-table-column title="耗时" :width="100"
                    ><template #cell="{ record }">{{ record.latencyMs }} ms</template></a-table-column
                  >
                  <a-table-column title="请求 ID" data-index="requestId" :width="210" />
                </template>
              </a-table>
            </template>
            <a-empty v-else description="没有调用记录查看权限" />
          </div>
        </template>
      </template>
    </div>
  </div>
</template>

<style scoped>
.openapi-detail__inner {
  min-width: 0;
  min-height: 0;
}
.openapi-detail__alert {
  margin-bottom: 14px;
}
.openapi-detail__loading {
  min-height: 240px;
}
.openapi-detail__summary {
  margin-bottom: 18px;
}
.openapi-detail__summary-content {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  gap: 18px;
  align-items: center;
}
.openapi-detail__summary-back {
  flex: none;
  white-space: nowrap;
}
.openapi-detail__header {
  display: flex;
  gap: 18px;
  align-items: center;
  justify-content: space-between;
}
.openapi-detail__identity {
  min-width: 0;
}
.openapi-detail__title,
.openapi-detail__meta {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  align-items: center;
}
.openapi-detail__title h1 {
  margin: 0;
  font-size: 22px;
  line-height: 1.4;
  color: var(--color-text-1);
}
.openapi-detail__meta {
  margin-top: 7px;
  font-size: 12px;
  color: var(--color-text-3);
}
.openapi-detail__meta code {
  overflow-wrap: anywhere;
}
.openapi-detail__tabs {
  min-width: 0;
}
.openapi-detail__panel code {
  overflow-wrap: anywhere;
}
.openapi-detail__logs-filter {
  width: 150px;
}

@media (width <= 760px) {
  .openapi-detail__summary-content {
    grid-template-columns: 1fr;
    gap: 8px;
  }
  .openapi-detail__header {
    flex-direction: column;
    align-items: flex-start;
  }
}
</style>
