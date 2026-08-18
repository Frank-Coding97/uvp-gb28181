import { beforeEach, describe, expect, it, vi } from "vitest";

const request = vi.hoisted(() => vi.fn());
vi.mock("@/utils/http", () => ({ http: { request } }));

import { getLoginLogDetailAPI, getLoginLogsAPI } from "./login-log";
import { baseUrlApi } from "./utils";

describe("login log API", () => {
  beforeEach(() => request.mockReset());

  it("uses only the RBAC-protected read routes", () => {
    getLoginLogsAPI({ pageNum: 2, pageSize: 20, username: "alice", result: "failure" });
    getLoginLogDetailAPI(17);

    expect(request).toHaveBeenNthCalledWith(1, "get", baseUrlApi("sysLoginLog/list"), {
      params: { pageNum: 2, pageSize: 20, username: "alice", result: "failure" }
    });
    expect(request).toHaveBeenNthCalledWith(2, "get", baseUrlApi("sysLoginLog/17"));
  });
});
