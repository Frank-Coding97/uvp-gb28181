<script setup lang="ts">
import MediaWorkspaceShell from "./MediaWorkspaceShell.vue";
import { useMediaWorkspaceRoute } from "./useMediaWorkspaceRoute";
import FFmpegPanel from "./ingress/FFmpegPanel.vue";
import ProxyPanel from "./ingress/ProxyPanel.vue";
import RTPPanel from "./ingress/RTPPanel.vue";

const workspace = useMediaWorkspaceRoute("ingress");
const views = [
  { key: "pull", label: "拉流代理", description: "外部源接入" },
  { key: "push", label: "推流代理", description: "推流目标管理" },
  { key: "ffmpeg", label: "FFmpeg 源", description: "转码接入任务" },
  { key: "rtp", label: "RTP 服务", description: "端口与 SSRC" }
];
</script>

<template>
  <MediaWorkspaceShell
    :title="workspace.definition.title" description="统一管理拉流、推流、FFmpeg 与 RTP 接入，节点级写操作要求明确范围。"
    :views="views.filter(view => workspace.allowedViews.value.includes(view.key))" :active-view="workspace.activeView.value"
    :scope="workspace.scope.value" :nodes="workspace.nodes.value" :status="workspace.status.value"
    :status-text="workspace.statusText.value" :last-success-at="workspace.lastSuccessAt.value"
    :auto-refresh="workspace.autoRefresh.value" :scope-loading="workspace.scopeLoading.value"
    :scope-error="workspace.scopeError.value ? '节点目录刷新失败' : ''"
    :allow-all="false" :requires-node="true"
    @update:active-view="workspace.setActiveView" @update:scope="workspace.setScope"
    @update:auto-refresh="workspace.autoRefresh.value = $event" @refresh="workspace.refreshScope" @refresh-scope="workspace.refreshScope"
  >
    <template #pull="{ active }"><ProxyPanel :active="active" kind="pull" :scope="workspace.scope.value" :nodes="workspace.nodes.value" /></template>
    <template #push="{ active }"><ProxyPanel :active="active" kind="push" :scope="workspace.scope.value" :nodes="workspace.nodes.value" /></template>
    <template #ffmpeg="{ active }"><FFmpegPanel :active="active" :scope="workspace.scope.value" :nodes="workspace.nodes.value" /></template>
    <template #rtp="{ active }"><RTPPanel :active="active" :scope="workspace.scope.value" :nodes="workspace.nodes.value" /></template>
  </MediaWorkspaceShell>
</template>
