import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/NodeList.vue"), "utf8");

describe("legacy node list route", () => {
  it("is a thin shell over the canonical governance panel", () => {
    expect(source).toContain("NodeListPanel");
    expect(source).toContain(":canonical=\"false\"");
    expect(source).not.toContain("listZLMNodes");
  });
});
