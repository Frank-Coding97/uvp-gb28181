import { beforeEach, describe, expect, it, vi } from "vitest";

const request = vi.hoisted(() => vi.fn());
vi.mock("@/utils/http", () => ({ http: { request } }));
vi.mock("@/api/utils", () => ({ baseUrlApi: (path: string) => `/api/${path}` }));

import { batchDeleteAlarms, clearAllAlarms, deleteAlarm, getAlarmDetail, listAlarms } from "./api";

describe("alarm management API", () => {
  beforeEach(() => {
    request.mockReset();
    request.mockResolvedValue({ code: 0, message: "", data: {} });
  });

  it("sends only the supplied list query", async () => {
    await listAlarms({ page: 2, pageSize: 20, deviceId: 12, sourceCode: "3701", priority: 2 });
    expect(request).toHaveBeenCalledWith("get", "/api/gb28181/alarms", {
      params: { page: 2, pageSize: 20, deviceId: 12, sourceCode: "3701", priority: 2 }
    });
  });

  it("preserves IDs beyond the JavaScript safe integer", async () => {
    const id = "9007199254740993";
    await getAlarmDetail(id);
    expect(request).toHaveBeenLastCalledWith("get", `/api/gb28181/alarms/${id}`);

    await deleteAlarm(id);
    expect(request).toHaveBeenLastCalledWith("delete", `/api/gb28181/alarms/${id}`);
  });

  it("posts batch delete with the id list in the body", async () => {
    await batchDeleteAlarms(["9007199254740993", "42"]);
    expect(request).toHaveBeenLastCalledWith("post", "/api/gb28181/alarms/batch-delete", {
      data: { ids: ["9007199254740993", "42"] }
    });
  });

  it("posts clear-all without a body", async () => {
    await clearAllAlarms();
    expect(request).toHaveBeenLastCalledWith("post", "/api/gb28181/alarms/clear-all");
  });

});
