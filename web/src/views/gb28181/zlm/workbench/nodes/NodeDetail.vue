<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { Modal } from "@arco-design/web-vue";
import { ArrowLeft, LogOut } from "lucide-vue-next";
import { onBeforeRouteLeave, onBeforeRouteUpdate, useRoute, useRouter } from "vue-router";

import { useZLMNodeCatalog, type MediaNodeCatalogNode } from "@/store/modules/media-workbench";
import { useZLMContextStore } from "@/store/modules/zlm-context";
import { useUserStoreHook } from "@/store/modules/user";
import type { ZLMNode } from "@/api/gb28181-zlm";
import ZLMNodeActionDialog from "../../ZLMNodeActionDialog.vue";
import MediaWorkspaceShell from "../MediaWorkspaceShell.vue";
import NodeConfigView from "./NodeConfigView.vue";

const props = withDefaults(defineProps<{ canonical?: boolean }>(), { canonical: true });

const route = useRoute();
const router = useRouter();
const context = useZLMContextStore();
const catalog = useZLMNodeCatalog();
const userStore = useUserStoreHook();
const configDirty = ref(false);
const kickVisible = ref(false);

const nodeId = computed(() => {
  const parsed = Number(route.params.id);
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : null;
});
const scopeNodes = computed<readonly MediaNodeCatalogNode[]>(() => catalog.nodes.value.length ? catalog.nodes.value : context.visibleNodes);
const selectedNode = computed(() => scopeNodes.value.find(node => node.id === nodeId.value));
const selectedActionNode = computed(() => selectedNode.value && "host" in selectedNode.value
  ? selectedNode.value as ZLMNode
  : null);
const canConfigUpdate = computed(() => userStore.account.permissions.includes("*:*:*")
  || userStore.account.permissions.includes("gb28181:zlm:config:update"));
const canKick = computed(() => userStore.account.permissions.includes("*:*:*")
  || userStore.account.permissions.includes("gb28181:zlm:node:kick"));
const views = [{ key: "config", label: "服务配置" }];

async function refreshCatalog() {
  try {
    const nodes = await catalog.refresh();
    context.reconcileVisibleNodes(nodes);
  } catch {
    // The shared catalog retains the last successful node list for retry.
  }
}

function goBack() {
  void router.push(props.canonical ? "/media/nodes" : "/gb28181/zlm/nodes");
}

function openEmergencyKick() {
  if (!canKick.value || !selectedActionNode.value) return;
  context.selectNode(selectedActionNode.value.id);
  kickVisible.value = true;
}

function confirmDiscardConfigDrafts() {
  if (!configDirty.value) return Promise.resolve(true);
  return new Promise<boolean>(resolve => {
    Modal.warning({
      title: "存在未保存配置",
      content: "离开当前节点会丢失尚未保存的服务配置草稿。是否继续离开？",
      okText: "继续离开",
      cancelText: "留在此页",
      hideCancel: false,
      okButtonProps: { status: "danger" },
      onOk: () => resolve(true),
      onCancel: () => resolve(false)
    });
  });
}

onBeforeRouteLeave(() => confirmDiscardConfigDrafts());
onBeforeRouteUpdate((to: { params: Record<string, unknown> }) => {
  if (String(to.params.id ?? "") === String(route.params.id ?? "")) return true;
  return confirmDiscardConfigDrafts();
});

onMounted(async () => {
  try {
    const nodes = await catalog.load();
    context.reconcileVisibleNodes(nodes);
    if (nodeId.value) context.selectNode(nodeId.value);
  } catch {
    // The service configuration request still reports its own load error.
  }
});
</script>

<template>
  <MediaWorkspaceShell
    title="服务配置"
    description="查看或修改指定媒体节点的服务配置。"
    :views="views"
    active-view="config"
    :scope="nodeId ?? 'all'"
    :nodes="scopeNodes"
    :show-scope="false"
    :auto-refresh="false"
    :show-auto-refresh="false"
    :allow-all="false"
    :requires-node="true"
    :scope-loading="catalog.loading.value"
    :scope-error="catalog.error.value ? '节点目录刷新失败' : ''"
    @refresh-scope="refreshCatalog"
  >
    <template #content>
      <section class="node-service-config-view" aria-label="节点服务配置">
        <header v-if="!nodeId" class="service-config-toolbar">
          <a-button class="back-btn" aria-label="返回节点管理" @click="goBack">
            <template #icon><ArrowLeft :size="16" /></template>
          </a-button>
          <div class="service-config-toolbar__node">
            <h1>服务配置</h1>
            <p>{{ selectedNode?.name || (nodeId ? `节点 #${nodeId}` : "节点地址无效") }}</p>
          </div>
        </header>

        <div v-if="!nodeId" class="config-state config-state--error" role="alert">
          <strong>节点地址无效</strong>
          <span>请返回节点管理并重新选择节点。</span>
          <a-button @click="goBack">返回节点管理</a-button>
        </div>
        <NodeConfigView
          v-else
          :node-id="nodeId"
          :node-name="selectedNode?.name"
          active
          :editable="canConfigUpdate"
          @dirty-change="configDirty = $event"
        >
          <template #heading>
            <div class="service-config-toolbar__node">
              <a-button class="back-btn" aria-label="返回节点管理" @click="goBack">
                <template #icon><ArrowLeft :size="16" /></template>
              </a-button>
              <div>
                <h1>服务配置</h1>
                <p>{{ selectedNode?.name || `节点 #${nodeId}` }} · 只允许热更新项进入提交；保存结果必须经过 ZLMediaKit 实际值回读。Secret 不会回显。</p>
              </div>
            </div>
          </template>
        </NodeConfigView>
        <section v-if="nodeId && canKick" class="emergency-actions" aria-labelledby="node-emergency-title">
          <div>
            <h3 id="node-emergency-title">紧急操作</h3>
            <p>立即终止该节点上的全部媒体会话。此操作不会停用节点。</p>
          </div>
          <a-button status="danger" :disabled="!selectedActionNode" @click="openEmergencyKick">
            <template #icon><LogOut :size="15" /></template>
            紧急终止全部会话
          </a-button>
        </section>
        <ZLMNodeActionDialog
          v-model:visible="kickVisible"
          :node="selectedActionNode"
          action="kick"
          @done="refreshCatalog"
        />
      </section>
    </template>
  </MediaWorkspaceShell>
</template>

<style scoped>
.node-service-config-view { min-width: 0; color: var(--zlm-text-2); }
.service-config-toolbar { display: flex; align-items: center; gap: 10px; margin-bottom: 10px; }
.service-config-toolbar__node { display: flex; min-width: 0; align-items: center; gap: 10px; }
.service-config-toolbar__node h1 { margin: 0; color: var(--zlm-text-1); font-size: 17px; }
.service-config-toolbar p, .service-config-toolbar__node p { margin: 3px 0 0; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); }
.back-btn { width: 38px; min-width: 38px; padding: 0; }
.config-state { display: flex; min-height: 280px; flex-direction: column; align-items: center; justify-content: center; gap: 10px; }
.config-state--error { color: var(--zlm-danger-600); }
.emergency-actions { display: flex; align-items: center; justify-content: space-between; gap: 20px; margin-top: 24px; padding-top: 20px; border-top: 1px solid var(--zlm-border); }.emergency-actions h3 { margin: 0; color: var(--zlm-text-1); font-size: 15px; }.emergency-actions p { margin: 5px 0 0; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); }
@media (max-width: 640px) { .emergency-actions { align-items: stretch; flex-direction: column; }.emergency-actions :deep(.arco-btn) { width: 100%; } }
</style>
