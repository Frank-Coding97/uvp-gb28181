import { describe, expect, it } from "vitest";
import { staticRoutes } from "./route";

describe("multi-screen playback route", () => {
    it("leaves registration to the database-backed layout routes", () => {
        const route = staticRoutes.find(item => item.name === "gb28181-multi-screen-playback");
        expect(route).toBeUndefined();
    });
});
