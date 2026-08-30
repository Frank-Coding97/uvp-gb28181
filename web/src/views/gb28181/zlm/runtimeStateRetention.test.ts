import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const panelCases = [
  ["workbench/monitoring/StreamPanel.vue", "watch([() => props.scope, () => props.nodeId]"],
  ["workbench/monitoring/NetworkSessionPanel.vue", "watch([() => props.scope, () => props.nodeId]"],
  ["workbench/ingress/ProxyPanel.vue", "watch([nodeId, () => props.active]"],
  ["workbench/ingress/FFmpegPanel.vue", "watch([nodeId, () => props.active]"],
  ["workbench/ingress/RTPPanel.vue", "watch([nodeId, () => props.active]"]
] as const;

const directMonitoringShells = [
  "RuntimeOverview.vue",
  "StreamManagement.vue",
  "SessionManagement.vue"
];

function readZLMSource(file: string) {
  return readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm", file), "utf8");
}

function watcher(source: string, token: string) {
  const start = source.indexOf(token);
  expect(start).toBeGreaterThanOrEqual(0);
  const end = source.indexOf("\n});", start);
  expect(end).toBeGreaterThan(start);
  return source.slice(start, end);
}

describe("ZLM runtime page state retention", () => {
  it.each(panelCases)("keeps filters and pagination while switching nodes in %s", (file, token) => {
    const nodeWatcher = watcher(readZLMSource(file), token);

    expect(nodeWatcher).not.toMatch(/\bpage\.value\s*=\s*1/);
    expect(nodeWatcher).not.toMatch(/\bviewerPage\.value\s*=\s*1/);
    expect(nodeWatcher).not.toMatch(/\bnetworkFilter\.page\s*=\s*1/);
    expect(nodeWatcher).not.toMatch(/\bpages\.(?:pull|push)\.page\s*=\s*1/);
  });

  it.each(directMonitoringShells)("does not rewrite the route while initializing %s", file => {
    const source = readZLMSource(file);

    expect(source).toContain("ZLMNodeContextBar");
    expect(source).toContain(":query-node-id=\"route.query.nodeId\"");
    expect(source).not.toContain("router.replace");
  });

  it("only syncs the legacy ingress query after a real node change", () => {
    const source = readZLMSource("workbench/ingress/LegacyIngressShell.vue");
    const nodeWatcher = watcher(source, "watch(context.selectedNodeId");

    expect(nodeWatcher).toContain("previousNodeId !== null");
    expect(nodeWatcher).toContain("router.replace");
  });
});
