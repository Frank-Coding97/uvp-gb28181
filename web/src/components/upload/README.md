# 上传组件文档

## 组件概述

本目录提供了五个上传相关的组件：
- [`image-upload.vue`](./image-upload.vue) - 单图上传组件
- [`multi-image-upload.vue`](./multi-image-upload.vue) - 多图上传组件（照片墙形式）
- [`file-upload.vue`](./file-upload.vue) - 通用文件上传组件
- [`affix-selector.vue`](./affix-selector.vue) - 附件选择器组件
- [`chunk-upload.vue`](./chunk-upload.vue) - 分片上传组件（支持大文件断点续传）

---

## 附件选择器组件 (affix-selector.vue)

### 特性

- ✅ **图片库选择** - 从已上传的图片库中选择图片
- ✅ **单选/多选模式** - 支持单选和多选两种模式
- ✅ **搜索功能** - 支持按图片名称搜索
- ✅ **分页展示** - 表格分页展示图片列表
- ✅ **图片预览** - 点击可预览图片大图
- ✅ **响应式设计** - 模态框形式，用户体验友好

### 基本用法

```vue
<template>
  <div>
    <!-- 单选模式 -->
    <affix-selector
      v-model:visible="visible"
      title="选择图片"
      :multiple="false"
      @confirm="handleConfirm"
      @close="handleClose"
    />

    <!-- 多选模式 -->
    <affix-selector
      v-model:visible="visible"
      title="选择多张图片"
      :multiple="true"
      @confirm="handleConfirmMultiple"
      @close="handleClose"
    />
  </div>
</template>

<script setup lang="ts">
import AffixSelector from '@/components/upload/affix-selector.vue';

const visible = ref(false);

// 单选回调
const handleConfirm = (url: string) => {
  console.log('选中的图片URL:', url);
};

// 多选回调
const handleConfirmMultiple = (urls: string[]) => {
  console.log('选中的图片URL数组:', urls);
};

const handleClose = () => {
  console.log('选择器已关闭');
};
</script>
```

### Props 属性

| 属性名 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| visible | boolean | false | 控制模态框显示/隐藏，支持 v-model |
| multiple | boolean | false | 是否为多选模式 |
| title | string | '选择图片' | 模态框标题 |

### Events 事件

| 事件名 | 说明 | 参数 |
|--------|------|------|
| update:visible | 显示状态变化时触发 | (visible: boolean) |
| confirm | 确认选择时触发 | 单选: (url: string), 多选: (urls: string[]) |
| close | 关闭模态框时触发 | - |

### 功能说明

#### 1. 搜索功能
- 支持按图片名称搜索
- 提供查询和重置按钮

#### 2. 图片列表
- 以表格形式展示
- 显示图片ID、名称、预览、文件大小、创建时间
- 支持分页

#### 3. 图片预览
- 点击预览列可查看图片大图
- 独立的预览模态框

#### 4. 选择模式
- **单选模式**: 使用 radio 选择，返回单个 URL
- **多选模式**: 使用 checkbox 选择，支持全选，返回 URL 数组

---

## 单图上传组件 (image-upload.vue)

### 特性

- ✅ **单图上传** - 支持单张图片上传
- ✅ **自定义尺寸** - 可配置上传区域宽高
- ✅ **图片预览** - 上传后可预览图片
- ✅ **缩略图支持** - 支持上传时生成缩略图
- ✅ **从图库选择** - 支持从附件库选择图片
- ✅ **上传进度** - 实时显示上传进度

### 基本用法

```vue
<template>
  <div>
    <!-- 基本用法 -->
    <image-upload v-model="avatarUrl" />

    <!-- 自定义配置 -->
    <image-upload
      v-model="coverImage"
      title="上传封面"
      :width="200"
      :height="150"
      :show-select-button="true"
    />

    <!-- 带缩略图 -->
    <image-upload
      v-model="thumbUrl"
      :is-thumb="1"
      :thumb-width="120"
      :thumb-height="120"
    />
  </div>
</template>

<script setup lang="ts">
import ImageUpload from '@/components/upload/image-upload.vue';

const avatarUrl = ref('');
const coverImage = ref('');
const thumbUrl = ref('');
</script>
```

### Props 属性

| 属性名 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| modelValue | string | '' | 双向绑定的图片URL |
| title | string | '上传图片' | 上传按钮的标题文字 |
| accept | string | 'image/*' | 接受的文件类型 |
| width | string \| number | 120 | 图片显示宽度 |
| height | string \| number | 120 | 图片显示高度 |
| disabled | boolean | false | 是否禁用上传 |
| isThumb | number | 0 | 是否生成缩略图 (0: 否, 1: 是) |
| thumbWidth | number | 120 | 缩略图宽度 |
| thumbHeight | number | 120 | 缩略图高度 |
| showSelectButton | boolean | true | 是否显示右侧选择按钮 |

### Events 事件

| 事件名 | 说明 | 参数 |
|--------|------|------|
| update:modelValue | 图片URL变化时触发 | (url: string) |
| change | 文件状态变化时触发 | (file: FileItem) |
| success | 上传成功时触发 | (url: string) |
| error | 上传失败时触发 | (error: Error) |
| uploadSuccess | 上传成功时触发 | (data: any) |

---

## 多图上传组件 (multi-image-upload.vue)

### 特性

- ✅ **照片墙形式** - 以网格布局展示已上传的图片
- ✅ **自定义上传请求** - 使用项目统一的API接口
- ✅ **双向绑定URL数组** - 支持v-model绑定图片URL数组
- ✅ **图片预览** - 点击图片可放大预览
- ✅ **删除功能** - 支持删除已上传的图片
- ✅ **上传进度显示** - 实时显示上传进度
- ✅ **数量限制** - 可配置最大上传数量
- ✅ **响应式设计** - 自适应不同屏幕尺寸
- ✅ **从图库选择** - 支持从附件库批量选择图片

### 基本用法

```vue
<template>
  <div>
    <!-- 基本用法 -->
    <multi-image-upload v-model="imageUrls" />
    
    <!-- 自定义配置 -->
    <multi-image-upload 
      v-model="productImages"
      title="上传商品图片"
      :width="120"
      :height="120"
      :max-count="5"
      accept=".jpg,.jpeg,.png,.gif"
    />

    <!-- 带缩略图 -->
    <multi-image-upload
      v-model="thumbUrls"
      :is-thumb="1"
      :thumb-width="120"
      :thumb-height="120"
    />
  </div>
</template>

<script setup lang="ts">
import MultiImageUpload from '@/components/upload/multi-image-upload.vue';

const imageUrls = ref<string[]>([]);
const productImages = ref<string[]>([]);
const thumbUrls = ref<string[]>([]);
</script>
```

### Props 属性

| 属性名 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| modelValue | string[] | [] | 双向绑定的图片URL数组 |
| title | string | '上传图片' | 上传按钮的标题文字 |
| accept | string | 'image/*' | 接受的文件类型 |
| width | string \| number | 120 | 图片显示宽度 |
| height | string \| number | 120 | 图片显示高度 |
| maxCount | number | 10 | 最大上传数量限制 |
| isThumb | number | 0 | 是否生成缩略图 (0: 否, 1: 是) |
| thumbWidth | number | 120 | 缩略图宽度 |
| thumbHeight | number | 120 | 缩略图高度 |
| showSelectButton | boolean | true | 是否显示选择按钮 |

### Events 事件

| 事件名 | 说明 | 参数 |
|--------|------|------|
| update:modelValue | 图片URL数组变化时触发 | (urls: string[]) |
| uploadSuccess | 上传成功时触发 | (data: any) |

### 功能说明

#### 1. 照片墙布局
- 已上传的图片以网格形式排列
- 支持鼠标悬停效果
- 响应式布局，自动换行

#### 2. 图片操作
- **预览**: 点击图片可放大预览
- **删除**: 点击删除按钮可移除图片
- **上传进度**: 上传过程中显示进度条

#### 3. 上传限制
- 支持配置最大上传数量
- 达到上限后上传按钮自动禁用
- 显示当前上传数量/最大数量

#### 4. 自定义上传
- 使用项目统一的 `uploadAffixAPI` 接口
- 支持FormData格式上传
- 自动处理上传成功/失败状态

---

## 通用文件上传组件 (file-upload.vue)

### 特性

- ✅ **任意文件上传** - 支持上传任意类型的文件
- ✅ **文件后缀限制** - 可通过 `accept` 属性限制文件类型
- ✅ **数量限制** - 可配置最大上传数量
- ✅ **双向绑定JSON数组** - 支持v-model绑定JSON数组字符串
- ✅ **文件列表展示** - 清晰展示已上传文件信息
- ✅ **上传进度显示** - 实时显示上传进度
- ✅ **删除功能** - 支持删除已上传的文件
- ✅ **重试功能** - 上传失败可重试
- ✅ **下载功能** - 支持下载已上传的文件

### 基本用法

```vue
<template>
  <div>
    <!-- 基本用法 - 上传任意文件 -->
    <file-upload v-model="files" />
    
    <!-- 限制文件类型和数量 -->
    <file-upload
      v-model="documents"
      title="上传文档"
      accept=".pdf,.doc,.docx,.xls,.xlsx"
      :max-count="5"
    />
    
    <!-- 上传图片 -->
    <file-upload
      v-model="images"
      title="上传图片"
      accept="image/*"
      :max-count="10"
    />
  </div>
</template>

<script setup lang="ts">
import FileUpload from '@/components/upload/file-upload.vue';

// 双向绑定的值为JSON数组字符串
const files = ref<string>('[]');
const documents = ref<string>('[]');
const images = ref<string>('[]');

// 监听变化
watch(() => files.value, (newVal) => {
  const fileList = JSON.parse(newVal);
  console.log('文件列表:', fileList);
});
</script>
```

### Props 属性

| 属性名 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| modelValue | string | '[]' | 双向绑定的文件信息JSON数组字符串 |
| title | string | '上传文件' | 上传按钮的标题文字 |
| accept | string | '*' | 接受的文件类型，如 '.pdf,.doc' 或 'image/*' |
| maxCount | number | 10 | 最大上传数量限制 |

### Events 事件

| 事件名 | 说明 | 参数 |
|--------|------|------|
| update:modelValue | 文件列表变化时触发 | (jsonString: string) |
| change | 文件状态变化时触发 | (fileList: FileInfo[]) |
| success | 上传成功时触发 | (fileData: AffixItem) |
| error | 上传失败时触发 | (error: Error) |

### 文件信息格式

双向绑定的值为 JSON 数组字符串，每个文件对象包含以下信息：

```typescript
interface FileInfo {
    id: number;        // 文件ID
    name: string;      // 文件名
    size: number;      // 文件大小（字节）
    url: string;       // 文件URL
    suffix: string;    // 文件后缀
    ftype: string;     // 文件类型
}
```

### 功能说明

#### 1. 文件类型限制

通过 `accept` 属性限制可上传的文件类型：

```vue
<!-- 限制为PDF和Word文档 -->
<file-upload v-model="files" accept=".pdf,.doc,.docx" />

<!-- 限制为所有图片 -->
<file-upload v-model="files" accept="image/*" />

<!-- 限制为特定类型 -->
<file-upload v-model="files" accept=".txt,.csv,.json" />
```

#### 2. 数量限制

通过 `maxCount` 属性限制最大上传数量：

```vue
<!-- 最多上传5个文件 -->
<file-upload v-model="files" :max-count="5" />
```

#### 3. 文件操作

- **预览**: 文件列表显示文件名、大小和上传状态
- **下载**: 点击下载按钮可下载已上传的文件
- **删除**: 点击删除按钮可移除文件
- **重试**: 上传失败的文件可点击重试

#### 4. 上传状态

- `已上传` - 文件上传成功
- `上传中` - 文件正在上传，显示进度百分比
- `上传失败` - 文件上传失败，可重试

### 示例场景

#### 场景1：上传用户身份证

```vue
<template>
  <file-upload
    v-model="idCardFiles"
    title="上传身份证"
    accept=".jpg,.jpeg,.png"
    :max-count="2"
  />
</template>

<script setup lang="ts">
import FileUpload from '@/components/upload/file-upload.vue';

const idCardFiles = ref<string>('[]');
</script>
```

#### 场景2：上传合同文档

```vue
<template>
  <file-upload
    v-model="contractFiles"
    title="上传合同"
    accept=".pdf,.doc,.docx"
    :max-count="10"
  />
</template>

<script setup lang="ts">
import FileUpload from '@/components/upload/file-upload.vue';

const contractFiles = ref<string>('[]');
</script>
```

---

## API 依赖

所有组件都依赖以下API和工具函数：

- `uploadAffixAPI` - 文件上传接口
- `getAffixListAPI` - 获取附件列表接口（affix-selector 使用）
- `handleUrl` - URL处理函数（图片组件使用）
- `Message` - 消息提示组件

确保这些依赖在项目中正确配置。

---

## 样式定制

组件使用 CSS 变量，可以通过以下方式自定义样式：

```css
/* 修改主题色 */
:root {
  --color-primary: #1890ff;
  --color-primary-light-1: #e6f7ff;
  --color-border-2: #e5e6eb;
  --color-border-3: #c9cdd4;
  --color-fill-1: #f7f8fa;
  --color-fill-2: #f2f3f5;
  --color-text-1: #1d2129;
  --color-text-2: #4e5969;
  --color-text-3: #86909c;
}
```

---

## 注意事项

1. **URL处理**: 图片组件会自动调用 `handleUrl` 函数处理相对路径
2. **文件类型**: 默认接受所有图片/文件类型，可通过 `accept` 属性限制
3. **上传状态**: 上传失败的文件会显示错误状态，可以重新上传
4. **性能优化**: 建议设置合理的 `maxCount` 避免上传过多图片/文件
5. **JSON格式**: `file-upload` 组件的 `modelValue` 是 JSON 数组字符串，使用前需要 `JSON.parse()` 解析

---

## 分片上传组件 (chunk-upload.vue)

### 特性

- ✅ **大文件分片上传** - 支持大文件自动分片上传，提高上传成功率
- ✅ **断点续传** - 支持暂停和恢复上传，网络中断后可继续上传
- ✅ **拖拽上传** - 支持拖拽文件到上传区域
- ✅ **实时进度** - 显示上传进度、上传速度、剩余时间
- ✅ **秒传功能** - 通过文件哈希值实现秒传
- ✅ **自动重试** - 上传失败自动重试，可配置重试次数
- ✅ **并发控制** - 支持多分片并发上传，提高上传效率
- ✅ **智能分片** - 根据文件大小自动判断是否使用分片上传
- ✅ **文件管理** - 支持删除、重试、暂停、恢复等操作

### 基本用法

```vue
<template>
  <div>
    <!-- 基本用法 -->
    <chunk-upload v-model="files" />
    
    <!-- 限制文件类型和数量 -->
    <chunk-upload
      v-model="documents"
      accept=".pdf,.doc,.docx,.zip"
      :max-count="5"
      :max-file-size="1024 * 1024 * 1024"
    />
    
    <!-- 自定义分片配置 -->
    <chunk-upload
      v-model="largeFiles"
      :chunk-size="5 * 1024 * 1024"
      :concurrency="3"
      :max-retry="3"
      :auto-start="true"
    />
    
    <!-- 手动控制上传 -->
    <chunk-upload
      ref="chunkUploadRef"
      v-model="manualFiles"
      :auto-start="false"
    />
    <a-button @click="startAllUploads">开始上传</a-button>
  </div>
</template>

<script setup lang="ts">
import ChunkUpload from '@/components/upload/chunk-upload.vue';

const files = ref<string>('[]');
const documents = ref<string>('[]');
const largeFiles = ref<string>('[]');
const manualFiles = ref<string>('[]');
const chunkUploadRef = ref();

// 监听上传成功事件
const handleSuccess = (data: any, fileItem: any) => {
  console.log('上传成功:', data);
};

// 监听上传进度
const handleProgress = (progress: any, fileItem: any) => {
  console.log('上传进度:', progress.percent);
};

// 监听上传错误
const handleError = (error: Error, fileItem: any) => {
  console.error('上传失败:', error.message);
};

// 手动开始所有上传
const startAllUploads = () => {
  // 组件会在 autoStart=false 时等待手动触发
  // 这里可以通过组件暴露的方法来控制
};

// 清除已完成的文件
const clearDoneFiles = () => {
  chunkUploadRef.value?.clearDone();
};

// 重置所有文件
const resetAllFiles = () => {
  chunkUploadRef.value?.reset();
};
</script>
```

### Props 属性

| 属性名 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| modelValue | string | '[]' | 双向绑定的文件信息JSON数组字符串 |
| accept | string | '*' | 接受的文件类型，如 '.pdf,.doc' 或 'image/*' |
| maxCount | number | 10 | 最大上传数量限制 |
| maxFileSize | number | 0 | 最大文件大小限制（字节），0表示使用环境变量或默认值 |
| chunkSize | number | 0 | 分片大小（字节），0表示使用环境变量或默认值 |
| concurrency | number | 0 | 并发上传分片数，0表示使用环境变量或默认值 |
| maxRetry | number | 0 | 最大重试次数，0表示使用环境变量或默认值 |
| largeFileThreshold | number | 0 | 大文件阈值（字节），超过此值使用分片上传，0表示使用环境变量或默认值 |
| autoStart | boolean | true | 是否自动开始上传 |

### Events 事件

| 事件名 | 说明 | 参数 |
|--------|------|------|
| update:modelValue | 文件列表变化时触发 | (jsonString: string) |
| change | 文件状态变化时触发 | (fileList: FileItem[]) |
| success | 上传成功时触发 | (data: any, fileItem: FileItem) |
| error | 上传失败时触发 | (error: Error, fileItem: FileItem) |
| progress | 上传进度变化时触发 | (progress: ChunkProgress, fileItem: FileItem) |

### 暴露方法

| 方法名 | 说明 | 参数 |
|--------|------|------|
| reset | 重置文件列表（取消所有上传并清空） | - |
| clearDone | 清除已完成的文件 | - |

### 文件状态

文件在上传过程中会经历以下状态：

| 状态 | 说明 |
|------|------|
| waiting | 等待上传 |
| hashing | 计算文件哈希值（用于秒传） |
| uploading | 上传中 |
| paused | 已暂停 |
| merging | 合并分片中 |
| done | 上传完成 |
| error | 上传失败 |

### 进度信息格式

```typescript
interface ChunkProgress {
    status: string;           // 当前状态
    totalChunks: number;      // 总分片数
    uploadedChunks: number;   // 已上传分片数
    percent: number;          // 上传进度百分比 (0-100)
    currentSpeed?: string;    // 当前上传速度（如 "2.5 MB/s"）
    remainingTime?: string;   // 剩余时间（如 "00:05:32"）
    hashingPercent?: number;  // 哈希计算进度百分比
}
```

### 功能说明

#### 1. 大文件分片上传

- 自动根据文件大小判断是否使用分片上传
- 默认配置可通过环境变量设置
- 支持自定义分片大小、并发数等参数

```vue
<chunk-upload
  v-model="files"
  :chunk-size="5 * 1024 * 1024"
  :concurrency="3"
  :large-file-threshold="100 * 1024 * 1024"
/>
```

#### 2. 断点续传

- 上传过程中可暂停和恢复
- 暂停后恢复不会重新计算哈希值
- 网络中断后可继续上传

```vue
<chunk-upload
  v-model="files"
  :auto-start="false"
/>
```

#### 3. 秒传功能

- 通过计算文件哈希值实现秒传
- 服务器已存在的文件可直接返回URL
- 哈希计算进度实时显示

#### 4. 拖拽上传

- 支持拖拽文件到上传区域
- 拖拽时有视觉反馈
- 可同时拖拽多个文件

#### 5. 文件管理

- **删除/取消**: 移除文件项，正在上传的会先取消
- **暂停**: 暂停当前上传任务
- **恢复**: 恢复暂停的上传任务
- **重试**: 重新上传失败的文件

### 配置说明

#### 环境变量配置

分片上传的默认配置可通过环境变量设置（优先级低于 props）：

```env
# .env.development
VITE_CHUNK_SIZE=5242880           # 分片大小 5MB
VITE_CONCURRENCY=3                # 并发数
VITE_MAX_RETRY=3                  # 最大重试次数
VITE_MAX_FILE_SIZE=1073741824     # 最大文件大小 1GB
VITE_LARGE_FILE_THRESHOLD=104857600  # 大文件阈值 100MB
```

#### Props 优先级

Props 配置 > 环境变量 > 默认值

### 示例场景

#### 场景1：上传大视频文件

```vue
<template>
  <chunk-upload
    v-model="videoFiles"
    accept=".mp4,.avi,.mov,.mkv"
    :max-count="1"
    :max-file-size="5 * 1024 * 1024 * 1024"
    :chunk-size="10 * 1024 * 1024"
    :concurrency="3"
    @success="handleVideoUpload"
  />
</template>

<script setup lang="ts">
const videoFiles = ref<string>('[]');

const handleVideoUpload = (data: any) => {
  console.log('视频上传成功:', data.url);
};
</script>
```

#### 场景2：批量上传文档

```vue
<template>
  <div>
    <chunk-upload
      ref="uploadRef"
      v-model="docFiles"
      accept=".pdf,.doc,.docx,.xls,.xlsx,.ppt,.pptx"
      :max-count="20"
      :max-file-size="500 * 1024 * 1024"
      :auto-start="false"
      @progress="handleProgress"
    />
    <a-space style="margin-top: 16px;">
      <a-button type="primary" @click="startUpload">开始上传</a-button>
      <a-button @click="clearDone">清除已完成</a-button>
      <a-button @click="resetAll">重置</a-button>
    </a-space>
  </div>
</template>

<script setup lang="ts">
const docFiles = ref<string>('[]');
const uploadRef = ref();

const handleProgress = (progress: any) => {
  console.log(`上传进度: ${progress.percent}%`);
};

const startUpload = () => {
  // 遍历文件列表，手动触发上传
  // 注意：当前实现需要在 addFiles 中处理
  console.log('开始上传');
};

const clearDone = () => {
  uploadRef.value?.clearDone();
};

const resetAll = () => {
  uploadRef.value?.reset();
};
</script>
```

#### 场景3：带进度监控的上传

```vue
<template>
  <div>
    <chunk-upload
      v-model="monitoredFiles"
      @progress="handleProgress"
      @success="handleSuccess"
      @error="handleError"
    />
    <div v-if="overallProgress > 0" style="margin-top: 16px;">
      <a-progress :percent="overallProgress / 100" />
      <p>总体进度: {{ overallProgress.toFixed(2) }}%</p>
      <p v-if="totalSpeed">总速度: {{ totalSpeed }}</p>
    </div>
  </div>
</template>

<script setup lang="ts">
const monitoredFiles = ref<string>('[]');
const overallProgress = ref(0);
const totalSpeed = ref('');

const handleProgress = (progress: any) => {
  // 计算总体进度
  const fileList = JSON.parse(monitoredFiles.value);
  // 这里可以根据实际需求计算总体进度
  overallProgress.value = progress.percent;
  totalSpeed.value = progress.currentSpeed || '';
};

const handleSuccess = (data: any) => {
  console.log('文件上传成功:', data.name);
};

const handleError = (error: Error) => {
  console.error('上传失败:', error.message);
};
</script>
```

### 注意事项

1. **文件大小限制**: 默认最大文件大小由环境变量控制，可通过 `maxFileSize` prop 覆盖
2. **分片大小**: 建议根据网络环境调整，一般 5-10MB 较为合适
3. **并发数**: 并发数过高可能导致服务器压力，建议 3-5 个
4. **重试次数**: 网络不稳定时可适当增加重试次数
5. **JSON格式**: `modelValue` 是 JSON 数组字符串，使用前需要 `JSON.parse()` 解析
6. **秒传功能**: 依赖服务器支持文件哈希查询
7. **断点续传**: 暂停后恢复不会重新计算哈希，但需要保持同一个 ChunkUploader 实例
