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
  responsibleUserId: ""
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
  form.responsibleUserId = "";
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
    ...(Number(form.responsibleUserId) > 0 ? { responsibleUserId: Number(form.responsibleUserId) } : {})
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
      <a-form-item label="负责人用户 ID">
        <a-input v-model="form.responsibleUserId" inputmode="numeric" allow-clear placeholder="可选，填写用户 ID" />
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
</style>
