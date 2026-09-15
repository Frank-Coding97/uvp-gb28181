<script setup lang="ts">
import { RefreshCw } from "lucide-vue-next";
import type { MediaNodeCatalogNode, MediaScope } from "@/store/modules/media-workbench";
import MediaScopeBar from "./components/MediaScopeBar.vue";

export type MediaWorkbenchStatus = "ready" | "partial" | "stale" | "error" | "denied";

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
  showAutoRefresh?: boolean;
  showToolbarActions?: boolean;
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
  showAutoRefresh: true,
  showToolbarActions: false,
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
    <h1 class="sr-only">{{ title }}</h1>

    <div class="media-workspace-shell__scope" data-shell-block="scope">
      <MediaScopeBar
        :model-value="scope"
        :nodes="nodes"
        :allow-all="allowAll"
        :requires-node="requiresNode"
        :loading="scopeLoading"
        :stale="scopeStale"
        :error-text="scopeError"
        :show-refresh="showToolbarActions"
        @update:model-value="emit('update:scope', $event)"
        @refresh="emit('refreshScope')"
      >
        <template v-if="showToolbarActions" #actions>
          <div class="media-workspace-shell__controls">
            <button
              v-if="showAutoRefresh"
              type="button"
              class="media-workspace-shell__toggle"
              :class="{ 'is-active': autoRefresh }"
              :aria-label="`${autoRefresh ? '关闭' : '开启'}${title}自动刷新`"
              :aria-pressed="autoRefresh"
              @click="emit('update:autoRefresh', !autoRefresh)"
            >
              <span aria-hidden="true"><i /></span>
              自动刷新
            </button>
            <button type="button" :aria-label="`刷新${title}`" :disabled="loading" @click="emit('refresh')">
              <RefreshCw :size="14" :class="{ 'is-spinning': loading }" aria-hidden="true" />
              刷新
            </button>
            <slot name="primary-action" />
          </div>
        </template>
      </MediaScopeBar>
      <div
        v-if="status !== 'ready' && statusText"
        class="media-workspace-shell__status"
        :data-tone="status"
        :role="status === 'error' || status === 'denied' ? 'alert' : 'status'"
      >
        {{ statusText }}
      </div>
    </div>

    <nav
      v-if="views.length > 1"
      class="media-workspace-shell__tabs"
      data-shell-block="tabs"
      :aria-label="`${title}功能视图`"
    >
      <button
        v-for="view in views"
        :key="view.key"
        type="button"
        :data-view="view.key"
        :class="{ 'is-active': activeView === view.key }"
        :aria-current="activeView === view.key ? 'page' : undefined"
        :title="view.description"
        @click="emit('update:activeView', view.key)"
      >
        {{ view.label }}
      </button>
    </nav>

    <main class="media-workspace-shell__panel" data-shell-block="panel">
      <slot name="content">
        <section
          v-for="view in views"
          v-show="activeView === view.key"
          :key="view.key"
          class="media-workspace-shell__panel-view"
          :data-panel-view="view.key"
          :aria-hidden="activeView === view.key ? 'false' : 'true'"
        >
          <slot :name="view.key" :active="activeView === view.key">
            <div class="media-workspace-shell__empty" role="status">当前视图暂无可展示内容。</div>
          </slot>
        </section>
      </slot>
    </main>
  </div>
</template>

<style scoped>
.media-workspace-shell { box-sizing: border-box; display: flex; width: 100%; height: 100%; min-height: 0; flex-direction: column; gap: 10px; padding: calc(var(--uvp-main-padding) + 4px) calc(var(--uvp-main-padding) + 8px) var(--uvp-workspace-gap); overflow: hidden; color: var(--zlm-text-2); container-name: media-workspace-content; container-type: inline-size; }
.sr-only { position: absolute; width: 1px; height: 1px; padding: 0; overflow: hidden; clip: rect(0, 0, 0, 0); white-space: nowrap; border: 0; }
.media-workspace-shell__scope { display: flex; flex: none; flex-direction: column; gap: 8px; }
.media-workspace-shell__controls { display: flex; flex: none; align-items: center; gap: 4px; padding-left: 10px; border-left: 1px solid var(--zlm-border); }
.media-workspace-shell__controls button { display: inline-flex; min-height: 30px; align-items: center; gap: 6px; padding: 0 9px; color: var(--zlm-text-2); background: transparent; border: 0; border-radius: var(--zlm-radius-md); cursor: pointer; white-space: nowrap; }
.media-workspace-shell__controls button:hover { color: var(--zlm-brand-600); background: var(--zlm-brand-50); }
.media-workspace-shell__controls button:focus-visible { outline: 2px solid var(--zlm-brand-500); outline-offset: 2px; }
.media-workspace-shell__controls button:disabled { cursor: wait; opacity: .65; }
.media-workspace-shell__toggle > span { display: inline-flex; width: 22px; height: 12px; align-items: center; padding: 2px; background: var(--zlm-fill-3); border-radius: 999px; transition: background-color .18s ease; }
.media-workspace-shell__toggle > span i { width: 8px; height: 8px; background: var(--zlm-card); border-radius: 50%; transition: transform .18s ease; }
.media-workspace-shell__toggle.is-active > span { background: var(--zlm-brand-500); }
.media-workspace-shell__toggle.is-active > span i { transform: translateX(10px); }
.media-workspace-shell__status { flex: none; padding: 7px 12px; color: var(--zlm-warn-600); background: var(--zlm-warn-50); border: 1px solid var(--zlm-warn-300); border-radius: var(--zlm-radius-md); font-size: var(--zlm-fs-caption); }
.media-workspace-shell__status[data-tone="error"], .media-workspace-shell__status[data-tone="denied"] { color: var(--zlm-danger-600); background: var(--zlm-danger-50); border-color: var(--zlm-danger-200); }
.media-workspace-shell__tabs { display: flex; flex: none; align-items: center; gap: 4px; min-height: 38px; padding: 0; background: transparent; border: 0; }
.media-workspace-shell__tabs button { position: relative; min-height: 38px; padding: 0 14px; color: var(--zlm-text-3); background: transparent; border: 0; cursor: pointer; }
.media-workspace-shell__tabs button:hover { color: var(--zlm-brand-600); background: transparent; }
.media-workspace-shell__tabs button.is-active { color: var(--zlm-brand-600); font-weight: var(--zlm-fw-semibold); background: transparent; }
.media-workspace-shell__tabs button.is-active::after { position: absolute; right: 12px; bottom: 0; left: 12px; height: 3px; background: var(--zlm-brand-500); border-radius: 3px 3px 0 0; content: ""; }
.media-workspace-shell__tabs button:focus-visible { outline: 2px solid var(--zlm-brand-500); outline-offset: 1px; }
.is-spinning { animation: media-shell-spin .8s linear infinite; }
@keyframes media-shell-spin { to { transform: rotate(360deg); } }
.media-workspace-shell__panel { min-width: 0; min-height: 0; flex: 1; overflow: auto; overscroll-behavior: contain; }
.media-workspace-shell__panel-view { box-sizing: border-box; width: 100%; height: 100%; min-height: 0; }
.media-workspace-shell__empty { display: grid; min-height: 260px; place-items: center; color: var(--zlm-text-3); background: var(--zlm-card); border: 1px dashed var(--zlm-border); border-radius: var(--zlm-radius-lg); }
@container media-workspace-content (max-width: 720px) { .media-workspace-shell__panel :deep(.runtime-summary-grid), .media-workspace-shell__panel :deep(.thread-analysis-grid), .media-workspace-shell__panel :deep(.thread-insights), .media-workspace-shell__panel :deep(.overview-chart-grid), .media-workspace-shell__panel :deep(.distribution-layout), .media-workspace-shell__panel :deep(.chart-grid) { grid-template-columns: 1fr; } .media-workspace-shell__panel :deep(.runtime-summary-kpis) { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
@media (prefers-reduced-motion: reduce) { .media-workspace-shell * { animation-duration: .01ms !important; transition-duration: .01ms !important; } }
@media (max-width: 768px) { .media-workspace-shell { height: auto; min-height: 100%; overflow: auto; } .media-workspace-shell__tabs { overflow-x: auto; } .media-workspace-shell__controls { flex-wrap: wrap; padding-left: 0; border-left: 0; } .media-workspace-shell__panel { overflow: visible; } }
</style>
