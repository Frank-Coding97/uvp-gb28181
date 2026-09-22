import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

import { buildSchedulerLogFilter, schedulerLogResultFromQuery } from "./schedulerLogState";

describe("scheduler log server-side filters", () => {
  it("sends time, result, node, strategy and stream filters to the backend", () => {
    expect(
      buildSchedulerLogFilter({
        timeRange: ["2026-08-30T00:00:00.000Z", "2026-08-30T01:00:00.000Z"],
        nodeId: 7,
        algorithm: "weighted",
        result: "failure",
        streamId: " camera-01 ",
        limit: 200
      })
    ).toEqual({
      from: "2026-08-30T00:00:00.000Z",
      to: "2026-08-30T01:00:00.000Z",
      nodeId: 7,
      algorithm: "weighted",
      result: "failure",
      streamId: "camera-01",
      limit: 200
    });
  });

  it("omits empty filters and rejects a reversed time range", () => {
    expect(buildSchedulerLogFilter({ timeRange: [], streamId: "", limit: 100 })).toEqual({ limit: 100 });
    expect(() =>
      buildSchedulerLogFilter({
        timeRange: ["2026-08-30T02:00:00Z", "2026-08-30T01:00:00Z"],
        limit: 100
      })
    ).toThrow("开始时间不能晚于结束时间");
  });

  it("accepts only supported scheduler result query values", () => {
    expect(schedulerLogResultFromQuery("success")).toBe("success");
    expect(schedulerLogResultFromQuery(["failure", "success"])).toBe("failure");
    expect(schedulerLogResultFromQuery("failed")).toBeUndefined();
  });

  it("binds the table request to the built filter rather than filtering a full client list", () => {
    const source = readFileSync(
      resolve(process.cwd(), "src/views/gb28181/zlm/workbench/scheduling/SchedulerLogPanel.vue"),
      "utf8"
    );
    const legacyShell = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/SchedulerLog.vue"), "utf8");
    expect(source).toContain("buildSchedulerLogFilter");
    expect(source).toMatch(/listSchedulerLogs\(filter/);
    expect(source).toContain("调度状态");
    expect(source).toContain("节点");
    expect(source).toContain("策略");
    expect(source).toContain("业务流");
    expect(legacyShell).toContain("SchedulerLogPanel");
    expect(legacyShell).toMatch(/\.scheduler-log-page\s*\{[^}]*height:\s*100%;[^}]*min-height:\s*0;[^}]*overflow:\s*hidden;/s);
    expect(legacyShell).toMatch(
      /\.scheduler-log-shell\s*\{[^}]*display:\s*flex;[^}]*height:\s*100%;[^}]*min-height:\s*0;[^}]*overflow:\s*hidden;/s
    );
  });
});
