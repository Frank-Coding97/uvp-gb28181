<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { Modal } from "@arco-design/web-vue";
import { ArrowLeft } from "lucide-vue-next";
import { onBeforeRouteLeave, onBeforeRouteUpdate, useRoute, useRouter } from "vue-router";

import { useZLMNodeCatalog, type MediaNodeCatalogNode } from "@/store/modules/media-workbench";
import { useZLMContextStore } from "@/store/modules/zlm-context";
import { useUserStoreHook } from "@/store/modules/user";
import MediaWorkspaceShell from "../MediaWorkspaceShell.vue";
import NodeConfigView from "./NodeConfigView.vue";

const props = withDefaults(defineProps<{ canonical?: boolean }>(), { canonical: true });

const route = useRoute();
const router = useRouter();
const context = useZLMContextStore();
const catalog = useZLMNodeCatalog();
const userStore = useUserStoreHook();
const configDirty = ref(false);

const nodeId = computed(() => {
  const parsed = Number(route.params.id);
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : null;
});
const scopeNodes = computed<readonly MediaNodeCatalogNode[]>(() => catalog.nodes.value.length ? catalog.nodes.value : context.visibleNodes);
const selectedNode = computed(() => scopeNodes.value.find(node => node.id === nodeId.value));
const canConfigUpdate = computed(() => userStore.account.permissions.includes("*:*:*")
  || userStore.account.permissions.includes("gb28181:zlm:config:update"));
const views = [{ key: "config", label: "服务配置" }];

async function refreshCatalog() {
  try {
    const nodes = await catalog.refresh();
    context.reconcileVisibleNodes(nodes);
  } catch {
    // The shared catalog retains the last successful node list for retry.
  }
}

function switchNode(next: number | "all") {
  if (typeof next !== "number" || next === nodeId.value) return;
  context.selectNode(next);
  const path = props.canonical ? `/media/nodes/${next}` : `/gb28181/zlm/nodes/${next}`;
  void router.push({ path, query: { nodeId: String(next) } });
}

function goBack() {
  void router.push(props.canonical ? "/media/nodes" : "/gb28181/zlm/nodes");
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
    :auto-refresh="false"
    :show-auto-refresh="false"
    :allow-all="false"
    :requires-node="true"
    :scope-loading="catalog.loading.value"
    :scope-error="catalog.error.value ? '节点目录刷新失败' : ''"
    @update:scope="switchNode"
    @refresh-scope="refreshCatalog"
  >
    <template #content>
      <section class="node-service-config-view" aria-label="节点服务配置">
        <header class="service-config-toolbar">
          <a-button class="back-btn" aria-label="返回节点管理" @click="goBack">
            <template #icon><ArrowLeft :size="16" /></template>
          </a-button>
          <div>
            <h2>服务配置</h2>
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
        />
      </section>
    </template>
  </MediaWorkspaceShell>
</template>

<style scoped>
.node-service-config-view { min-width: 0; color: var(--zlm-text-2); }
.service-config-toolbar { display: flex; align-items: center; gap: 10px; margin-bottom: 16px; }
.service-config-toolbar h2 { margin: 0; color: var(--zlm-text-1); font-size: 17px; }
.service-config-toolbar p { margin: 3px 0 0; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); }
.back-btn { width: 38px; min-width: 38px; padding: 0; }
.config-state { display: flex; min-height: 280px; flex-direction: column; align-items: center; justify-content: center; gap: 10px; }
.config-state--error { color: var(--zlm-danger-600); }
</style>
