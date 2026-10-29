import { describe, it, expect } from "vitest";
import { sortDevicesOnlineFirst } from "./directorySort";
import type { DirectoryNode } from "./api";

const dir = (key: string, children: DirectoryNode[] = []): DirectoryNode => ({
  key,
  name: key,
  type: "area",
  readOnly: true,
  count: children.length,
  onlineCount: 0,
  depth: 0,
  children
});

const dev = (key: string, online: boolean): DirectoryNode => ({
  key,
  name: key,
  type: "device",
  readOnly: true,
  count: 0,
  onlineCount: online ? 1 : 0,
  depth: 1,
  device: { id: Number(key.replace(/\D/g, "")) || 0, deviceId: key, name: key, alias: "", online }
});

const keys = (nodes: DirectoryNode[] | undefined) => (nodes || []).map(node => node.key);

describe("sortDevicesOnlineFirst", () => {
  it("把在线设备提到同级最前，离线沉底，同一状态内保持后端原始顺序", () => {
    const tree = [dir("root", [dev("a", false), dev("b", true), dev("c", false), dev("d", true)])];
    expect(keys(sortDevicesOnlineFirst(tree)[0].children)).toEqual(["b", "d", "a", "c"]);
  });

  it("目录节点位置不动：设备只在原来的槽位上换内容", () => {
    const tree = [dir("root", [dir("area-x"), dev("off", false), dir("area-y"), dev("on", true)])];
    const children = sortDevicesOnlineFirst(tree)[0].children;
    expect(keys(children)).toEqual(["area-x", "on", "area-y", "off"]);
    // 目录节点本身不能被替换成新对象（负责人/展开态都挂在原节点上）
    expect(children?.[0].key).toBe("area-x");
  });

  it("递归到多级目录，只影响各自父节点下的兄弟", () => {
    const tree = [
      dir("province", [
        dir("city-a", [dev("a-off", false), dev("a-on", true)]),
        dir("city-b", [dev("b-off", false), dev("b-on", true)])
      ])
    ];
    const [province] = sortDevicesOnlineFirst(tree);
    expect(keys(province.children?.[0].children)).toEqual(["a-on", "a-off"]);
    expect(keys(province.children?.[1].children)).toEqual(["b-on", "b-off"]);
  });

  it("顺序没变时复用原数组与原节点，不制造无谓重渲染", () => {
    const children = [dev("on", true), dev("off", false)];
    const root = dir("root", children);
    const result = sortDevicesOnlineFirst([root]);
    expect(result[0]).toBe(root);
    expect(result[0].children).toBe(children);
  });

  it("不改动入参（后端顺序要留给增量同步做 diff）", () => {
    const children = [dev("off", false), dev("on", true)];
    const tree = [dir("root", children)];
    sortDevicesOnlineFirst(tree);
    expect(keys(children)).toEqual(["off", "on"]);
  });
});
