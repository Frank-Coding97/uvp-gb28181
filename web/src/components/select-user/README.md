# SelectUser 用户选择组件

一个功能完善的用户选择组件，支持单选和多选模式，提供弹窗选择、标签展示、搜索过滤等功能。

## 功能特性

- ✅ 支持单选/多选模式切换
- ✅ 标签组展示已选用户，标签可关闭
- ✅ 弹窗分页展示用户列表
- ✅ 支持按用户名/昵称搜索
- ✅ 已选用户高亮标记
- ✅ 点击行快速选中/取消
- ✅ 一键清空所有选中
- ✅ 禁用状态支持

## 基础用法

### 单选模式

单选模式下，绑定的值为用户 ID（数值类型），选中一个用户后会自动替换之前的选中。

```vue
<template>
  <select-user v-model="userId" />
</template>

<script setup lang="ts">
import { ref } from 'vue';

const userId = ref(0);
</script>
```

### 多选模式

多选模式下，绑定的值为逗号分隔的用户 ID 字符串，支持累加选择。

```vue
<template>
  <select-user v-model="userIds" :multiple="true" />
</template>

<script setup lang="ts">
import { ref } from 'vue';

const userIds = ref('');
</script>
```

### 禁用状态

```vue
<template>
  <select-user v-model="userId" :disabled="true" />
</template>
```

### 自定义占位符

```vue
<template>
  <select-user v-model="userId" placeholder="请选择负责人" />
</template>
```

### 自定义标签文本长度

```vue
<template>
  <!-- 标签最多显示 15 个字符，超出显示省略号 -->
  <select-user v-model="userId" :max-label-length="15" />
</template>
```

> **注意**: 鼠标悬停在标签上时会显示完整的用户名

## API

### Props

| 属性 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| modelValue | `number \| string \| undefined` | - | **必填**。绑定值，单选时为数值，多选时为逗号分隔的字符串，支持 `undefined` |
| multiple | `boolean` | `false` | 是否多选模式 |
| disabled | `boolean` | `false` | 是否禁用 |
| placeholder | `string` | `'请选择用户'` | 占位符文本 |
| maxLabelLength | `number` | `10` | 标签文本最大长度，超出显示省略号 |

### Events

| 事件名 | 说明 | 参数类型 |
|--------|------|----------|
| update:modelValue | 绑定值变化时触发 | `number \| string \| undefined` |

## 数据格式说明

### 单选模式

```typescript
// 绑定值类型
const userId: number | undefined = 123;

// 未选中时
const userId: number | undefined = 0;
// 或者
const userId: number | undefined = undefined;
```

### 多选模式

```typescript
// 绑定值类型（逗号分隔的字符串）
const userIds: string | undefined = '123,456,789';

// 未选中时
const userIds: string | undefined = '';
// 或者
const userIds: string | undefined = undefined;
```

## 完整示例

```vue
<template>
  <a-form :model="form">
    <a-form-item label="负责人（单选）">
      <select-user v-model="form.managerId" placeholder="请选择负责人" />
    </a-form-item>

    <a-form-item label="团队成员（多选）">
      <select-user 
        v-model="form.memberIds" 
        :multiple="true" 
        placeholder="请选择团队成员"
      />
    </a-form-item>

    <a-form-item label="审批人（禁用）">
      <select-user 
        v-model="form.approverId" 
        :disabled="true" 
        placeholder="不可选择"
      />
    </a-form-item>
  </a-form>
</template>

<script setup lang="ts">
import { reactive } from 'vue';

const form = reactive({
  managerId: 0,           // 单选，初始未选中
  memberIds: '',          // 多选，初始未选中
  approverId: 123,        // 单选，已选中用户 123
  optionalId: undefined   // 单选，使用 undefined 表示未选中
});
</script>
```

## 组件结构

```
select-user/
├── index.vue       # 主组件文件
└── README.md       # 使用文档
```

## 依赖说明

- 依赖 `@/api/user` 中的 `getAccountListAPI` 获取用户列表
- 使用 Arco Design Vue 组件库的 `a-tag`、`a-button`、`a-modal`、`a-table`、`a-pagination` 等组件

## 注意事项

1. **undefined 支持**: 组件完全支持 `modelValue` 为 `undefined` 的情况，可以安全地用于可选字段
2. **数据同步**: 组件会自动根据用户 ID 获取用户详细信息（用户名、昵称）进行展示
3. **性能优化**: 用户列表采用分页加载，避免一次性加载大量数据
4. **状态管理**: 弹窗中的选中操作是临时的，只有点击"确定"按钮后才会更新绑定值
5. **搜索功能**: 搜索支持模糊匹配用户名称

## 更新日志

### v1.0.0 (2024-02-09)

- 初始版本发布
- 支持单选/多选模式
- 支持弹窗选择、标签展示
- 支持搜索和分页功能
