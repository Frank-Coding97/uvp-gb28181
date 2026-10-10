import { describe, expect, it } from "vitest";
import { staticRoutes } from "./route";

describe("security preview route", () => {
  it("is loaded from the backend menu instead of a public static route", () => {
    const route = staticRoutes.find(item => item.path === "/security-preview");

    expect(route).toBeUndefined();
  });
});
