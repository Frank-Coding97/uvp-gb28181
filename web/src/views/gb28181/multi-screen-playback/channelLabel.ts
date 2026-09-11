import type { ChannelVO } from "../device-mgmt/api";

type NameSource = Pick<ChannelVO, "alias" | "name" | "channelId"> | null | undefined;

/**
 * 通道展示名：优先用「设备列表 - 通道 - 编辑」里维护的通道别名，
 * 没有别名才回落到国标上报的通道名称，最后是通道编码。
 */
export function channelDisplayName(channel: NameSource, fallback = ""): string {
    return channel?.alias?.trim() || channel?.name?.trim() || channel?.channelId || fallback;
}
