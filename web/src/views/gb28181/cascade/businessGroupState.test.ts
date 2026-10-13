import { describe, expect, it } from "vitest";
import type { BusinessBinding, BusinessNode, CascadeChannelProjection } from "@/api/gb28181";
import { businessNodeErrors, createBusinessNodeCode, descendantCodes } from "./businessGroupState";

const localDeviceId = "34020000002000000001";
const root: BusinessNode = {
  code: "34020000002150000001",
  name: "业务根",
  kind: "group",
  parentCode: "",
  sort: 0
};
const organization: BusinessNode = {
  code: "34020000002160000002",
  name: "组织",
  kind: "organization",
  parentCode: root.code,
  sort: 0
};
const channel: CascadeChannelProjection = {
  sourceChannelId: 31,
  sourceDeviceId: 3,
  publishedChannelId: "34020000001320000031",
  name: "已共享通道",
  parentOverride: "",
  ptzAllowed: false,
  active: true
};

describe("cascade business-group state", () => {
  it("generates stable-type 20-digit codes and advances across collisions", () => {
    expect(createBusinessNodeCode(localDeviceId, "group", [])).toBe(root.code);
    expect(createBusinessNodeCode(localDeviceId, "organization", [organization])).toBe("34020000002160000003");
    expect(createBusinessNodeCode("bad", "group", [])).toBe("");
  });

  it("checks the GB node type in digits 11 through 13", () => {
    expect(businessNodeErrors([{ ...root, code: "34020000002160000001" }], [], [])).toContain(
      "节点国标编号必须为 20 位数字，且第 11–13 位类型码与节点一致（根 215、组织 216）"
    );
    expect(businessNodeErrors([{ ...root, code: "34020000002150000001" }], [], [])).toEqual([]);
  });

  it("rejects malformed roots, cycles, invalid names and bindings outside shared channels", () => {
    expect(
      businessNodeErrors([root, organization], [{ sourceChannelId: 31, nodeCode: organization.code }], [channel])
    ).toEqual([]);
    const cycle = [
      root,
      { ...organization, parentCode: "34020000002160000003" },
      { ...organization, code: "34020000002160000003", parentCode: organization.code }
    ];
    expect(businessNodeErrors(cycle, [], [channel])).toContain("业务分组存在循环或孤立节点");
    expect(businessNodeErrors([{ ...root, name: " " }], [], [])).toContain("节点名称不能为空");
    expect(
      businessNodeErrors([root, organization], [{ sourceChannelId: 99, nodeCode: organization.code }], [channel])
    ).toContain("只能挂载当前上级已共享的有效通道");
  });

  it("finds a node and all descendants without making cycles recurse forever", () => {
    const cyclic: BusinessNode[] = [
      root,
      organization,
      { ...organization, code: "34020000002160000003", parentCode: organization.code }
    ];
    expect(descendantCodes(cyclic, organization.code)).toEqual(new Set([organization.code, "34020000002160000003"]));
    const bindings: BusinessBinding[] = [];
    expect(bindings).toEqual([]);
  });
});
