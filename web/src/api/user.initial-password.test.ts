import { beforeEach, describe, expect, it, vi } from "vitest";

const request = vi.hoisted(() => vi.fn());

vi.mock("@/utils/http", () => ({ http: { request } }));
vi.mock("./utils", () => ({ baseUrlApi: (path: string) => `/api/${path}` }));

import { changeInitialPasswordAPI } from "./user";

describe("initial password API", () => {
  beforeEach(() => {
    request.mockReset().mockResolvedValue({ code: 0, message: "", data: null });
  });

  it("sends the new password pair to the dedicated endpoint", async () => {
    const payload = { password: "new-password!", confirmPassword: "new-password!" };

    await changeInitialPasswordAPI(payload);

    expect(request).toHaveBeenCalledWith("put", "/api/users/changeInitialPassword", { data: payload });
  });
});
