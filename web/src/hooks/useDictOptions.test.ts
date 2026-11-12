import { beforeEach, describe, expect, it, vi } from "vitest";
import { ref } from "vue";
import { createPinia, setActivePinia } from "pinia";
import {
  dictLabelsFromItems,
  dictOptionsFromItems,
  isDictItemEnabled,
  useDictItems,
  useDictLabel,
  useDictLabelMap,
  useDictOptions
} from "./useDictOptions";
import { useSystemStore } from "@/store/modules/system";

describe("dict item normalization", () => {
  it("treats a missing status as enabled and only explicit 0/false as disabled", () => {
    expect(isDictItemEnabled({})).toBe(true);
    expect(isDictItemEnabled({ status: 1 })).toBe(true);
    expect(isDictItemEnabled({ status: "1" })).toBe(true);
    expect(isDictItemEnabled({ status: 0 })).toBe(false);
    expect(isDictItemEnabled({ status: false })).toBe(false);
  });

  it("drops disabled / blank / duplicate entries and keeps dictionary order", () => {
    expect(
      dictOptionsFromItems([
        { name: "禁用", value: "0", status: 1 },
        { name: "启用", value: "1", status: 1 },
        { name: "已停用项", value: "2", status: 0 },
        { name: "  ", value: "3", status: 1 },
        { name: "空值", value: "", status: 1 },
        { name: "重复启用", value: "1", status: 1 }
      ])
    ).toEqual([
      { label: "禁用", value: "0" },
      { label: "启用", value: "1" }
    ]);
  });

  it("falls back to a copied constant list when the dictionary is empty", () => {
    const fallback = [{ label: "启用", value: "1" }];
    const options = dictOptionsFromItems([], fallback);
    expect(options).toEqual(fallback);
    // 必须拷贝，调用方改到结果不能污染常量
    expect(options[0]).not.toBe(fallback[0]);

    expect(dictOptionsFromItems(null)).toEqual([]);
    expect(dictOptionsFromItems(undefined, fallback)).toEqual(fallback);
  });

  it("lays the fallback first and lets dictionary entries win per value", () => {
    const labels = dictLabelsFromItems(
      [
        { name: "正常", value: "1", status: 1 },
        { name: "停用", value: "0", status: 0 }
      ],
      { "0": "禁用", "1": "启用" }
    );
    // 字典命中 → 覆盖兜底
    expect(labels["1"]).toBe("正常");
    // 字典项被停用 → 不参与，兜底生效
    expect(labels["0"]).toBe("禁用");
  });

  it("does not invent an entry for values outside the dictionary range", () => {
    expect(dictLabelsFromItems([{ name: "在线", value: "1", status: 1 }], {})).toEqual({ "1": "在线" });
  });
});

describe("dictionary consumption composables", () => {
  beforeEach(() => {
    // system store 依赖自动导入的 `ref`，单测环境里要显式补上（同 route-config.test.ts）
    vi.stubGlobal("ref", ref);
    setActivePinia(createPinia());
  });

  const seed = (list: unknown) => {
    const store = useSystemStore();
    store.dict = list as never;
  };

  it("resolves items by code and returns an empty list when absent", () => {
    seed([
      { code: "status", list: [{ name: "启用", value: "1", status: 1 }] },
      { code: "gender", list: [{ name: "男", value: "1", status: 1 }] }
    ]);
    expect(useDictItems("status").value).toEqual([{ name: "启用", value: "1", status: 1 }]);
    expect(useDictItems("missing").value).toEqual([]);
  });

  it("tolerates a null / malformed store payload without throwing", () => {
    seed(null);
    expect(useDictItems("status").value).toEqual([]);
    expect(useDictOptions("status", [{ label: "启用", value: "1" }]).value).toEqual([{ label: "启用", value: "1" }]);
  });

  it("builds select options from the live dictionary", () => {
    seed([
      {
        code: "status",
        list: [
          { name: "禁用", value: "0", status: 1 },
          { name: "启用", value: "1", status: 1 }
        ]
      }
    ]);
    const options = useDictOptions("status");
    expect(options.value).toEqual([
      { label: "禁用", value: "0" },
      { label: "启用", value: "1" }
    ]);
  });

  it("translates with the dictionary, falls back to the constant, and echoes unknown values", () => {
    seed([{ code: "status", list: [{ name: "启用", value: "1", status: 1 }] }]);
    const label = useDictLabel("status", { "0": "禁用", "1": "启用" });

    expect(label(1)).toBe("启用");
    expect(label("1")).toBe("启用");
    // 字典未收录该值 → 兜底表命中
    expect(label(0)).toBe("禁用");
    // 两者都未命中 → 原值回显（排障要看得见设备报了什么）
    expect(label(7)).toBe("7");
    // 空值不渲染占位
    expect(label(null)).toBe("");
    expect(label(undefined)).toBe("");
    expect(label("")).toBe("");
  });

  it("keeps the label map reactive to store updates", () => {
    seed([]);
    const labels = useDictLabelMap("status", { "1": "启用" });
    expect(labels.value["1"]).toBe("启用");

    seed([{ code: "status", list: [{ name: "正常", value: "1", status: 1 }] }]);
    expect(labels.value["1"]).toBe("正常");
  });

  it("never lets a hardcoded label leak when the dictionary drives the value", () => {
    // 字典改名后，界面必须跟着变 —— 这是"字典驱动"与"硬编码"的分水岭
    seed([{ code: "status", list: [{ name: "生效中", value: "1", status: 1 }] }]);
    expect(useDictLabel("status", { "1": "启用" })(1)).toBe("生效中");
  });
});
