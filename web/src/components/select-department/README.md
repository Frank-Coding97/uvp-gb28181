# SelectDepartment 部门选择组件

一个功能完善的部门选择组件，支持单选和多选模式，提供弹窗选择、标签展示、树形结构展示、搜索过滤等功能。

## 功能特性

- ✅ 支持单选/多选模式切换
- ✅ 标签组展示已选部门，标签可关闭
- ✅ 弹窗树形展示部门层级结构
- ✅ 支持按部门名称搜索
- ✅ 已选部门高亮标记
- ✅ 点击节点快速选中/取消
- ✅ 一键清空所有选中
- ✅ 禁用状态支持

## 基础用法

### 单选模式

单选模式下，绑定的值为部门 ID（数值类型），选中一个部门后会自动替换之前的选中。

```vue
<template>
  <select-department v-model="departmentId" />
</template>

<script setup lang="ts">
import { ref } from 'vue';

const departmentId = ref(0);
</script>
```

### 多选模式

多选模式下，绑定的值为逗号分隔的部门 ID 字符串，支持累加选择。

```vue
<template>
  <select-department v-model="departmentIds" :multiple="true" />
</template>

<script setup lang="ts">
import { ref } from 'vue';

const departmentIds = ref('');
</script>
```

### 禁用状态

```vue
<template>
  <select-department v-model="departmentId" :disabled="true" />
</template>
```

### 自定义占位符

```vue
<template>
  <select-department v-model="departmentId" placeholder="请选择所属部门" />
</template>
```

### 自定义标签文本长度

```vue
<template>
  <!-- 标签最多显示 15 个字符，超出显示省略号 -->
  <select-department v-model="departmentId" :max-label-length="15" />
</template>
```

> **注意**: 鼠标悬停在标签上时会显示完整的部门名称

## API

### Props

| 属性 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| modelValue | `number \| string \| undefined` | - | **必填**。绑定值，单选时为数值，多选时为逗号分隔的字符串，支持 `undefined` |
| multiple | `boolean` | `false` | 是否多选模式 |
| disabled | `boolean` | `false` | 是否禁用 |
| placeholder | `string` | `'请选择部门'` | 占位符文本 |
| maxLabelLength | `number` | `10` | 标签文本最大长度，超出显示省略号 |

### Events

| 事件名 | 说明 | 参数类型 |
|--------|------|----------|
| update:modelValue | 绑定值变化时触发 | `number \| string \| undefined` |

## 数据格式说明

### 单选模式

```typescript
// 绑定值类型
const departmentId: number | undefined = 1;

// 未选中时
const departmentId: number | undefined = 0;
// 或者
const departmentId: number | undefined = undefined;
```

### 多选模式

```typescript
// 绑定值类型（逗号分隔的字符串）
const departmentIds: string | undefined = '1,2,3';

// 未选中时
const departmentIds: string | undefined = '';
// 或者
const departmentIds: string | undefined = undefined;
```

## 完整示例

```vue
<template>
  <a-form :model="form">
    <a-form-item label="所属部门（单选）">
      <select-department v-model="form.departmentId" placeholder="请选择所属部门" />
    </a-form-item>

    <a-form-item label="管辖部门（多选）">
      <select-department 
        v-model="form.manageDeptIds" 
        :multiple="true" 
        placeholder="请选择管辖部门"
      />
    </a-form-item>

    <a-form-item label="部门（禁用）">
      <select-department 
        v-model="form.fixedDeptId" 
        :disabled="true" 
        placeholder="不可选择"
      />
    </a-form-item>
  </a-form>
</template>

<script setup lang="ts">
import { reactive } from 'vue';

const form = reactive({
  departmentId: 0,        // 单选，初始未选中
  manageDeptIds: '',      // 多选，初始未选中
  fixedDeptId: 1,         // 单选，已选中部门 1
  optionalId: undefined   // 单选，使用 undefined 表示未选中
});
</script>
```

## 组件结构

```
select-department/
├── index.vue       # 主组件文件
└── README.md       # 使用文档
```

## 依赖说明

- 依赖 `@/api/department` 中的 `getDivisionAPI` 获取部门树形数据
- 依赖 `@/api/department` 中的 `getDivisionByIdAPI` 获取单个部门详情
- 使用 Arco Design Vue 组件库的 `a-tag`、`a-button`、`a-modal`、`a-tree` 等组件

## 注意事项

1. **undefined 支持**: 组件完全支持 `modelValue` 为 `undefined` 的情况，可以安全地用于可选字段
2. **数据同步**: 组件会自动根据部门 ID 获取部门详细信息（部门名称）进行展示
3. **树形结构**: 部门数据以树形结构展示，支持展开/收起子部门
4. **状态管理**: 弹窗中的选中操作是临时的，只有点击"确定"按钮后才会更新绑定值
5. **搜索功能**: 搜索支持模糊匹配部门名称，自动过滤并展示匹配的节点及其父节点

## 与 SelectUser 组件的差异

| 特性 | SelectUser | SelectDepartment |
|------|-----------|------------------|
| 数据展示方式 | 表格 + 分页 | 树形结构 |
| 数据加载 | 分页加载 | 一次性加载全部 |
| 搜索范围 | 用户名称/昵称 | 部门名称 |
| 选择方式 | 点击行选中 | 点击节点选中/多选复选框 |

## 更新日志

### v1.0.0 (2024-02-09)

- 初始版本发布
- 支持单选/多选模式
- 支持弹窗选择、标签展示
- 支持树形结构展示和搜索功能
