import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const pages = [
  "StreamManagement.vue",
  "SessionManagement.vue",
  "ProxyManagement.vue",
  "FFmpegSources.vue",
  "RTPServices.vue"
];

function selectedNodeWatcher(source: string) {
  const start = source.indexOf("watch(selectedNodeId");
  expect(start).toBeGreaterThanOrEqual(0);
  const end = source.indexOf("\n});", start);
  expect(end).toBeGreaterThan(start);
  return source.slice(start, end);
}

describe("ZLM runtime page state retention", () => {
  it.each(pages)("keeps filters and pagination while switching nodes in %s", file => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm", file), "utf8");
    const watcher = selectedNodeWatcher(source);

    expect(watcher).not.toMatch(/\bpage\.value\s*=\s*1/);
    expect(watcher).not.toMatch(/\bviewerPage\.value\s*=\s*1/);
    expect(watcher).not.toMatch(/\bnetworkFilter\.page\s*=\s*1/);
    expect(watcher).not.toMatch(/\bpages\.(?:pull|push)\.page\s*=\s*1/);
  });
});
