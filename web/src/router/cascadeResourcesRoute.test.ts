import { describe, expect, it } from "vitest";
import { staticRoutes } from "./route";

describe("cascade resource workspace route", () => {
  it("registers the hidden workspace under the authenticated layout", () => {
    const layout = staticRoutes.find(route => route.name === "layout");
    const detail = layout?.children?.find(route => route.name === "gb28181-cascade-resources");
    expect(detail?.path).toBe("/gb28181/cascade/:id/resources");
    expect(detail?.meta?.hide).toBe(true);
  });

  it("exposes the service upgrade route with its read permission metadata", () => {
    const layout = staticRoutes.find(route => route.name === "layout");
    const upgrade = layout?.children?.find(route => route.name === "system-service-upgrade");
    expect(upgrade?.meta?.hide).toBe(true);
    expect(upgrade?.meta?.permission).toBe("system:upgrade:read");
  });
});
