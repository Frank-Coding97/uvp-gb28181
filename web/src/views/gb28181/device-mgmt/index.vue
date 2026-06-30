<script setup lang="ts">
/**
 * DeviceMgmtPage — 设备管理页顶层(tasks §3 C1)
 *
 * 布局根:`.dm-page-root` 已定义为 height:100vh + overflow:hidden + grid(rows: topbar / 1fr)
 * 见 plan §2.2:整页 `height:100vh + overflow:hidden`,workspace/main/drawer-body 各自内部
 *      overflow:auto,防 sticky CTA / 分页 / 视野气泡被撑出视口
 *
 * 三栏:
 *   - aside 280px(目录树,C2 实现)
 *   - main  1fr(视图切换器 + 列表/卡片/地图,C3/D1/D3 各自实现)
 *   - drawer(可选,详情抽屉,E1 实现;由 URL ?node= 控制)
 *
 * 视图切换走 URL query `?view=list|card|map`,默认 list
 */
import { computed, KeepAlive } from "vue";
import { useRoute, useRouter } from "vue-router";

const route = useRoute();
const router = useRouter();

type ViewMode = "list" | "card" | "map";
const ALLOWED_VIEWS: ViewMode[] = ["list", "card", "map"];

const currentView = computed<ViewMode>(() => {
  const v = route.query.view as string | undefined;
  return ALLOWED_VIEWS.includes(v as ViewMode) ? (v as ViewMode) : "list";
});

function switchView(view: ViewMode) {
  if (view === currentView.value) return;
  router.push({ query: { ...route.query, view } });
}

// 抽屉:URL ?node= 决定是否打开;E1 实现具体内容
const drawerOpen = computed(() => !!route.query.node);
</script>

<template>
  <div class="dm-page-root">
    <!-- TopBar(C2 实现具体内容,此处先占位 chrome) -->
    <header class="dm-topbar">
      <div class="dm-topbar__left">
        <span class="dm-topbar__logo">UVP 设备管理</span>
      </div>
      <div class="dm-topbar__center">
        <div class="dm-view-switcher" role="tablist">
          <button
            v-for="v in ALLOWED_VIEWS"
            :key="v"
            role="tab"
            :aria-selected="currentView === v"
            :class="['dm-view-switcher__btn', { active: currentView === v }]"
            @click="switchView(v)"
          >
            {{ v === "list" ? "列表" : v === "card" ? "卡片" : "地图" }}
          </button>
        </div>
      </div>
      <div class="dm-topbar__right">
        <!-- 筛选 / 新建 / 设置 留 C2 / C4 -->
      </div>
    </header>

    <!-- 工作区:目录树 + 主视图 + 可选抽屉 -->
    <section :class="['dm-workspace', { 'with-drawer': drawerOpen }]">
      <aside class="dm-aside">
        <!-- C2 DirectoryAside 进来这里 -->
        <div class="dm-aside__placeholder">目录树(C2)</div>
      </aside>

      <main class="dm-main">
        <KeepAlive>
          <component :is="`view-${currentView}`">
            <div class="dm-view-placeholder">{{ currentView }} 视图(C3/D1/D3)</div>
          </component>
        </KeepAlive>
      </main>

      <aside v-if="drawerOpen" class="dm-drawer">
        <!-- E1 DetailDrawer 进来这里 -->
        <div class="dm-drawer__placeholder">详情抽屉(E1)<br />node={{ route.query.node }}</div>
      </aside>
    </section>
  </div>
</template>

<style lang="scss" scoped>
@use "@/style/var/index.scss" as *;

.dm-topbar {
  height: var(--topbar-height);
  background: var(--color-bg-2);
  border-bottom: 1px solid var(--color-border-2);
  display: grid;
  grid-template-columns: 280px 1fr 280px;
  align-items: center;
  padding: 0 var(--space-4);
  overflow: hidden;

  &__logo {
    font-size: var(--font-16);
    font-weight: 600;
    color: var(--color-text-1);
    letter-spacing: 0.02em;
  }

  &__center {
    display: flex;
    justify-content: center;
  }
}

.dm-view-switcher {
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
  overflow: auto; // plan §2.2 子区域各自滚动
}

.dm-aside {
  background: var(--color-bg-2);
  border-right: 1px solid var(--color-border-2);

  &__placeholder {
    padding: var(--space-4);
    color: var(--color-text-4);
    font-size: var(--font-12);
  }
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
  }
}

.dm-view-placeholder {
  color: var(--color-text-4);
  text-align: center;
  margin-top: 30vh;
  font-size: var(--font-16);
}
</style>
