import { describe, expect, it } from "vitest";
import { displayFields, renderLogLine } from "./log-line";

// 取自实测的注册记录，覆盖排序表字段、未排名字段、进程常量与需转义的值。
const registerFields = {
  call_id: "a83740c74ab2fb18@192.168.10.112",
  cseq: "1",
  device_id: "37010301021180000007",
  event: "gb28181.register.succeeded",
  outcome: "succeeded",
  stage: "terminal"
};

describe("displayFields", () => {
  it("orders by the shared rank table, then alphabetically", () => {
    // 与后端 consoleFieldOrder 同序：device_id → call_id → event → stage → outcome，
    // transport 也是排名字段但要排在 outcome 之后；cseq 未排名，落在最后并按字典序。
    const keys = displayFields({ ...registerFields, is_first: true, transport: "UDP" }).map(field => field.key);
    expect(keys).toEqual(["device_id", "call_id", "event", "stage", "outcome", "transport", "cseq", "is_first"]);
  });

  it("flags machine fields for dimming without reordering them", () => {
    const fields = displayFields(registerFields);
    expect(fields.map(field => `${field.key}:${field.muted}`)).toEqual([
      "device_id:false",
      "call_id:false",
      "event:true",
      "stage:true",
      "outcome:true",
      "cseq:true"
    ]);
  });

  it("hides the per-process constants", () => {
    const fields = displayFields({ ...registerFields, service: "uvp-gb28181", version: "v1", instance: "node" });
    expect(fields.map(field => field.key)).not.toContain("service");
    expect(fields.map(field => field.key)).not.toContain("version");
    expect(fields.map(field => field.key)).not.toContain("instance");
  });

  it("quotes values whose raw form is ambiguous", () => {
    const fields = displayFields({ header: "line1\nline2", note: "", plain: "ok", reason_code: "no response" });
    const byKey = Object.fromEntries(fields.map(field => [field.key, field.display]));
    expect(byKey.header).toBe('"line1\\nline2"');
    expect(byKey.note).toBe('""');
    expect(byKey.plain).toBe("ok");
    expect(byKey.reason_code).toBe('"no response"');
  });

  it("renders non-string scalars without quoting", () => {
    const fields = displayFields({ sn: 3, is_first: true, complete: false });
    const byKey = Object.fromEntries(fields.map(field => [field.key, field.display]));
    expect(byKey).toEqual({ sn: "3", is_first: "true", complete: "false" });
  });

  it("returns an empty list for missing fields", () => {
    expect(displayFields(undefined)).toEqual([]);
  });
});

describe("renderLogLine", () => {
  it("renders one plain-text line without a JSON blob", () => {
    const line = renderLogLine({
      time: "2026-09-14 17:16:25.061",
      level: "info",
      module: "gb28181.register",
      message: "GB28181 设备注册成功",
      fields: { ...registerFields, is_first: true, transport: "UDP" }
    });
    // 与后端 console 编码器的输出逐字保持同版式。
    expect(line).toBe(
      "2026-09-14 17:16:25.061 INFO  gb28181.register     GB28181 设备注册成功  " +
        "device_id=37010301021180000007 call_id=a83740c74ab2fb18@192.168.10.112 " +
        "event=gb28181.register.succeeded stage=terminal outcome=succeeded transport=UDP cseq=1 is_first=true"
    );
    expect(line).not.toContain('{"');
    expect(line).not.toContain("\n");
  });

  it("appends the stack on its own line", () => {
    const line = renderLogLine({ time: "t", level: "warn", module: "ptz", event: "ptz.response.unmatched", stack: "frame" });
    expect(line.split("\n")).toEqual(["t WARN  ptz                  ptz.response.unmatched", "frame"]);
  });

  it("falls back to the event name when the message is empty", () => {
    expect(renderLogLine({ time: "t", level: "info", module: "app", event: "app.started" })).toContain("app.started");
  });
});
