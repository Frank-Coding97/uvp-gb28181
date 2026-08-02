import { describe, expect, it } from "vitest";
import type { ChannelVO } from "./api";
import { closeRecordQueryEntry, createRecordQueryEntryState, openRecordQueryEntry } from "./recordQueryEntryState";

const channel = (id: number) => ({ id, channelId: `channel-${id}`, name: `通道 ${id}` } as ChannelVO);

describe("record query entry state", () => {
    it("opens the exact channel from either list or card entry", () => {
        const opened = openRecordQueryEntry(createRecordQueryEntryState(), channel(31));
        expect(opened).toMatchObject({ visible: true, target: { id: 31 }, token: 1 });
    });

    it("switches targets with a new token so stale work cannot own the current entry", () => {
        const first = openRecordQueryEntry(createRecordQueryEntryState(), channel(31));
        const second = openRecordQueryEntry(first, channel(32));
        expect(second).toMatchObject({ visible: true, target: { id: 32 }, token: 2 });
        expect(first.target?.id).toBe(31);
    });

    it("clears target and increments token on close", () => {
        const opened = openRecordQueryEntry(createRecordQueryEntryState(), channel(31));
        expect(closeRecordQueryEntry(opened)).toEqual({ visible: false, target: null, token: 2 });
    });
});
