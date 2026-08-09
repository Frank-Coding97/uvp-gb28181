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

        expect(staticServiceConfigLabels).toHaveLength(15);
        expect(staticServiceConfigLabels).toContain("扩展 SDP 兼容模式");
        expect(draft.positionHistoryRetentionDays).toBe(7);
        expect(draft.ptzSpeed).toBe(56);
        expect(draft.sipTimeoutSec).toBe(10);
        expect(draft.notifyCacheMaxLength).toBe(10000);
        expect(playbackServiceConfigLabels).toHaveLength(6);
        expect(cascadeServiceConfigLabels).toHaveLength(7);
        expect(draft.playback.autoInvite).toBe(true);
        expect(draft.playback.inviteTimeoutMs).toBe(10000);
        expect(draft.playback.stopWhenUnwatched).toBe(true);
        expect(draft.cascade.parentInviteTimeoutMs).toBe(60000);
        expect(draft.cascade.intercomStreamMode).toBe("TCP被动");
        expect(draft.cascade.offlineRetryIntervalSec).toBe(60);
        expect(staticServiceConfigLabels).not.toContain("SIP平台 ID");
        expect(staticServiceConfigLabels).not.toContain("SIP端口");
        expect(staticServiceConfigLabels).not.toContain("SIP域");
        expect(staticServiceConfigLabels).not.toContain("SIP密码");
    });
});
