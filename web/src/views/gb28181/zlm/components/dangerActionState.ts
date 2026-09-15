export interface DangerActionSnapshotInput {
  contextVersion: number;
  nodeId: number;
  nodeName: string;
  targetKey: string;
  targetLabel: string;
  fingerprint?: string;
  impacts: string[];
  confirmPhrase: string;
  requireReason: boolean;
}

export interface DangerActionSnapshot extends Omit<DangerActionSnapshotInput, "impacts"> {
  impacts: readonly string[];
}

export interface DangerActionCurrentIdentity {
  contextVersion: number;
  nodeId: number | null;
  targetKey: string;
  fingerprint?: string;
}

export function createDangerActionSnapshot(input: DangerActionSnapshotInput): DangerActionSnapshot {
  return Object.freeze({
    ...input,
    impacts: Object.freeze([...input.impacts])
  });
}

export function dangerActionSnapshotMatches(snapshot: DangerActionSnapshot, current: DangerActionCurrentIdentity) {
  return snapshot.contextVersion === current.contextVersion
    && snapshot.nodeId === current.nodeId
    && snapshot.targetKey === current.targetKey
    && (snapshot.fingerprint ?? "") === (current.fingerprint ?? "");
}
