import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = () => readFileSync(resolve(process.cwd(), "src/views/gb28181/device-assignment/index.vue"), "utf-8");

describe("permission workbench page shell", () => {
    it("uses summary counts and has both assignment and sharing views", () => {
        const content = source();
        expect(content).toContain("getPermissionWorkbenchSummary");
        expect(content).toContain("设备归属");
        expect(content).toContain("共享授权");
        expect(content).toContain("summary");
        expect(content).not.toContain("class=\"workbench-head\"");
        expect(content).toContain("class=\"segmented workbench-views\"");
    });

    it("keeps a debounced search path and a stable operation column", () => {
        const content = source();
        expect(content).toContain("useDebounceFn");
        expect(content).toContain("operation-column");
        expect(content).not.toContain("scroll.x=1200");
    });

    it("normalizes Arco selection events before updating cross-page state", () => {
        const content = source();
        expect(content).toContain("normalizeSelectedIds");
        expect(content).toContain("@select=\"onTableSelect\"");
        expect(content).toContain("@select-all=\"onTableSelectAll\"");
    });

    it("explains the business meaning and four-step operation flow", () => {
        const content = source();
        expect(content).toContain("这个工作台是做什么的？");
        expect(content).toContain("调整归属");
        expect(content).toContain("共享授权");
        expect(content).toContain("四步操作流程");
        expect(content).toContain("guideExpanded");
    });

    it("uses semantic icons and distinct styles for summary cards", () => {
        const content = source();
        expect(content).toContain("summary-strip__item--all");
        expect(content).toContain("summary-strip__item--assigned");
        expect(content).toContain("summary-strip__item--unassigned");
        expect(content).toContain("summary-strip__item--departments");
        expect(content).toContain("<Monitor");
        expect(content).toContain("<CheckCircle2");
        expect(content).toContain("<CircleAlert");
    });

    it("keeps the operation column fixed with identifiable action icons", () => {
        const content = source();
        expect(content).toContain('title="操作" :width="190" align="center" fixed="right"');
        expect(content).toContain("device-operation-actions");
        expect(content).toContain("<UserRoundCog :size=\"13\"");
        expect(content).toContain("<Share2 :size=\"13\"");
    });

    it("adds semantic colors for branch and leaf department icons", () => {
        const content = source();
        expect(content).toContain("uvp-tree-node-icon--branch");
        expect(content).toContain("uvp-tree-node-icon--leaf");
        expect(content).toContain("#c47a18");
        expect(content).toContain("#0f8b83");
    });
});
