import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

/**
 * 「接口管理」的 API 分组必须来自后端受控清单，不能退回自由输入。
 *
 * ⛔ 回归背景：api_group 过去是 a-input 自由文本，加上两个 SQL 生成器各自写死兜底分组
 *    （`按钮权限目录` / `游客权限依赖`），开发库里漂出 39 个分组 —— 同模块被拆成兄弟组、
 *    `GB28181设备管理` 缺空格、甚至出现双重编码的乱码组名。
 *    后端现在有 GET /sysApi/groups + 写入校验，前端这里必须用下拉接上，否则校验只会报错。
 */
const read = (path: string) => readFileSync(resolve(process.cwd(), path), "utf-8");

const sysapiPage = read("src/views/system/sysapi/sysapi.vue");
const apiPermission = read("src/components/s-api-permission/index.vue");
const apiModule = read("src/api/sysapi.ts");

describe("API 分组受控下拉", () => {
  it("页面从受控清单接口取分组选项", () => {
    expect(apiModule).toMatch(/export const getSysApiGroupsAPI = \(\) => \{[\s\S]*?baseUrlApi\("sysApi\/groups"\)/);
    expect(sysapiPage).toContain("getSysApiGroupsAPI");
    expect(sysapiPage).toMatch(/const groupOptions = ref<string\[\]>\(\[\]\)/);
    expect(sysapiPage).toMatch(/groupOptions\.value = data \|\| \[\]/);
    // 必须在挂载时拉一次，否则下拉是空的、用户存不进任何 API
    expect(sysapiPage).toMatch(/onMounted\(\(\) => \{\s*loadGroupOptions\(\);/);
  });

  it("新增/编辑表单的分组是下拉而非自由输入", () => {
    expect(sysapiPage).toMatch(
      /<a-select v-model="addFrom\.apiGroup"[\s\S]*?<a-option v-for="item in groupOptions" :key="item" :value="item">/
    );
    expect(sysapiPage).not.toMatch(/v-model="addFrom\.apiGroup"[^>]*allow-create/);
    expect(sysapiPage).not.toMatch(/<a-input v-model="addFrom\.apiGroup"/);
  });

  it("查询条件的分组也是下拉，且不再允许自由输入", () => {
    expect(sysapiPage).toMatch(
      /<a-select v-model="form\.apiGroup"[\s\S]*?<a-option v-for="item in groupOptions" :key="item" :value="item">/
    );
    expect(sysapiPage).not.toMatch(/<a-input v-model="form\.apiGroup"/);
    expect(apiPermission).toMatch(
      /<a-select[\s\S]*?v-model="searchForm\.apiGroup"[\s\S]*?<a-option v-for="item in groupOptions" :key="item" :value="item">/
    );
    expect(apiPermission).not.toMatch(/<a-input[\s\S]{0,200}?v-model="searchForm\.apiGroup"/);
  });

  it("权限分配抽屉也拉受控清单", () => {
    expect(apiPermission).toContain("getSysApiGroupsAPI");
    expect(apiPermission).toMatch(/loadGroupOptions\(\);\s*\n\s*loadMenuApis\(\);/);
  });
});
