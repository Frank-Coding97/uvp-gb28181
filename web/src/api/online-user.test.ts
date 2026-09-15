import { beforeEach, describe, expect, it, vi } from "vitest";

const request = vi.hoisted(() => vi.fn());
vi.mock("@/utils/http", () => ({ http: { request } }));

import { forceLogoutOnlineSessionAPI, getOnlineUsersAPI, sessionHeartbeatAPI } from "./online-user";
import { baseUrlApi } from "./utils";

describe("online user API", () => {
  beforeEach(() => request.mockReset());

  it("uses the RBAC-protected list and force-logout routes", () => {
    getOnlineUsersAPI({ pageNum: 2, pageSize: 30, status: "active" });
    forceLogoutOnlineSessionAPI("session-2");

    expect(request).toHaveBeenNthCalledWith(1, "get", baseUrlApi("sysOnlineUser/list"), {
      params: { pageNum: 2, pageSize: 30, status: "active" }
    });
    expect(request).toHaveBeenNthCalledWith(2, "post", baseUrlApi("sysOnlineUser/forceLogout"), {
      data: { sid: "session-2" }
    });
  });

  it("keeps the background heartbeat silent", () => {
    sessionHeartbeatAPI();

    expect(request).toHaveBeenCalledWith(
      "post",
      baseUrlApi("users/session/heartbeat"),
      undefined,
      { showErrorMessage: false }
    );
  });
});
