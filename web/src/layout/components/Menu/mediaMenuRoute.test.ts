import { describe, expect, it } from "vitest";

import { mediaMenuLocation } from "./mediaMenuRoute";

describe("mediaMenuLocation", () => {
  it("does not carry a concrete node into the all-node scheduling page", () => {
    expect(mediaMenuLocation("/media/scheduling", "2")).toBe("/media/scheduling");
  });

  it("does not carry a concrete node into the all-node monitoring page", () => {
    expect(mediaMenuLocation("/media/monitoring", "2")).toBe("/media/monitoring");
  });

  it("keeps the current node context for node-scoped media pages", () => {
    expect(mediaMenuLocation("/media/overview", "2")).toEqual({ path: "/media/overview", query: { nodeId: "2" } });
  });

  it("leaves routes without a valid node context unchanged", () => {
    expect(mediaMenuLocation("/media/overview")).toBe("/media/overview");
    expect(mediaMenuLocation("/system/users", "2")).toBe("/system/users");
  });
});
