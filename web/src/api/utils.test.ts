import { describe, expect, it } from "vitest";
import { joinApiPath, normalizeAppBaseUrl } from "./utils";

/**
 * 回归背景（2026-10-22 线上事故）：热修包把接口基址带成了 `/`，
 * `baseUrlApi` 拼出 `//api/login`（协议相对 URL），浏览器把 `api` 当主机名，
 * 全站请求 `ERR_NAME_NOT_RESOLVED` ⇒ 登录页能开、一点登录就没反应。
 * 这组用例锁死「任何形式的基址都不会拼出双斜杠」。
 */
describe("normalizeAppBaseUrl", () => {
  it("根部署的空串与单斜杠都归一成空串", () => {
    expect(normalizeAppBaseUrl("")).toBe("");
    expect(normalizeAppBaseUrl("/")).toBe("");
    expect(normalizeAppBaseUrl(undefined)).toBe("");
    expect(normalizeAppBaseUrl(null)).toBe("");
  });

  it("子目录部署只去掉结尾斜杠，保留前缀", () => {
    expect(normalizeAppBaseUrl("/uvp/")).toBe("/uvp");
    expect(normalizeAppBaseUrl("/uvp//")).toBe("/uvp");
    expect(normalizeAppBaseUrl(" /uvp/ ")).toBe("/uvp");
  });

  it("绝对源站与相对基址按原样保留（去掉结尾斜杠）", () => {
    expect(normalizeAppBaseUrl("https://demo.example.com/")).toBe("https://demo.example.com");
    expect(normalizeAppBaseUrl("https://demo.example.com/uvp/")).toBe("https://demo.example.com/uvp");
  });
});

describe("joinApiPath", () => {
  it("根部署下只有单斜杠，绝不允许出现 //api", () => {
    for (const base of ["", "/", "/" + "/", undefined]) {
      const path = joinApiPath(base as string, "users/login");
      expect(path).toBe("/api/users/login");
      expect(path.startsWith("//")).toBe(false);
    }
  });

  it("子目录部署把前缀带在前面", () => {
    expect(joinApiPath("/uvp", "users/login")).toBe("/uvp/api/users/login");
    expect(joinApiPath("/uvp/", "users/login")).toBe("/uvp/api/users/login");
  });

  it("绝对源站基址保持完整", () => {
    expect(joinApiPath("https://demo.example.com/", "config/get")).toBe("https://demo.example.com/api/config/get");
  });

  it("不吞掉接口路径自身的层级", () => {
    expect(joinApiPath("", "gb28181/openapi-clients/2/scopes")).toBe("/api/gb28181/openapi-clients/2/scopes");
  });
});
