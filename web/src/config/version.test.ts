import { describe, expect, it } from "vitest";
import versionInfo from "../../version.json";
import { APP_VERSION, APP_VERSION_TEXT } from "./version";

describe("product version", () => {
  it("uses the synchronized frontend version as its only runtime value", () => {
    expect(APP_VERSION).toBe(versionInfo.version);
    expect(APP_VERSION).toMatch(/^\d+\.\d+\.\d+(?:-rc\.\d+)?$/);
    expect(APP_VERSION_TEXT).toBe(`v${versionInfo.version}`);
  });
});

