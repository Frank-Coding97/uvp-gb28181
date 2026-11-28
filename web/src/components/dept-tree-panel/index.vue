<script setup lang="ts">
import { computed, nextTick, ref, watch } from "vue";
import { Building2, ChevronDown, Folder, FolderOpen } from "@lucide/vue";
import type { DivisionItem } from "@/api/department";
import { filterDeptTree } from "./filter";

/**
 * 左侧「组织部门」树面板（账号管理 / 设备权限工作台共用）。
 *
 * 只负责展示与搜索：面板头、关键字过滤、默认全展开、图标分色、行内操作扩展位。
 * 「选中后怎么查列表」「行内放什么操作」由使用方决定，组件不碰业务。
 */
const props = withDefaults(
  defineProps<{
    /** 部门树数据（`sysDepartment/getDivision` 原样返回的 list）。 */
    data: DivisionItem[];
    /** 面板标题。 */
    title?: string;
    /** 搜索框占位文案。 */
    placeholder?: string;
    /** 是否展示搜索框。 */
    searchable?: boolean;
    /** 受控选中项（单选语义，取第一个）。 */
    selectedKeys?: Array<string | number>;
    /** 无部门数据时的文案。 */
    emptyText?: string;
    /** 搜索无命中时的文案。 */
    noMatchText?: string;
  }>(),
  {
    title: "组织部门",
    placeholder: "请输入部门名称",
    searchable: true,
    selectedKeys: () => [],
    emptyText: "暂无部门数据",
    noMatchText: "没有匹配的部门"
  }
);

const emit = defineEmits<{
  "update:selectedKeys": [keys: Array<string | number>];
  select: [keys: Array<string | number>, node: DivisionItem | undefined];
}>();

defineSlots<{
  /** 面板头右侧扩展位（统计、刷新按钮等）。 */
  "head-extra"?: () => unknown;
  /** 行内操作位，默认 hover / 聚焦才显示（如「分配本部门设备」）。 */
  "node-extra"?: (props: { node: DivisionItem }) => unknown;
}>();

const treeRef = ref<{ expandAll?: () => void }>();
const keyword = ref("");
const fieldNames = { key: "id", title: "name", children: "children" };

const treeData = computed(() => filterDeptTree(props.data, keyword.value));

const nodeById = computed(() => {
  const map = new Map<number, DivisionItem>();
  const walk = (nodes: DivisionItem[]) => {
    for (const node of nodes) {
      map.set(node.id, node);
      if (node.children?.length) walk(node.children);
    }
  };
  walk(props.data);
  return map;
});

// ⛔ `default-expand-all` 只在首次挂载时算一次：数据后到（接口异步）或搜索命中深层节点时
// 必须补一次 expandAll，否则命中的节点藏在折叠节点里，看着像"搜不到"。
const expandAll = () => nextTick(() => treeRef.value?.expandAll?.());
watch(() => props.data, expandAll, { flush: "post" });
watch(keyword, expandAll);

const onSelect = (keys: Array<string | number>) => {
  emit("update:selectedKeys", keys);
  emit("select", keys, keys.length ? nodeById.value.get(Number(keys[0])) : undefined);
};

/** 清空搜索框（页面「重置」用）。 */
const clearSearch = () => {
  keyword.value = "";
};

defineExpose({ expandAll, clearSearch, keyword });
</script>

<template>
  <div class="uvp-tree-panel dept-tree-panel">
    <div class="uvp-tree-panel__head">
      <span class="uvp-tree-panel__title">{{ title }}</span>
      <slot name="head-extra" />
    </div>
    <a-input v-if="searchable" v-model="keyword" class="uvp-tree-panel__search" :placeholder="placeholder" allow-clear>
      <template #prefix>
        <icon-search />
      </template>
    </a-input>
    <div class="uvp-tree-panel__body">
      <a-tree
        v-if="data.length"
        ref="treeRef"
        :data="treeData"
        :field-names="fieldNames"
        :selected-keys="selectedKeys"
        default-expand-all
        show-line
        @select="onSelect"
      >
        <template #icon="{ isLeaf, expanded }">
          <component
            :is="isLeaf ? Building2 : expanded ? FolderOpen : Folder"
            :class="['uvp-tree-node-icon', isLeaf ? 'uvp-tree-node-icon--leaf' : 'uvp-tree-node-icon--branch']"
            :size="15"
            :stroke-width="2"
          />
        </template>
        <template #switcher-icon>
          <ChevronDown class="arco-tree-node-switcher-icon" :size="13" :stroke-width="2.2" />
        </template>
        <template #title="node">
          <div class="dept-tree-node">
            <span class="dept-tree-node__name">{{ node.name }}</span>
            <span v-if="$slots['node-extra']" class="dept-tree-node__extra">
              <slot name="node-extra" :node="node" />
            </span>
          </div>
        </template>
      </a-tree>
      <div v-else class="dept-tree-empty">{{ emptyText }}</div>
      <div v-if="data.length && treeData.length === 0" class="dept-tree-empty">{{ noMatchText }}</div>
    </div>
  </div>
</template>

<style scoped lang="scss">
// 目录型节点（可展开）与末级部门用两种颜色区分层级；hover/选中变强调色由
// uvp-ui-language.scss 的 `.uvp-tree-panel` 公共规则接管（优先级更高，无需在这里重复）。
.dept-tree-panel :deep(.uvp-tree-node-icon--branch) {
  color: #c47a18;
}

.dept-tree-panel :deep(.uvp-tree-node-icon--leaf) {
  color: #0f8b83;
}

.dept-tree-node {
  display: inline-flex;
  gap: 6px;
  align-items: center;
  cursor: pointer;

  &__name {
    font-size: 13px;
  }

  // ⛔ 行内操作默认隐藏，hover / 键盘聚焦才出现：常显会把部门名挤成省略号。
  &__extra {
    display: none;
    font-size: 12px;
  }
}

.dept-tree-node:hover .dept-tree-node__extra,
.dept-tree-node:focus-within .dept-tree-node__extra {
  display: inline;
}

.dept-tree-empty {
  padding: 24px 8px;
  font-size: 13px;
  color: var(--uvp-text-tertiary);
  text-align: center;
}
</style>
