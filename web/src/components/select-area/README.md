# SelectArea 地区选择组件

基于 Arco Design Vue 的 `a-cascader` 封装的地区选择组件，支持省/市/区/县/乡镇四级联动选择。

## 特性

- 🌳 支持省/市/区/县/乡镇四级联动选择
- 🔍 支持搜索地区名称和编码
- 💾 内置数据缓存机制，避免重复请求
- 🔄 支持双向绑定（v-model）
- ✅ 支持单选和多选模式
- 🎨 完全兼容 Arco Design Vue 样式规范
- ⚡ TypeScript 支持

## 组件说明

本目录包含两个组件：

| 组件               | 文件                           | 说明                                   |
| ------------------ | ------------------------------ | -------------------------------------- |
| SelectArea         | [`index.vue`](index.vue)       | 单选模式组件，绑定值为逗号分隔的字符串 |
| SelectAreaMultiple | [`multiple.vue`](multiple.vue) | 多选模式组件，绑定值为数组             |

## 单选组件 (SelectArea)

### 基础用法

```vue
<template>
  <select-area v-model="areaCode" />
</template>

<script setup lang="ts">
import { ref } from "vue";
import SelectArea from "@/components/select-area/index.vue";

const areaCode = ref("");
</script>
```

### API

#### Props

| 参数       | 说明                             | 类型                  | 默认值      |
| ---------- | -------------------------------- | --------------------- | ----------- |
| modelValue | 绑定值，逗号分隔的地区编码字符串 | `string \| undefined` | `undefined` |
| level      | 地区选择级数                     | `number`              | `3`         |

#### Events

| 事件名            | 说明         | 参数              |
| ----------------- | ------------ | ----------------- |
| update:modelValue | 值变化时触发 | `(value: string)` |

## 多选组件 (SelectAreaMultiple)

### 基础用法

```vue
<template>
  <select-area-multiple v-model="areaCodes" />
</template>

<script setup lang="ts">
import { ref } from "vue";
import SelectAreaMultiple from "@/components/select-area/multiple.vue";

const areaCodes = ref<string[]>([]);
</script>
```

### API

#### Props

| 参数       | 说明                 | 类型                    | 默认值      |
| ---------- | -------------------- | ----------------------- | ----------- |
| modelValue | 绑定值，地区编码数组 | `string[] \| undefined` | `undefined` |
| level      | 地区选择级数         | `number`                | `3`         |

#### Events

| 事件名            | 说明         | 参数                |
| ----------------- | ------------ | ------------------- |
| update:modelValue | 值变化时触发 | `(value: string[])` |

## 单选组件示例

### 基础用法

```vue
<template>
  <a-form>
    <a-form-item label="地区选择">
      <select-area v-model="areaCode" />
    </a-form-item>
  </a-form>
</template>

<script setup lang="ts">
import { ref } from "vue";
import SelectArea from "@/components/select-area/index.vue";

const areaCode = ref("");
</script>
```

### 带默认值

```vue
<template>
  <select-area v-model="areaCode" placeholder="请选择所在地区" />
</template>

<script setup lang="ts">
import { ref } from "vue";
import SelectArea from "@/components/select-area/index.vue";

// 设置默认值：北京市市辖区
const areaCode = ref("11,1101");
</script>
```

### 监听变化

```vue
<template>
  <select-area v-model="areaCode" @change="handleAreaChange" />
</template>

<script setup lang="ts">
import { ref } from "vue";
import SelectArea from "@/components/select-area/index.vue";
import type { AreaItem } from "@/api/area";

const areaCode = ref("");

const handleAreaChange = (value: string, selectedOptions: AreaItem[]) => {
  console.log("选中的地区编码:", value);
  console.log("选中的地区信息:", selectedOptions);
  // selectedOptions 示例:
  // [
  //   { value: '11', label: '北京市', level: '1', parent: '' },
  //   { value: '1101', label: '市辖区', level: '2', parent: '11' }
  // ]
};
</script>
```

### 禁用状态

```vue
<template>
  <select-area v-model="areaCode" :disabled="true" />
</template>
```

### 禁用清空

```vue
<template>
  <select-area v-model="areaCode" :clearable="false" />
</template>
```

### 禁用搜索

```vue
<template>
  <select-area v-model="areaCode" :allow-search="false" />
</template>
```

### 强制刷新数据

```vue
<template>
  <select-area ref="areaSelector" v-model="areaCode" />
  <a-button @click="refreshData">刷新数据</a-button>
</template>

<script setup lang="ts">
import { ref } from "vue";
import SelectArea from "@/components/select-area/index.vue";

const areaSelector = ref();
const areaCode = ref("");

const refreshData = () => {
  // 清除缓存并重新加载
  areaSelector.value?.clearCache();
  areaSelector.value?.reload();
};
</script>
```

## 数据格式

### 单选组件绑定值格式

单选组件的 `modelValue` 是一个逗号分隔的地区编码字符串：

```
省: "11"
市: "11,1101"
区/县: "11,1101,110101"
乡镇: "11,1101,110101,110101001"
```

### 多选组件绑定值格式

多选组件的 `modelValue` 是一个地区编码数组，每个元素是完整的地区路径：

```typescript
// 示例：选中了北京市市辖区和东城区
["11,1101", "11,1101,110101"][
  // 示例：选中了多个不同地区的完整路径
  ("11,1101,110101", "31,3101,310101", "44,4401,440101")
];
```

**注意**：多选组件的绑定值不需要任何转换，直接使用数组格式。支持四级联动选择（省/市/区/县/乡镇）。

### 地区数据结构

```typescript
interface AreaItem {
  value: string; // 地区编码
  label: string; // 地区名称
  level: string; // 级别（1:省/直辖市, 2:市, 3:区/县）
  parent: string; // 父级编码
  children?: AreaItem[]; // 子级地区
}
```

## 数据源

组件从以下地址获取地区数据：

```
import.meta.env.VITE_APP_BASE_URL + "/public/area/area.json"
```

数据格式示例：

```json
[
  {
    "value": "11",
    "label": "北京市",
    "level": "1",
    "parent": "",
    "children": [
      {
        "value": "1101",
        "label": "市辖区",
        "level": "2",
        "parent": "11",
        "children": [
          {
            "value": "110101",
            "label": "东城区",
            "level": "3",
            "parent": "1101",
            "children": []
          }
        ]
      }
    ]
  }
]
```

## 缓存机制

组件实现了两级缓存机制：

1. **内存缓存**：首次加载后，数据保存在内存中，后续请求直接返回缓存
2. **Promise 缓存**：防止并发请求，多个组件同时加载时只发起一次请求

如需强制刷新数据，可调用 `clearCache()` 和 `reload()` 方法。

## 多选组件示例

### 基础多选

```vue
<template>
  <select-area v-model="areaCode" placeholder="请选择所在地区" />
</template>

<script setup lang="ts">
import { ref } from "vue";
import SelectArea from "@/components/select-area/index.vue";

// 设置默认值：北京市市辖区
const areaCode = ref("11,1101");
</script>
```

#### 自定义选择级数

```vue
<template>
  <select-area v-model="provinceCode" :level="1" placeholder="请选择省份" />
</template>

<script setup lang="ts">
import { ref } from "vue";
import SelectArea from "@/components/select-area/index.vue";

const provinceCode = ref("");
</script>
```

### 多选组件示例

#### 基础多选

```vue
<template>
  <a-form>
    <a-form-item label="地区选择（可多选）">
      <select-area-multiple v-model="selectedAreas" />
    </a-form-item>
  </a-form>
</template>

<script setup lang="ts">
import { ref } from "vue";
import SelectAreaMultiple from "@/components/select-area/multiple.vue";

const selectedAreas = ref<string[]>([]);
</script>
```

#### 带默认值

```vue
<template>
  <select-area-multiple v-model="areaCodes" placeholder="请选择多个地区" />
</template>

<script setup lang="ts">
import { ref } from "vue";
import SelectAreaMultiple from "@/components/select-area/multiple.vue";

// 设置默认值：北京市市辖区和东城区
const areaCodes = ref<string[]>(["11,1101", "11,1101,110101"]);
</script>
```

#### 自定义选择级数

```vue
<template>
  <select-area-multiple v-model="cityCodes" :level="2" placeholder="请选择城市（最多到市级）" />
</template>

<script setup lang="ts">
import { ref } from "vue";
import SelectAreaMultiple from "@/components/select-area/multiple.vue";

const cityCodes = ref<string[]>([]);
</script>
```

#### 表单提交

```vue
<template>
  <a-form @submit="handleSubmit">
    <a-form-item label="负责地区" field="areas">
      <select-area-multiple v-model="form.areas" />
    </a-form-item>
    <a-button type="primary" html-type="submit">提交</a-button>
  </a-form>
</template>

<script setup lang="ts">
import { ref } from "vue";
import { Message } from "@arco-design/web-vue";
import SelectAreaMultiple from "@/components/select-area/multiple.vue";

const form = ref({
  areas: [] as string[]
});

const handleSubmit = () => {
  console.log("提交的地区数据:", form.value.areas);
  // 输出示例: ['11,1101,110101', '31,3101,310101']
  Message.success("提交成功");
};
</script>
```

## 注意事项

1. 组件需要服务器端提供 `/public/area/area.json` 接口
2. 数据量较大，首次加载可能需要一些时间
3. 组件使用 `path-mode` 模式，返回完整的地区路径编码
4. 支持选择任意级别的地区（省、市、区/县、乡镇）
5. 单选组件绑定值为字符串（逗号分隔），多选组件绑定值为数组
6. 多选组件的值无需转换，直接使用数组格式即可
7. 支持四级联动选择，可通过 `level` 属性控制选择级数（1-4级）
