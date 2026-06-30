<script setup lang="ts">
/**
 * DetailDrawer — 右侧详情抽屉根(E1)
 *
 * URL ?node=<id>&drawerType=channel|device|node 控制打开 + 节点类型分发:
 *   drawerType=channel → ChannelDetail(E2,本期占位)
 *   drawerType=device  → DeviceDetail(E3,本期占位)
 *   其他 / 缺省          → NodeDetail(E3,本期占位)
 *
 * 内部 grid:hero / tabs / body(flex:1 overflow:auto) / sticky footer
 * 整体 height:100% + overflow:hidden,plan §2.2 踩坑兜底
 *
 * ESC 关闭
 */
import { computed, onMounted, onUnmounted } from "vue";
import { useRoute, useRouter } from "vue-router";
import ChannelDetail from "./ChannelDetail.vue";
import DeviceDetail from "./DeviceDetail.vue";
import NodeDetail from "./NodeDetail.vue";

const route = useRoute();
const router = useRouter();

const targetId = computed(() => {
  const v = route.query.node;
  if (typeof v !== "string") return null;
  const n = Number(v);
  return Number.isFinite(n) && n > 0 ? n : null;
});

const drawerType = computed<"channel" | "device" | "node">(() => {
  const v = route.query.drawerType;
  if (v === "channel" || v === "device") return v;
  return "node";
});

function close() {
  // 清掉 node / drawerType,保留视图/分页/筛选
  const { node, drawerType: _t, ...rest } = route.query;
  void node;
  void _t;
  router.replace({ query: rest });
}

function onKey(e: KeyboardEvent) {
  if (e.key === "Escape" && targetId.value != null) {
    close();
  }
}

onMounted(() => window.addEventListener("keydown", onKey));
onUnmounted(() => window.removeEventListener("keydown", onKey));
</script>

<template>
  <div v-if="targetId" class="detail-drawer">
    <header class="drawer-head">
      <div class="crumb">
        <span class="cur">{{ drawerType === "channel" ? "通道" : drawerType === "device" ? "设备" : "节点" }}</span>
        <span class="sep">·</span>
        <span class="id dm-mono">#{{ targetId }}</span>
      </div>
      <button class="close-btn" type="button" title="关闭(Esc)" @click="close">
        <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2">
          <line x1="18" y1="6" x2="6" y2="18" />
          <line x1="6" y1="6" x2="18" y2="18" />
        </svg>
      </button>
    </header>

    <section class="drawer-body">
      <ChannelDetail v-if="drawerType === 'channel'" :channel-id="targetId" />
      <DeviceDetail v-else-if="drawerType === 'device'" :device-id="targetId" />
      <NodeDetail v-else :node-id="targetId" />
    </section>

    <footer class="drawer-footer">
      <button v-if="drawerType === 'channel'" class="cta primary" type="button" disabled>
        <svg viewBox="0 0 24 24" width="14" height="14" fill="currentColor">
          <polygon points="6 4 20 12 6 20 6 4" />
        </svg>
        立即点播
      </button>
      <button v-else class="cta secondary" type="button" disabled>更多操作</button>
    </footer>
  </div>
</template>

<style lang="scss" scoped>
.detail-drawer {
  display: grid;
  grid-template-rows: auto 1fr auto;
  height: 100%;
  min-height: 0;
  overflow: hidden;
}

.drawer-head {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-3) var(--space-4);
  border-bottom: 1px solid var(--color-border-2);

  .crumb {
    flex: 1;
    display: flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--font-13);
    color: var(--color-text-3);

    .cur {
      color: var(--color-text-1);
      font-weight: 500;
    }

    .sep {
      color: var(--color-text-4);
    }

    .id {
      color: var(--color-text-3);
      font-size: 11px;
    }
  }

  .close-btn {
    width: 28px;
    height: 28px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: transparent;
    border: 0;
    border-radius: var(--dm-radius-1);
    color: var(--color-text-3);
    cursor: pointer;
    transition: background var(--duration-fast) var(--ease-out);

    &:hover {
      background: var(--color-bg-3);
      color: var(--color-text-1);
    }
  }
}

.drawer-body {
  overflow: auto;
  padding: var(--space-4);
  min-height: 0;
}

.placeholder {
  color: var(--color-text-3);
  line-height: var(--line-relaxed);

  h3 {
    font-size: var(--font-16);
    color: var(--color-text-1);
    margin-bottom: var(--space-2);
  }

  .muted {
    color: var(--color-text-4);
    font-size: var(--font-13);
  }
}

.drawer-footer {
  display: flex;
  gap: var(--space-2);
  padding: var(--space-3) var(--space-4);
  border-top: 1px solid var(--color-border-2);
  background: var(--color-bg-2);
  // plan §2.2 sticky 在 height:100vh + overflow:hidden 根下自然生效

  .cta {
    flex: 1;
    height: 36px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: var(--space-2);
    border: 0;
    border-radius: var(--dm-radius-2);
    font-size: var(--font-13);
    font-weight: 500;
    cursor: pointer;
    transition: background var(--duration-fast) var(--ease-out);

    &.primary {
      background: var(--primary-6);
      color: white;

      &:not(:disabled):hover {
        background: var(--primary-7);
      }
    }

    &.secondary {
      background: var(--color-bg-3);
      color: var(--color-text-2);
    }

    &:disabled {
      opacity: 0.5;
      cursor: not-allowed;
    }
  }
}
</style>
