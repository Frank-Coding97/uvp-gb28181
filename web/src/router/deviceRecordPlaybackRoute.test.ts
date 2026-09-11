import { describe, expect, it } from "vitest";
import { staticRoutes } from "./route";

describe("device record playback route", () => {
    it("leaves playback registration to the database-backed layout routes", () => {
        const route = staticRoutes.find(item => item.name === "gb28181-device-record-playback");
        expect(route).toBeUndefined();
    });
});
