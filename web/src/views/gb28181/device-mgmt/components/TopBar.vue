<script setup lang="ts">
/**
 * TopBar — 设备管理页顶栏(C2)
 *
 * 布局(mockup list.html .topbar):
 * - 左:Logo + 面包屑(当前节点 path 反查链)
 * - 中:Cmd+K 搜索占位 + 视图切换器
 * - 右:筛选 / 新建 / 设置 icon button(本期占位)
 */
import { computed } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useCatalogStore } from "../stores/catalog";
import type { CatalogNode } from "../api/catalog";

const route = useRoute();
const router = useRouter();
const store = useCatalogStore();

type ViewMode = "list" | "card" | "map";
const VIEWS: { key: ViewMode; label: string }[] = [
  { key: "list", label: "列表" },
  { key: "card", label: "卡片" },
  { key: "map", label: "地图" },
];

const currentView = computed<ViewMode>(() => {
  const v = route.query.view;
  return v === "card" || v === "map" ? v : "list";
});

function switchView(view: ViewMode) {
  if (view === currentView.value) return;
  router.push({ query: { ...route.query, view } });
}

// 面包屑:从 active 节点反查 path 链
const breadcrumb = computed(() => {
  const v = route.query.node;
  if (typeof v !== "string") return [];
  const id = Number(v);
  if (!Number.isFinite(id)) return [];

  const cur = store.nodes.get(id);
  if (!cur) return [];

  // path 格式 "/12/47/189/" - 切出 id 序列
  const segments = cur.path.split("/").filter(Boolean).map(Number);
  return segments
    .map((sid: number) => store.nodes.get(sid))
    .filter((n: CatalogNode | undefined): n is CatalogNode => n != null);
});

function jumpToBreadcrumb(id: number) {
  router.push({ query: { ...route.query, node: String(id) } });
}
</script>

<template>
  <header class="dm-topbar">
    <div class="dm-topbar__left">
      <span class="dm-topbar__logo">
        <span class="logo-dot" />
        UVP
      </span>
      <nav class="breadcrumb" v-if="breadcrumb.length">
        <template v-for="(n, idx) in breadcrumb" :key="n.id">
          <span class="sep" v-if="idx > 0">/</span>
          <span
            :class="['crumb', { cur: idx === breadcrumb.length - 1 }]"
            @click="jumpToBreadcrumb(n.id)"
          >
            {{ n.name }}
          </span>
        </template>
      </nav>
      <span v-else class="breadcrumb-empty">设备管理</span>
    </div>

    <div class="dm-topbar__center">
      <button class="cmdk" type="button" title="Cmd+K 搜索(Phase 2 实现)">
        <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2">
          <circle cx="11" cy="11" r="7" />
          <path d="m21 21-4.3-4.3" />
        </svg>
        <span class="hint">搜索设备 / 通道 / 编码 ...</span>
        <span class="kbd">
          <kbd>⌘</kbd>
          <kbd>K</kbd>
        </span>
      </button>

      <div class="view-switcher" role="tablist">
        <button
          v-for="v in VIEWS"
          :key="v.key"
          role="tab"
          :aria-selected="currentView === v.key"
          :class="['view-switcher__btn', { active: currentView === v.key }]"
          @click="switchView(v.key)"
          :title="`${v.label}视图`"
        >
          {{ v.label }}
        </button>
      </div>
    </div>

    <div class="dm-topbar__right">
      <button class="icon-btn" type="button" title="筛选(C4 实现)" disabled>
        <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2">
          <polygon points="22 3 2 3 10 12.46 10 19 14 21 14 12.46 22 3" />
        </svg>
      </button>
      <button class="icon-btn" type="button" title="设置" disabled>
        <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2">
          <circle cx="12" cy="12" r="3" />
          <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z" />
        </svg>
      </button>
    </div>
  </header>
</template>

<style lang="scss" scoped>
.dm-topbar {
  height: var(--topbar-height);
  background: var(--color-bg-2);
  border-bottom: 1px solid var(--color-border-2);
  display: grid;
  grid-template-columns: minmax(280px, 1fr) auto minmax(120px, 1fr);
  align-items: center;
  padding: 0 var(--space-4);
  overflow: hidden;
  gap: var(--space-4);

  &__left {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    overflow: hidden;
    min-width: 0;
  }

  &__center {
    display: flex;
    align-items: center;
    gap: var(--space-3);
  }

  &__right {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: var(--space-2);
  }

  &__logo {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    color: var(--color-text-1);
    font-weight: 600;
    font-size: var(--font-16);
    letter-spacing: 0.02em;

    .logo-dot {
      width: 8px;
      height: 8px;
      border-radius: 50%;
      background: var(--primary-6);
      box-shadow: 0 0 8px var(--primary-fade-24);
    }
  }
}

.breadcrumb {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--font-13);
  color: var(--color-text-3);
  overflow: hidden;

  .crumb {
    cursor: pointer;
    transition: color var(--duration-fast) var(--ease-out);
    white-space: nowrap;

    &:hover {
      color: var(--color-text-1);
    }

    &.cur {
      color: var(--color-text-1);
      font-weight: 500;
    }
  }

  .sep {
    color: var(--color-text-4);
  }
}

.breadcrumb-empty {
  color: var(--color-text-3);
  font-size: var(--font-13);
}

.cmdk {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  height: 32px;
  padding: 0 var(--space-3);
  background: var(--color-bg-3);
  border: 1px solid var(--color-border-2);
  border-radius: var(--dm-radius-2);
  color: var(--color-text-3);
  cursor: pointer;
  font-size: var(--font-12);
  min-width: 260px;
  transition: background var(--duration-fast) var(--ease-out);

  &:hover {
    background: var(--color-bg-2);
    border-color: var(--color-border-3);
  }

  .hint {
    flex: 1;
    text-align: left;
  }

  .kbd {
    display: inline-flex;
    gap: 2px;

    kbd {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      min-width: 18px;
      height: 18px;
      padding: 0 4px;
      background: var(--color-bg-1);
      border: 1px solid var(--color-border-2);
      border-radius: var(--dm-radius-1);
      font-size: 10px;
      color: var(--color-text-3);
      font-family: var(--font-mono);
    }
  }
}

.view-switcher {
  display: inline-flex;
  background: var(--color-bg-3);
  border-radius: var(--dm-radius-2);
  padding: 2px;

  &__btn {
    appearance: none;
    border: 0;
    background: transparent;
    color: var(--color-text-3);
    padding: 6px 14px;
    font-size: var(--font-13);
    border-radius: var(--dm-radius-1);
    cursor: pointer;
    transition: background var(--duration-fast) var(--ease-out),
      color var(--duration-fast) var(--ease-out);

    &:hover {
      color: var(--color-text-1);
    }

    &.active {
      background: var(--primary-fade-16);
      color: var(--primary-5);
    }
  }
}

.icon-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  background: transparent;
  border: 0;
  border-radius: var(--dm-radius-1);
  color: var(--color-text-3);
  cursor: pointer;
  transition: background var(--duration-fast) var(--ease-out);

  &:hover:not(:disabled) {
    background: var(--color-bg-3);
    color: var(--color-text-1);
  }

  &:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }
}
</style>
