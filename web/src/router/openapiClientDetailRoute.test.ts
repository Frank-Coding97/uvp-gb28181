import { describe, expect, it } from "vitest";
import { staticRoutes } from "./route";

describe("OpenAPI client detail route", () => {
  it("registers a hidden detail workspace under the authenticated layout", () => {
    const layout = staticRoutes.find(route => route.name === "layout");
    const detail = layout?.children?.find(route => route.name === "gb28181-openapi-client-detail");

    expect(detail?.path).toBe("/gb28181/openapi-client/:id");
    expect(detail?.meta?.hide).toBe(true);
  });
});
