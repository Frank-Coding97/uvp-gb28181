import { beforeEach, describe, expect, it, vi } from "vitest";

const request = vi.hoisted(() => vi.fn());
vi.mock("@/utils/http", () => ({ http: { request } }));

import { clearLoginLogsAPI, deleteLoginLogsAPI, getLoginLogDetailAPI, getLoginLogsAPI, unlockLoginLogAccountAPI } from "./login-log";
import { baseUrlApi } from "./utils";

describe("login log API", () => {
  beforeEach(() => request.mockReset());

  it("uses the RBAC-protected read and mutation routes", () => {
    getLoginLogsAPI({ pageNum: 2, pageSize: 20, username: "alice", result: "failure" });
    getLoginLogDetailAPI(17);

    expect(request).toHaveBeenNthCalledWith(1, "get", baseUrlApi("sysLoginLog/list"), {
      params: { pageNum: 2, pageSize: 20, username: "alice", result: "failure" }
    });
    expect(request).toHaveBeenNthCalledWith(2, "get", baseUrlApi("sysLoginLog/17"));

    deleteLoginLogsAPI([4, 5]);
    clearLoginLogsAPI();
    unlockLoginLogAccountAPI(17);
    expect(request).toHaveBeenNthCalledWith(3, "delete", baseUrlApi("sysLoginLog/delete"), { data: { ids: [4, 5] } });
    expect(request).toHaveBeenNthCalledWith(4, "post", baseUrlApi("sysLoginLog/clear"));
    expect(request).toHaveBeenNthCalledWith(5, "post", baseUrlApi("sysLoginLog/unlock"), { data: { id: 17 } });
  });
});
