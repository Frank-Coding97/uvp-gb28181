<script setup lang="ts">
/**
 * DirectoryAside — 左侧 280px 目录树(C2)
 *
 * 内容:
 * - head:目录标题 + 折叠按钮(本期占位)
 * - search:本地名称过滤
 * - tree:递归 DirectoryTreeNode + lazy children
 * - 底部 AnomalyEntry:跳异常治理页
 *
 * 行为:
 * - 选中节点 → URL `?node=id`,父级 watch URL 重查列表
 * - 展开节点 → store.toggleExpand → lazy load children
 */
import { computed, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useCatalogStore } from "../stores/catalog";
import type { CatalogNode } from "../api/catalog";
import DirectoryTreeNode from "./DirectoryTreeNode.vue";
import AnomalyEntry from "./AnomalyEntry.vue";

const route = useRoute();
const router = useRouter();
const store = useCatalogStore();

const searchKeyword = ref("");

const activeId = computed<number | null>(() => {
  const v = route.query.node;
  if (typeof v !== "string") return null;
  const n = Number(v);
  return Number.isFinite(n) && n > 0 ? n : null;
});

const filteredRoots = computed(() => {
  const kw = searchKeyword.value.trim().toLowerCase();
  if (!kw) return store.roots;
  return store.roots.filter((n: CatalogNode) => n.name.toLowerCase().includes(kw));
});

function selectNode(id: number) {
  router.push({ query: { ...route.query, node: String(id) } });
}

function toggle(id: number) {
  store.toggleExpand(id);
}

onMounted(() => {
  store.loadRoots();
  store.loadAnomalyCount();
});

watch(activeId, async newId => {
  if (newId == null) return;
  // 命中节点需要确保其祖先链都已展开 - Phase 1 简化版:仅展开节点本身
  if (!store.isExpanded(newId) && store.nodes.get(newId)?.nodeType !== "channel") {
    await store.toggleExpand(newId);
  }
});
</script>

<template>
  <div class="directory-aside">
    <div class="aside-head">
      <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2">
        <path d="M3 7v10a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2V9a2 2 0 0 0-2-2h-7l-2-3H5a2 2 0 0 0-2 2z" />
      </svg>
      <span class="title">目录</span>
    </div>

    <div class="aside-search">
      <input v-model="searchKeyword" placeholder="筛选节点 ..." />
    </div>

    <nav class="tree">
      <div v-if="store.loadingRoots" class="hint">加载中…</div>
      <div v-else-if="filteredRoots.length === 0" class="hint">
        {{ searchKeyword ? "无匹配节点" : "暂无目录数据,等待 catalog NOTIFY 入库" }}
      </div>
      <DirectoryTreeNode
        v-else
        v-for="root in filteredRoots"
        :key="root.id"
        :node="root"
        :active-id="activeId"
        @toggle="toggle"
        @select="selectNode"
      />
    </nav>

    <AnomalyEntry />
  </div>
</template>

<style lang="scss" scoped>
.directory-aside {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
}

.aside-head {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  height: 40px;
  padding: 0 var(--space-4);
  color: var(--color-text-3);
  border-bottom: 1px solid var(--color-border-1);
  font-size: var(--font-13);
  flex-shrink: 0;

  .title {
    color: var(--color-text-2);
    font-weight: 500;
    flex: 1;
  }
}

.aside-search {
  padding: var(--space-2) var(--space-3);
  flex-shrink: 0;

  input {
    width: 100%;
    height: 28px;
    padding: 0 var(--space-3);
    background: var(--color-bg-3);
    border: 1px solid var(--color-border-2);
    border-radius: var(--dm-radius-1);
    color: var(--color-text-1);
    font-size: var(--font-12);
    outline: none;

    &::placeholder {
      color: var(--color-text-4);
    }

    &:focus {
      border-color: var(--primary-6);
    }
  }
}

.tree {
  flex: 1;
  overflow-y: auto;
  padding: var(--space-1) 0;
  min-height: 0;
}

.hint {
  color: var(--color-text-4);
  padding: var(--space-4);
  font-size: var(--font-12);
  text-align: center;
}
</style>
