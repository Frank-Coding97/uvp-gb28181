<script setup lang="ts">
import { computed, reactive, ref, watch } from "vue";
import {
  OPENAPI_CLIENT_DATA_SCOPE_OPTIONS,
  OPENAPI_CLIENT_DEFAULT_DATA_SCOPE,
  type OpenAPIClientDataScope,
  type OpenAPIClientCreateInput,
  type OpenAPIClientView,
  type OpenAPIManagedDepartment,
  type OpenAPIClientAudit,
  type OpenAPIRevocationStatus,
  type OpenAPIScopeView
} from "@/api/gb28181-openapi";

const props = defineProps<{
  visible: boolean;
  mode: "create" | "detail";
  client: OpenAPIClientView | null;
  scopes: OpenAPIScopeView[];
  capabilities: string[];
  capabilityGroups?: {
    code: string;
    name: string;
    capabilities: {
      scope: string;
      name: string;
      method: string;
      externalPath: string;
      resourceType: string;
      risk: string;
      idempotencyRequired: boolean;
    }[];
  }[];
  capabilitiesReady: boolean;
  detailReady: boolean;
  detailClientId: number | null;
  detailRowVersion: number | null;
  departments: OpenAPIManagedDepartment[];
  submitting: boolean;
  error: string;
  canGrant: boolean;
  canStatus: boolean;
  canAudit: boolean;
  revocationStatus: OpenAPIRevocationStatus | null;
  revocationError: string;
  revocationLoading: boolean;
  auditItems: OpenAPIClientAudit[];
  auditLoading: boolean;
}>();

const emit = defineEmits<{
  close: [];
  create: [input: OpenAPIClientCreateInput];
  saveScopes: [scopes: string[]];
  refreshRevocation: [];
  loadAudits: [];
}>();

const form = reactive({
  name: "",
  ownerDeptId: undefined as number | undefined,
  dataScope: OPENAPI_CLIENT_DEFAULT_DATA_SCOPE as OpenAPIClientDataScope,
  responsibleUserId: ""
});
const selectedScopes = ref<string[]>([]);
const touched = reactive({ name: false, ownerDeptId: false });
const dataScopeHint = computed(
  () => OPENAPI_CLIENT_DATA_SCOPE_OPTIONS.find(option => option.value === form.dataScope)?.description || ""
);

const title = computed(() => (props.mode === "create" ? "新建 OpenAPI 客户端" : "OpenAPI 客户端详情"));
const canSubmit = computed(() => {
  if (props.mode === "create") return form.name.trim().length > 0 && !!form.ownerDeptId && !props.submitting;
  return (
    props.canGrant &&
    !!props.client &&
    props.capabilitiesReady &&
    props.detailReady &&
    props.client.id === props.detailClientId &&
    props.client.rowVersion === props.detailRowVersion &&
    !props.submitting
  );
});

const detailBindingReady = computed(
  () =>
    props.capabilitiesReady &&
    props.detailReady &&
    !!props.client &&
    props.client.id === props.detailClientId &&
    props.client.rowVersion === props.detailRowVersion
);

const scopeLabels: Record<string, string> = {
  "device:list": "设备列表",
  "device:detail": "设备详情",
  "device:status": "设备状态",
  "channel:list": "通道列表",
  "channel:detail": "通道详情",
  "channel:status": "通道状态",
  "play:live:apply": "申请实时播放"
};

function scopeLabel(scope: string) {
  return scopeLabels[scope] || scope;
}

function dataScopeLabel(scope: OpenAPIClientDataScope) {
  return OPENAPI_CLIENT_DATA_SCOPE_OPTIONS.find(option => option.value === scope)?.label || `数据范围 #${scope}`;
}

function resetForm() {
  form.name = "";
  form.ownerDeptId = undefined;
  form.dataScope = OPENAPI_CLIENT_DEFAULT_DATA_SCOPE;
  form.responsibleUserId = "";
  touched.name = false;
  touched.ownerDeptId = false;
  selectedScopes.value = props.scopes.filter(item => item.enabled).map(item => item.scope);
  if (props.client) {
    form.name = props.client.name;
    form.ownerDeptId = props.client.ownerDeptId;
    form.responsibleUserId = props.client.responsibleUserId ? String(props.client.responsibleUserId) : "";
  }
}

watch(
  () => props.visible,
  visible => {
    if (visible) resetForm();
  }
);

watch(
  () => props.scopes,
  () => {
    if (props.visible && props.mode === "detail")
      selectedScopes.value = props.scopes.filter(item => item.enabled).map(item => item.scope);
  },
  { deep: true }
);

function submit() {
  touched.name = true;
  touched.ownerDeptId = true;
  if (!canSubmit.value) return;
  if (props.mode === "create") {
    emit("create", {
      name: form.name.trim(),
      ownerDeptId: form.ownerDeptId!,
      dataScope: form.dataScope,
      ...(Number(form.responsibleUserId) > 0 ? { responsibleUserId: Number(form.responsibleUserId) } : {})
    });
    return;
  }
  emit("saveScopes", [...selectedScopes.value]);
}

defineExpose({ form, selectedScopes, resetForm, submit, canSubmit });
</script>

<template>
  <a-drawer
    :visible="props.visible"
    :title="title"
    :width="520"
    :style="{ width: 'min(520px, 100vw)' }"
    placement="right"
    class="uvp-system-drawer openapi-client-detail-drawer"
    body-class="uvp-system-dialog__body"
    :mask-style="{ backgroundColor: 'rgba(15, 23, 42, 0.32)' }"
    unmount-on-close
    @cancel="emit('close')"
  >
    <a-alert v-if="props.error" type="error" class="openapi-client-drawer__error" role="alert">{{ props.error }}</a-alert>

    <template v-if="props.mode === 'create'">
      <a-form layout="vertical" class="openapi-client-drawer__form">
        <a-form-item
          label="客户端名称"
          required
          :validate-status="touched.name && !form.name.trim() ? 'error' : ''"
          :help="touched.name && !form.name.trim() ? '请输入客户端名称' : ''"
        >
          <a-input v-model="form.name" allow-clear maxlength="100" placeholder="例如：现场集成服务" @blur="touched.name = true" />
        </a-form-item>
        <a-form-item
          label="归属部门"
          required
          :validate-status="touched.ownerDeptId && !form.ownerDeptId ? 'error' : ''"
          :help="touched.ownerDeptId && !form.ownerDeptId ? '请选择归属部门' : ''"
        >
          <a-tree-select
            v-model="form.ownerDeptId"
            :data="props.departments"
            :field-names="{ key: 'id', title: 'name', children: 'children' }"
            allow-clear
            allow-search
            placeholder="请选择当前可管理部门"
            @blur="touched.ownerDeptId = true"
          />
        </a-form-item>
        <a-form-item label="数据范围" required>
          <a-select v-model="form.dataScope" placeholder="请选择数据范围">
            <a-option v-for="option in OPENAPI_CLIENT_DATA_SCOPE_OPTIONS" :key="option.value" :value="option.value">
              {{ option.label }}
            </a-option>
          </a-select>
          <div class="openapi-client-drawer__scope-hint">{{ dataScopeHint }}</div>
        </a-form-item>
        <a-form-item label="负责人用户 ID">
          <a-input v-model="form.responsibleUserId" inputmode="numeric" allow-clear placeholder="可选，填写用户 ID" />
        </a-form-item>
      </a-form>
      <a-alert type="info">客户端按上方数据范围访问设备，不包含共享可见设备；创建成功后能力默认为空。</a-alert>
    </template>

    <template v-else>
      <a-descriptions v-if="props.client" :column="1" bordered class="openapi-client-drawer__summary">
        <a-descriptions-item label="名称">{{ props.client.name }}</a-descriptions-item>
        <a-descriptions-item label="AK"
          ><code>{{ props.client.ak }}</code></a-descriptions-item
        >
        <a-descriptions-item label="归属部门">{{
          props.departments.find(item => item.id === props.client?.ownerDeptId)?.name || `部门 #${props.client.ownerDeptId}`
        }}</a-descriptions-item>
        <a-descriptions-item label="数据范围">{{ dataScopeLabel(props.client.dataScope) }}</a-descriptions-item>
        <a-descriptions-item label="认证状态">
          <a-tag :color="props.client.status === 'active' ? 'green' : props.client.status === 'disabled' ? 'orange' : 'red'">
            {{ props.client.status === "active" ? "启用" : props.client.status === "disabled" ? "停用" : "已撤销" }}
          </a-tag>
        </a-descriptions-item>
        <a-descriptions-item label="rowVersion"
          ><code>{{ props.client.rowVersion }}</code></a-descriptions-item
        >
      </a-descriptions>
      <a-divider>已授权能力</a-divider>
      <p class="openapi-client-drawer__scope-note">能力授权与后台按钮权限独立；这里只提交外部 API scope。</p>
      <a-alert v-if="props.canGrant && !detailBindingReady" type="warning" class="openapi-client-drawer__warning" role="alert">
        详情、能力目录或 rowVersion 尚未确认，暂不能保存能力配置。
      </a-alert>
      <a-checkbox-group
        v-model="selectedScopes"
        :disabled="!props.canGrant || !canSubmit || props.submitting"
        class="openapi-client-drawer__scopes"
      >
        <template v-if="props.capabilityGroups?.length">
          <div v-for="group in props.capabilityGroups" :key="group.code" class="openapi-client-drawer__capability-group">
            <div class="openapi-client-drawer__capability-group-title">{{ group.name }}</div>
            <a-checkbox v-for="capability in group.capabilities" :key="capability.scope" :value="capability.scope">
              {{ capability.name }}（{{ capability.scope }}）
            </a-checkbox>
          </div>
        </template>
        <template v-else>
          <a-checkbox v-for="scope in props.capabilities" :key="scope" :value="scope"
            >{{ scopeLabel(scope) }}（{{ scope }}）</a-checkbox
          >
        </template>
      </a-checkbox-group>
      <a-empty v-if="!props.capabilitiesReady" description="能力目录暂不可用" />
      <a-empty v-else-if="!props.capabilities.length" description="当前暂无可授权能力" />

      <a-divider>观看连接清退</a-divider>
      <div class="openapi-client-drawer__revocation">
        <div>
          <span class="openapi-client-drawer__status-label">认证状态</span>
          <a-tag :color="props.client?.status === 'active' ? 'green' : props.client?.status === 'disabled' ? 'orange' : 'red'">
            {{ props.client?.status === "active" ? "已启用" : props.client?.status === "disabled" ? "已停用" : "已撤销" }}
          </a-tag>
        </div>
        <div>
          <span class="openapi-client-drawer__status-label">清退状态</span>
          <a-tag v-if="props.revocationStatus" :color="props.revocationStatus.status === 'closed' ? 'green' : 'orange'">
            {{
              props.revocationStatus.status === "closed"
                ? "已确认清退"
                : props.revocationStatus.status === "pending"
                  ? "清退中"
                  : "状态未知"
            }}
          </a-tag>
          <span v-else class="openapi-client-drawer__muted">尚未查询</span>
          <span v-if="props.revocationStatus" class="openapi-client-drawer__counts"
            >待清退 {{ props.revocationStatus.pending }}，已关闭 {{ props.revocationStatus.closed }}</span
          >
        </div>
        <div v-if="props.revocationError" class="openapi-client-drawer__warning" role="alert">{{ props.revocationError }}</div>
        <a-button v-if="props.canStatus" size="small" :loading="props.revocationLoading" @click="emit('refreshRevocation')"
          >刷新清退进度</a-button
        >
      </div>

      <a-divider>调用审计</a-divider>
      <a-button v-if="props.canAudit" size="small" :loading="props.auditLoading" @click="emit('loadAudits')"
        >加载最近审计</a-button
      >
      <a-empty
        v-if="props.canAudit && !props.auditLoading && props.auditItems.length === 0"
        description="暂无审计记录或尚未加载"
      />
      <div v-if="props.auditItems.length" class="openapi-client-drawer__audits">
        <div
          v-for="item in props.auditItems"
          :key="`${item.requestId}-${item.createdAt}`"
          class="openapi-client-drawer__audit-row"
        >
          <span>{{ item.createdAt }}</span>
          <span>{{ item.scope || "管理操作" }}</span>
          <a-tag :color="item.result === 'success' ? 'green' : 'orange'">{{ item.result }}</a-tag>
          <span>{{ item.reasonClass || "-" }}</span>
        </div>
      </div>
    </template>

    <template #footer>
      <a-button @click="emit('close')">关闭</a-button>
      <a-button
        v-if="props.mode === 'create' || props.canGrant"
        type="primary"
        :loading="props.submitting"
        :disabled="!canSubmit"
        @click="submit"
      >
        {{ props.mode === "create" ? "创建并显示一次性 SK" : "保存能力" }}
      </a-button>
    </template>
  </a-drawer>
</template>

<style scoped>
.openapi-client-drawer__error {
  margin-bottom: 16px;
}

.openapi-client-drawer__form {
  margin-bottom: 16px;
}

.openapi-client-drawer__summary {
  margin-bottom: 18px;
}

.openapi-client-drawer__scope-note {
  margin: -6px 0 14px;
  font-size: 12px;
  color: var(--color-text-3);
}

.openapi-client-drawer__scope-hint {
  margin-top: 6px;
  font-size: 12px;
  line-height: 1.5;
  color: var(--color-text-3);
}

.openapi-client-drawer__scopes {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.openapi-client-drawer__revocation {
  display: flex;
  flex-direction: column;
  gap: 10px;
  font-size: 13px;
  color: var(--color-text-2);
}

.openapi-client-drawer__status-label {
  display: inline-block;
  min-width: 76px;
  color: var(--color-text-3);
}

.openapi-client-drawer__counts,
.openapi-client-drawer__muted {
  margin-left: 8px;
  color: var(--color-text-3);
}

.openapi-client-drawer__warning {
  font-size: 12px;
  color: rgb(var(--warning-6));
}

.openapi-client-drawer__audits {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-top: 12px;
}

.openapi-client-drawer__audit-row {
  display: grid;
  grid-template-columns: 1.3fr 1fr auto 1fr;
  gap: 8px;
  align-items: center;
  padding: 7px 0;
  font-size: 12px;
  color: var(--color-text-2);
  border-bottom: 1px solid var(--color-border-2);
}
</style>
