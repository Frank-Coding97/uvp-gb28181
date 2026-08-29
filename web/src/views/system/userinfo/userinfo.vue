<template>
  <div class="snow-page">
    <div class="snow-inner uvp-page-shell-flat userinfo-page">
      <a-spin :loading="loading" tip="loading...">
        <a-card class="uvp-system-panel userinfo-content" :bordered="false">
          <section class="userinfo-profile">
            <div class="avatar-container" :class="{ 'mobile-avatar': isMobile }">
              <a-avatar
                class="userinfo-avatar"
                shape="square"
                :size="isMobile ? 80 : 100"
                @click="showAvatarUpload"
                trigger-type="mask"
                :imageUrl="userInfo.avatar"
              >
                <IconUser />
                <template #trigger-icon>
                  <IconEdit />
                </template>
              </a-avatar>
            </div>
            <div class="userinfo-profile__details">
              <a-descriptions
                class="uvp-system-description uvp-system-description--compact"
                :data="detail"
                :column="isMobile ? 1 : 3"
                title="用户资料"
                :align="{ label: isMobile ? 'left' : 'right' }"
              >
                <template #value="{ value, data }">
                  <span v-if="data.key === 'roles'">
                    {{ (Array.isArray(value) && value.map((curr: any) => curr.name).join(",")) || "-" }}
                  </span>
                  <span v-else-if="data.key === 'status'">
                    {{ value === 1 ? "启用" : "禁用" }}
                  </span>
                  <span v-else-if="data.key === 'sex'">
                    {{ getSexName(value) }}
                  </span>
                  <span v-else-if="data.key === 'createTime'">
                    {{ formatTime(value) }}
                  </span>
                  <span v-else>{{ value || "-" }}</span>
                </template>
              </a-descriptions>
            </div>
          </section>

          <a-divider class="userinfo-divider" />

          <section class="userinfo-settings">
            <a-tabs class="uvp-system-tabs" :active-key="activeTabs" @change="onChangeTab">
              <a-tab-pane key="1" title="基本信息">
                <BasicInfo v-model="userInfo" @refresh="refresh" />
              </a-tab-pane>
              <a-tab-pane key="2" title="安全设置">
                <SecuritySettings v-model="userInfo" @refresh="refresh" />
              </a-tab-pane>
            </a-tabs>
          </section>
        </a-card>
      </a-spin>
    </div>

    <a-modal
      modal-class="uvp-system-dialog"
      v-model:visible="avatarModalVisible"
      title="上传头像"
      :width="isMobile ? '95%' : 600"
      :footer="false"
      draggable
      @close="resetAvatarUpload"
    >
      <div class="avatar-upload-modal">
        <a-row :gutter="isMobile ? 0 : 20">
          <a-col :span="isMobile ? 24 : 14" :style="isMobile ? 'width: 100%; height: 200px' : 'width: 200px; height: 200px'">
            <VueCropper
              ref="cropperRef"
              :img="cropperOptions.img"
              :info="true"
              :auto-crop="cropperOptions.autoCrop"
              :auto-crop-width="cropperOptions.autoCropWidth"
              :auto-crop-height="cropperOptions.autoCropHeight"
              :fixed-box="cropperOptions.fixedBox"
              :fixed="cropperOptions.fixed"
              :full="cropperOptions.full"
              :center-box="cropperOptions.centerBox"
              :can-move="cropperOptions.canMove"
              :output-type="cropperOptions.outputType"
              :output-size="cropperOptions.outputSize"
              @real-time="handleRealTime"
            />
          </a-col>
          <a-col :span="isMobile ? 24 : 10" :class="{ 'mobile-preview': isMobile }">
            <div class="avatar-preview">
              <h4>预览</h4>
              <div class="preview-container">
                <div :style="previewStyle">
                  <div :style="previews.div">
                    <img :src="previews.url" :style="previews.img" alt="预览头像" />
                  </div>
                </div>
              </div>
              <p>160*160像素</p>
            </div>
          </a-col>
        </a-row>
        <div class="avatar-upload-modal__actions">
          <a-space>
            <a-button type="primary" @click="confirmUploadAvatar">确定</a-button>
            <a-button @click="resetAvatarUpload">取消</a-button>
          </a-space>
        </div>
      </div>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { useRoute } from "vue-router";
import BasicInfo from "@/views/system/userinfo/components/basic-info.vue";
import SecuritySettings from "@/views/system/userinfo/components/security-settings.vue";

import useGlobalProperties from "@/hooks/useGlobalProperties";
import { useRouteConfigStore } from "@/store/modules/route-config";
import { type ProfileItem, uploadAvatarAPI, getProfileAPI } from "@/api/user";
import { formatTime } from "@/globals";
import { IconEdit, IconUser } from "@arco-design/web-vue/es/icon";
import { useDevicesSize } from "@/hooks/useDevicesSize";
import { VueCropper } from "vue-cropper";
import "vue-cropper/dist/index.css";
import { handleUrl } from "@/utils/app";
import { useUserStoreHook } from "@/store/modules/user";
const { isMobile } = useDevicesSize();

const route = useRoute();
const proxy = useGlobalProperties();
const routerStore = useRouteConfigStore();

interface Detail {
  key: string;
  label: string;
  value: unknown;
}

const activeTabs = ref(route.query.type || "1");


// 头像裁剪相关
const avatarModalVisible = ref(false);
const cropperRef = ref<any>(null);
const previews = ref<any>({});
const previewStyle = ref<any>({});
const selectedFileName = ref<string>("");

// 裁剪选项
const cropperOptions = reactive({
  img: "",
  autoCrop: true,
  autoCropWidth: 160,
  autoCropHeight: 160,
  fixedBox: true,
  fixed: true,
  full: false,
  centerBox: true,
  canMove: true,
  outputSize: 1,
  outputType: "png"
});

const onChangeTab = (e: string) => {
    activeTabs.value = e;
};

const buildDetail = (profile: ProfileItem): Detail[] => {
  const details: Detail[] = [
    { key: "userName", label: "用户名", value: profile.userName || "-" },
    { key: "nickName", label: "用户昵称", value: profile.nickName || "-" },
    { key: "sex", label: "性别", value: profile.sex },
    { key: "roles", label: "角色", value: profile.roles || [] },
    { key: "status", label: "状态", value: profile.status },
    { key: "email", label: "邮箱", value: profile.email || "-" },
    { key: "phone", label: "手机号", value: profile.phone || "-" },
    { key: "deptName", label: "部门", value: profile.department?.name || "-" },
    { key: "createTime", label: "注册时间", value: profile.createdAt },
    { key: "description", label: "描述", value: profile.description || "-" }
  ];

  return details;
};

const detail = ref<Detail[]>([]);


// 头像上传
const showAvatarUpload = () => {
    // 通过JavaScript创建input元素
  const fileInput = document.createElement("input");
  fileInput.type = "file";
  fileInput.accept = "image/*";
  fileInput.style.display = "none";

    // 添加change事件监听器
  fileInput.addEventListener("change", handleFileChange);

    // 触发文件选择
  fileInput.click();
};

// 处理文件选择
const handleFileChange = (event: Event) => {
    const input = event.target as HTMLInputElement;
    if (input.files && input.files[0]) {
        const file = input.files[0];
        // 保存文件名到ref中，以便后续使用
    selectedFileName.value = file.name;
    const reader = new FileReader();
    reader.onload = e => {
      cropperOptions.img = e.target?.result as string;
      avatarModalVisible.value = true;
    };
    reader.readAsDataURL(file);
    }
  input.value = "";
};

// 实时预览
const handleRealTime = (data: any) => {
    previewStyle.value = {
    width: `${data.w}px`,
    height: `${data.h}px`,
    overflow: "hidden",
    margin: "0",
    zoom: 160 / data.h
  };
  previews.value = data;
};

// 确认上传头像
const confirmUploadAvatar = () => {
    cropperRef.value.getCropBlob((data: Blob) => {
        // 上传头像
    const formData = new FormData();
    formData.append("file", data, selectedFileName.value);
    uploadAvatarAPI(formData).then(res => {
      const { data } = res;
      const avatarUrl = handleUrl(data.url);
      userInfo.value.avatar = avatarUrl;
      useUserStoreHook().account.avatar = avatarUrl;
      resetAvatarUpload();
      proxy.$message.success("头像上传成功");
    });
    });
};

// 重置头像上传
const resetAvatarUpload = () => {
  avatarModalVisible.value = false;
  cropperOptions.img = "";
};

const refresh = () => {
    getUserInfo();
};

const loading = ref<boolean>(false);
const userInfo = ref<ProfileItem>({} as ProfileItem);
const getUserInfo = async () => {
    try {
      loading.value = true;
      const data = await getProfileAPI();
      userInfo.value = data.data;
      userInfo.value.avatar = handleUrl(userInfo.value.avatar);
      detail.value = buildDetail(userInfo.value);
    } finally {
      loading.value = false;
    }
};


const sexOption = ref(dictFilter("gender"));
const getSexName = (sex: number) => {
  const sexItem = sexOption.value.find((item: any) => item.value === sex);
  return sexItem ? sexItem.name : "-";
};

getUserInfo();
routerStore.setTabsTitle(`用户${route.query.userName ? " - " + route.query.userName : "信息"}`);
</script>

<style lang="scss" scoped>
.userinfo-page {
  min-height: 100%;
}

.userinfo-content {
  :deep(.arco-card-body) {
    padding: 0;
  }
}

.userinfo-page :deep(.userinfo-form .arco-input-wrapper) {
  box-sizing: border-box;
  background: var(--uvp-search-control-bg) !important;
  border: 1px solid var(--uvp-search-secondary-btn-border) !important;
  border-radius: 10px !important;
  box-shadow: var(--uvp-search-control-shadow) !important;
}

.userinfo-page :deep(.userinfo-form .arco-input-wrapper:focus-within) {
  border-color: var(--uvp-brand) !important;
  box-shadow: var(--uvp-search-control-focus-shadow) !important;
}

.userinfo-page :deep(.userinfo-form .arco-textarea-wrapper) {
  background: var(--uvp-search-control-bg) !important;
  border: 1px solid var(--uvp-search-secondary-btn-border) !important;
  border-radius: 10px !important;
  box-shadow: var(--uvp-search-control-shadow) !important;
}

.userinfo-page :deep(.userinfo-form .arco-textarea-wrapper:focus-within) {
  border-color: var(--uvp-brand) !important;
  box-shadow: var(--uvp-search-control-focus-shadow) !important;
}

.userinfo-page :deep(.userinfo-form .arco-input::placeholder),
.userinfo-page :deep(.userinfo-form .arco-textarea::placeholder) {
  color: var(--uvp-text-tertiary) !important;
  opacity: 1;
}

.userinfo-page :deep(.userinfo-form .arco-btn) {
  box-sizing: border-box;
  border-radius: 10px;
}

.userinfo-page :deep(.userinfo-form .arco-form-item-extra) {
  color: var(--uvp-text-secondary);
  font-size: 13px;
  line-height: 20px;
}

.userinfo-profile {
  display: flex;
  align-items: center;
  gap: 28px;
  padding: 28px;

  :deep(.arco-descriptions-item-label) {
    color: var(--uvp-text-secondary) !important;
    background: transparent !important;
    font-weight: 500;
  }

  :deep(.arco-descriptions-item-value) {
    color: var(--uvp-text-primary);
  }
}

.userinfo-profile__details {
  flex: 1;
  min-width: 0;
}

.userinfo-divider {
  margin: 0 28px;
}

.userinfo-settings {
  padding: 24px 28px 28px;
}

.avatar-container {
  display: flex;
  flex: none;
  align-items: center;
  justify-content: center;

  &.mobile-avatar {
    margin-bottom: 12px;
  }
}

.userinfo-avatar {
  border-radius: 18px;
}

.avatar-upload-modal__actions {
  padding-top: 20px;
  text-align: center;
}

.avatar-preview {
  text-align: center;

  h4 {
    margin-bottom: 15px;
    font-weight: bold;
  }

  .preview-container {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 160px;
    height: 160px;
    margin: 0 auto;
    overflow: hidden;
    border: 1px dashed #ccc;
    border-radius: 18px;
  }

  p {
    margin-top: 10px;
    font-size: 12px;
    color: #999;
  }
}

.mobile-preview {
  margin-top: 20px;
}

@media (max-width: 768px) {
  .userinfo-profile {
    flex-direction: column;
    align-items: stretch;
    gap: 16px;
    padding: 20px;
  }

  .userinfo-divider {
    margin: 0 20px;
  }

  .userinfo-settings {
    padding: 20px;
  }

  .avatar-preview {
    .preview-container {
      width: 120px;
      height: 120px;
      border-radius: 16px;
    }
  }
}
</style>
