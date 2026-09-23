import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/system/log/log.vue"), "utf-8");

describe("operation log workbench filters", () => {
    it("supports path filtering and query initialization", () => {
        expect(source).toContain("form.path");
        expect(source).toContain("query.path");
        expect(source).toContain('path: form.value.path');
    });
});
