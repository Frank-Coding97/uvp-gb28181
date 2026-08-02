import type { ChannelVO } from "./api";

export interface RecordQueryEntryState {
    visible: boolean;
    target: ChannelVO | null;
    token: number;
}

export function createRecordQueryEntryState(): RecordQueryEntryState {
    return { visible: false, target: null, token: 0 };
}

export function openRecordQueryEntry(_state: RecordQueryEntryState, _channel: ChannelVO): RecordQueryEntryState {
    return { visible: true, target: _channel, token: _state.token + 1 };
}

export function closeRecordQueryEntry(_state: RecordQueryEntryState): RecordQueryEntryState {
    return { visible: false, target: null, token: _state.token + 1 };
}
