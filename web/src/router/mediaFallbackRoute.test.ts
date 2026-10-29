import { createMemoryHistory, createRouter } from "vue-router";
import { describe, expect, it } from "vitest";

import { staticRoutes } from "./route";

describe("media access fallback route", () => {
  it("renders MediaEntry inside layout when no authorized media route exists", () => {
    const router = createRouter({ history: createMemoryHistory(), routes: staticRoutes as never });
    const resolved = router.resolve("/media");

    expect(resolved.name).toBe("media-access-fallback");
    expect(resolved.matched.map((record: { name?: unknown }) => record.name)).toEqual(["layout", "media-access-fallback"]);
  });

  it("lets an authorized exact workspace route outrank the scoped fallback", () => {
    const router = createRouter({ history: createMemoryHistory(), routes: staticRoutes as never });
    router.addRoute("layout", {
      path: "/media/monitoring",
      name: "authorized-media-monitoring",
      component: { template: "<div />" }
    });

    expect(router.resolve("/media/monitoring").name).toBe("authorized-media-monitoring");
    expect(router.resolve("/media/not-authorized").name).toBe("media-access-fallback");
  });
});
