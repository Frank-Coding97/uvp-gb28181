import { describe, expect, it } from "vitest";
import {
    cascadeServiceConfigLabels,
    createStaticServiceConfigDraft,
    playbackServiceConfigLabels,
    staticServiceConfigLabels
} from "./serviceConfigState";

describe("static service config draft", () => {
    it("matches the reference controls without SIP access identity fields", () => {
        const draft = createStaticServiceConfigDraft();
        expect(draft.defaultChannelStreamTransport).toBe("TCP-Passive");
        expect(draft.globalSubscriptionItems).toEqual([]);
        expect(draft.defaultChannelAudioEnabled).toBe(true);

        expect(staticServiceConfigLabels).toHaveLength(15);
        expect(staticServiceConfigLabels).toContain("新通道默认流传输模式");
        expect(staticServiceConfigLabels).toContain("全局订阅项目");
        expect(staticServiceConfigLabels).toContain("全局通道开启音频");
        expect(staticServiceConfigLabels).toContain("扩展 SDP 兼容模式");
        expect(staticServiceConfigLabels).toContain("云台默认速度");
        expect(staticServiceConfigLabels).toContain("忽略通道离线/异常通知");
        expect(staticServiceConfigLabels).toContain("心跳恢复设备在线状态");
        expect(staticServiceConfigLabels).toContain("SIP 命令超时时间（秒）");
        expect(draft.positionHistoryRetentionDays).toBe(7);
        expect(draft.sipLogRetentionDays).toBe(7);
        expect(staticServiceConfigLabels).toContain("SIP 日志保留天数（天）");
        expect(draft.ptzSpeed).toBe(6);
        expect(draft.syncChannelsOnOnline).toBe(true);
        expect(draft.ignoreChannelOfflineStatusNotify).toBe(false);
        expect(draft.sipTimeoutSec).toBe(10);
        expect(draft).not.toHaveProperty("notifyCacheMaxLength");
        expect(draft).not.toHaveProperty("useRequestIpAsStreamIp");
        expect(draft).not.toHaveProperty("useDeviceSourceIpAsReplyIp");
        expect(draft).not.toHaveProperty("broadcastMissingGbId");
        expect(staticServiceConfigLabels).not.toContain("使用来源请求ip作为streamIp");
        expect(staticServiceConfigLabels).not.toContain("是否使用设备来源IP作为回复IP");
        expect(staticServiceConfigLabels).not.toContain("缺少国标ID是否给所有上级发送消息");
        expect(staticServiceConfigLabels).not.toContain("设置notify缓存队列最大长度");
        expect(playbackServiceConfigLabels).toHaveLength(6);
        expect(playbackServiceConfigLabels).not.toContain("推流是否录制");
        expect(playbackServiceConfigLabels).not.toContain("推流鉴权");
        expect(cascadeServiceConfigLabels).toHaveLength(7);
        expect(draft.playback.fixedAddressEnabled).toBe(false);
        expect(draft.playback.autoOnDemandEnabled).toBe(false);
        expect(draft.playback).not.toHaveProperty("autoInvite");
        expect(draft.playback.playTimeoutMs).toBe(10000);
        expect(draft.playback.onDemandLive).toBe(true);
        expect(draft.playback.cloudRecordingEnabled).toBe(false);
        expect(playbackServiceConfigLabels).toContain("按需直播");
        expect(playbackServiceConfigLabels).toContain("固定播放地址");
        expect(playbackServiceConfigLabels).toContain("自动点播");
        expect(playbackServiceConfigLabels).not.toContain("是否开启无人观看自动停止");
        expect(draft.playback).not.toHaveProperty("recordPushStream");
        expect(draft.playback).not.toHaveProperty("pushAuth");
        expect(draft.cascade.parentInviteTimeoutMs).toBe(60000);
        expect(draft.cascade.intercomStreamMode).toBe("TCP被动");
        expect(draft.cascade.offlineRetryIntervalSec).toBe(60);
        expect(staticServiceConfigLabels).not.toContain("SIP平台 ID");
        expect(staticServiceConfigLabels).not.toContain("SIP端口");
        expect(staticServiceConfigLabels).not.toContain("SIP域");
        expect(staticServiceConfigLabels).not.toContain("SIP密码");
    });
});
