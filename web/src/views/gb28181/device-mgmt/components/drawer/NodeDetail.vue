<script setup lang="ts">
/**
 * NodeDetail — 目录节点详情(E3)
 *
 * 适配 3 种 node_type:civil_code / biz_group / virtual_org
 *
 * 来源:GET /catalog/tree/:id(单节点) + /catalog/tree/:id/subtree(下属树)
 *
 * 渲染:
 *   1. 节点元信息(类型 / 编码 / 行政区 / 物化路径 / anomaly)
 *   2. 派生:子树设备数 / 通道数 / 在线数(从 subtree 列表算)
 *   3. 子节点 mini 列表(深度限制 1 级,展开二级时跳目录树本身)
 */
import { computed, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { getCatalogNode, getCatalogSubtree, type CatalogNode } from "../../api/catalog";
import AnomalyFlag from "../shared/AnomalyFlag.vue";

interface Props {
  nodeId: number;
}

const props = defineProps<Props>();

const route = useRoute();
const router = useRouter();

const node = ref<CatalogNode | null>(null);
const subtree = ref<CatalogNode[]>([]);
const loading = ref(false);
const err = ref<string | null>(null);

async function load() {
  loading.value = true;
  err.value = null;
  node.value = null;
  subtree.value = [];
  try {
    const [nR, sR] = await Promise.all([getCatalogNode(props.nodeId), getCatalogSubtree(props.nodeId)]);
    node.value = nR?.data ?? null;
    subtree.value = sR?.data?.list ?? [];
  } catch (e) {
    err.value = e instanceof Error ? e.message : "加载失败";
  } finally {
    loading.value = false;
  }
}

onMounted(load);
watch(() => props.nodeId, load);

const stats = computed(() => {
  let deviceCount = 0;
  let channelCount = 0;
  let bizGroupCount = 0;
  for (const n of subtree.value) {
    if (n.id === props.nodeId) continue;
    if (n.nodeType === "device") deviceCount++;
    else if (n.nodeType === "channel") channelCount++;
    else if (n.nodeType === "biz_group") bizGroupCount++;
  }
  return { deviceCount, channelCount, bizGroupCount };
});

const directChildren = computed(() =>
  subtree.value.filter(n => n.parentId === props.nodeId).slice(0, 50)
);

const typeLabel = computed(() => {
  const v = node.value?.nodeType;
  const map = {
    civil_code: "行政区划",
    biz_group: "业务分组",
    virtual_org: "虚拟组织",
    device: "设备",
    channel: "通道",
  } as const;
  return v ? map[v] : "—";
});

function gotoChild(child: CatalogNode) {
  // 按 nodeType 切换 drawerType
  const dt =
    child.nodeType === "channel" ? "channel" : child.nodeType === "device" ? "device" : "node";
  router.push({ query: { ...route.query, node: String(child.id), drawerType: dt } });
}
</script>

<template>
  <div class="node-detail">
    <div v-if="err" class="banner banner-error">{{ err }}</div>
    <div v-if="loading && !node" class="loading">加载中…</div>

    <template v-else-if="node">
      <section class="group">
        <h4 class="group-title">节点</h4>
        <div class="field-grid">
          <div class="k">名称</div>
          <div class="v">
            {{ node.name }}
            <AnomalyFlag v-if="node.anomaly" :reason="node.anomalyReason" size="sm" />
          </div>
          <div class="k">类型</div>
          <div class="v">{{ typeLabel }}</div>
          <div class="k">编码</div>
          <div class="v dm-mono">{{ node.code || "—" }}</div>
          <div class="k" v-if="node.civilCode">行政区码</div>
          <div class="v dm-mono" v-if="node.civilCode">{{ node.civilCode }}</div>
          <div class="k">路径</div>
          <div class="v dm-mono">{{ node.path }}</div>
          <div class="k">深度</div>
          <div class="v">{{ node.depth }}</div>
          <div class="k">来源</div>
          <div class="v">{{ node.source }}</div>
          <div class="k" v-if="node.anomaly">兜底原因</div>
          <div class="v" v-if="node.anomaly">{{ node.anomalyReason }}</div>
          <div class="k" v-if="node.rawCode">原始编码</div>
          <div class="v dm-mono" v-if="node.rawCode">{{ node.rawCode }}</div>
        </div>
      </section>

      <section class="group">
        <h4 class="group-title">下属统计(整子树)</h4>
        <div class="stat-row">
          <div class="stat">
            <div class="stat-v">{{ stats.deviceCount }}</div>
            <div class="stat-l">设备</div>
          </div>
          <div class="stat">
            <div class="stat-v">{{ stats.channelCount }}</div>
            <div class="stat-l">通道</div>
          </div>
          <div class="stat">
            <div class="stat-v">{{ stats.bizGroupCount }}</div>
            <div class="stat-l">分组</div>
          </div>
        </div>
      </section>

      <section class="group">
        <h4 class="group-title">直接子节点({{ directChildren.length }})</h4>
        <ul v-if="directChildren.length" class="child-list">
          <li
            v-for="c in directChildren"
            :key="c.id"
            class="child-item"
            @click="gotoChild(c)"
          >
            <span :class="['type-tag', `t-${c.nodeType}`]">{{ c.nodeType.slice(0, 1).toUpperCase() }}</span>
            <span class="name">{{ c.name }}</span>
            <span v-if="c.anomaly" class="anomaly-mark" title="anomaly">⚠</span>
          </li>
        </ul>
        <p v-else class="empty">无下级节点</p>
      </section>
    </template>
  </div>
</template>

<style lang="scss" scoped>
.node-detail {
  display: flex;
  flex-direction: column;
  gap: var(--space-6);
}

.banner-error {
  padding: var(--space-2) var(--space-3);
  background: var(--status-danger-fade);
  color: var(--status-danger);
  border-radius: var(--dm-radius-1);
  font-size: var(--font-12);
}

.loading {
  color: var(--color-text-4);
  text-align: center;
  padding: var(--space-8) 0;
}

.group-title {
  font-size: var(--font-12);
  color: var(--color-text-4);
  font-weight: 500;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  margin-bottom: var(--space-3);
}

.field-grid {
  display: grid;
  grid-template-columns: 80px 1fr;
  row-gap: var(--space-2);
  column-gap: var(--space-3);
  font-size: var(--font-13);

  .k {
    color: var(--color-text-4);
  }

  .v {
    color: var(--color-text-2);
    display: flex;
    align-items: center;
    gap: var(--space-2);
    overflow-wrap: anywhere;
  }
}

.stat-row {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: var(--space-2);
}

.stat {
  background: var(--color-bg-3);
  padding: var(--space-3);
  border-radius: var(--dm-radius-2);
  text-align: center;

  .stat-v {
    font-size: var(--font-20);
    color: var(--color-text-1);
    font-family: var(--font-mono);
    font-weight: 600;
  }

  .stat-l {
    margin-top: 4px;
    font-size: var(--font-12);
    color: var(--color-text-4);
  }
}

.child-list {
  list-style: none;
  padding: 0;
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.child-item {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  height: 32px;
  padding: 0 var(--space-3);
  border-radius: var(--dm-radius-1);
  cursor: pointer;
  font-size: var(--font-13);
  transition: background var(--duration-fast) var(--ease-out);

  &:hover {
    background: var(--color-bg-3);
  }

  .name {
    flex: 1;
    color: var(--color-text-2);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .type-tag {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 18px;
    height: 18px;
    border-radius: var(--dm-radius-1);
    font-size: 10px;
    font-weight: 600;

    &.t-civil_code {
      background: rgba(167, 139, 250, 0.12);
      color: var(--node-civil);
    }
    &.t-biz_group {
      background: rgba(6, 182, 212, 0.12);
      color: var(--node-biz);
    }
    &.t-virtual_org {
      background: rgba(251, 146, 60, 0.12);
      color: var(--node-virtual);
    }
    &.t-device {
      background: var(--primary-fade-16);
      color: var(--node-device);
    }
    &.t-channel {
      background: var(--status-online-fade);
      color: var(--node-channel-on);
    }
  }

  .anomaly-mark {
    color: var(--status-warning);
    font-size: 12px;
  }
}

.empty {
  color: var(--color-text-4);
  font-size: var(--font-12);
}
</style>
