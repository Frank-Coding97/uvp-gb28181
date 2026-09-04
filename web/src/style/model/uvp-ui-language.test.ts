import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/style/model/uvp-ui-language.scss"), "utf8");

describe("table action spacing", () => {
  it("keeps one compact 4px gap between operation icons and labels", () => {
    expect(source).toMatch(/\.uvp-data-table \.uvp-table-action,\s*\.uvp-data-table \.arco-link\s*\{[^}]*gap:\s*4px;/s);
    expect(source).toMatch(/\.uvp-data-table \.arco-link-icon\s*\{[^}]*margin-right:\s*0;/s);
    expect(source).toMatch(/\.uvp-data-table \.arco-btn:not\(\.arco-btn-only-icon\) \.arco-btn-icon\s*\{[^}]*margin-right:\s*4px;/s);
  });
});

describe("media workbench theme tokens", () => {
  it("maps ZLM surfaces to the active UVP body theme for legacy and canonical routes", () => {
    expect(source).toMatch(/body,\s*\.gb28181-page,[^{]*\{[^}]*--zlm-card:\s*var\(--uvp-panel-bg\)/s);
    expect(source).toMatch(/body\[arco-theme="dark"\],\s*body\[arco-theme="dark"\]\s+\.gb28181-page,[^{]*\{/s);
  });
});

describe("table loading mask theme", () => {
  it("uses a dark translucent surface instead of the light loading mask in dark mode", () => {
    expect(source).toMatch(/body\[arco-theme="dark"\]\s+\.uvp-data-table\s+\.arco-spin-mask\s*\{[^}]*background:\s*rgb\(16 25 35\s*\/\s*78%\)/s);
  });
});
