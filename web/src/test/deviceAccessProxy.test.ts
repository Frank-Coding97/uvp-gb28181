// @vitest-environment node
import {
  createServer as createHttpServer,
  request as httpRequest,
  type IncomingMessage,
  type RequestOptions,
  type Server
} from "node:http";
import { request as httpsRequest } from "node:https";
import { createServer as createViteServer } from "vite";
import { afterEach, describe, expect, it } from "vitest";
import {
  createDeviceAccessProxyRuntime,
  createDeviceProxyRules,
  startDeviceHttpCompanion
} from "../../build/device-access-proxy";
import { DEVICE_ACCESS_PROXY_TEST_CERT, DEVICE_ACCESS_PROXY_TEST_KEY } from "./fixtures/device-access-proxy-cert";

const servers: Array<{ close(): Promise<void> }> = [];
type RequestOptionsWithTLS = RequestOptions & { rejectUnauthorized?: boolean };
type ProxyRequestListener = (
  proxyRequest: { setHeader(name: string, value: string): void },
  request: IncomingMessage,
  options?: unknown
) => void;

afterEach(async () => {
  await Promise.all(
    servers
      .splice(0)
      .reverse()
      .map(server => server.close())
  );
});

function listen(server: Server, port = 0): Promise<number> {
  return new Promise((resolvePort, reject) => {
    server.once("error", reject);
    server.listen(port, "127.0.0.1", () => {
      server.off("error", reject);
      const address = server.address();
      if (!address || typeof address === "string") return reject(new Error("test server has no TCP port"));
      resolvePort(address.port);
    });
  });
}

function readRequest(request: IncomingMessage): Promise<string> {
  return new Promise((resolveBody, reject) => {
    const chunks: Buffer[] = [];
    request.on("data", chunk => chunks.push(Buffer.from(chunk)));
    request.on("end", () => resolveBody(Buffer.concat(chunks).toString("utf8")));
    request.on("error", reject);
  });
}

function requestJSON(options: RequestOptionsWithTLS, body?: string, secure = false): Promise<{ status: number; body: any }> {
  return new Promise((resolveResponse, reject) => {
    const request = (secure ? httpsRequest : httpRequest)(options, response => {
      const chunks: Buffer[] = [];
      response.on("data", chunk => chunks.push(Buffer.from(chunk)));
      response.on("end", () => {
        resolveResponse({ status: response.statusCode ?? 0, body: JSON.parse(Buffer.concat(chunks).toString("utf8")) });
      });
    });
    request.on("error", reject);
    if (body) request.write(body);
    request.end();
  });
}

function requestStatus(options: RequestOptions): Promise<number> {
  return new Promise((resolveStatus, reject) => {
    const request = httpRequest(options, response => {
      response.resume();
      response.on("end", () => resolveStatus(response.statusCode ?? 0));
    });
    request.on("error", reject);
    request.end();
  });
}

function createEchoBackend() {
  const server = createHttpServer(async (request, response) => {
    const body = await readRequest(request);
    response.setHeader("content-type", "application/json");
    response.end(JSON.stringify({ method: request.method, path: request.url, body, headers: request.headers }));
  });
  return server;
}

async function reserveVitePort(): Promise<number> {
  for (let attempt = 0; attempt < 20; attempt += 1) {
    const mainReservation = createHttpServer();
    const mainPort = await listen(mainReservation);
    const companionReservation = createHttpServer();
    try {
      await listen(companionReservation, mainPort + 1);
      await Promise.all([
        new Promise<void>(resolveClose => mainReservation.close(() => resolveClose())),
        new Promise<void>(resolveClose => companionReservation.close(() => resolveClose()))
      ]);
      return mainPort;
    } catch {
      await Promise.all([
        new Promise<void>(resolveClose => mainReservation.close(() => resolveClose())),
        new Promise<void>(resolveClose => companionReservation.close(() => resolveClose()))
      ]);
    }
  }
  throw new Error("unable to reserve adjacent Vite and device companion ports");
}

function createHttpsVite(target: string, port: number) {
  const deviceAccessProxy = createDeviceAccessProxyRuntime(target);
  return createViteServer({
    configFile: false,
    logLevel: "silent",
    appType: "custom",
    plugins: [deviceAccessProxy.plugin],
    server: {
      host: "127.0.0.1",
      port,
      strictPort: true,
      https: { key: DEVICE_ACCESS_PROXY_TEST_KEY, cert: DEVICE_ACCESS_PROXY_TEST_CERT },
      proxy: deviceAccessProxy.proxy
    }
  });
}

describe("device access development proxy", () => {
  it("overrides device and forwarded headers from the actual incoming request", () => {
    const rules = createDeviceProxyRules("http://127.0.0.1:8280", () => undefined);
    expect(Object.keys(rules)).toEqual(["/api", "/gb28181", "/openapi", "/public"]);
    const proxy = {
      on: (_event: string, callback: ProxyRequestListener) => callback({ setHeader: setHeader }, request, {}),
      onCalls: 0
    };
    const headers = new Map<string, string>();
    const setHeader = (name: string, value: string) => headers.set(name.toLowerCase(), value);
    const request = {
      headers: {
        host: "lan-device.example:5177",
        "x-uvp-device-base-url": "http://attacker.example",
        "x-forwarded-host": "attacker.example",
        "x-forwarded-proto": "https"
      },
      socket: { encrypted: true, remoteAddress: "192.0.2.10" }
    } as any;

    for (const rule of Object.values(rules)) rule.configure(proxy as any);

    expect(Object.fromEntries(headers)).toMatchObject({
      host: "lan-device.example:5177",
      "x-forwarded-host": "lan-device.example:5177",
      "x-forwarded-proto": "https",
      "x-forwarded-for": "192.0.2.10",
      "x-uvp-device-base-url": "http://lan-device.example:5177"
    });

    const missingHostHeaders = new Map<string, string>();
    const missingHostRequest = {
      headers: {
        "x-uvp-device-base-url": "http://attacker.example",
        "x-forwarded-for": "203.0.113.99",
        "x-forwarded-host": "attacker.example",
        "x-forwarded-proto": "https"
      },
      socket: { encrypted: false, remoteAddress: "192.0.2.11" }
    } as any;
    const missingHostProxy = {
      on: (_event: string, callback: ProxyRequestListener) =>
        callback(
          { setHeader: (name: string, value: string) => missingHostHeaders.set(name.toLowerCase(), value) },
          missingHostRequest,
          {}
        )
    };
    rules["/api"].configure(missingHostProxy as any);
    expect(Object.fromEntries(missingHostHeaders)).toMatchObject({
      host: "",
      "x-forwarded-host": "",
      "x-forwarded-proto": "http",
      "x-forwarded-for": "192.0.2.11",
      "x-uvp-device-base-url": ""
    });

    const invalidHostHeaders = new Map<string, string>();
    const invalidHostRequest = {
      headers: {
        host: "attacker.example/path",
        "x-uvp-device-base-url": "http://attacker.example",
        "x-forwarded-for": "203.0.113.99",
        "x-forwarded-host": "attacker.example",
        "x-forwarded-proto": "https"
      },
      socket: { encrypted: false, remoteAddress: "192.0.2.12" }
    } as any;
    const invalidHostProxy = {
      on: (_event: string, callback: ProxyRequestListener) =>
        callback(
          { setHeader: (name: string, value: string) => invalidHostHeaders.set(name.toLowerCase(), value) },
          invalidHostRequest,
          {}
        )
    };
    rules["/api"].configure(invalidHostProxy as any);
    expect(Object.fromEntries(invalidHostHeaders)).toMatchObject({
      host: "",
      "x-forwarded-host": "",
      "x-forwarded-proto": "http",
      "x-forwarded-for": "192.0.2.12",
      "x-uvp-device-base-url": ""
    });
  });

  it("runs an HTTP device companion for HTTPS Vite and preserves bodies for QR and snapshot paths", async () => {
    const backend = createEchoBackend();
    const backendPort = await listen(backend);
    servers.push({ close: () => new Promise(resolveClose => backend.close(() => resolveClose())) });
    const target = `http://127.0.0.1:${backendPort}`;
    const vite = await createHttpsVite(target, 0);
    servers.push(vite);
    await vite.listen();
    const address = vite.httpServer!.address();
    if (!address || typeof address === "string") throw new Error("vite has no TCP port");
    const mainPort = address.port;
    const companionPort = mainPort + 1;

    const main = await requestJSON(
      {
        hostname: "127.0.0.1",
        port: mainPort,
        path: "/gb28181/qr",
        method: "GET",
        rejectUnauthorized: false,
        headers: {
          host: `device.example:${mainPort}`,
          "x-uvp-device-base-url": "http://attacker.example",
          "x-forwarded-for": "203.0.113.99",
          "x-forwarded-host": "attacker.example",
          "x-forwarded-proto": "http"
        }
      },
      undefined,
      true
    );
    expect(main.status).toBe(200);
    expect(main.body.headers.host).toBe(`device.example:${mainPort}`);
    expect(main.body.headers["x-forwarded-proto"]).toBe("https");
    expect(main.body.headers["x-forwarded-for"]).toBe("127.0.0.1");
    expect(main.body.headers["x-uvp-device-base-url"]).toBe(`http://device.example:${companionPort}`);

    const landing = await requestJSON({
      hostname: "127.0.0.1",
      port: companionPort,
      path: "/gb28181/qr",
      method: "GET",
      headers: {
        host: `device.example:${companionPort}`,
        "x-uvp-device-base-url": "http://attacker.example",
        "x-forwarded-for": "203.0.113.99",
        "x-forwarded-host": "attacker.example",
        "x-forwarded-proto": "https"
      }
    });
    expect(landing.status).toBe(200);
    expect(landing.body.path).toBe("/gb28181/qr");
    expect(landing.body.headers.host).toBe(`device.example:${companionPort}`);
    expect(landing.body.headers["x-forwarded-proto"]).toBe("http");
    expect(landing.body.headers["x-forwarded-for"]).toBe("127.0.0.1");
    expect(landing.body.headers["x-uvp-device-base-url"]).toBe(`http://device.example:${companionPort}`);

    const exchange = await requestJSON(
      {
        hostname: "127.0.0.1",
        port: companionPort,
        path: "/api/gb28181/sip/qr/exchange",
        method: "POST",
        headers: { "content-type": "application/json" }
      },
      '{"token":"one-time"}'
    );
    expect(exchange).toMatchObject({ status: 200, body: { method: "POST", body: '{"token":"one-time"}' } });

    const upload = await requestJSON(
      {
        hostname: "127.0.0.1",
        port: companionPort,
        path: "/api/gb28181/device-snapshots/uploads/token/image.jpg",
        method: "PUT",
        headers: { "content-type": "image/jpeg" }
      },
      "jpeg-data"
    );
    expect(upload).toMatchObject({ status: 200, body: { method: "PUT", body: "jpeg-data" } });
    expect(await requestStatus({ hostname: "127.0.0.1", port: companionPort, path: "/not-device-access", method: "GET" })).toBe(
      404
    );

    const companion = await startDeviceHttpCompanion({ target, host: "127.0.0.1", port: 0 });
    servers.push(companion);
    const companionAddress = companion.address();
    expect(companionAddress?.port).toBeGreaterThan(0);
    await companion.close();
    expect(companion.server.listening).toBe(false);
    await vite.close();
    await expect(
      requestStatus({ hostname: "127.0.0.1", port: companionPort, path: "/gb28181/qr", method: "GET" })
    ).rejects.toMatchObject({
      code: expect.stringMatching(/^ECONN(?:REFUSED|RESET)$/)
    });

    const restarted = await createHttpsVite(target, mainPort);
    servers.push(restarted);
    await restarted.listen();
    const restartedRequest = await requestJSON(
      { hostname: "127.0.0.1", port: mainPort, path: "/gb28181/qr", method: "GET", rejectUnauthorized: false },
      undefined,
      true
    );
    expect(restartedRequest.status).toBe(200);
    await restarted.close();
  });

  it("fails Vite startup when the HTTPS device companion port is occupied", async () => {
    const backend = createEchoBackend();
    const backendPort = await listen(backend);
    servers.push({ close: () => new Promise(resolveClose => backend.close(() => resolveClose())) });
    const mainPort = await reserveVitePort();
    const blocker = createHttpServer();
    await listen(blocker, mainPort + 1);
    servers.push({ close: () => new Promise(resolveClose => blocker.close(() => resolveClose())) });
    const vite = await createHttpsVite(`http://127.0.0.1:${backendPort}`, mainPort);
    servers.push(vite);

    await expect(vite.listen()).rejects.toThrow(`设备 HTTP 入口 ${mainPort + 1} 无法绑定`);
  });
});
