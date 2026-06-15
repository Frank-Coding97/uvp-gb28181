<template>
    <div>
        <a-card>
            <a-row :gutter="24" justify="center">
                <a-col :span="8">
                    <span class="text-right-gap">展开全部</span>
                    <a-switch type="round" v-model="treeSwitch.expandAll" @change="onExpandAll">
                        <template #checked> 是 </template>
                        <template #unchecked> 否 </template>
                    </a-switch>
                </a-col>
                <a-col :span="8">
                    <span class="text-right-gap">全选节点</span>
                    <a-switch type="round" v-model="treeSwitch.selectAll" @change="onSelectAll">
                        <template #checked> 是 </template>
                        <template #unchecked> 否 </template>
                    </a-switch>
                </a-col>
                <a-col :span="8">
                    <a-tooltip content="权限树的父子节点独立，因为若节点关联，父节点会存在半选情况，半选节点的ID不会返回，会导致菜单无法渲染">
                        <span>父子关联 <icon-question-circle-fill /></span>
                    </a-tooltip>
                </a-col>
            </a-row>
        </a-card>
        <a-tree ref="treeRef" :fieldNames="{
            key: 'id',
            title: 'i18n',
            children: 'children'
        }" :check-strictly="true" :checkable="true" :show-line="true" :unmount-on-close="true"
            v-model:checked-keys="permissionKeys" :data="permissionTree" />
    </div>
</template>

<script lang="ts" setup>
import { ref, watch, onMounted } from 'vue'
import { getMenuListAPI, convertMenuItemsToRoutes, type ConvertedRouteItem } from '@/api/menu'
import useGlobalProperties from "@/hooks/useGlobalProperties";

const props = defineProps<{
    modelValue?: string // 逗号分隔的菜单ID字符串
}>()

const emit = defineEmits<{
    'update:modelValue': [value: string]
}>()

const proxy = useGlobalProperties();
const treeRef = ref()
const treeSwitch = ref({
    expandAll: true, // 展开全部
    selectAll: false // 全选
})
const permissionTree = ref<ConvertedRouteItem[]>([])
const permissionKeys = ref<number[]>([])

// 监听外部传入的 modelValue 变化，同步到 permissionKeys
watch(() => props.modelValue, (newVal) => {
    if (newVal) {
        permissionKeys.value = newVal.split(',').map(id => Number(id)).filter(id => !isNaN(id));
    } else {
        permissionKeys.value = [];
    }
}, { immediate: true })

// 监听 permissionKeys 变化，同步到外部
watch(permissionKeys, (newVal) => {
    emit('update:modelValue', newVal.join(','));
})

// 获取菜单树数据
const getMenuList = async () => {
    let { data } = await getMenuListAPI();
    let menuList = convertMenuItemsToRoutes(data);
    translation(menuList);
    permissionTree.value = menuList;
};

// 语言转化
const translation = (tree: any) => {
    tree.forEach((item: any) => {
        if (item.children) translation(item.children);
        if (item.meta.title) {
            item.i18n = proxy.$t(`menu.${item.meta.title}`);
        }
    });
};

// 展开全部
const onExpandAll = (state: boolean) => {
    treeRef.value.expandAll(state);
};

// 全选
const onSelectAll = (state: boolean) => {
    treeRef.value.checkAll(state);
};

// 重置节点操作开关
const treeSwitchReset = () => {
    treeSwitch.value = {
        expandAll: true, // 固定
        selectAll: false // 全选
    };
};

// 初始化菜单树数据
const initMenuTree = async () => {
    await getMenuList();
    treeRef.value?.expandAll(treeSwitch.value.expandAll);
};

// 暴露方法给父组件
defineExpose({
    initMenuTree,
    treeSwitchReset
})

onMounted(() => {
    initMenuTree();
})
</script>

<style scoped>
.text-right-gap {
    margin-right: 8px;
}
</style>