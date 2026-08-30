<script setup lang="ts">
import type { MediaNodeCatalogNode, MediaScope } from "@/store/modules/media-workbench";
import MediaScopeBar from "./components/MediaScopeBar.vue";
import MediaWorkbenchHeader, { type MediaWorkbenchStatus } from "./components/MediaWorkbenchHeader.vue";

export interface MediaWorkspaceView {
  key: string;
  label: string;
  description?: string;
}

withDefaults(defineProps<{
  title: string;
  description: string;
  views: readonly MediaWorkspaceView[];
  activeView: string;
  scope: MediaScope;
  nodes: readonly MediaNodeCatalogNode[];
  status?: MediaWorkbenchStatus;
  statusText?: string;
  lastSuccessAt?: string | null;
  loading?: boolean;
  autoRefresh?: boolean;
  allowAll?: boolean;
  requiresNode?: boolean;
  scopeLoading?: boolean;
  scopeStale?: boolean;
  scopeError?: string;
}>(), {
  status: "ready",
  statusText: "",
  lastSuccessAt: null,
  loading: false,
  autoRefresh: true,
  allowAll: true,
  requiresNode: false,
  scopeLoading: false,
  scopeStale: false,
  scopeError: ""
});

const emit = defineEmits<{
  "update:activeView": [view: string];
  "update:scope": [scope: MediaScope];
  "update:autoRefresh": [enabled: boolean];
  refresh: [];
  refreshScope: [];
}>();
</script>

<template>
  <div class="media-workspace-shell">
    <div data-shell-block="header">
      <MediaWorkbenchHeader
        :title="title"
        :description="description"
        :status="status"
        :status-text="statusText"
        :last-success-at="lastSuccessAt"
        :loading="loading"
        :auto-refresh="autoRefresh"
        @refresh="emit('refresh')"
        @update:auto-refresh="emit('update:autoRefresh', $event)"
      >
        <template #primary-action><slot name="primary-action" /></template>
      </MediaWorkbenchHeader>
    </div>

    <div data-shell-block="scope">
      <MediaScopeBar
        :model-value="scope"
        :nodes="nodes"
        :allow-all="allowAll"
        :requires-node="requiresNode"
        :loading="scopeLoading"
        :stale="scopeStale"
        :error-text="scopeError"
        @update:model-value="emit('update:scope', $event)"
        @refresh="emit('refreshScope')"
      />
    </div>

    <nav class="media-workspace-shell__tabs" data-shell-block="tabs" :aria-label="`${title}功能视图`">
      <button
        v-for="view in views"
        :key="view.key"
        type="button"
        :data-view="view.key"
        :class="{ 'is-active': activeView === view.key }"
        :aria-current="activeView === view.key ? 'page' : undefined"
        @click="emit('update:activeView', view.key)"
      >
        <strong>{{ view.label }}</strong>
        <span v-if="view.description">{{ view.description }}</span>
      </button>
    </nav>

    <main class="media-workspace-shell__panel" data-shell-block="panel">
      <slot :name="activeView" :active="true">
        <div class="media-workspace-shell__empty" role="status">当前视图暂无可展示内容。</div>
      </slot>
    </main>
  </div>
</template>

<style scoped>
.media-workspace-shell { box-sizing: border-box; display: flex; width: 100%; height: 100%; min-height: 0; flex-direction: column; gap: 12px; padding: 4px 8px 16px; overflow: hidden; color: var(--zlm-text-2); }
.media-workspace-shell__tabs { display: flex; flex: none; min-width: 0; gap: 4px; padding: 4px; overflow-x: auto; background: var(--zlm-fill-1); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-lg); }
.media-workspace-shell__tabs button { display: flex; min-width: 112px; min-height: 42px; flex-direction: column; align-items: flex-start; justify-content: center; gap: 1px; padding: 6px 12px; color: var(--zlm-text-3); text-align: left; background: transparent; border: 1px solid transparent; border-radius: var(--zlm-radius-md); cursor: pointer; }
.media-workspace-shell__tabs button:hover { color: var(--zlm-text-1); background: var(--zlm-fill-2); }
.media-workspace-shell__tabs button.is-active { color: var(--zlm-brand-600); background: var(--zlm-card); border-color: var(--zlm-border); box-shadow: 0 2px 8px rgb(15 23 42 / 6%); }
.media-workspace-shell__tabs button:focus-visible { outline: 2px solid var(--zlm-brand-500); outline-offset: 1px; }
.media-workspace-shell__tabs strong { font-size: 13px; font-weight: var(--zlm-fw-semibold); }
.media-workspace-shell__tabs span { font-size: 10px; white-space: nowrap; }
.media-workspace-shell__panel { min-width: 0; min-height: 0; flex: 1; overflow: auto; overscroll-behavior: contain; }
.media-workspace-shell__empty { display: grid; min-height: 260px; place-items: center; color: var(--zlm-text-3); background: var(--zlm-card); border: 1px dashed var(--zlm-border); border-radius: var(--zlm-radius-lg); }
@media (max-width: 768px) { .media-workspace-shell { gap: 10px; padding: 0 0 12px; } .media-workspace-shell__tabs button { min-width: 100px; } }
</style>
