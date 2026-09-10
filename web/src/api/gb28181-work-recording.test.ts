import { describe, expect, it, vi } from "vitest";

const tokenState = vi.hoisted(() => ({ accessToken: "token value" as string | null }));

vi.mock("@/utils/auth", () => ({
  getAccessToken: () =>
    tokenState.accessToken ? { accessToken: tokenState.accessToken, accessTokenExpires: 0 } : null
}));

import { workOrderDownloadUrl, workOrderFileUrl } from "./gb28181-work-recording";

describe("work order browser-direct links", () => {
    it("carries the access token so window.open and <a href> can authenticate", () => {
        const download = workOrderDownloadUrl("b-1");
        expect(download).toContain("/work-orders/b-1/download");
        expect(download).toContain("token=token%20value");

        const file = workOrderFileUrl("b-1", 7);
        expect(file).toContain("/work-orders/b-1/files/7");
        expect(file).toContain("token=token%20value");
    });

    it("omits the query when no access token is stored", () => {
        tokenState.accessToken = null;
        expect(workOrderDownloadUrl("b-1")).not.toContain("token=");
        expect(workOrderFileUrl("b-1", 7)).not.toContain("token=");
    });

    it("escapes the identifier so a crafted id cannot break out of the path", () => {
        tokenState.accessToken = "abc";
        expect(workOrderDownloadUrl("a/../b")).toContain("/work-orders/a%2F..%2Fb/download");
    });
});
