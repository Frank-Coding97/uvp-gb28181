<template>
  <div class="snow-fill sysconfig-page">
    <div class="snow-fill-inner">
      <a-tabs class="uvp-system-tabs sysconfig-tabs" v-model:active-key="activeTab" :animation="true">
        <template #extra>
          <div class="sysconfig-tabs__actions">
            <a-button
              type="primary"
              :loading="sysConfigStore.loading"
              :disabled="sysConfigStore.loading"
              @click="onSave"
              v-hasPerm="['system:config:update']"
            >
              <template #icon>
                <icon-save />
              </template>
              <span>保存配置</span>
            </a-button>
          </div>
        </template>

        <!-- 服务器配置 -->
        <a-tab-pane key="server" title="服务器配置">
          <a-card :bordered="false" class="uvp-system-panel uvp-system-panel--dense mb-4">
            <a-form class="uvp-system-form" :layout="layoutMode.layout" :model="configData.system" auto-label-width>
              <a-row :gutter="24">
                <a-col :span="24">
                  <a-form-item field="systemLogo" label="系统Logo">
                    <!-- 图片上传组件 -->
                    <ImageUpload
                      :width="50"
                      :height="50"
                      v-model="configData.system.systemLogo"
                      :title="'系统Logo'"
                      :accept="'.svg'"
                    />
                    <template #extra>
                      <div>显示在登录页面和系统导航栏的网站图标（建议 .svg 格式）</div>
                    </template>
                  </a-form-item>
                </a-col>
                <a-col :span="24">
                  <a-form-item field="systemIcon" label="系统图标">
                    <!-- 图片上传组件 -->
                    <ImageUpload
                      :width="50"
                      :height="50"
                      v-model="configData.system.systemIcon"
                      :title="'系统图标'"
                      :accept="'.ico'"
                    />
                    <template #extra>
                      <div>浏览器标签页显示的网站图标（建议 .ico 格式）</div>
                    </template>
                  </a-form-item>
                </a-col>

                <a-col :span="isMobile ? 24 : 12">
                  <a-form-item field="systemName" label="系统名称">
                    <a-input v-model="configData.system.systemName" allow-clear placeholder="请输入系统名称" />
                    <template #extra>
                      <div>显示在浏览器标题栏和登录界面的系统名称</div>
                    </template>
                  </a-form-item>
                </a-col>

                <a-col :span="isMobile ? 24 : 12">
                  <a-form-item field="systemCopyright" label="版权信息">
                    <a-input v-model="configData.system.systemCopyright" allow-clear placeholder="请输入版权信息" />
                    <template #extra>
                      <div>显示在页面底部的版权声明文本</div>
                    </template>
                  </a-form-item>
                </a-col>
                <a-col :span="isMobile ? 24 : 12">
                  <a-form-item field="systemRecordNo" label="备案号">
                    <a-input v-model="configData.system.systemRecordNo" allow-clear placeholder="请输入备案号" />
                    <template #extra>
                      <div>工信部 ICP 备案编号 如：粤ICP备12345678号</div>
                    </template>
                  </a-form-item>
                </a-col>
              </a-row>
              <a-row>
                <a-col :span="isMobile ? 24 : 12">
                  <a-form-item field="defaultusername" label="默认用户名">
                    <a-input v-model="configData.system.defaultusername" allow-clear placeholder="请输入默认用户名" />
                    <template #extra>
                      <div>系统默认登录用户名</div>
                    </template>
                  </a-form-item>
                </a-col>

                <a-col :span="isMobile ? 24 : 12">
                  <a-form-item field="defaultpassword" label="默认密码">
                    <a-input-password v-model="configData.system.defaultpassword" allow-clear placeholder="请输入默认密码" />
                    <template #extra>
                      <div>系统默认登录密码</div>
                    </template>
                  </a-form-item>
                </a-col>
              </a-row>
            </a-form>
          </a-card>
        </a-tab-pane>

        <!-- 验证码配置 -->
        <a-tab-pane key="captcha" title="验证码配置">
          <a-card :bordered="false" class="uvp-system-panel uvp-system-panel--dense mb-4">
            <a-form class="uvp-system-form" :layout="layoutMode.layout" :model="configData.captcha" auto-label-width>
              <a-row :gutter="24">
                <a-col :span="isMobile ? 24 : 12">
                  <a-form-item field="open" label="启用验证码">
                    <a-switch v-model="configData.captcha.open">
                      <template #checked>开启</template>
                      <template #unchecked>关闭</template>
                    </a-switch>
                  </a-form-item>
                </a-col>
              </a-row>

              <a-row :gutter="24">
                <a-col :span="isMobile ? 24 : 12">
                  <a-form-item field="length" label="验证码长度">
                    <s-number-field
                      ref="captchaLengthField"
                      v-model="configData.captcha.length"
                      :min="4"
                      :max="8"
                      required
                      placeholder="请输入验证码长度"
                    />
                    <template #extra>
                      <div>验证码字符长度，默认4位</div>
                    </template>
                  </a-form-item>
                </a-col>
              </a-row>
            </a-form>
          </a-card>
        </a-tab-pane>

        <a-tab-pane key="safe" title="安全配置">
          <a-card :bordered="false" class="uvp-system-panel uvp-system-panel--dense mb-4">
            <a-form class="uvp-system-form" :layout="layoutMode.layout" :model="configData.safe" auto-label-width>
              <a-row :gutter="24">
                <a-col :span="isMobile ? 24 : 12">
                  <a-form-item field="loginLockThreshold" label="登录失败锁定阈值(次)">
                    <s-number-field
                      ref="loginLockThresholdField"
                      v-model="configData.safe.loginLockThreshold"
                      :min="0"
                      :max="10"
                      required
                      placeholder="请输入登录失败次数"
                    />
                    <template #extra>
                      <div>指定时间内登录失败次数达到此阈值后，账号将被锁定，0表示不限制</div>
                    </template>
                  </a-form-item>
                </a-col>
                <a-col :span="isMobile ? 24 : 12">
                  <a-form-item field="loginLockExpire" label="登录失败次数缓存时间(秒)">
                    <s-number-field
                      ref="loginLockExpireField"
                      v-model="configData.safe.loginLockExpire"
                      :min="1"
                      :max="1440"
                      required
                      placeholder="请输入缓存时间"
                    />
                    <template #extra>
                      <div>指定时间内登录失败次数缓存时间，单位秒</div>
                    </template>
                  </a-form-item>
                </a-col>
                <a-col :span="isMobile ? 24 : 12">
                  <a-form-item field="loginLockDuration" label="账号锁定时长(秒)">
                    <s-number-field
                      ref="loginLockDurationField"
                      v-model="configData.safe.loginLockDuration"
                      :min="1"
                      :max="1440"
                      required
                      placeholder="请输入锁定时长"
                    />
                    <template #extra>
                      <div>账号锁定时长，单位秒</div>
                    </template>
                  </a-form-item>
                </a-col>
                <a-col :span="isMobile ? 24 : 12">
                  <a-form-item field="minPasswordLength" label="密码最小长度">
                    <s-number-field
                      ref="minPasswordLengthField"
                      v-model="configData.safe.minPasswordLength"
                      :min="6"
                      :max="32"
                      required
                      placeholder="请输入密码最小长度"
                    />
                    <template #extra>
                      <div>密码最小长度，默认6位</div>
                    </template>
                  </a-form-item>
                </a-col>
                <a-col :span="isMobile ? 24 : 12">
                  <a-form-item field="requireSpecialChar" label="密码必须包含特殊字符">
                    <a-switch v-model="configData.safe.requireSpecialChar">
                      <template #checked>是</template>
                      <template #unchecked>否</template>
                    </a-switch>
                    <template #extra>
                      <div>是否要求密码必须包含特殊字符（如：!@#$%）</div>
                    </template>
                  </a-form-item>
                </a-col>
              </a-row>
            </a-form>
          </a-card>
        </a-tab-pane>

        <a-tab-pane key="logCleanup" title="日志清理">
          <a-card :bordered="false" class="uvp-system-panel uvp-system-panel--dense mb-4">
            <a-alert class="mb-4" type="warning">
              保留天数按 1–365 天配置。保存只更新策略，不会立即删除数据；清理任务按计划执行。
            </a-alert>
            <a-form class="uvp-system-form" :layout="layoutMode.layout" :model="logCleanupDraft" auto-label-width>
              <a-row :gutter="24">
                <a-col v-for="field in logCleanupFields" :key="field.key" :span="isMobile ? 24 : 12">
                  <a-form-item :field="field.key" :label="field.label">
                    <s-number-field
                      :ref="instance => setLogCleanupField(field.key, instance)"
                      v-model="logCleanupDraft[field.key]"
                      :min="1"
                      :max="365"
                      required
                      placeholder="请输入 1–365 天"
                      :disabled="sysConfigStore.loading"
                    />
                    <template #extra>
                      <div>{{ field.hint }}</div>
                    </template>
                  </a-form-item>
                </a-col>
              </a-row>
            </a-form>
          </a-card>
        </a-tab-pane>
      </a-tabs>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, computed, reactive } from "vue";
import { useRoute } from "vue-router";
import { useSysConfigStore } from "@/store/modules/sys-config";
import { defaultLogCleanupConfig } from "@/api/sysconfig";
import type { LogCleanupDraft, LogCleanupUpdateConfig, RegularConfigRequestData } from "@/api/sysconfig";
import ImageUpload from "@/components/upload/image-upload.vue";
import { useDevicesSize } from "@/hooks/useDevicesSize";
import SNumberField from "@/components/s-number-field/index.vue";
import { buildConfigUpdatePayload, isLogCleanupConfigValid, logCleanupPageRoute } from "./sysconfigState";
const { isMobile } = useDevicesSize();
const route = useRoute();
const layoutMode = computed(() => {
  let info = {
    mobile: {
      width: "95%",
      layout: "vertical"
    },
    desktop: {
      width: "40%",
      layout: "horizontal"
    }
  };
  return isMobile.value ? info.mobile : info.desktop;
});

const activeTab = ref(
  typeof route.query.tab === "string" && route.query.tab === logCleanupPageRoute.query.tab
    ? logCleanupPageRoute.query.tab
    : "server"
);

// 使用系统配置 store
const sysConfigStore = useSysConfigStore();

// 配置数据（使用 store 中的数据）
const configData = ref({
  system: sysConfigStore.systemConfig,
  captcha: sysConfigStore.captchaConfig,
  safe: sysConfigStore.safeConfig
});
const logCleanupDraft = reactive<LogCleanupDraft>({ ...defaultLogCleanupConfig });
const logCleanupFields: Array<{ key: keyof LogCleanupUpdateConfig; label: string; hint: string }> = [
  { key: "sipRetentionDays", label: "SIP 日志保留天数", hint: "SIP 信令和已结束诊断数据的保留期。" },
  { key: "operationRetentionDays", label: "操作日志保留天数", hint: "包含已软删除的过期操作日志。" },
  { key: "loginRetentionDays", label: "登录日志保留天数", hint: "登录审计记录的保留期。" },
  { key: "jobRetentionDays", label: "定时任务日志保留天数", hint: "只清理执行结果，不删除任务定义。" },
  { key: "playbackRetentionDays", label: "播放日志保留天数", hint: "活动中的播放记录会继续保留。" },
  { key: "schedulerRetentionDays", label: "调度日志保留天数", hint: "媒体节点选择记录的保留期。" }
];
type NumberFieldInstance = InstanceType<typeof SNumberField>;
const captchaLengthField = ref<NumberFieldInstance | null>(null);
const loginLockThresholdField = ref<NumberFieldInstance | null>(null);
const loginLockExpireField = ref<NumberFieldInstance | null>(null);
const loginLockDurationField = ref<NumberFieldInstance | null>(null);
const minPasswordLengthField = ref<NumberFieldInstance | null>(null);
const numberFields = computed(() => [
  captchaLengthField,
  loginLockThresholdField,
  loginLockExpireField,
  loginLockDurationField,
  minPasswordLengthField
]);
const logCleanupNumberFields = new Map<keyof LogCleanupUpdateConfig, NumberFieldInstance>();

function setLogCleanupField(key: keyof LogCleanupUpdateConfig, instance: unknown) {
  if (instance) logCleanupNumberFields.set(key, instance as NumberFieldInstance);
  else logCleanupNumberFields.delete(key);
}

// 获取配置信息
const getConfig = async () => {
  try {
    await sysConfigStore.getConfig();
    // 更新本地引用
    configData.value = {
      system: sysConfigStore.systemConfig,
      captcha: sysConfigStore.captchaConfig,
      safe: sysConfigStore.safeConfig
    };
    Object.assign(logCleanupDraft, sysConfigStore.logCleanupConfig);
  } catch (error) {
    console.error("获取配置失败:", error);
    arcoMessage("error", "获取配置失败");
  }
};

// 保存配置
const onSave = async () => {
  const numberFieldError =
    activeTab.value === "logCleanup"
      ? [...logCleanupNumberFields.values()].map(field => field.error || "").find(Boolean)
      : numberFields.value.map(field => field.value?.error || "").find(Boolean);
  if (numberFieldError) {
    arcoMessage("error", numberFieldError);
    return;
  }
  if (activeTab.value === "logCleanup" && !isLogCleanupConfigValid(logCleanupDraft)) {
    arcoMessage("error", "请输入 1-365 之间的整数");
    return;
  }
  try {
    const payload = buildConfigUpdatePayload(activeTab.value, configData.value as RegularConfigRequestData, logCleanupDraft);
    const response = await sysConfigStore.updateConfig(payload);
    if (activeTab.value === "logCleanup") {
      const savedConfig = response?.data?.logCleanup;
      if (savedConfig) Object.assign(logCleanupDraft, savedConfig);
      else Object.assign(logCleanupDraft, sysConfigStore.logCleanupConfig);
    }
    arcoMessage("success", "保存成功");
  } catch (error) {
    console.error("保存配置失败:", error);
    arcoMessage("error", (error as Error)?.message || "保存配置失败");
  }
};

onMounted(() => {
  getConfig();
});
</script>

<style lang="scss" scoped>
.sysconfig-page :deep(.uvp-system-form .arco-input-wrapper) {
  box-sizing: border-box;
  background: var(--uvp-search-control-bg) !important;
  border: 1px solid var(--uvp-search-secondary-btn-border) !important;
  border-radius: 10px !important;
  box-shadow: var(--uvp-search-control-shadow) !important;
}

.sysconfig-page :deep(.uvp-system-form .arco-input-wrapper:focus-within) {
  border-color: var(--uvp-brand) !important;
  box-shadow: var(--uvp-search-control-focus-shadow) !important;
}

.sysconfig-page :deep(.uvp-system-form .arco-input::placeholder) {
  color: var(--uvp-text-tertiary) !important;
  opacity: 1;
}

.sysconfig-page :deep(.uvp-system-form .arco-form-item-label) {
  font-weight: 500;
  color: var(--uvp-text-primary);
}

.sysconfig-page :deep(.uvp-system-form .arco-form-item-extra) {
  font-size: 13px;
  line-height: 20px;
  color: var(--uvp-text-secondary);
}

.sysconfig-page :deep(.sysconfig-tabs__actions .arco-btn) {
  box-sizing: border-box;
  border-radius: 10px;
}

.sysconfig-tabs {
  :deep(.arco-tabs-nav) {
    display: flex;
    gap: 12px;
    align-items: flex-start;
  }

  :deep(.arco-tabs-nav-tab) {
    flex: 0 0 auto;
  }

  :deep(.arco-tabs-nav-extra) {
    display: flex;
    align-items: center;
    padding-right: 0;
    margin-left: auto;
  }
}

.sysconfig-tabs__actions {
  display: flex;
  align-items: center;
}

.mb-4 {
  margin-bottom: 16px;
}
</style>
