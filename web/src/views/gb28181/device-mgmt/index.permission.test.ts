import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/device-mgmt/index.vue"), "utf8");

describe("device-mgmt guest permission boundary", () => {
    it("defines the read, playback, recording and runtime permission gates", () => {
        expect(source).toContain('const hasPermission = (permission: string) => permissions.value.includes("*:*:*") || permissions.value.includes(permission);');
        for (const permission of [
            "canViewDevices",
            "canStartPlayback",
            "canQueryDeviceRecords",
            "canViewTraffic",
            "canAddDevice",
            "canEditDevice",
            "canDeleteDevice",
            "canRefreshCatalog",
            "canManageSubscriptions",
            "canEditChannel",
            "canEditTransport",
            "canUpdateRecording",
            "canStopPlayback"
        ]) {
            expect(source).toContain(`const ${permission} = computed`);
        }
    });

    it("rechecks permissions at every device-management action entry", () => {
        const guardedEntries = [
            ["showDeviceChannels", "canViewDevices"],
            ["openChannel", "canViewDevices"],
            ["openDevice", "canViewDevices"],
            ["openSubscriptionManager", "canManageSubscriptions"],
            ["openStatusEvents", "canViewTraffic"],
            ["playChannel", "canStartPlayback"],
            ["openRecordQuery", "canQueryDeviceRecords"],
            ["handleRefreshDeviceCatalog", "canRefreshCatalog"],
            ["openCreateDeviceModal", "canAddDevice"],
            ["handleCreateDevice", "canAddDevice"],
            ["openEditDeviceModal", "canEditDevice"],
            ["handleEditDevice", "canEditDevice"],
            ["openEditChannelModal", "canEditChannel"],
            ["handleEditChannel", "canEditChannel"],
            ["handleDeleteDevice", "canDeleteDevice"],
            ["handleDeleteChannel", "canDeleteChannel"],
            ["handleStreamTransportChange", "canEditTransport"],
            ["handlePtzTypeChange", "canEditChannel"],
            ["handleAudioEnabledChange", "canEditChannel"],
            ["handleOnDemandLiveChange", "canEditChannel"],
            ["handleCloudRecordingChange", "canUpdateRecording"],
            ["handleStopChannel", "canStopPlayback"]
        ] as const;

        for (const [entry, permission] of guardedEntries) {
            const start = source.indexOf(`function ${entry}`);
            const asyncStart = source.indexOf(`async function ${entry}`);
            const bodyStart = Math.max(start, asyncStart);
            expect(bodyStart, entry).toBeGreaterThan(-1);
            const body = source.slice(bodyStart, source.indexOf("\n}", bodyStart) + 2);
            expect(body, entry).toContain(`if (!${permission}.value) return;`);
        }
    });

    it("does not expose guest write controls or runtime monitoring", () => {
        for (const gate of [
            'v-if="canAddDevice"',
            'v-if="canRefreshCatalog"',
            'v-if="canManageSubscriptions"',
            'v-if="canEditDevice"',
            'v-if="canDeleteDevice"',
            'v-if="canEditChannel"',
            'v-if="canEditTransport"',
            'v-if="canUpdateRecording"',
            'v-if="canStopPlayback && isChannelPlaying',
            'v-if="canViewTraffic"'
        ]) {
            expect(source).toContain(gate);
        }
        expect(source).toContain('v-else class="device-status-ribbon device-status-readonly"');
        expect(source).toContain('v-else class="status-inline status-readonly"');
    });

    it("does not submit transport changes through the general channel edit without its permission", () => {
        const start = source.indexOf("async function handleEditChannel");
        const end = source.indexOf("\n}\n\nfunction cancelEditDevice", start);
        const body = source.slice(start, end);
        expect(body).toMatch(/canEditTransport\.value\s*\?\s*updateChannelStreamTransport/);
        expect(body).toContain("if (canEditTransport.value)");
    });

    it("loads only read dependencies for a guest device detail", () => {
        expect(source).toContain("canManageSubscriptions.value ? listDeviceSubscriptions(record.id) : Promise.resolve(null)");
        expect(source).toContain("if (canEditChannel.value) await loadPtzTypeDict()");
        expect(source).toContain("if (!canViewTraffic.value) return;");
    });
});
