import { describe, expect, it } from "vitest";
import { staticRoutes } from "./route";

describe("device record playback route", () => {
    it("registers a hidden authenticated full-screen route", () => {
        const route = staticRoutes.find(item => item.name === "gb28181-device-record-playback");
        expect(route).toMatchObject({
            path: "/gb28181/device-record-playback/:channelId",
            meta: { hide: true, keepAlive: false }
        });
    });
});
