# SExcelReader Excel 文件读取组件

一个支持拖拽和点击选择 Excel 文件的 Vue 3 组件，可以读取 Excel 文件中的数据并通过事件向外发送。

## 功能特性

- 📁 支持点击选择文件
- 🖱️ 支持拖拽上传文件
- 📊 自动读取所有工作表数据
- 📝 自动提取表头信息
- 🔄 支持单文件和多文件模式
- ⚡ 完整的 TypeScript 类型支持
- 🎨 基于 Arco Design 的 UI 设计

## 安装依赖

组件依赖 `xlsx` 库来解析 Excel 文件，已安装：

```bash
pnpm add xlsx
pnpm add -D @types/xlsx
```

## 基础用法

### 单文件模式（默认）

```vue
<template>
  <s-excel-reader
    title="上传 Excel 文件"
    description="支持 .xlsx、.xls 格式"
    @success="handleSuccess"
    @error="handleError"
    @loading="handleLoading"
  />
</template>

<script setup lang="ts">
import SExcelReader from '@/components/s-excel-reader/index.vue';

interface ExcelSheetData {
  sheetName: string;
  data: Record<string, any>[];
  headers: string[];
}

const handleSuccess = (data: ExcelSheetData) => {
  console.log('读取成功:', data);
  // data.sheetName: 工作表名称
  // data.data: 数据数组（每行是一个对象，键为表头）
  // data.headers: 表头数组
};

const handleError = (error: Error) => {
  console.error('读取失败:', error);
};

const handleLoading = (isLoading: boolean) => {
  console.log('加载状态:', isLoading);
};
</script>
```

### 多文件模式

```vue
<template>
  <s-excel-reader
    :multiple="true"
    @success="handleMultipleSuccess"
  />
</template>

<script setup lang="ts">
const handleMultipleSuccess = (data: ExcelSheetData[]) => {
  console.log('读取成功，共', data.length, '个文件');
  data.forEach((sheetData, index) => {
    console.log(`文件 ${index + 1}:`, sheetData);
  });
};
</script>
```

### 通过 ref 触发上传

```vue
<template>
  <div>
    <a-button @click="triggerUpload">选择文件</a-button>
    <s-excel-reader ref="excelReaderRef" @success="handleSuccess" />
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue';

const excelReaderRef = ref();

const triggerUpload = () => {
  excelReaderRef.value?.triggerUpload();
};

const handleSuccess = (data: ExcelSheetData) => {
  console.log('读取成功:', data);
};
</script>
```

## Props

| 参数 | 说明 | 类型 | 默认值 |
|------|------|------|--------|
| title | 上传区域标题 | `string` | '点击或拖拽上传 Excel 文件' |
| description | 上传区域描述 | `string` | '支持 .xlsx、.xls 格式' |
| multiple | 是否支持多文件上传 | `boolean` | `false` |
| readAsArray | 是否读取为数组格式（保留） | `boolean` | `false` |

## Events

| 事件名 | 说明 | 回调参数 |
|--------|------|----------|
| success | 文件读取成功时触发 | `ExcelSheetData \| ExcelSheetData[]` |
| error | 文件读取失败时触发 | `Error` |
| loading | 加载状态变化时触发 | `boolean` |

## 数据结构

### ExcelSheetData

```typescript
interface ExcelSheetData {
  sheetName: string;        // 工作表名称
  data: Record<string, any>[];  // 数据数组
  headers: string[];        // 表头数组
}
```

### 数据示例

假设 Excel 文件内容如下：

| 姓名 | 年龄 | 部门 |
|------|------|------|
| 张三 | 25 | 技术部 |
| 李四 | 30 | 产品部 |

读取后的数据结构：

```javascript
{
  sheetName: "Sheet1",
  headers: ["姓名", "年龄", "部门"],
  data: [
    { "姓名": "张三", "年龄": 25, "部门": "技术部" },
    { "姓名": "李四", "年龄": 30, "部门": "产品部" }
  ]
}
```

## 样式定制

组件使用了 Arco Design 的 CSS 变量，可以通过修改这些变量来定制样式：

```scss
.s-excel-reader {
  // 修改边框颜色
  border-color: var(--color-border-2);
  
  // 修改背景色
  background-color: var(--color-fill-1);
  
  // 修改主色调
  --primary-6: rgb(var(--primary-6));
}
```

## 注意事项

1. 组件只支持 `.xlsx` 和 `.xls` 格式的 Excel 文件
2. 默认将 Excel 的第一行作为表头
3. 多文件模式下，会读取所有文件的所有工作表
4. 空的工作表会被跳过
5. 大文件读取可能会有性能问题，建议限制文件大小

## License

MIT
