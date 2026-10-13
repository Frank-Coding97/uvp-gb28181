<template>
  <a-modal
    v-model:visible="visible"
    title="上传固件"
    modal-class="uvp-system-dialog"
    :mask-closable="false"
    @cancel="handleCancel"
    @before-ok="handleUpload"
  >
    <a-form :model="form" layout="vertical">
      <a-form-item
        label="固件文件"
        required
        :validate-status="touched.file && fileError ? 'error' : ''"
        :help="touched.file ? fileError : ''"
      >
        <a-upload
          :auto-upload="false"
          :show-file-list="true"
          :limit="1"
          @change="handleFileChange"
          @exceed-limit="handleExceedLimit"
        >
          <template #upload-button>
            <a-button type="primary">
              <template #icon>
                <Upload :size="16" />
              </template>
              选择文件
            </a-button>
          </template>
        </a-upload>
        <div class="upload-hint">最大支持 2GB</div>
      </a-form-item>

      <a-form-item
        label="固件版本"
        required
        :validate-status="touched.version && versionError ? 'error' : ''"
        :help="touched.version ? versionError : ''"
      >
        <a-input v-model="form.version" placeholder="例如: v1.2.3" allow-clear :max-length="50" @blur="touched.version = true" />
      </a-form-item>

      <a-form-item
        label="厂商"
        required
        :validate-status="touched.manufacturer && manufacturerError ? 'error' : ''"
        :help="touched.manufacturer ? manufacturerError : ''"
      >
        <a-input
          v-model="form.manufacturer"
          placeholder="例如: 海康威视"
          allow-clear
          :max-length="100"
          @blur="touched.manufacturer = true"
        />
      </a-form-item>

      <a-form-item label="适用型号">
        <a-input v-model="form.modelPattern" placeholder="例如: DS-2CD.*（支持正则）" allow-clear :max-length="200" />
      </a-form-item>

      <a-form-item label="备注">
        <a-textarea
          v-model="form.remark"
          placeholder="可填写升级说明、注意事项等"
          allow-clear
          :max-length="500"
          :auto-size="{ minRows: 3, maxRows: 6 }"
        />
      </a-form-item>
    </a-form>
  </a-modal>
</template>

<script setup lang="ts">
import { ref, computed, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import { Upload } from "lucide-vue-next";
import { uploadFirmware } from "./api";
import type { FileItem } from "@arco-design/web-vue";

interface Props {
  visible: boolean;
}

interface Emits {
  (e: "update:visible", value: boolean): void;
  (e: "success"): void;
}

const props = defineProps<Props>();
const emit = defineEmits<Emits>();

const visible = computed({
  get: () => props.visible,
  set: value => emit("update:visible", value)
});

const form = ref({
  file: null as File | null,
  version: "",
  manufacturer: "",
  modelPattern: "",
  remark: ""
});

const touched = ref({
  file: false,
  version: false,
  manufacturer: false
});

const MAX_FILE_SIZE = 2 * 1024 * 1024 * 1024; // 2GB

const fileError = computed(() => {
  if (!form.value.file) {
    return "请选择固件文件";
  }
  if (form.value.file.size > MAX_FILE_SIZE) {
    return `文件大小超过限制(最大 2GB)`;
  }
  return "";
});

const versionError = computed(() => {
  const version = form.value.version.trim();
  if (!version) {
    return "请填写固件版本";
  }
  if (version.length > 50) {
    return "版本号长度不能超过 50 个字符";
  }
  return "";
});

const manufacturerError = computed(() => {
  const manufacturer = form.value.manufacturer.trim();
  if (!manufacturer) {
    return "请填写厂商";
  }
  if (manufacturer.length > 100) {
    return "厂商名称长度不能超过 100 个字符";
  }
  return "";
});

const hasError = computed(() => {
  return !!fileError.value || !!versionError.value || !!manufacturerError.value;
});

function handleFileChange(fileList: FileItem[]) {
  touched.value.file = true;
  if (fileList.length > 0 && fileList[0].file) {
    form.value.file = fileList[0].file;
  } else {
    form.value.file = null;
  }
}

function handleExceedLimit() {
  Message.warning("只能上传 1 个文件");
}

function handleCancel() {
  visible.value = false;
}

async function handleUpload() {
  // 标记所有字段为 touched
  touched.value.file = true;
  touched.value.version = true;
  touched.value.manufacturer = true;

  if (hasError.value) {
    Message.error("请检查表单填写");
    return false;
  }

  try {
    await uploadFirmware({
      file: form.value.file!,
      version: form.value.version.trim(),
      manufacturer: form.value.manufacturer.trim(),
      modelPattern: form.value.modelPattern.trim() || undefined,
      remark: form.value.remark.trim() || undefined
    });

    Message.success("固件上传成功");
    emit("success");
    visible.value = false;
    return true;
  } catch (error: any) {
    Message.error(error.message || "上传失败");
    return false;
  }
}

// 重置表单
watch(visible, newVisible => {
  if (!newVisible) {
    form.value = {
      file: null,
      version: "",
      manufacturer: "",
      modelPattern: "",
      remark: ""
    };
    touched.value = {
      file: false,
      version: false,
      manufacturer: false
    };
  }
});
</script>

<style scoped>
.upload-hint {
  margin-top: 8px;
  font-size: 12px;
  color: var(--color-text-3);
}
</style>
