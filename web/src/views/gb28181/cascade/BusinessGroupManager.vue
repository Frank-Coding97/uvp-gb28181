<template>
  <a-modal
    :visible="visible"
    :title="`业务分组 · ${platformName}`"
    width="min(1040px, calc(100vw - 24px))"
    modal-class="uvp-system-dialog cascade-business-group-dialog"
    :mask-closable="!saving"
    :closable="!saving"
    :esc-to-close="!saving"
    @cancel="close"
  >
    <a-spin :loading="loading" class="business-group-manager">
      <a-alert v-if="!canWrite" type="info" class="business-group-readonly">
        当前账号只有查看权限，业务分组为只读。需要“级联共享”权限才能修改。
      </a-alert>
      <a-alert v-if="loadError" type="error" class="business-group-alert">
        {{ loadError }}
        <a-button size="mini" data-action="retry-load" @click="load">重试</a-button>
      </a-alert>

      <template v-else-if="snapshot">
        <section v-if="!nodes.length && !creatingRoot" class="business-group-empty">
          <strong>尚未配置业务分组</strong>
          <p>先显式创建一个业务根，再添加虚拟组织并挂载当前上级已共享的通道。</p>
          <a-button type="primary" data-action="create-root" :disabled="!canWrite" @click="createRoot">创建业务根</a-button>
        </section>

        <div v-else class="business-group-workspace">
          <aside class="business-group-tree-panel">
            <div class="business-group-panel-heading">
              <strong>目录结构</strong>
              <a-button
                size="mini"
                data-action="add-organization"
                :disabled="!canWrite || !nodes.length"
                @click="addOrganization"
              >
                添加组织
              </a-button>
            </div>
            <div class="business-group-tree" role="tree" aria-label="业务分组目录">
              <button
                v-for="row in treeRows"
                :key="row.node.code"
                type="button"
                role="treeitem"
                class="business-group-tree-row"
                :class="{ 'is-selected': selectedNodeCode === row.node.code }"
                :style="{ '--tree-depth': row.depth }"
                :data-code="row.node.code"
                @click="selectedNodeCode = row.node.code"
              >
                <span class="business-group-tree-kind">{{ row.node.kind === "group" ? "根" : "组织" }}</span>
                <span class="business-group-tree-name">{{ row.node.name || "未命名节点" }}</span>
                <span class="business-group-tree-count">{{ bindingCount(row.node.code) }}</span>
              </button>
            </div>
          </aside>

          <section v-if="selectedNode" class="business-group-detail">
            <div class="business-group-panel-heading">
              <strong>{{ selectedNode.kind === "group" ? "业务根" : "虚拟组织" }}配置</strong>
              <a-button
                type="text"
                status="danger"
                data-action="delete-node"
                :disabled="!canWrite || nodeHasContents(selectedNode.code)"
                @click="deleteNode(selectedNode.code)"
              >
                删除节点
              </a-button>
            </div>
            <div class="business-group-fields">
              <label>
                <span>节点名称 <small>必填，最多 128 个字符</small></span>
                <a-input
                  aria-label="节点名称"
                  :model-value="selectedNode.name"
                  :maxlength="128"
                  :disabled="!canWrite"
                  show-word-limit
                  @update:model-value="updateNode(selectedNode.code, 'name', $event)"
                />
              </label>
              <label>
                <span>
                  节点国标编号
                  <small>
                    第 11–13 位类型码 {{ selectedNode.kind === "group" ? "215 业务分组" : "216 虚拟组织" }}，固定 20 位（{{
                      selectedNode.code.length
                    }}/20）
                  </small>
                </span>
                <a-input
                  aria-label="节点国标编号"
                  :model-value="selectedNode.code"
                  :readonly="!creatingRoot || selectedNode.kind !== 'group' || nodeHasContents(selectedNode.code)"
                  :disabled="!canWrite"
                  maxlength="20"
                  inputmode="numeric"
                  @update:model-value="updateNode(selectedNode.code, 'code', $event)"
                />
              </label>
              <label v-if="selectedNode.kind === 'organization'">
                <span>父节点</span>
                <a-select
                  aria-label="父节点"
                  :model-value="selectedNode.parentCode"
                  :disabled="!canWrite"
                  @update:model-value="updateNode(selectedNode.code, 'parentCode', String($event))"
                >
                  <a-option v-for="parent in allowedParents(selectedNode)" :key="parent.code" :value="parent.code">
                    {{ parent.name }}
                  </a-option>
                </a-select>
              </label>
            </div>

            <div class="business-group-bindings">
              <div class="business-group-panel-heading">
                <strong>已挂载通道</strong>
                <span>当前上级已共享的通道</span>
              </div>
              <div v-if="selectedNode.kind === 'organization' && selectedBindings.length" class="business-group-binding-list">
                <div v-for="binding in selectedBindings" :key="binding.sourceChannelId" class="business-group-binding-row">
                  <span>{{ channelName(binding.sourceChannelId) }}</span>
                  <a-button
                    size="mini"
                    data-action="remove-binding"
                    :disabled="!canWrite"
                    @click="removeBinding(binding.sourceChannelId)"
                  >
                    移除挂载
                  </a-button>
                </div>
              </div>
              <a-empty v-else :description="selectedNode.kind === 'group' ? '业务根下不能直接挂载通道' : '该组织尚未挂载通道'" />

              <div v-if="selectedNode.kind === 'organization'" class="business-group-attach">
                <a-select
                  v-model="attachChannelId"
                  aria-label="已共享通道"
                  placeholder="选择当前上级已共享通道"
                  :disabled="!canWrite"
                >
                  <a-option v-for="channel in availableChannels" :key="channel.sourceChannelId" :value="channel.sourceChannelId">
                    {{ channel.name || channel.publishedChannelId }}
                  </a-option>
                </a-select>
                <a-select v-model="attachNodeCode" aria-label="业务归属节点" placeholder="选择虚拟组织" :disabled="!canWrite">
                  <a-option v-for="node in organizationNodes" :key="node.code" :value="node.code">{{ node.name }}</a-option>
                </a-select>
                <a-button
                  data-action="add-binding"
                  :disabled="!canWrite || !attachChannelId || !attachNodeCode"
                  @click="addBinding"
                >
                  挂载 / 移动
                </a-button>
              </div>
              <p class="business-group-note">仅移除业务挂载，不会取消共享；未共享通道不会出现在此处。</p>
            </div>
          </section>
        </div>
      </template>
    </a-spin>

    <a-alert v-if="validationError" type="error" class="business-group-alert">{{ validationError }}</a-alert>
    <a-alert v-if="saveError" type="error" class="business-group-alert">{{ saveError }}</a-alert>
    <template #footer>
      <div class="business-group-footer">
        <span v-if="snapshot">修订号 {{ snapshot.revision }} · 保存只更新业务分组配置</span>
        <div>
          <a-button :disabled="saving" @click="close">关闭</a-button>
          <a-button
            type="primary"
            data-action="save"
            :loading="saving"
            :disabled="!canWrite || !snapshot || loading || Boolean(loadError)"
            @click="save"
          >
            保存业务分组
          </a-button>
        </div>
      </div>
    </template>
  </a-modal>
</template>

<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import {
  getCascadeBusinessGroup,
  replaceCascadeBusinessGroup,
  type BusinessBinding,
  type BusinessNode,
  type CascadeBusinessGroup
} from "@/api/gb28181";
import { businessNodeErrors, createBusinessNodeCode, descendantCodes } from "./businessGroupState";

const props = defineProps<{
  visible: boolean;
  platformId: number;
  platformName: string;
  localDeviceId: string;
  canWrite: boolean;
}>();
const emit = defineEmits<{ "update:visible": [value: boolean] }>();

const loading = ref(false);
const saving = ref(false);
const loadError = ref("");
const saveError = ref("");
const validationError = ref("");
const snapshot = ref<CascadeBusinessGroup | null>(null);
const nodes = ref<BusinessNode[]>([]);
const bindings = ref<BusinessBinding[]>([]);
const channels = ref<CascadeBusinessGroup["channels"]>([]);
const selectedNodeCode = ref("");
const attachChannelId = ref<number | "">("");
const attachNodeCode = ref("");
const creatingRoot = ref(false);
let loadSequence = 0;

const selectedNode = computed(() => nodes.value.find(node => node.code === selectedNodeCode.value));
const organizationNodes = computed(() => nodes.value.filter(node => node.kind === "organization"));
const selectedBindings = computed(() => bindings.value.filter(binding => binding.nodeCode === selectedNodeCode.value));
const availableChannels = computed(() => channels.value.filter(channel => channel.active !== false));

const treeRows = computed(() => {
  const result: Array<{ node: BusinessNode; depth: number }> = [];
  const visited = new Set<string>();
  const append = (parentCode: string, depth: number) => {
    nodes.value
      .filter(node => node.parentCode === parentCode && !visited.has(node.code))
      .sort((left, right) => left.sort - right.sort || left.code.localeCompare(right.code))
      .forEach(node => {
        visited.add(node.code);
        result.push({ node, depth });
        append(node.code, depth + 1);
      });
  };
  append("", 0);
  nodes.value
    .filter(node => !visited.has(node.code))
    .forEach(node => {
      visited.add(node.code);
      result.push({ node, depth: 0 });
      append(node.code, 1);
    });
  return result;
});

watch(
  () => [props.visible, props.platformId] as const,
  ([visible]) => {
    if (visible) void load();
    else reset();
  },
  { immediate: true }
);

function unwrap<T>(response: any): T {
  return (response?.data?.data || response?.data || response) as T;
}

function requestErrorMessage(error: any): string {
  return String(error?.response?.data?.message || error?.response?.data?.msg || error?.message || "");
}

function reset() {
  loadSequence++;
  snapshot.value = null;
  nodes.value = [];
  bindings.value = [];
  channels.value = [];
  selectedNodeCode.value = "";
  attachChannelId.value = "";
  attachNodeCode.value = "";
  creatingRoot.value = false;
  loadError.value = "";
  saveError.value = "";
  validationError.value = "";
  loading.value = false;
}

async function load() {
  const requestSequence = ++loadSequence;
  loading.value = true;
  loadError.value = "";
  saveError.value = "";
  validationError.value = "";
  snapshot.value = null;
  try {
    const result = unwrap<CascadeBusinessGroup>(await getCascadeBusinessGroup(props.platformId));
    if (!props.visible || requestSequence !== loadSequence || result.platformId !== props.platformId) return;
    snapshot.value = result;
    nodes.value = result.nodes.map(node => ({ ...node }));
    bindings.value = result.bindings.map(binding => ({ ...binding }));
    channels.value = result.channels.map(channel => ({ ...channel }));
    selectedNodeCode.value = nodes.value[0]?.code || "";
    creatingRoot.value = false;
  } catch (error: any) {
    if (props.visible && requestSequence === loadSequence) {
      const message = requestErrorMessage(error);
      loadError.value = message ? `业务分组读取失败：${message}` : "业务分组读取失败，请重试。";
    }
  } finally {
    if (requestSequence === loadSequence) loading.value = false;
  }
}

function createRoot() {
  if (!props.canWrite || nodes.value.some(node => node.kind === "group")) return;
  const code = createBusinessNodeCode(props.localDeviceId, "group", nodes.value);
  if (!code) {
    validationError.value = "无法根据本平台设备 ID 生成业务根编码，请先检查本平台身份。";
    return;
  }
  const node: BusinessNode = { code, name: "新建业务根", kind: "group", parentCode: "", sort: 0 };
  nodes.value.push(node);
  selectedNodeCode.value = code;
  creatingRoot.value = true;
  validationError.value = "";
}

function addOrganization() {
  if (!props.canWrite || !selectedNode.value) return;
  const code = createBusinessNodeCode(props.localDeviceId, "organization", nodes.value);
  if (!code) {
    validationError.value = "无法生成虚拟组织编码，请检查本平台身份或已用编号。";
    return;
  }
  const parent = selectedNode.value;
  nodes.value.push({
    code,
    name: "新建组织",
    kind: "organization",
    parentCode: parent.code,
    sort: nodes.value.filter(node => node.parentCode === parent.code).length
  });
  selectedNodeCode.value = code;
  validationError.value = "";
}

function updateNode(code: string, field: "name" | "parentCode" | "code", value: string) {
  if (!props.canWrite) return;
  const node = nodes.value.find(item => item.code === code);
  if (node && field === "code" && creatingRoot.value && node.kind === "group") {
    node.code = value;
    selectedNodeCode.value = value;
    bindings.value = bindings.value.map(binding => ({
      ...binding,
      nodeCode: binding.nodeCode === code ? value : binding.nodeCode
    }));
  } else if (node) node[field] = value;
  validationError.value = "";
  saveError.value = "";
}

function allowedParents(node: BusinessNode) {
  const excluded = descendantCodes(nodes.value, node.code);
  return nodes.value.filter(candidate => !excluded.has(candidate.code));
}

function nodeHasContents(code: string) {
  return nodes.value.some(node => node.parentCode === code) || bindings.value.some(binding => binding.nodeCode === code);
}

function deleteNode(code: string) {
  if (!props.canWrite || nodeHasContents(code)) return;
  nodes.value = nodes.value.filter(node => node.code !== code);
  selectedNodeCode.value = nodes.value.find(node => node.kind === "group")?.code || "";
  validationError.value = "";
}

function bindingCount(code: string) {
  return bindings.value.filter(binding => binding.nodeCode === code).length;
}

function channelName(channelId: number) {
  const channel = channels.value.find(item => item.sourceChannelId === channelId);
  return channel?.name || channel?.publishedChannelId || `通道 ${channelId}`;
}

function removeBinding(sourceChannelId: number) {
  if (!props.canWrite) return;
  bindings.value = bindings.value.filter(binding => binding.sourceChannelId !== sourceChannelId);
  validationError.value = "";
}

function addBinding() {
  if (!props.canWrite || !attachChannelId.value || !attachNodeCode.value) return;
  if (!availableChannels.value.some(channel => channel.sourceChannelId === attachChannelId.value)) {
    validationError.value = "只能挂载当前上级已共享的有效通道";
    return;
  }
  bindings.value = bindings.value.filter(binding => binding.sourceChannelId !== attachChannelId.value);
  bindings.value.push({ sourceChannelId: Number(attachChannelId.value), nodeCode: attachNodeCode.value });
  attachChannelId.value = "";
  validationError.value = "";
  saveError.value = "";
}

async function save() {
  if (!snapshot.value || !props.canWrite || saving.value) return;
  const targetPlatformId = props.platformId;
  const requestSequence = loadSequence;
  const errors = businessNodeErrors(nodes.value, bindings.value, channels.value);
  if (errors.length) {
    validationError.value = errors[0];
    return;
  }
  saving.value = true;
  saveError.value = "";
  validationError.value = "";
  try {
    const result = unwrap<CascadeBusinessGroup>(
      await replaceCascadeBusinessGroup(targetPlatformId, {
        nodes: nodes.value.map(node => ({ ...node })),
        bindings: bindings.value.map(binding => ({ ...binding })),
        expectedProjectionRevision: snapshot.value.revision
      })
    );
    if (
      !props.visible ||
      requestSequence !== loadSequence ||
      targetPlatformId !== props.platformId ||
      result.platformId !== targetPlatformId
    )
      return;
    snapshot.value = result;
    nodes.value = result.nodes.map(node => ({ ...node }));
    bindings.value = result.bindings.map(binding => ({ ...binding }));
    channels.value = result.channels.map(channel => ({ ...channel }));
    creatingRoot.value = false;
    Message.success("业务分组配置已保存；目录推送结果请在级联平台列表单独查看。");
  } catch (error: any) {
    if (!props.visible || requestSequence !== loadSequence || targetPlatformId !== props.platformId) return;
    const message = requestErrorMessage(error);
    const isConflict =
      error?.response?.status === 409 || message.toLowerCase().includes("revision conflict") || message.includes("资源状态冲突");
    if (isConflict) {
      saveError.value = "数据已被其他操作修改，当前草稿已保留。请关闭并重新打开业务分组后再合并保存。";
    } else if (message) {
      saveError.value = `${message}（当前草稿已保留）`;
    } else {
      saveError.value = "业务分组保存失败，当前草稿已保留。";
    }
  } finally {
    saving.value = false;
  }
}

function close() {
  if (saving.value) return;
  emit("update:visible", false);
}
</script>

<style scoped>
.business-group-manager {
  min-height: 360px;
}

.business-group-alert,
.business-group-readonly {
  margin-bottom: 14px;
}

.business-group-empty {
  display: grid;
  justify-items: center;
  gap: 12px;
  min-height: 330px;
  align-content: center;
  text-align: center;
}

.business-group-empty p,
.business-group-note {
  color: var(--color-text-3);
  margin: 0;
}

.business-group-workspace {
  display: grid;
  grid-template-columns: minmax(250px, 0.8fr) minmax(440px, 1.4fr);
  gap: 20px;
  min-height: 360px;
}

.business-group-tree-panel,
.business-group-detail {
  min-width: 0;
  border: 1px solid var(--color-border-2);
  border-radius: 8px;
  padding: 14px;
}

.business-group-panel-heading,
.business-group-footer,
.business-group-binding-row,
.business-group-footer > div {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.business-group-panel-heading {
  margin-bottom: 12px;
}

.business-group-panel-heading > span,
.business-group-fields small,
.business-group-footer > span {
  color: var(--color-text-3);
  font-size: 12px;
  font-weight: 400;
}

.business-group-tree {
  display: grid;
  gap: 4px;
  max-height: 430px;
  overflow: auto;
}

.business-group-tree-row {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  min-height: 36px;
  padding: 5px 8px 5px calc(8px + var(--tree-depth) * 18px);
  border: 0;
  border-radius: 5px;
  background: transparent;
  color: var(--color-text-1);
  text-align: left;
  cursor: pointer;
}

.business-group-tree-row:hover,
.business-group-tree-row.is-selected {
  background: var(--color-fill-2);
}

.business-group-tree-kind {
  flex: none;
  color: rgb(var(--primary-6));
  font-size: 11px;
}

.business-group-tree-name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.business-group-tree-count {
  margin-left: auto;
  color: var(--color-text-3);
  font-size: 11px;
}

.business-group-fields {
  display: grid;
  gap: 14px;
}

.business-group-fields label {
  display: grid;
  gap: 6px;
}

.business-group-fields label > span {
  display: flex;
  justify-content: space-between;
  gap: 8px;
}

.business-group-bindings {
  margin-top: 20px;
  padding-top: 16px;
  border-top: 1px solid var(--color-border-2);
}

.business-group-binding-list {
  display: grid;
  gap: 6px;
  margin-bottom: 12px;
}

.business-group-binding-row {
  min-height: 36px;
  border-bottom: 1px solid var(--color-border-2);
}

.business-group-attach {
  display: grid;
  grid-template-columns: minmax(130px, 1fr) minmax(130px, 1fr) auto;
  gap: 8px;
  margin-top: 12px;
}

.business-group-note {
  margin-top: 10px;
  font-size: 12px;
}

.business-group-footer {
  justify-content: space-between;
}

@media (max-width: 700px) {
  .business-group-workspace {
    grid-template-columns: minmax(0, 1fr);
  }

  .business-group-tree {
    max-height: 180px;
  }

  .business-group-attach {
    grid-template-columns: minmax(0, 1fr);
  }

  .business-group-footer {
    align-items: flex-start;
    flex-direction: column;
  }
}
</style>
