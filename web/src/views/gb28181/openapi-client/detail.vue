<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter, onBeforeRouteLeave } from "vue-router";
import { Message, Modal } from "@arco-design/web-vue";
import { ArrowLeft, KeyRound, RefreshCw, RotateCcw, ShieldOff } from "lucide-vue-next";
import { useUserStoreHook } from "@/store/modules/user";
import {
  OPENAPI_CLIENT_DATA_SCOPE_OPTIONS,
  disableOpenAPIClient,
  enableOpenAPIClient,
  getOpenAPIClient,
  getOpenAPIClientCapabilities,
  getOpenAPIClientCapabilityCatalog,
  getOpenAPIClientRevocationStatus,
  isOpenAPISuccess,
  listOpenAPIClientAudits,
  revokeOpenAPIClient,
  rotateOpenAPIClientSecret,
  updateOpenAPIClientScopes,
  type OpenAPIClientAudit,
  type OpenAPIClientAuditListParams,
  type OpenAPIClientView,
  type OpenAPICapabilityGroup,
  type OpenAPIRevocationStatus,
  type OpenAPIScopeView
} from "@/api/gb28181-openapi";
import OpenAPICapabilityWorkbench from "./OpenAPICapabilityWorkbench.vue";
import OpenAPISecretDialog from "./OpenAPISecretDialog.vue";

type DetailTab = "overview" | "capabilities" | "security" | "logs";
type StatusAction = "enable" | "disable" | "revoke";
type AuditResultFilter = "all" | "success" | "failure";

const tabs: { name: DetailTab; label: string }[] = [
  { name: "overview", label: "概览" },
  { name: "capabilities", label: "能力授权" },
  { name: "security", label: "凭证与安全" },
  { name: "logs", label: "调用记录" }
];
const route = useRoute();
const router = useRouter();
const userStore = useUserStoreHook();
const permissions = computed(() => userStore.account?.permissions || []);
const permitted = (key: string) =>
  permissions.value.includes("*:*:*") || permissions.value.includes(`gb28181:openapi:client:${key}`);
const canRead = computed(() => permitted("read"));
const canGrant = computed(() => permitted("grant"));
const canRotate = computed(() => permitted("rotate"));
const canStatus = computed(() => permitted("status"));
const canAudit = computed(() => permitted("audit"));

const clientId = computed(() => Number(route.params.id));
const activeTab = ref<DetailTab>(tabs.some(tab => tab.name === route.query.tab) ? (route.query.tab as DetailTab) : "overview");
const client = ref<OpenAPIClientView | null>(null);
const scopes = ref<OpenAPIScopeView[]>([]);
const groups = ref<OpenAPICapabilityGroup[]>([]);
const availableScopes = ref<string[]>([]);
const detailReady = ref(false);
const catalogReady = ref(false);
const loading = ref(false);
const saving = ref(false);
const actionLoading = ref(false);
const error = ref("");
const scopeError = ref("");
const securityError = ref("");
const auditError = ref("");
const revocationError = ref("");
const audits = ref<OpenAPIClientAudit[]>([]);
const auditLoading = ref(false);
const auditLoaded = ref(false);
const auditPagination = reactive({ current: 1, pageSize: 10, total: 0, showTotal: true, showJumper: true, showPageSize: true });
const revocation = ref<OpenAPIRevocationStatus | null>(null);
const revocationLoading = ref(false);
const dirty = ref(false);
const workbench = ref<InstanceType<typeof OpenAPICapabilityWorkbench> | null>(null);
const secret = ref<{ accessKey: string; secretKey: string; operation: "rotate" } | null>(null);
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
  revocation.value = null;
  secret.value = null;
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
    if (activeTab.value === "security") void refreshRevocation();
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

async function refreshRevocation() {
  if (!canStatus.value || !client.value || revocationLoading.value) return;
  const token = generation;
  const id = client.value.id;
  revocationLoading.value = true;
  revocationError.value = "";
  try {
    const result = await getOpenAPIClientRevocationStatus(id);
    if (token !== generation || client.value?.id !== id) return;
    if (!isOpenAPISuccess(result)) throw new Error(result.message || "清退进度加载失败");
    revocation.value = result.data;
  } catch (cause) {
    if (token !== generation) return;
    if (!handleAccessDenied(cause)) revocationError.value = errorMessage(cause, "清退进度加载失败");
  } finally {
    if (token === generation) revocationLoading.value = false;
  }
}

async function rotateSecret() {
  if (!canRotate.value || !client.value || actionLoading.value) return;
  const token = generation;
  const id = client.value.id;
  const version = client.value.rowVersion;
  actionLoading.value = true;
  securityError.value = "";
  try {
    const result = await rotateOpenAPIClientSecret(id, version);
    if (token !== generation || client.value?.id !== id) return;
    if (!isOpenAPISuccess(result) || !result.data?.secretKey || !result.data.client) {
      throw new Error(result.message || "轮换 SK 失败");
    }
    client.value = result.data.client;
    secret.value = { accessKey: result.data.client.ak, secretKey: result.data.secretKey, operation: "rotate" };
  } catch (cause) {
    if (token !== generation) return;
    if (handleAccessDenied(cause)) return;
    if (httpStatus(cause) === 409) {
      await refreshDetail().catch(() => false);
      securityError.value = "客户端状态已变化，已刷新详情，请重新确认后操作。";
    } else securityError.value = errorMessage(cause, "轮换 SK 失败");
  } finally {
    if (token === generation) actionLoading.value = false;
  }
}

async function changeStatus(action: StatusAction) {
  if (!canStatus.value || !client.value || actionLoading.value) return;
  const token = generation;
  const id = client.value.id;
  const version = client.value.rowVersion;
  actionLoading.value = true;
  securityError.value = "";
  try {
    const request = action === "enable" ? enableOpenAPIClient : action === "disable" ? disableOpenAPIClient : revokeOpenAPIClient;
    const result = await request(id, version);
    if (token !== generation || client.value?.id !== id) return;
    if (!isOpenAPISuccess(result) || !result.data?.client) throw new Error(result.message || "客户端状态变更失败");
    client.value = result.data.client;
    await refreshDetail();
    revocation.value = action === "enable" ? null : { status: result.data.revocationStatus || "pending", pending: 0, closed: 0 };
    Message.success(
      action === "enable"
        ? "客户端已启用"
        : action === "disable"
          ? "客户端已停用，请确认清退进度"
          : "客户端已撤销，请确认清退进度"
    );
  } catch (cause) {
    if (token !== generation) return;
    if (handleAccessDenied(cause)) return;
    if (httpStatus(cause) === 409) {
      await refreshDetail().catch(() => false);
      securityError.value = "客户端状态已变化，已刷新详情，请重新确认后操作。";
    } else securityError.value = errorMessage(cause, "客户端状态变更失败");
  } finally {
    if (token === generation) actionLoading.value = false;
  }
}

function confirmRotate() {
  if (!canRotate.value || !client.value) return;
  Modal.confirm({
    title: "轮换 OpenAPI 客户端 SK",
    content: "旧 SK 将立即失效，新 SK 只展示一次。确认继续吗？",
    okText: "确认轮换",
    cancelText: "取消",
    okButtonProps: { status: "warning" },
    onOk: rotateSecret
  });
}

function confirmStatus(action: StatusAction) {
  if (!canStatus.value || !client.value) return;
  Modal.confirm({
    title: action === "revoke" ? "撤销客户端" : action === "disable" ? "停用客户端" : "启用客户端",
    content:
      action === "revoke"
        ? "撤销不可恢复；认证会立即失败，观看连接的关闭情况需另行确认。"
        : action === "disable"
          ? "停用会立即阻止新请求，观看连接清退需以服务端进度为准。"
          : "启用不会恢复旧的播放授权，请确认客户端仍属于有效部门。",
    okText: action === "revoke" ? "确认撤销" : action === "disable" ? "确认停用" : "确认启用",
    cancelText: "取消",
    okButtonProps: action === "enable" ? undefined : { status: "danger" },
    onOk: () => changeStatus(action)
  });
}

function switchTab(tab: DetailTab) {
  if (tab === activeTab.value) return;
  if (dirty.value && !window.confirm("能力授权尚未保存，放弃本次修改并切换页面？")) return;
  activeTab.value = tab;
  void router.replace({ query: { ...route.query, tab } });
  if (tab === "logs" && !auditLoaded.value) void loadAudits();
  if (tab === "security" && !revocation.value) void refreshRevocation();
}

function handleTabChange(value: string | number) {
  const tab = String(value) as DetailTab;
  if (tabs.some(item => item.name === tab)) switchTab(tab);
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
  const accepted = window.confirm("能力授权尚未保存，放弃本次修改并离开页面？");
  if (accepted) allowLeave = true;
  return accepted;
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
  () => [canGrant.value, canRotate.value, canStatus.value, canAudit.value],
  ([grant, rotate, status, audit]) => {
    if (!grant) dirty.value = false;
    if (!rotate) secret.value = null;
    if (!status) revocation.value = null;
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
  if (activeTab.value === "security") void refreshRevocation();
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

          <a-tabs class="uvp-system-tabs openapi-detail__tabs" :active-key="activeTab" @change="handleTabChange">
            <a-tab-pane key="overview" title="概览">
              <a-card class="uvp-system-panel openapi-detail__panel" :bordered="false">
                <a-descriptions class="uvp-system-description" title="客户端信息" :column="2" bordered>
                  <a-descriptions-item label="客户端名称">{{ client.name }}</a-descriptions-item>
                  <a-descriptions-item label="Access Key"
                    ><code>{{ client.ak }}</code></a-descriptions-item
                  >
                  <a-descriptions-item label="归属部门">部门 #{{ client.ownerDeptId }}</a-descriptions-item>
                  <a-descriptions-item label="数据范围">{{ dataScopeLabel() }}</a-descriptions-item>
                  <a-descriptions-item label="负责人用户">
                    {{ client.responsibleUserId ? `用户 #${client.responsibleUserId}` : "-" }}
                  </a-descriptions-item>
                  <a-descriptions-item label="认证状态">{{ statusLabel(client.status) }}</a-descriptions-item>
                  <a-descriptions-item label="调用限速">
                    {{ client.rateLimit }} / 秒，突发 {{ client.burst }}
                  </a-descriptions-item>
                  <a-descriptions-item label="观看配额">{{ client.viewerQuota }}</a-descriptions-item>
                  <a-descriptions-item label="创建时间">{{ client.createdAt }}</a-descriptions-item>
                  <a-descriptions-item label="更新时间">{{ client.updatedAt }}</a-descriptions-item>
                </a-descriptions>
                <div class="openapi-detail__overview-actions">
                  <a-button type="primary" @click="switchTab('capabilities')">查看能力授权</a-button>
                  <a-button v-if="canAudit" @click="switchTab('logs')">查看调用记录</a-button>
                </div>
              </a-card>
            </a-tab-pane>

            <a-tab-pane key="capabilities" title="能力授权">
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
            </a-tab-pane>

            <a-tab-pane key="security" title="凭证与安全">
              <a-alert v-if="securityError" type="error" class="openapi-detail__alert" role="alert">{{ securityError }}</a-alert>
              <a-card class="uvp-system-panel openapi-detail__panel" :bordered="false" title="凭证与安全">
                <template #extra>
                  <div class="openapi-detail__security-actions">
                    <a-button v-if="canRotate && client.status !== 'revoked'" :loading="actionLoading" @click="confirmRotate">
                      <template #icon><KeyRound :size="15" /></template>轮换 SK
                    </a-button>
                    <a-button
                      v-if="client.status === 'disabled' && canStatus"
                      :loading="actionLoading"
                      @click="confirmStatus('enable')"
                    >
                      启用
                    </a-button>
                    <a-button
                      v-if="client.status === 'active' && canStatus"
                      status="warning"
                      :loading="actionLoading"
                      @click="confirmStatus('disable')"
                    >
                      <template #icon><ShieldOff :size="15" /></template>停用
                    </a-button>
                    <a-button
                      v-if="client.status !== 'revoked' && canStatus"
                      status="danger"
                      :loading="actionLoading"
                      @click="confirmStatus('revoke')"
                      >撤销</a-button
                    >
                  </div>
                </template>
                <a-descriptions class="uvp-system-description" :column="1" bordered>
                  <a-descriptions-item label="Access Key"
                    ><code>{{ client.ak }}</code></a-descriptions-item
                  >
                  <a-descriptions-item label="Secret Key">不可找回，轮换后旧密钥立即失效</a-descriptions-item>
                  <a-descriptions-item label="认证状态">{{ statusLabel(client.status) }}</a-descriptions-item>
                  <a-descriptions-item v-if="canStatus" label="观看连接清退">
                    <span v-if="revocation">
                      {{ revocation.status === "closed" ? "已确认清退" : "清退中或状态待确认" }}，待清退
                      {{ revocation.pending }}，已关闭 {{ revocation.closed }}
                    </span>
                    <span v-else>尚未查询</span>
                    <a-button type="text" size="small" :loading="revocationLoading" @click="refreshRevocation">
                      <template #icon><RotateCcw :size="15" /></template>刷新进度
                    </a-button>
                    <span v-if="revocationError" class="openapi-detail__error">{{ revocationError }}</span>
                  </a-descriptions-item>
                </a-descriptions>
              </a-card>
            </a-tab-pane>

            <a-tab-pane key="logs" title="调用记录">
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
            </a-tab-pane>
          </a-tabs>
        </template>
      </template>
    </div>

    <OpenAPISecretDialog
      :visible="!!secret"
      :access-key="secret?.accessKey || ''"
      :secret-key="secret?.secretKey || ''"
      operation="rotate"
      @close="secret = null"
    />
  </div>
</template>

<style scoped>
.openapi-detail__inner {
  min-width: 0;
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
.openapi-detail__overview-actions,
.openapi-detail__security-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
}
.openapi-detail__overview-actions {
  margin-top: 20px;
}
.openapi-detail__error {
  margin-left: 10px;
  color: rgb(var(--danger-6));
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
  .openapi-detail__security-actions {
    justify-content: flex-start;
  }
}
</style>
