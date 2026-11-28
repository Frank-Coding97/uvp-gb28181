import versionInfo from "../../version.json";

export const APP_VERSION = versionInfo.version;
export const APP_VERSION_TEXT = `v${APP_VERSION}`;
export const APP_BUILD_ID = import.meta.env.VITE_BUILD_ID || "";

export interface RuntimeBuildIdentity {
  version?: string;
  build_id?: string;
}

export interface BuildIdentityCheck {
  matches: boolean;
  versionMatches: boolean;
  buildIDMatches: boolean;
  clientBuildIDMissing: boolean;
}

/**
 * Compare the identity embedded in the browser bundle with the identity
 * reported by the running backend. A release build always carries a build
 * ID; an unconfigured development bundle is treated as unknown rather than
 * claiming that it matches a signed release.
 */
export function checkRuntimeBuildIdentity(runtime?: RuntimeBuildIdentity): BuildIdentityCheck {
  const runtimeVersion = runtime?.version?.trim() && runtime.version.trim() !== "unknown" ? runtime.version.trim() : "";
  const runtimeBuildID = runtime?.build_id?.trim() && runtime.build_id.trim() !== "unknown" ? runtime.build_id.trim() : "";
  const versionMatches = !runtimeVersion || runtimeVersion === APP_VERSION;
  const clientBuildIDMissing = Boolean(runtimeBuildID && !APP_BUILD_ID);
  const buildIDMatches = !runtimeBuildID || (!!APP_BUILD_ID && runtimeBuildID === APP_BUILD_ID);

  return {
    matches: versionMatches && buildIDMatches,
    versionMatches,
    buildIDMatches,
    clientBuildIDMissing
  };
}

export function markClientBuildIdentity() {
  document.documentElement.dataset.uvpBuildId = APP_BUILD_ID;
}
