<script setup lang="ts">
import { computed, reactive, watch } from "vue";
import {
  OPENAPI_CLIENT_DATA_SCOPE_OPTIONS,
  OPENAPI_CLIENT_DEFAULT_DATA_SCOPE,
  type OpenAPIClientCreateInput,
  type OpenAPIClientDataScope,
  type OpenAPIManagedDepartment
} from "@/api/gb28181-openapi";

const props = defineProps<{
  visible: boolean;
  departments: OpenAPIManagedDepartment[];
  submitting: boolean;
  error: string;
}>();

const emit = defineEmits<{
  close: [];
  create: [input: OpenAPIClientCreateInput];
}>();

const form = reactive({
  name: "",
  ownerDeptId: undefined as number | undefined,
  dataScope: OPENAPI_CLIENT_DEFAULT_DATA_SCOPE as OpenAPIClientDataScope,
  rateLimit: 10,
  burst: 20,
  viewerQuota: 10,
  responsibleOrgName: "",
  responsibleName: "",
  responsibleContact: ""
});
const touched = reactive({ name: false, ownerDeptId: false });
const dataScopeHint = computed(
  () => OPENAPI_CLIENT_DATA_SCOPE_OPTIONS.find(option => option.value === form.dataScope)?.description || ""
);
const departmentTree = computed(() => {
  const nodes = props.departments.map(item => ({ ...item, children: [] as OpenAPIManagedDepartment[] }));
  const byId = new Map(nodes.map(item => [item.id, item]));
  const roots: OpenAPIManagedDepartment[] = [];
  for (const node of nodes) {
    const parent = node.parentId ? byId.get(node.parentId) : undefined;
    if (parent) parent.children?.push(node);
    else roots.push(node);
  }
  return roots;
});

function resetForm() {
  form.name = "";
  form.ownerDeptId = undefined;
  form.dataScope = OPENAPI_CLIENT_DEFAULT_DATA_SCOPE;
  form.rateLimit = 10;
  form.burst = 20;
  form.viewerQuota = 10;
  form.responsibleOrgName = "";
  form.responsibleName = "";
  form.responsibleContact = "";
  touched.name = false;
  touched.ownerDeptId = false;
}

watch(
  () => props.visible,
  visible => {
    if (visible) resetForm();
  }
);

function submit() {
  touched.name = true;
  touched.ownerDeptId = true;
  if (!form.name.trim() || !form.ownerDeptId || props.submitting) return;
  emit("create", {
    name: form.name.trim(),
    ownerDeptId: form.ownerDeptId,
    dataScope: form.dataScope,
    rateLimit: form.rateLimit,
    burst: form.burst,
    viewerQuota: form.viewerQuota,
    ...(form.responsibleOrgName.trim() ? { responsibleOrgName: form.responsibleOrgName.trim() } : {}),
    ...(form.responsibleName.trim() ? { responsibleName: form.responsibleName.trim() } : {}),
    ...(form.responsibleContact.trim() ? { responsibleContact: form.responsibleContact.trim() } : {})
  });
}

defineExpose({ form, resetForm, submit });
</script>

<template>
  <a-modal
    :visible="props.visible"
    title="新建 OpenAPI 客户端"
    width="560px"
    modal-class="uvp-system-dialog openapi-client-create-dialog"
    :mask-closable="false"
    :ok-loading="props.submitting"
    :ok-button-props="{ disabled: !form.name.trim() || !form.ownerDeptId }"
    ok-text="创建并显示一次性 SK"
    cancel-text="取消"
    unmount-on-close
    @cancel="emit('close')"
    @ok="submit"
  >
    <a-alert v-if="props.error" type="error" class="openapi-client-drawer__error" role="alert">{{ props.error }}</a-alert>
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
          :data="departmentTree"
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
        <div class="openapi-client-create-dialog__scope-hint">{{ dataScopeHint }}</div>
      </a-form-item>
      <div class="openapi-client-create-dialog__section-title">访问限制</div>
      <div class="openapi-client-create-dialog__limits-grid">
        <a-form-item label="调用速度">
          <div class="openapi-client-create-dialog__input-unit">
            <a-input-number v-model="form.rateLimit" :min="1" :max="10000" :precision="0" hide-button />
            <span class="openapi-client-create-dialog__unit">次 / 秒</span>
          </div>
        </a-form-item>
        <a-form-item label="突发容量">
          <div class="openapi-client-create-dialog__input-unit">
            <a-input-number v-model="form.burst" :min="1" :max="10000" :precision="0" hide-button />
            <span class="openapi-client-create-dialog__unit">次</span>
          </div>
        </a-form-item>
        <a-form-item label="观看配额">
          <div class="openapi-client-create-dialog__input-unit">
            <a-input-number v-model="form.viewerQuota" :min="1" :max="10000" :precision="0" hide-button />
            <span class="openapi-client-create-dialog__unit">路并发</span>
          </div>
        </a-form-item>
      </div>
      <a-form-item label="组织 / 公司名称">
        <a-input v-model="form.responsibleOrgName" allow-clear maxlength="200" placeholder="可选，例如：某某科技有限公司" />
      </a-form-item>
      <a-form-item label="负责人姓名">
        <a-input v-model="form.responsibleName" allow-clear maxlength="100" placeholder="可选，填写对接负责人姓名" />
      </a-form-item>
      <a-form-item label="负责人联系方式">
        <a-input v-model="form.responsibleContact" allow-clear maxlength="100" placeholder="可选，填写手机号、座机或邮箱" />
      </a-form-item>
    </a-form>
    <a-alert type="info">客户端按上方数据范围访问设备，不包含共享可见设备；创建成功后能力默认为空。</a-alert>
  </a-modal>
</template>

<style scoped>
.openapi-client-create-dialog__scope-hint {
  margin-top: 6px;
  font-size: 12px;
  line-height: 1.5;
  color: var(--color-text-3);
}

.openapi-client-create-dialog__section-title {
  margin: 4px 0 12px;
  font-weight: 600;
  color: var(--color-text-1);
}

.openapi-client-create-dialog__limits-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
}

.openapi-client-create-dialog__unit {
  flex: 0 0 42px;
  width: 42px;
  font-size: 12px;
  color: var(--color-text-3);
  white-space: nowrap;
}

.openapi-client-create-dialog__input-unit {
  display: flex;
  gap: 6px;
  align-items: center;
  min-width: 0;
}

.openapi-client-create-dialog__input-unit :deep(.arco-input-number) {
  flex: 1 1 auto;
  width: auto;
  min-width: 0;
}

@media (width <= 600px) {
  .openapi-client-create-dialog__limits-grid {
    grid-template-columns: 1fr;
  }
}
</style>
