import type { ConfigItem, ConfigMode, UpdateConfigResp } from "@/api/gb28181-zlm";

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
  return item.mode === "hot_reload";
}

export function buildHotReloadChanges(items: ConfigItem[], drafts: Record<string, string>) {
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

export function updateResultRows(items: ConfigItem[], changes: Record<string, string>, response: UpdateConfigResp): ConfigUpdateResultRow[] {
  const byKey = new Map(items.map(item => [item.key, item]));
  const applied = new Set(response.applied ?? []);
  const mismatched = new Set(response.mismatched ?? []);
  const requiresRestart = new Set(response.requiresRestart ?? []);
  const unknown = new Set(response.unknown ?? []);
  return Object.entries(changes).map(([key, newValue]) => {
    const oldValue = byKey.get(key)?.value ?? "";
    const actualValue = response.actual?.[key] ?? (applied.has(key) ? newValue : "未返回");
    if (mismatched.has(key)) {
      return { key, oldValue, newValue, commandResult: "回读不一致", actualValue, tone: "danger" };
    }
    if (applied.has(key)) {
      return { key, oldValue, newValue, commandResult: "已回读生效", actualValue, tone: "success" };
    }
    if (requiresRestart.has(key)) {
      return { key, oldValue, newValue, commandResult: "后端拒绝：需重启", actualValue, tone: "warning" };
    }
    if (unknown.has(key)) {
      return { key, oldValue, newValue, commandResult: "后端拒绝：未知配置", actualValue, tone: "danger" };
    }
    return { key, oldValue, newValue, commandResult: "未确认生效", actualValue, tone: "danger" };
  });
}
