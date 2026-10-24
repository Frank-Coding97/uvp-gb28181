import { describe, expect, it } from "vitest";
import versionInfo from "../../version.json";
import { APP_BUILD_ID, APP_VERSION, APP_VERSION_TEXT, checkRuntimeBuildIdentity, markClientBuildIdentity } from "./version";

describe("product version", () => {
  it("uses the synchronized frontend version as its only runtime value", () => {
    expect(APP_VERSION).toBe(versionInfo.version);
    expect(APP_VERSION).toMatch(/^\d+\.\d+\.\d+(?:-rc\.\d+)?$/);
    expect(APP_VERSION_TEXT).toBe(`v${versionInfo.version}`);
  });

  it("marks the compiled client identity without rendering it", () => {
    delete document.documentElement.dataset.uvpBuildId;

    markClientBuildIdentity();

    expect(document.documentElement.dataset.uvpBuildId).toBe(APP_BUILD_ID);
  });

  it("does not treat a release backend as matching an unbuilt or different frontend", () => {
    expect(checkRuntimeBuildIdentity({ version: APP_VERSION, build_id: "release-build" }).matches).toBe(
      APP_BUILD_ID === "release-build"
    );
    expect(checkRuntimeBuildIdentity({ version: "0.0.0", build_id: APP_BUILD_ID }).matches).toBe(false);
  });

  it("allows the development backend response without a build id", () => {
    expect(checkRuntimeBuildIdentity({ version: APP_VERSION }).matches).toBe(true);
  });
});
