import { describe, it, expect } from "vitest";
import { mergeDirectorySync } from "./directorySync";
import type { DirectoryNode } from "./api";
const node = (key: string, children: DirectoryNode[] = []): DirectoryNode => ({key,name:key,type:"area",readOnly:true,count:0,onlineCount:0,depth:0,children});
describe("directory incremental merge", () => {
 it("keeps unchanged references and applies updates, removals and root order", () => {
  const a=node("a"),b=node("b"),root=node("root",[a,b]);
  const same=mergeDirectorySync([root],{revision:"1",reset:false,roots:["root"],upserts:[],removed:[]});
  expect(same[0]).toBe(root);
  const result=mergeDirectorySync(same,{revision:"2",reset:false,roots:["root","new"],removed:["b"],upserts:[
   {node:{...root,count:1},children:["a"]}, {node:node("new"),children:[]}
  ]});
  expect(result.map(n=>n.key)).toEqual(["root","new"]);
  expect(result[0].children).toEqual([a]);
  expect(result[0].children?.[0]).toBe(a);
 });
 it("applies a leaf state update without mutating old data", () => {
  const a=node("a"),root=node("root",[a]);
  const result=mergeDirectorySync([root],{revision:"2",reset:false,roots:["root"],removed:[],upserts:[{node:{...a,name:"new"},children:[]}]});
  expect(result[0].children?.[0].name).toBe("new");
  expect(a.name).toBe("a");
 });
 it("accepts full empty resets",()=>{
  expect(mergeDirectorySync([node("root")],{revision:"3",reset:true,roots:[],upserts:[],removed:[]})).toEqual([]);
 });
});
