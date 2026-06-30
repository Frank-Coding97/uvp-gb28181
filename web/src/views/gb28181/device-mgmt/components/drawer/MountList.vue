<script setup lang="ts">
/**
 * MountList — 通道多挂载位置列表(E2)
 *
 * 来源:GET /channel/:id/mounts(B2)
 * 主挂载青色描边 + "主"徽章;路径用 › 分隔,可点击跳转到对应节点
 */
import { useRoute, useRouter } from "vue-router";
import type { ChannelMountVO } from "../../api/device";

interface Props {
  mounts: ChannelMountVO[];
}

defineProps<Props>();

const route = useRoute();
const router = useRouter();

function gotoNode(parentNodeId: number) {
  router.push({
    query: { ...route.query, node: String(parentNodeId), drawerType: "node" },
  });
}

function formatPath(path: string): string[] {
  return path.split("/").filter(Boolean);
}
</script>

<template>
  <div class="mount-list">
    <div v-if="mounts.length === 0" class="empty">未挂载到任何目录节点</div>
    <ul v-else class="list">
      <li v-for="m in mounts" :key="m.id" :class="['item', { primary: m.isPrimary }]">
        <div class="row1">
          <span class="name">{{ m.displayName || m.parentName }}</span>
          <span v-if="m.isPrimary" class="badge">主</span>
          <span class="src" :title="`来源:${m.mountSource}`">{{ m.mountSource }}</span>
        </div>
        <div class="row2">
          <span
            v-for="(seg, idx) in formatPath(m.parentPath)"
            :key="idx"
            class="seg"
            :title="seg"
            @click.stop="gotoNode(Number(seg))"
          >
            {{ seg }}
            <span v-if="idx < formatPath(m.parentPath).length - 1" class="sep">›</span>
          </span>
        </div>
      </li>
    </ul>
  </div>
</template>

<style lang="scss" scoped>
.mount-list {
  .empty {
    color: var(--color-text-4);
    font-size: var(--font-12);
    padding: var(--space-3);
    background: var(--color-bg-3);
    border-radius: var(--dm-radius-1);
  }

  .list {
    list-style: none;
    padding: 0;
    margin: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .item {
    padding: var(--space-3);
    background: var(--color-bg-3);
    border-radius: var(--dm-radius-2);
    border: 1px solid transparent;

    &.primary {
      border-color: rgba(6, 182, 212, 0.4);
    }

    .row1 {
      display: flex;
      align-items: center;
      gap: var(--space-2);
      margin-bottom: 4px;

      .name {
        flex: 1;
        color: var(--color-text-1);
        font-weight: 500;
        font-size: var(--font-13);
      }

      .badge {
        padding: 1px 8px;
        background: var(--node-mount);
        color: var(--color-bg-1);
        border-radius: var(--dm-radius-pill);
        font-size: 10px;
        font-weight: 600;
      }

      .src {
        font-size: 10px;
        color: var(--color-text-4);
        text-transform: uppercase;
      }
    }

    .row2 {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: 2px;
      font-family: var(--font-mono);
      font-size: 11px;
      color: var(--color-text-4);

      .seg {
        cursor: pointer;
        transition: color var(--duration-fast) var(--ease-out);

        &:hover {
          color: var(--color-text-2);
        }
      }

      .sep {
        margin: 0 4px;
        color: var(--color-border-3);
      }
    }
  }
}
</style>
