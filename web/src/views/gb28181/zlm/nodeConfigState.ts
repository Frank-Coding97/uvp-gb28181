import type { ConfigGroup, ConfigItem, ConfigMode, UpdateConfigResp } from "@/api/gb28181-zlm";

export const CONFIG_GROUP_ORDER = [
  "GB28181 国标",
  "运行时策略",
  "协议开关",
  "录制与截图",
  "Hook 回调",
  "性能调优",
  "网络端口",
  "安全"
] as const;

export function orderConfigGroups(groups: ConfigGroup[]) {
  const order = new Map<string, number>(CONFIG_GROUP_ORDER.map((name, index) => [name, index]));
  return groups
    .map((group, index) => ({ group, index }))
    .sort(
      (left, right) =>
        (order.get(left.group.name) ?? CONFIG_GROUP_ORDER.length) - (order.get(right.group.name) ?? CONFIG_GROUP_ORDER.length) ||
        left.index - right.index
    )
    .map(({ group }) => group);
}

export interface ConfigModePresentation {
  label: string;
  reason: string;
  tone: "success" | "warning" | "danger" | "neutral";
}

const modePresentations: Record<ConfigMode, ConfigModePresentation> = {
  hot_reload: {
    label: "可热更新",
    reason: "保存后由后端下发并立即回读核对实际值",
    tone: "success"
  },
  restart_required: {
    label: "重启后生效",
    reason: "保存到媒体节点并回读核对，重启媒体节点后生效",
    tone: "warning"
  },
  platform_managed: {
    label: "平台管理",
    reason: "该项由 UVP 平台管理，不能在服务器配置页覆盖",
    tone: "warning"
  },
  restart_required_unsupported: {
    label: "需重启（不支持）",
    reason: "当前版本不支持重启后应用，页面不会构造提交",
    tone: "danger"
  },
  read_only: {
    label: "只读",
    reason: "该项为只读配置，仅供查看，页面不会构造提交",
    tone: "neutral"
  }
};

export function configModePresentation(mode: ConfigMode): ConfigModePresentation {
  return modePresentations[mode] ?? modePresentations.read_only;
}

export function isConfigEditable(item: ConfigItem) {
  return item.mode === "hot_reload" || item.mode === "restart_required";
}

export function isHookRoutingChange(key: string) {
  return key === "hook.enable" || key.startsWith("hook.on_");
}

export function isNetworkPortChange(key: string) {
  return (
    key === "http.port" ||
    key === "http.sslport" ||
    key === "rtmp.port" ||
    key === "rtmp.sslport" ||
    key === "rtsp.port" ||
    key === "rtsp.sslport" ||
    key === "rtp_proxy.port" ||
    key === "rtp_proxy.port_range" ||
    key === "shell.port"
  );
}

export function configDictionaryOptions(
  item: ConfigItem,
  dictionaryItems: readonly { name?: string | null; value?: string | null; status?: number }[]
) {
  if (!item.dictCode) return [];
  const options = dictionaryItems
    .filter(option => option.status !== 0 && option.name && option.value)
    .map(option => ({ label: option.name as string, value: option.value as string }));
  if (options.length > 0) return options;
  if (item.dictCode === "status")
    return [
      { label: "禁用", value: "0" },
      { label: "启用", value: "1" }
    ];
  return [];
}

export function buildConfigChanges(items: ConfigItem[], drafts: Record<string, string>) {
  const editable = new Set(items.filter(isConfigEditable).map(item => item.key));
  return Object.fromEntries(Object.entries(drafts).filter(([key]) => editable.has(key)));
}

export interface ConfigUpdateResultRow {
  key: string;
  oldValue: string;
  newValue: string;
  commandResult: string;
  actualValue: string;
  tone: "success" | "warning" | "danger";
}

export function updateResultRows(
  items: ConfigItem[],
  changes: Record<string, string>,
  response: UpdateConfigResp
): ConfigUpdateResultRow[] {
  const byKey = new Map(items.map(item => [item.key, item]));
  const applied = new Set(response.applied ?? []);
  const mismatched = new Set(response.mismatched ?? []);
  const requiresRestart = new Set(response.requiresRestart ?? []);
  const unknown = new Set(response.unknown ?? []);
  return Object.entries(changes).map(([key, newValue]) => {
    const oldValue = byKey.get(key)?.value ?? "";
    const actualValue = response.actual?.[key] ?? (applied.has(key) || requiresRestart.has(key) ? newValue : "未返回");
    if (mismatched.has(key)) {
      return { key, oldValue, newValue, commandResult: "回读不一致", actualValue, tone: "danger" };
    }
    if (applied.has(key)) {
      return { key, oldValue, newValue, commandResult: "已回读生效", actualValue, tone: "success" };
    }
    if (requiresRestart.has(key)) {
      return { key, oldValue, newValue, commandResult: "已保存，重启媒体节点后生效", actualValue, tone: "warning" };
    }
    if (unknown.has(key)) {
      return { key, oldValue, newValue, commandResult: "后端拒绝：未知配置", actualValue, tone: "danger" };
    }
    return { key, oldValue, newValue, commandResult: "未确认生效", actualValue, tone: "danger" };
  });
}
