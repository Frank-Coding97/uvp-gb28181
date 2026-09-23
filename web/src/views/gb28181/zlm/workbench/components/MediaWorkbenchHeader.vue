<script setup lang="ts">
import { computed } from "vue";
import { RefreshCw } from "lucide-vue-next";

export type MediaWorkbenchStatus = "ready" | "partial" | "stale" | "error" | "denied";

const props = withDefaults(defineProps<{
  title: string;
  description: string;
  status?: MediaWorkbenchStatus;
  statusText?: string;
  lastSuccessAt?: string | null;
  loading?: boolean;
  autoRefresh?: boolean;
}>(), {
  status: "ready",
  statusText: "",
  lastSuccessAt: null,
  loading: false,
  autoRefresh: true
});

const emit = defineEmits<{
  refresh: [];
  "update:autoRefresh": [enabled: boolean];
}>();

const statusLabel = computed(() => ({
  ready: "数据正常",
  partial: "部分可用",
  stale: "数据已过期",
  error: "刷新失败",
  denied: "暂无权限"
}[props.status]));

const statusRole = computed(() => props.status === "error" || props.status === "denied" ? "alert" : "status");
</script>

<template>
  <header class="media-workbench-header">
    <div class="media-workbench-header__identity">
      <span class="media-workbench-header__eyebrow">MEDIA CONTROL PLANE</span>
      <div class="media-workbench-header__title-row">
        <h1>{{ title }}</h1>
        <span
          class="media-workbench-header__status"
          :data-tone="status"
          :role="statusRole"
          aria-live="polite"
        >
          <i aria-hidden="true" />
          {{ statusLabel }}
        </span>
      </div>
      <p>{{ description }}</p>
      <p v-if="statusText" class="media-workbench-header__message">{{ statusText }}</p>
    </div>

    <div class="media-workbench-header__controls">
      <div class="media-workbench-header__freshness">
        <span>最后成功</span>
        <strong>{{ lastSuccessAt || "尚未获取" }}</strong>
      </div>
      <button
        type="button"
        class="media-workbench-header__toggle"
        :class="{ 'is-active': autoRefresh }"
        :aria-label="`${autoRefresh ? '关闭' : '开启'}${title}自动刷新`"
        :aria-pressed="autoRefresh"
        @click="emit('update:autoRefresh', !autoRefresh)"
      >
        <span aria-hidden="true"><i /></span>
        自动刷新
      </button>
      <button
        type="button"
        class="media-workbench-header__refresh"
        :aria-label="`刷新${title}`"
        :disabled="loading"
        @click="emit('refresh')"
      >
        <RefreshCw :size="15" :class="{ 'is-spinning': loading }" aria-hidden="true" />
        刷新
      </button>
      <slot name="primary-action" />
    </div>
  </header>
</template>

<style scoped>
.media-workbench-header {
  display: flex;
  flex: none;
  align-items: flex-start;
  justify-content: space-between;
  gap: 24px;
  padding: 20px 22px;
  color: var(--zlm-text-2);
  background:
    linear-gradient(135deg, color-mix(in srgb, var(--zlm-brand-500) 7%, transparent), transparent 48%),
    var(--zlm-card);
  border: 1px solid var(--zlm-border);
  border-radius: var(--zlm-radius-xl);
  box-shadow: var(--uvp-panel-shadow);
}

.media-workbench-header__identity { min-width: 0; }
.media-workbench-header__eyebrow { color: var(--zlm-brand-600); font-family: var(--zlm-font-mono); font-size: 10px; font-weight: var(--zlm-fw-semibold); letter-spacing: .14em; }
.media-workbench-header__title-row { display: flex; align-items: center; gap: 10px; margin-top: 5px; }
.media-workbench-header h1 { margin: 0; color: var(--zlm-text-1); font-size: 22px; line-height: 1.3; }
.media-workbench-header p { max-width: 720px; margin: 6px 0 0; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); line-height: 1.6; }
.media-workbench-header__message { color: var(--zlm-warn-600) !important; }
.media-workbench-header__status { display: inline-flex; align-items: center; gap: 6px; padding: 3px 8px; color: var(--zlm-success-600); background: var(--zlm-success-50); border-radius: 999px; font-size: 11px; white-space: nowrap; }
.media-workbench-header__status i { width: 6px; height: 6px; background: currentColor; border-radius: 50%; }
.media-workbench-header__status[data-tone="partial"], .media-workbench-header__status[data-tone="stale"] { color: var(--zlm-warn-600); background: var(--zlm-warn-50); }
.media-workbench-header__status[data-tone="error"], .media-workbench-header__status[data-tone="denied"] { color: var(--zlm-danger-600); background: var(--zlm-danger-50); }
.media-workbench-header__controls { display: flex; flex: none; align-items: center; justify-content: flex-end; gap: 8px; flex-wrap: wrap; }
.media-workbench-header__freshness { display: flex; flex-direction: column; align-items: flex-end; margin-right: 5px; font-size: 11px; }
.media-workbench-header__freshness span { color: var(--zlm-text-4); }
.media-workbench-header__freshness strong { color: var(--zlm-text-2); font-family: var(--zlm-font-mono); font-weight: var(--zlm-fw-medium); }
.media-workbench-header button { display: inline-flex; min-height: 32px; align-items: center; gap: 6px; padding: 0 11px; color: var(--zlm-text-2); background: var(--zlm-card); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-md); cursor: pointer; }
.media-workbench-header button:hover { color: var(--zlm-brand-600); border-color: var(--zlm-brand-500); }
.media-workbench-header button:focus-visible { outline: 2px solid var(--zlm-brand-500); outline-offset: 2px; }
.media-workbench-header button:disabled { cursor: wait; opacity: .65; }
.media-workbench-header__toggle > span { display: inline-flex; width: 22px; height: 12px; align-items: center; padding: 2px; background: var(--zlm-fill-3); border-radius: 999px; transition: background-color .18s ease; }
.media-workbench-header__toggle > span i { width: 8px; height: 8px; background: var(--zlm-card); border-radius: 50%; transition: transform .18s ease; }
.media-workbench-header__toggle.is-active > span { background: var(--zlm-brand-500); }
.media-workbench-header__toggle.is-active > span i { transform: translateX(10px); }
.is-spinning { animation: media-header-spin .8s linear infinite; }
@keyframes media-header-spin { to { transform: rotate(360deg); } }
@media (prefers-reduced-motion: reduce) { .media-workbench-header *, .media-workbench-header *::before, .media-workbench-header *::after { animation-duration: .01ms !important; transition-duration: .01ms !important; } }
@media (max-width: 1024px) { .media-workbench-header { flex-direction: column; } .media-workbench-header__controls { width: 100%; justify-content: flex-start; } .media-workbench-header__freshness { align-items: flex-start; } }
@media (max-width: 768px) { .media-workbench-header { padding: 16px; } .media-workbench-header__title-row { align-items: flex-start; flex-direction: column; } .media-workbench-header__controls { align-items: stretch; } .media-workbench-header__freshness { width: 100%; } }
</style>
