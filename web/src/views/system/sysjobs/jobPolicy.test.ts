import { describe, expect, it } from "vitest";
import {
  JOB_BLOCKING_POLICY_LABEL_FALLBACK,
  JOB_EXECUTE_POLICY_LABEL_FALLBACK,
  jobPolicyLabelFrom,
  jobPolicyLabelsFrom,
  jobPolicyOptionsFromLabels
} from "./jobPolicy";

describe("任务调度策略（执行 / 阻塞）", () => {
  it("兜底值域逐条锁定 —— 档位是后端契约（sysjobs.go），改这里必须同步后端枚举", () => {
    expect(JOB_EXECUTE_POLICY_LABEL_FALLBACK).toEqual({ 0: "单次执行", 1: "重复执行" });
    expect(JOB_BLOCKING_POLICY_LABEL_FALLBACK).toEqual({ 0: "丢弃", 1: "并行" });
  });

  it("字典为空 → 全用兜底（界面不塌）", () => {
    const labels = jobPolicyLabelsFrom([], JOB_EXECUTE_POLICY_LABEL_FALLBACK);
    expect(jobPolicyOptionsFromLabels(labels, JOB_EXECUTE_POLICY_LABEL_FALLBACK)).toEqual([
      { value: 0, label: "单次执行" },
      { value: 1, label: "重复执行" }
    ]);
  });

  it("字典可以改展示名", () => {
    const labels = jobPolicyLabelsFrom(
      [
        { value: "0", name: "跑一次", status: 1 },
        { value: "1", name: "循环跑", status: 1 }
      ],
      JOB_EXECUTE_POLICY_LABEL_FALLBACK
    );
    expect(jobPolicyOptionsFromLabels(labels, JOB_EXECUTE_POLICY_LABEL_FALLBACK)).toEqual([
      { value: 0, label: "跑一次" },
      { value: 1, label: "循环跑" }
    ]);
  });

  it("⛔ 白名单：字典里多出来的档位必须被丢弃（不许改变提交给后端的值域）", () => {
    const labels = jobPolicyLabelsFrom(
      [
        { value: "2", name: "字典瞎加的第三档", status: 1 },
        { value: "0", name: "单次执行", status: 1 }
      ],
      JOB_EXECUTE_POLICY_LABEL_FALLBACK
    );
    expect(labels["2"]).toBeUndefined();
    expect(jobPolicyOptionsFromLabels(labels, JOB_EXECUTE_POLICY_LABEL_FALLBACK).map(o => o.value)).toEqual([0, 1]);
  });

  it("⛔ 白名单：字典缺了一档也必须补回兜底（不许把档位改没）", () => {
    // 只给了 0 这一档 ⇒ 1 必须补回兜底，且 0 的改名照常生效
    const labels = jobPolicyLabelsFrom([{ value: "0", name: "跑一次", status: 1 }], JOB_BLOCKING_POLICY_LABEL_FALLBACK);
    expect(jobPolicyOptionsFromLabels(labels, JOB_BLOCKING_POLICY_LABEL_FALLBACK)).toEqual([
      { value: 0, label: "跑一次" },
      { value: 1, label: "并行" }
    ]);
  });

  it("停用项（status=0）不生效，仍用兜底", () => {
    const labels = jobPolicyLabelsFrom([{ value: "0", name: "被停用的名字", status: 0 }], JOB_EXECUTE_POLICY_LABEL_FALLBACK);
    expect(labels["0"]).toBe("单次执行");
  });

  it('未命中 / 空值一律回 "-"（与改动前一致）', () => {
    const labels = jobPolicyLabelsFrom([], JOB_EXECUTE_POLICY_LABEL_FALLBACK);
    expect(jobPolicyLabelFrom(labels, 9)).toBe("-");
    expect(jobPolicyLabelFrom(labels, undefined)).toBe("-");
    expect(jobPolicyLabelFrom(labels, null)).toBe("-");
    expect(jobPolicyLabelFrom(null, 0)).toBe("-");
  });

  it('命中时回字典名（数字 0 / 字符串 "0" 都要认）', () => {
    const labels = jobPolicyLabelsFrom([{ value: "0", name: "只跑一次", status: 1 }], JOB_EXECUTE_POLICY_LABEL_FALLBACK);
    expect(jobPolicyLabelFrom(labels, 0)).toBe("只跑一次");
    expect(jobPolicyLabelFrom(labels, "0")).toBe("只跑一次");
  });
});
