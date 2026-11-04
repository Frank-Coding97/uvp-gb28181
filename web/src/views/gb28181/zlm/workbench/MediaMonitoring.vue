<script setup lang="ts">
import { computed, ref } from "vue";
import { useRoute } from "vue-router";
import { Network, Radio, Users } from "lucide-vue-next";

import MediaScopeBar from "./components/MediaScopeBar.vue";
import MediaWorkspaceShell from "./MediaWorkspaceShell.vue";
import NetworkSessionPanel from "./monitoring/NetworkSessionPanel.vue";
import StreamPanel from "./monitoring/StreamPanel.vue";
import { useMediaWorkspaceRoute } from "./useMediaWorkspaceRoute";

const workspace = useMediaWorkspaceRoute("monitoring", { defaultScope: "all" });
const route = useRoute();
const streamPanel = ref<{ refresh: () => void } | null>(null);
const sessionPanel = ref<{ refresh: () => void } | null>(null);
const views = [
  { key: "streams", label: "流媒体", description: "在线媒体与录制态", icon: Radio },
  { key: "sessions", label: "网络会话", description: "网络连接与资源归属", icon: Network },
  { key: "viewers", label: "媒体观看者", description: "媒体流观看连接", icon: Users }
];
const nodeId = computed(() => (typeof workspace.scope.value === "number" ? workspace.scope.value : null));

function refreshActivePanel() {
  if (workspace.activeView.value === "streams") streamPanel.value?.refresh();
  if (["sessions", "viewers"].includes(workspace.activeView.value)) sessionPanel.value?.refresh();
}

function refreshAll() {
  void workspace.refreshScope();
  refreshActivePanel();
}
</script>

<template>
  <MediaWorkspaceShell
    class="media-monitoring-shell"
    :title="workspace.definition.title"
    description="按节点范围巡检在线媒体与网络会话，定位异常并安全处置。"
    :views="views.filter(view => workspace.allowedViews.value.includes(view.key))"
    :active-view="workspace.activeView.value"
    :scope="workspace.scope.value"
    :nodes="workspace.nodes.value"
    :status="workspace.status.value"
    :status-text="workspace.statusText.value"
    :last-success-at="workspace.lastSuccessAt.value"
    :auto-refresh="workspace.autoRefresh.value"
    :scope-loading="workspace.scopeLoading.value"
    :scope-error="workspace.scopeError.value ? '节点目录刷新失败' : ''"
    :allow-all="true"
    :requires-node="false"
    :show-scope="false"
    @update:active-view="workspace.setActiveView"
    @update:scope="workspace.setScope"
    @update:auto-refresh="workspace.autoRefresh.value = $event"
    @refresh="refreshAll"
    @refresh-scope="workspace.refreshScope"
  >
    <template #content>
      <section v-show="workspace.activeView.value === 'streams'" class="media-monitoring-view" data-panel-view="streams">
        <StreamPanel
          ref="streamPanel"
          :active="workspace.activeView.value === 'streams'"
          :auto-refresh="workspace.autoRefresh.value"
          :scope="workspace.scope.value"
          :node-id="nodeId"
          :initial-query="route.query"
        >
          <template #scope>
            <MediaScopeBar
              compact
              :model-value="workspace.scope.value"
              :nodes="workspace.nodes.value"
              :allow-all="true"
              :requires-node="false"
              :loading="workspace.scopeLoading.value"
              :error-text="workspace.scopeError.value ? '节点目录刷新失败' : ''"
              :show-refresh="false"
              @update:model-value="workspace.setScope"
            />
          </template>
        </StreamPanel>
      </section>
      <section
        v-show="['sessions', 'viewers'].includes(workspace.activeView.value)"
        class="media-monitoring-view"
        :data-panel-view="workspace.activeView.value"
      >
        <NetworkSessionPanel
          ref="sessionPanel"
          :active="['sessions', 'viewers'].includes(workspace.activeView.value)"
          :view="workspace.activeView.value === 'viewers' ? 'viewers' : 'network'"
          :scope="workspace.scope.value"
          :node-id="nodeId"
          :initial-query="route.query"
        >
          <template #scope>
            <MediaScopeBar
              compact
              :model-value="workspace.scope.value"
              :nodes="workspace.nodes.value"
              :allow-all="true"
              :requires-node="false"
              :loading="workspace.scopeLoading.value"
              :error-text="workspace.scopeError.value ? '节点目录刷新失败' : ''"
              :show-refresh="false"
              @update:model-value="workspace.setScope"
            />
          </template>
        </NetworkSessionPanel>
      </section>
    </template>
  </MediaWorkspaceShell>
</template>

<style scoped>
/* ⛔ 这里是「垂直滚动条归表格、不归外框」的关键一环：面板区（shell 的 main）
   是 `overflow: auto` 的滚动容器，只要这里面有一层高度链断掉（height 退回 auto），
   表格就会撑高整页 ⇒ 竖向滚动条跑到最外层，表头跟着一起滚走。
   所以每一层都必须 height:100% + min-height:0，把确定高度一路传到 a-table
   （a-table 收到 `scroll.y` 字符串时会把该值写成自身行内 height）。 */
.media-monitoring-view {
  box-sizing: border-box;
  display: flex;
  flex-direction: column;
  width: 100%;
  height: 100%;
  min-height: 0;
}

/* ⛔⛔ 面板区（shell 的 main）不能留 `overflow: auto`：弹性列算出来的高度是**小数**
   （实测内容 423.2px、可视 423px），Chrome 的 scrollHeight 向上取整成 424 ⇒ 判定「可滚动」
   ⇒ 右侧凭空多一条几乎满高的竖滚动条，而实际一个像素都滚不动。
   本页三个面板都是「表体自己滚」的结构，外层本来就不该滚，所以直接关掉。
   ⚠️ 只在桌面断点生效：窄屏（≤768px）shell 会切成整页滚动，那里必须留给页面。 */
@media (width >= 769px) {
  .media-monitoring-shell :deep(.media-workspace-shell__panel) {
    overflow: hidden;
  }
}
</style>
