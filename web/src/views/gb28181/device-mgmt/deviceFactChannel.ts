import type { ChannelVO } from "./api";

/**
 * 设备详情抽屉里「设备控制 / 录像存储 / 报警控制 / 设备状态 / 存储卡」五页的通道选项。
 *
 * ⛔ 这些内容都是**通道级**的：接口是 `/channel/:id/device-control`、
 *    `/channel/:id/device-configs`、`/channel/:id/device-status`、`/channel/:id/storage-cards`，
 *    落库也按 `(device_id, target_code=通道编码)` 记（见 controllers/device_storage_card.go）。
 *    所以「设备详情」这个设备级入口必须先落到一个具体通道上，才能问出事实 ——
 *    这不是界面取舍，是数据模型的形状。
 */
export interface FactChannelOption {
  value: number;
  label: string;
  online: boolean;
}

export function factChannelLabel(channel: ChannelVO): string {
  const name = String(channel?.alias || channel?.name || "").trim();
  const code = String(channel?.channelId || "").trim();
  if (name && code) return `${name} · ${code}`;
  return name || code || `通道 #${channel?.id ?? ""}`;
}

export function buildFactChannelOptions(channels: ChannelVO[] | null | undefined): FactChannelOption[] {
  const list = Array.isArray(channels) ? channels : [];
  return list
    .filter(channel => Number.isFinite(channel?.id) && channel.id > 0)
    .map(channel => ({ value: channel.id, label: factChannelLabel(channel), online: channel.status === 1 }));
}

/**
 * 默认落到**第一个在线通道**。
 *
 * 离线通道不是不能查（缓存里还有上一次的事实），只是查了也不会变，
 * 所以全离线时退回第一个通道，而不是空选 —— 空选会让面板停在"无目标"上，
 * 用户得自己再选一次才有内容看。
 */
export function pickDefaultFactChannel(options: FactChannelOption[], current: number | null): number | null {
  if (options.length === 0) return null;
  if (current !== null && options.some(option => option.value === current)) return current;
  const online = options.find(option => option.online);
  return (online ?? options[0]).value;
}
