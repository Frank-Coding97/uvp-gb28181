<script setup lang="ts">
/**
 * DeviceMgmtPage — 设备管理页顶层(tasks §3 C1+C2 集成)
 *
 * 布局根:`.dm-page-root` height:100vh + overflow:hidden + grid(rows: topbar / 1fr)
 * (plan §2.2 踩坑)
 *
 * 三栏:
 *   - aside 280px  → DirectoryAside(C2)
 *   - main  1fr    → 视图切换器 + 列表/卡片/地图(C3/D1/D3,各自实现)
 *   - drawer 40%   → DetailDrawer(可选,E1)由 URL ?node= 控制
 *
 * 视图切换:URL `?view=list|card|map`,默认 list
 */
import { computed } from "vue";
import { useRoute } from "vue-router";
import TopBar from "./components/TopBar.vue";
import DirectoryAside from "./components/DirectoryAside.vue";
import DeviceListView from "./components/list/DeviceListView.vue";

const route = useRoute();

type ViewMode = "list" | "card" | "map";

const currentView = computed<ViewMode>(() => {
  const v = route.query.view;
  return v === "card" || v === "map" ? v : "list";
});

const drawerOpen = computed(() => !!route.query.node);
</script>

<template>
  <div class="dm-page-root">
    <TopBar />

    <section :class="['dm-workspace', { 'with-drawer': drawerOpen }]">
      <aside class="dm-aside">
        <DirectoryAside />
      </aside>

      <main class="dm-main">
        <DeviceListView v-if="currentView === 'list'" />
        <div v-else class="dm-view-placeholder">
          {{ currentView === "card" ? "卡片视图(D1 待实现)" : "地图视图(D3 待实现)" }}
          <p class="hint">
            选中左侧节点筛选 / 切换视图试试。当前过滤:
            <code v-if="route.query.node">node={{ route.query.node }}</code>
            <code v-else>全部</code>
          </p>
        </div>
      </main>

      <aside v-if="drawerOpen" class="dm-drawer">
        <div class="dm-drawer__placeholder">
          详情抽屉(E1 待实现)
          <p class="hint">node={{ route.query.node }}</p>
        </div>
      </aside>
    </section>
  </div>
</template>

<style lang="scss" scoped>
.dm-workspace {
  display: grid;
  grid-template-columns: var(--aside-width) 1fr;
  grid-template-rows: 1fr;
  min-height: 0;
  overflow: hidden;

  &.with-drawer {
    grid-template-columns: var(--aside-width) 1fr var(--drawer-width);
  }
}

.dm-aside,
.dm-main,
.dm-drawer {
  min-height: 0;
  overflow: auto;
}

.dm-aside {
  background: var(--color-bg-2);
  border-right: 1px solid var(--color-border-2);
}

.dm-main {
  background: var(--color-bg-1);
  padding: var(--space-4);
}

.dm-drawer {
  background: var(--color-bg-2);
  border-left: 1px solid var(--color-border-2);
  min-width: var(--drawer-min-width);

  &__placeholder {
    padding: var(--space-4);
    color: var(--color-text-3);
    line-height: var(--line-relaxed);

    .hint {
      color: var(--color-text-4);
      font-size: var(--font-12);
      margin-top: var(--space-2);
    }
  }
}

.dm-view-placeholder {
  color: var(--color-text-4);
  text-align: center;
  margin-top: 30vh;
  font-size: var(--font-16);

  .hint {
    margin-top: var(--space-3);
    font-size: var(--font-13);

    code {
      background: var(--color-bg-3);
      padding: 2px 6px;
      border-radius: var(--dm-radius-1);
      font-family: var(--font-mono);
      color: var(--color-text-2);
      margin-left: var(--space-1);
    }
  }
}
</style>
