# WangEditor 富文本编辑器组件

基于 [wangEditor V5](https://www.wangeditor.com/) 封装的 Vue 3 富文本编辑器组件，支持图片和视频上传。

## 功能特性

- ✅ 基于 wangEditor V5，功能强大
- ✅ 支持图片上传（通过自定义上传接口）
- ✅ 支持视频上传（通过自定义上传接口）
- ✅ 支持默认模式和简洁模式
- ✅ 双向绑定（v-model）
- ✅ TypeScript 类型支持
- ✅ 自定义占位符文本
- ✅ 可自定义工具栏配置

## 安装依赖

确保已安装 wangEditor 相关依赖：

```bash
npm install @wangeditor/editor-for-vue @wangeditor/editor
# 或
pnpm install @wangeditor/editor-for-vue @wangeditor/editor
# 或
yarn add @wangeditor/editor-for-vue @wangeditor/editor
```

## 基础用法

### 默认模式

```vue
<template>
  <wang-editor v-model="content" />
</template>

<script setup lang="ts">
import { ref } from 'vue';
import WangEditor from '@/components/wang-editor/index.vue';

const content = ref('<p>默认内容</p>');
</script>
```

### 简洁模式

```vue
<template>
  <wang-editor 
    v-model="content" 
    mode="simple"
    placeholder="请输入文章内容..."
  />
</template>

<script setup lang="ts">
import { ref } from 'vue';
import WangEditor from '@/components/wang-editor/index.vue';

const content = ref('');
</script>
```

### 监听内容变化

```vue
<template>
  <wang-editor 
    v-model="content" 
    @change="handleChange"
  />
</template>

<script setup lang="ts">
import { ref } from 'vue';
import WangEditor from '@/components/wang-editor/index.vue';

const content = ref('');

const handleChange = (value: string) => {
  console.log('内容已变更:', value);
};
</script>
```

### 自定义工具栏配置

```vue
<template>
  <wang-editor 
    v-model="content" 
    :toolbar-config="toolbarConfig"
  />
</template>

<script setup lang="ts">
import { ref } from 'vue';
import WangEditor from '@/components/wang-editor/index.vue';

const content = ref('');

// 自定义工具栏配置
const toolbarConfig = {
  // 排除某些菜单项
  excludeKeys: ['group-video']
};
</script>
```

## API

### Props

| 属性 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| modelValue | `string` | `''` | **必填**。编辑器内容，支持 v-model 双向绑定 |
| placeholder | `string` | `'请输入内容...'` | 编辑器占位符文本 |
| mode | `'default' \| 'simple'` | `'default'` | 编辑器模式，default 为默认模式，simple 为简洁模式 |
| toolbarConfig | `Record<string, any>` | `{}` | 工具栏配置对象 |

### Events

| 事件名 | 说明 | 参数类型 |
|--------|------|----------|
| update:modelValue | 内容变化时触发（v-model） | `string` |
| change | 内容变化时触发 | `string` |

## 样式自定义

组件默认样式如下：

```scss
.editor—wrapper {
    border: 1px solid #cccccc;
    z-index: 100;
    .toolbar-container {
        border-bottom: 1px solid #cccccc;
    }
    .editor-container {
        overflow-y: hidden;
    }
}

.w-e-text-container .w-e-scroll {
    height: 500px !important;   /* 编辑器高度 */
    -webkit-overflow-scrolling: touch;    /* 开启平滑滚动 */
}
```

如需自定义样式，可以通过覆盖 CSS 类名实现：

```vue
<template>
  <wang-editor v-model="content" class="custom-editor" />
</template>

<style lang="scss">
.custom-editor {
    .editor—wrapper {
        border-color: #1890ff;
    }
    .w-e-text-container .w-e-scroll {
        height: 600px !important;
    }
}
</style>
```

## 上传配置说明

组件内部已配置图片和视频上传功能，使用 [`uploadAffixAPI`](../../api/file) 接口进行文件上传。

### 图片上传

- 支持的图片格式：由上传接口决定
- 上传成功后自动插入到编辑器中
- 上传失败会显示错误提示

### 视频上传

- 支持的视频格式：由上传接口决定
- 上传成功后自动插入到编辑器中
- 上传失败会显示错误提示

如需修改上传逻辑，请编辑 [`index.vue`](./index.vue) 文件中的 `editorConfig.MENU_CONF` 配置。

## 注意事项

1. **编辑器实例管理**：组件内部使用 `shallowRef` 管理编辑器实例，并在组件销毁时自动清理，无需手动处理。

2. **内容格式**：编辑器内容为 HTML 格式字符串，提交到后端时注意处理 XSS 安全问题。

3. **高度设置**：默认编辑器高度为 500px，如需修改请覆盖 `.w-e-text-container .w-e-scroll` 样式。

4. **z-index**：默认 z-index 为 100，如需调整请覆盖 `.editor—wrapper` 样式。

5. **依赖引入**：确保在项目中正确引入了 wangEditor 的样式文件：

   ```typescript
   import "@wangeditor/editor/dist/css/style.css";
   ```

## 完整示例

```vue
<template>
  <div class="editor-demo">
    <wang-editor 
      v-model="articleContent"
      placeholder="请输入文章内容..."
      mode="default"
      :toolbar-config="toolbarConfig"
      @change="handleContentChange"
    />
    
    <a-button type="primary" @click="submitArticle">
      提交文章
    </a-button>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue';
import WangEditor from '@/components/wang-editor/index.vue';
import { Message } from '@arco-design/web-vue';

const articleContent = ref('');

const toolbarConfig = {
  // 可根据需要配置工具栏
};

const handleContentChange = (value: string) => {
  console.log('内容长度:', value.length);
};

const submitArticle = () => {
  if (!articleContent.value) {
    Message.warning('请输入文章内容');
    return;
  }
  
  // 提交逻辑
  console.log('提交内容:', articleContent.value);
  Message.success('提交成功');
};
</script>

<style lang="scss" scoped>
.editor-demo {
  padding: 20px;
}
</style>
```

## 参考文档

- [wangEditor 官方文档](https://www.wangeditor.com/)
- [wangEditor V5 文档](https://www.wangeditor.com/v5/for-frame.html#%E5%AE%89%E8%A3%85)
