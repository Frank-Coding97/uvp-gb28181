import { computed, onMounted, onUnmounted, ref } from "vue";
import {
  clearIgnoredServiceUpgradeNotice,
  getServiceUpgradeNotice,
  ignoreServiceUpgradeNotice,
  setServiceUpgradeAutoCheck,
  type UpgradeNotice
} from "@/api/service-upgrade";
import { APP_VERSION } from "@/config/version";
import { hasPermission } from "@/hooks/usePermission";

/**
 * 侧边栏版本徽标的数据源。
 *
 * ⛔ 这里**只读缓存**：真正查官方源的是升级助手的后台任务（每 6 小时一次）。
 *    在布局层发检查请求会让每个用户每次开页都打一次官方源，也会让离线部署
 *    每次开页都报一次错。
 *
 * 页面开着很久时靠 30 分钟轮询把后台的新结果取回来；徽标本身不阻塞渲染，
 * 拿不到数据就退回「只显示本地版本号」。
 */
const NOTICE_REFRESH_INTERVAL = 30 * 60 * 1000;

/** 能进服务升级页的人就看到提示；接口按这条权限放行，否则会 403。 */
const NOTICE_PERMISSION = "system:upgrade:read";
/** 「忽略此版本」是写操作，只有管理权限才能点。 */
const IGNORE_PERMISSION = "system:upgrade:manage";
/** 定时检查开关同样是管理动作：改的是整台机器的行为，不是个人偏好。 */
const AUTO_CHECK_PERMISSION = "system:upgrade:manage";

export function useUpgradeNotice() {
  const notice = ref<UpgradeNotice>();
  const refreshing = ref(false);
  const ignoring = ref(false);
  let timer: ReturnType<typeof setInterval> | undefined;

  const canRead = computed(() => hasPermission(NOTICE_PERMISSION));
  const canIgnore = computed(() => hasPermission(IGNORE_PERMISSION));
  const currentVersion = computed(() => notice.value?.currentVersion || APP_VERSION);
  const latestVersion = computed(() => notice.value?.latestVersion || "");
  /**
   * 橙点只在「确实有更新、且没被忽略、且这台机器能连官方源」三者同时成立时出现。
   * 忽略只作用于当前这一个版本：出现更新版本时后端返回的 ignored 会自动变 false。
   */
  const pending = computed(
    () => canRead.value && Boolean(notice.value?.checkEnabled && notice.value?.hasUpdate && !notice.value?.ignored)
  );
  const checkedAt = computed(() => notice.value?.checkedAt || "");
  /**
   * 定时检查默认开启（后端缺文件即视为开），所以只有明确的 false 才算关闭。
   * 没有官方源（离线部署 / 助手未运行）时开关没有意义，直接置灰。
   */
  const autoCheckEnabled = computed(() => notice.value?.autoCheck !== false);
  const canSetAutoCheck = computed(
    () => canRead.value && Boolean(notice.value?.checkEnabled) && hasPermission(AUTO_CHECK_PERMISSION)
  );
  const settingAutoCheck = ref(false);

  const refresh = async () => {
    if (!hasPermission(NOTICE_PERMISSION)) return;
    refreshing.value = true;
    try {
      const response = await getServiceUpgradeNotice();
      notice.value = response.data;
    } catch {
      // 助手未启动、离线部署、权限刚被回收：都按「没有更新信息」处理，
      // 徽标退回只显示版本号，不打扰用户。
    } finally {
      refreshing.value = false;
    }
  };

  const ignoreCurrentVersion = async () => {
    const version = latestVersion.value;
    if (!version || ignoring.value) return;
    ignoring.value = true;
    try {
      await ignoreServiceUpgradeNotice(version);
      await refresh();
    } finally {
      ignoring.value = false;
    }
  };

  const restoreIgnoredVersion = async () => {
    if (ignoring.value) return;
    ignoring.value = true;
    try {
      await clearIgnoredServiceUpgradeNotice();
      await refresh();
    } finally {
      ignoring.value = false;
    }
  };

  /**
   * 切换定时检查。成功返回 true，失败返回 false 由调用方提示——
   * 这个 hook 同时服务侧边栏徽标，那里不该弹 toast。
   */
  const setAutoCheck = async (enabled: boolean) => {
    if (settingAutoCheck.value) return false;
    settingAutoCheck.value = true;
    try {
      await setServiceUpgradeAutoCheck(enabled);
      await refresh();
      return true;
    } catch {
      return false;
    } finally {
      settingAutoCheck.value = false;
    }
  };

  onMounted(() => {
    void refresh();
    timer = setInterval(refresh, NOTICE_REFRESH_INTERVAL);
  });
  onUnmounted(() => {
    if (timer) clearInterval(timer);
  });

  return {
    notice,
    canRead,
    canIgnore,
    currentVersion,
    latestVersion,
    checkedAt,
    pending,
    refreshing,
    ignoring,
    refresh,
    ignoreCurrentVersion,
    restoreIgnoredVersion,
    autoCheckEnabled,
    canSetAutoCheck,
    settingAutoCheck,
    setAutoCheck
  };
}
