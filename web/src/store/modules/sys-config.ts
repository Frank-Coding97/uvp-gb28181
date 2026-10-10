import { defineStore } from "pinia";
import persistedstateConfig from "@/store/config/index";
import { ref, computed } from "vue";

import { getConfigAPI, updateConfigAPI } from "@/api/sysconfig";
import { defaultLogCleanupConfig } from "@/api/sysconfig";
import type { SystemConfig, SafeConfig, CaptchaConfig, ConfigRequestData, LogCleanupConfig } from "@/api/sysconfig";
import { handleUrl } from "@/utils/app";
/**
 * 系统配置管理
 * @methods getConfig 获取系统配置
 * @methods updateConfig 更新系统配置
 */
const sysConfigStore = () => {
  // 系统配置数据
  const systemConfig = ref<SystemConfig>({
    systemLogo: "",
    systemIcon: "",
    systemName: "",
    systemCopyright: "",
    systemRecordNo: "",
    defaultusername: "",
    defaultpassword: ""
  });

  // 安全配置数据
  const safeConfig = ref<SafeConfig>({
    loginLockThreshold: 0,
    loginLockExpire: 0,
    loginLockDuration: 0,
    minPasswordLength: 0,
    requireSpecialChar: false
  });

  // 验证码配置数据
  const captchaConfig = ref<CaptchaConfig>({
    open: false,
    length: 0
  });

  const logCleanupConfig = ref<LogCleanupConfig>({ ...defaultLogCleanupConfig });

  // 配置加载状态
  const loading = ref(false);

  // 处理后的系统Logo URL
  const systemLogo = computed(() => {
    return handleUrl(systemConfig.value.systemLogo);
  });

  // 处理后的系统Icon URL
  const systemIcon = computed(() => {
    return handleUrl(systemConfig.value.systemIcon);
  });

  // 获取系统配置
  async function getConfig() {
    try {
      loading.value = true;
      const { data } = await getConfigAPI();

      if (data) {
        systemConfig.value = data.system || systemConfig.value;
        safeConfig.value = data.safe || safeConfig.value;
        captchaConfig.value = data.captcha || captchaConfig.value;
        logCleanupConfig.value = data.logCleanup || logCleanupConfig.value;
      }

      return data;
    } catch (error) {
      console.error("获取系统配置失败:", error);
      throw error;
    } finally {
      loading.value = false;
    }
  }

  // 更新系统配置
  async function updateConfig(configData: ConfigRequestData) {
    try {
      loading.value = true;
      const response = await updateConfigAPI(configData);
      if (response.code !== 0) throw new Error(response.message || "更新系统配置失败");

      const { data } = response;

      // 保存接口成功后先同步本次提交内容，再用后端返回值覆盖服务器管理字段。
      if ("system" in configData && configData.system) {
        systemConfig.value = { ...systemConfig.value, ...configData.system };
      }
      if ("safe" in configData && configData.safe) {
        safeConfig.value = { ...safeConfig.value, ...configData.safe };
      }
      if ("captcha" in configData && configData.captcha) {
        captchaConfig.value = { ...captchaConfig.value, ...configData.captcha };
      }
      if ("logCleanup" in configData) {
        logCleanupConfig.value = { ...logCleanupConfig.value, ...configData.logCleanup };
      }
      if (data?.logCleanup) logCleanupConfig.value = data.logCleanup;

      // 更新接口可能没有 data；刷新完整配置，确认服务端最终持久化值及 configured 标记。
      try {
        await getConfig();
      } catch (error) {
        console.warn("配置已保存，但刷新系统配置失败:", error);
      }

      return response;
    } catch (error) {
      console.error("更新系统配置失败:", error);
      throw error;
    } finally {
      loading.value = false;
    }
  }

  // 重置配置
  function resetConfig() {
    systemConfig.value = {
      systemLogo: "",
      systemIcon: "",
      systemName: "",
      systemCopyright: "",
      systemRecordNo: "",
      defaultusername: "",
      defaultpassword: ""
    };

    safeConfig.value = {
      loginLockThreshold: 0,
      loginLockExpire: 0,
      loginLockDuration: 0,
      minPasswordLength: 0,
      requireSpecialChar: false
    };

    captchaConfig.value = {
      open: false,
      length: 0
    };

    logCleanupConfig.value = { ...defaultLogCleanupConfig };
  }

  return {
    systemConfig,
    safeConfig,
    captchaConfig,
    logCleanupConfig,
    loading,
    systemLogo,
    systemIcon,
    getConfig,
    updateConfig,
    resetConfig
  };
};

export const useSysConfigStore = defineStore("sys-config", sysConfigStore, {
  persist: persistedstateConfig("sys-config", ["systemConfig", "safeConfig", "captchaConfig", "logCleanupConfig"])
});
