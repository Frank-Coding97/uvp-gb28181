import { beforeEach, describe, expect, it, vi } from "vitest";
const request = vi.hoisted(() => vi.fn());
vi.mock("@/utils/http", () => ({ http: { request } }));
vi.mock("./utils", () => ({ baseUrlApi: (path: string) => "/api/" + path }));
import { applyHTTPS, checkHTTPSPort, validateHTTPSCertificate, getHTTPSStatus } from "./https-management";
describe("HTTPS management API", () => {
  beforeEach(() => request.mockReset().mockResolvedValue({ code: 0, data: {} }));
  it("uses fixed routes and keeps sync choice in multipart options", async () => {
    await getHTTPSStatus();
    expect(request).toHaveBeenLastCalledWith("get", "/api/system/https/status");
    await checkHTTPSPort(18443);
    expect(request).toHaveBeenLastCalledWith("post", "/api/system/https/check-port", { data: { port: 18443 } });
    const crt = new File(["cert"], "cert.pem"),
      key = new File(["key"], "key.pem");
    await validateHTTPSCertificate(crt, key);
    expect(request.mock.calls.at(-1)![1]).toBe("/api/system/https/certificate");
    await applyHTTPS({ enabled: false, port: 18443, syncZlm: false }, crt, key);
    const [, url, options] = request.mock.calls.at(-1)!;
    expect(url).toBe("/api/system/https/apply");
    expect(JSON.parse(options.data.get("options"))).toEqual({ enabled: false, port: 18443, syncZlm: false });
    expect(options.data.get("certificate")).toBe(crt);
    expect(options.data.get("privateKey")).toBe(key);
  });
});
