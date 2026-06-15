<template>
  <div
    class="s-excel-reader"
    :class="{ 'is-dragover': isDragOver, 'is-loading': loading }"
    @dragenter.prevent="handleDragEnter"
    @dragover.prevent="handleDragOver"
    @dragleave.prevent="handleDragLeave"
    @drop.prevent="handleDrop"
  >
    <div class="excel-upload-area" @click="handleClick">
      <input
        ref="fileInputRef"
        type="file"
        accept=".xlsx,.xls"
        style="display: none"
        @change="handleFileChange"
      />
      <div class="upload-content">
        <div class="upload-icon">
          <icon-file :size="48" />
        </div>
        <div class="upload-text">
          <div class="upload-title">{{ title }}</div>
          <div class="upload-desc">{{ description }}</div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue';
import { read, utils, WorkBook } from 'xlsx';
import { IconFile } from '@arco-design/web-vue/es/icon';

interface ExcelSheetData {
  sheetName: string;
  data: Record<string, any>[];
  headers: string[];
}

interface Props {
  title?: string;
  description?: string;
  multiple?: boolean;
  readAsArray?: boolean;
}

interface Emits {
  (e: 'success', data: ExcelSheetData[] | ExcelSheetData): void;
  (e: 'error', error: Error): void;
  (e: 'loading', isLoading: boolean): void;
}

const props = withDefaults(defineProps<Props>(), {
  title: '点击或拖拽上传 Excel 文件',
  description: '支持 .xlsx、.xls 格式',
  multiple: false,
  readAsArray: false
});

const emit = defineEmits<Emits>();

// 文件输入框引用
const fileInputRef = ref<HTMLInputElement>();

// 拖拽状态
const isDragOver = ref(false);
const dragCounter = ref(0);

// 加载状态
const loading = ref(false);

// 处理拖拽进入
const handleDragEnter = () => {
  dragCounter.value++;
  isDragOver.value = true;
};

// 处理拖拽悬停
const handleDragOver = () => {
  isDragOver.value = true;
};

// 处理拖拽离开
const handleDragLeave = () => {
  dragCounter.value--;
  if (dragCounter.value === 0) {
    isDragOver.value = false;
  }
};

// 处理拖拽释放
const handleDrop = (e: DragEvent) => {
  dragCounter.value = 0;
  isDragOver.value = false;

  const files = e.dataTransfer?.files;
  if (!files || files.length === 0) return;

  handleFiles(Array.from(files));
};

// 处理点击上传
const handleClick = () => {
  fileInputRef.value?.click();
};

// 处理文件选择
const handleFileChange = (e: Event) => {
  const target = e.target as HTMLInputElement;
  const files = target.files;
  if (!files || files.length === 0) return;

  handleFiles(Array.from(files));

  // 清空 input，允许重复选择同一文件
  target.value = '';
};

// 处理文件
const handleFiles = async (files: File[]) => {
  if (files.length === 0) return;

  // 过滤非 Excel 文件
  const excelFiles = files.filter(file => {
    const extension = file.name.split('.').pop()?.toLowerCase();
    return extension === 'xlsx' || extension === 'xls';
  });

  if (excelFiles.length === 0) {
    emit('error', new Error('请选择有效的 Excel 文件（.xlsx 或 .xls）'));
    return;
  }

  // 如果不允许多文件，只处理第一个
  const filesToProcess = props.multiple ? excelFiles : [excelFiles[0]];

  try {
    loading.value = true;
    emit('loading', true);

    const results: ExcelSheetData[] = [];

    for (const file of filesToProcess) {
      const fileData = await readExcelFile(file);
      results.push(...fileData);
    }

    // 根据配置返回单个或多个结果
    if (props.multiple) {
      emit('success', results);
    } else {
      emit('success', results[0]);
    }
  } catch (error) {
    console.error('读取 Excel 文件失败:', error);
    emit('error', error as Error);
  } finally {
    loading.value = false;
    emit('loading', false);
  }
};

// 读取 Excel 文件
const readExcelFile = (file: File): Promise<ExcelSheetData[]> => {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();

    reader.onload = (e) => {
      try {
        const data = e.target?.result;
        if (!data) {
          reject(new Error('文件读取失败'));
          return;
        }

        const workbook: WorkBook = read(data, { type: 'array' });
        const result: ExcelSheetData[] = [];

        // 遍历所有工作表
        workbook.SheetNames.forEach(sheetName => {
          const worksheet = workbook.Sheets[sheetName];

          // 读取为对象数组（第一行作为表头）
          const jsonData = utils.sheet_to_json(worksheet) as Record<string, any>[];

          // 获取表头
          const headers: string[] = [];
          if (jsonData.length > 0) {
            const firstRow = jsonData[0];
            Object.keys(firstRow).forEach(key => {
              headers.push(key);
            });
          }

          result.push({
            sheetName,
            data: jsonData,
            headers
          });
        });

        resolve(result);
      } catch (error) {
        reject(error);
      }
    };

    reader.onerror = () => {
      reject(new Error('文件读取失败'));
    };

    reader.readAsArrayBuffer(file);
  });
};

// 暴露方法供外部调用
defineExpose({
  triggerUpload: () => fileInputRef.value?.click()
});
</script>

<style lang="scss" scoped>
.s-excel-reader {
  width: 100%;
  height: 100%;
  min-height: 200px;
  border: 2px dashed var(--color-border-2);
  border-radius: 4px;
  transition: all 0.3s;
  background-color: var(--color-fill-1);
  cursor: pointer;

  &:hover {
    border-color: var(--color-border-3);
    background-color: var(--color-fill-2);
  }

  &.is-dragover {
    border-color: rgb(var(--primary-6));
    background-color: rgba(var(--primary-1), 0.1);
  }

  &.is-loading {
    cursor: not-allowed;
    opacity: 0.7;
  }

  .excel-upload-area {
    width: 100%;
    height: 100%;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 20px;

    .upload-content {
      display: flex;
      flex-direction: column;
      align-items: center;
      gap: 16px;
      text-align: center;

      .upload-icon {
        color: var(--color-text-3);
        transition: color 0.3s;
      }

      .upload-text {
        .upload-title {
          font-size: 14px;
          color: var(--color-text-1);
          margin-bottom: 4px;
        }

        .upload-desc {
          font-size: 12px;
          color: var(--color-text-3);
        }
      }
    }
  }

  &:hover .upload-icon {
    color: rgb(var(--primary-6));
  }

  &.is-dragover .upload-icon {
    color: rgb(var(--primary-6));
  }
}
</style>
