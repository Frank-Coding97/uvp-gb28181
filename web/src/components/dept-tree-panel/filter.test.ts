import { describe, expect, it } from "vitest";
import type { DivisionItem } from "@/api/department";
import { filterDeptTree } from "./filter";

const dept = (id: number, name: string, children: DivisionItem[] = []): DivisionItem =>
  ({ id, name, parentId: 0, children }) as DivisionItem;

/**
 * 部门树过滤的三条契约见 filter.ts。这里锁的是「搜不到」与「搜了等于没搜」两个静默故障。
 */
describe("filterDeptTree", () => {
  const tree = [
    dept(1, "总部", [
      dept(11, "研发中心", [dept(111, "前端组"), dept(112, "后端组")]),
      dept(12, "市场部", [dept(121, "华东区")])
    ]),
    dept(2, "分公司", [dept(21, "客服部")])
  ];

  it("关键字为空时原样返回整棵树", () => {
    expect(filterDeptTree(tree, "")).toBe(tree);
    expect(filterDeptTree(tree, "   ")).toBe(tree);
  });

  it("命中末级节点时保留它的整条父链", () => {
    const result = filterDeptTree(tree, "前端");

    expect(result.map(node => node.name)).toEqual(["总部"]);
    expect(result[0].children?.map(node => node.name)).toEqual(["研发中心"]);
    expect(result[0].children?.[0].children?.map(node => node.name)).toEqual(["前端组"]);
  });

  it("命中中间节点时保留它、裁掉它下面的无关分支", () => {
    const result = filterDeptTree(tree, "研发");

    expect(result[0].children?.map(node => node.name)).toEqual(["研发中心"]);
    // ⛔ 命中节点自身命中 ≠ 展示其全部子树：否则一搜就退回整棵树
    expect(result[0].children?.[0].children).toEqual([]);
  });

  it("命中多个分支时各自保留父链，非命中叶子被裁掉", () => {
    const result = filterDeptTree(tree, "部");

    expect(result.map(node => node.name)).toEqual(["总部", "分公司"]);
    expect(result[0].children?.map(node => node.name)).toEqual(["市场部"]);
    // 「分公司」自身没命中，靠子节点「客服部」命中而被保留
    expect(result[1].children?.map(node => node.name)).toEqual(["客服部"]);
  });

  it("完全不命中时返回空树（页面据此展示无匹配文案）", () => {
    expect(filterDeptTree(tree, "不存在的部门")).toEqual([]);
  });

  it("不改动入参", () => {
    const snapshot = JSON.stringify(tree);
    filterDeptTree(tree, "研发");
    expect(JSON.stringify(tree)).toBe(snapshot);
  });
});
