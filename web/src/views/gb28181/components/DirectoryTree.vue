<script setup lang="ts">
import { onMounted, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import {
    fetchDirectoryTree,
    type DirectoryDimension,
    type DirectoryNode
} from "@/api/gb28181";

/**
 * 通用目录树组件(3 个维度共用)
 *
 * 职责:
 *  1. 首次挂载:拉根节点
 *  2. 点击展开:懒加载子节点(缓存到 childrenCache)
 *  3. 选中叶子:emit select(通道 / 未分配桶节点)
 *  4. 空态:通过 empty slot 让父组件自定义空引导
 *
 * 使用:
 *   <DirectoryTree dimension="native" @select="onSelect" />
 *   <DirectoryTree dimension="biz_group" :with-counts="true" />
 */

interface Props {
    /** 维度(必填) */
    dimension: DirectoryDimension;
    /** 是否附加统计数(挂载数/通道数) */
    withCounts?: boolean;
    /** 只允许选中通道类型(叶子)才 emit select,默认 true */
    onlySelectChannels?: boolean;
}

const props = withDefaults(defineProps<Props>(), {
    withCounts: false,
    onlySelectChannels: true
});

const emit = defineEmits<{
    /** 选中节点(默认只在选中通道叶子时 emit) */
    select: [node: DirectoryNode];
    /** 空态触发(首次拉数据后 list 空) */
    empty: [];
}>();

const treeData = ref<DirectoryNode[]>([]);
const loading = ref(false);
// 子节点缓存: parentID → DirectoryNode[]
const childrenCache = ref<Record<string, DirectoryNode[]>>({});

/**
 * 加载根节点(初次或维度切换)
 */
async function loadRoots() {
    loading.value = true;
    try {
        const res: any = await fetchDirectoryTree({
            dimension: props.dimension,
            withCounts: props.withCounts
        });
        treeData.value = res.data?.list || [];
        childrenCache.value = {};
        if (treeData.value.length === 0) {
            emit("empty");
        }
    } catch (err: any) {
        Message.error(`加载目录失败: ${err.message || err}`);
    } finally {
        loading.value = false;
    }
}

/**
 * 懒加载:节点展开时拉子节点
 */
async function onLoadMore(node: DirectoryNode): Promise<void> {
    if (!node.id || node.isLeaf) return;
    if (childrenCache.value[node.id]) return;
    try {
        const res: any = await fetchDirectoryTree({
            dimension: props.dimension,
            parentId: node.id,
            withCounts: props.withCounts
        });
        childrenCache.value[node.id] = res.data?.list || [];
    } catch (err: any) {
        Message.error(`加载子节点失败: ${err.message || err}`);
    }
}

/**
 * 选中节点(a-tree select 事件)
 */
function onSelect(selectedKeys: (string | number)[]) {
    if (!selectedKeys.length) return;
    const node = findNodeById(String(selectedKeys[0]));
    if (!node) return;
    if (props.onlySelectChannels && node.nodeType !== "channel") return;
    emit("select", node);
}

/**
 * 递归查找节点(在 treeData 和缓存里找)
 */
function findNodeById(id: string): DirectoryNode | null {
    for (const n of treeData.value) {
        if (n.id === id) return n;
    }
    for (const parentID of Object.keys(childrenCache.value)) {
        const cached = childrenCache.value[parentID];
        for (const n of cached) {
            if (n.id === id) return n;
        }
    }
    return null;
}

/**
 * 转 a-tree 所需的 nodeData(挂 children)
 */
function toArcoTreeData(): any[] {
    return treeData.value.map((n) => wrapNode(n));
}

function wrapNode(n: DirectoryNode): any {
    const children = childrenCache.value[n.id];
    return {
        key: n.id,
        title: n.name,
        isLeaf: n.isLeaf,
        // 只有已展开过的节点才有 children,其他节点靠 load-more 拉取
        ...(children ? { children: children.map((c) => wrapNode(c)) } : {}),
        // 存原始 node 供 select 事件使用
        _raw: n
    };
}

/**
 * 对外暴露刷新方法(父组件用 autoRefresh 触发)
 */
defineExpose({
    reload: loadRoots
});

// 监听维度变化(理论上父组件用 v-if 切换会重挂,但保险起见)
watch(
    () => props.dimension,
    () => {
        loadRoots();
    }
);

onMounted(loadRoots);
</script>

<template>
    <div class="directory-tree">
        <a-spin :loading="loading" class="tree-spin">
            <template v-if="treeData.length > 0">
                <a-tree
                    :data="toArcoTreeData()"
                    :load-more="onLoadMore"
                    :default-expanded-keys="[]"
                    show-line
                    @select="onSelect"
                >
                    <template #title="nodeData">
                        <span
                            class="node-title"
                            :class="`node-${nodeData._raw?.nodeType || 'unknown'}`"
                        >
                            {{ nodeData.title }}
                            <span
                                v-if="nodeData._raw?.channelCount"
                                class="count-badge"
                            >
                                ({{ nodeData._raw.channelCount }})
                            </span>
                        </span>
                    </template>
                </a-tree>
            </template>
            <template v-else-if="!loading">
                <slot name="empty">
                    <a-empty description="暂无数据" />
                </slot>
            </template>
        </a-spin>
    </div>
</template>

<style scoped>
.directory-tree {
    padding: 4px;
}

.tree-spin {
    display: block;
    width: 100%;
    min-height: 200px;
}

.node-title {
    font-size: 13px;
}

.node-channel {
    color: var(--color-text-2);
}

.node-device {
    font-weight: 500;
}

.node-civil_code {
    color: var(--color-text-1);
    font-weight: 500;
}

.node-biz_group {
    color: var(--color-primary-6);
}

.node-unassigned {
    color: var(--color-warning-6, #ff7d00);
    font-weight: 500;
}

.count-badge {
    color: var(--color-text-3);
    font-size: 12px;
    margin-left: 4px;
}
</style>
