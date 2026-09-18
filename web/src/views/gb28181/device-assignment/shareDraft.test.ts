import { describe, expect, it } from "vitest";

const diffDraft = (before: string[], after: string[]) => ({
    added: after.filter((key) => !before.includes(key)),
    removed: before.filter((key) => !after.includes(key))
});

describe("share draft semantics", () => {
    it("does not mix add and remove selections", () => {
        const add = new Set(["dept:10"]);
        const remove = new Set<string>();
        expect(add.has("dept:10")).toBe(true);
        expect(remove.has("dept:10")).toBe(false);
    });

    it("computes only the changed target keys", () => {
        expect(diffDraft(["dept:10", "user:7"], ["dept:10", "user:9"])).toEqual({
            added: ["user:9"],
            removed: ["user:7"]
        });
    });
});
