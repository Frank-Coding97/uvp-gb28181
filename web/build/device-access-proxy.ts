import { createServer, request as httpRequest, type IncomingMessage, type Server, type ServerResponse } from "node:http";
import { request as httpsRequest } from "node:https";
import { URL } from "node:url";
import type { AddressInfo } from "node:net";
import type { Plugin } from "vite";

const deviceProxyPrefixes = ["/api", "/gb28181", "/openapi", "/public"] as const;

type ProxyRequest = {
  setHeader(name: string, value: string): void;
};

type ViteProxy = {
  on(event: "proxyReq", listener: (proxyRequest: ProxyRequest, request: IncomingMessage) => void): void;
};

export interface DeviceHttpCompanion {
  server: Server;
  address(): AddressInfo | undefined;
  close(): Promise<void>;
}

export interface DeviceAccessProxyRuntime {
  plugin: Plugin;
  proxy: Record<string, DeviceProxyRule>;
}

export interface DeviceProxyRule {
  target: string;
  changeOrigin: boolean;
  xfwd: boolean;
  configure(proxy: ViteProxy): void;
}

function isDeviceProxyPath(rawURL: string | undefined): boolean {
  const pathname = new URL(rawURL ?? "/", "http://device-proxy.invalid").pathname;
  return deviceProxyPrefixes.some(prefix => pathname === prefix || pathname.startsWith(`${prefix}/`));
}

function requestHost(request: Pick<IncomingMessage, "headers">): string | undefined {
  const host = request.headers.host?.trim();
  if (!host || /[\s/?#@]/.test(host)) return undefined;
  try {
    const parsed = new URL(`http://${host}`);
    return parsed.pathname === "/" && !parsed.search && !parsed.hash && !parsed.username && !parsed.password ? host : undefined;
  } catch {
    return undefined;
  }
}

function requestProtocol(request: IncomingMessage): "http" | "https" {
  return (request.socket as { encrypted?: boolean }).encrypted ? "https" : "http";
}

function requestAddress(request: IncomingMessage): string {
  return request.socket.remoteAddress ?? "";
}

function replacePort(host: string, port: number): string | undefined {
  try {
    const parsed = new URL(`http://${host}`);
    const hostname = parsed.hostname.includes(":") ? `[${parsed.hostname.replace(/^\[|\]$/g, "")}]` : parsed.hostname;
    return `${hostname}:${port}`;
  } catch {
    return undefined;
  }
}

function deviceBaseURL(host: string | undefined, companionPort?: number): string | undefined {
  if (!host) return undefined;
  const authority = companionPort === undefined ? host : replacePort(host, companionPort);
  return authority ? `http://${authority}` : undefined;
}

function setDeviceAccessHeaders(proxyRequest: ProxyRequest, request: IncomingMessage, companionPort?: number) {
  const host = requestHost(request);
  proxyRequest.setHeader("host", host ?? "");
  proxyRequest.setHeader("x-forwarded-host", host ?? "");
  proxyRequest.setHeader("x-forwarded-proto", requestProtocol(request));
  proxyRequest.setHeader("x-forwarded-for", requestAddress(request));
  const base = deviceBaseURL(host, companionPort);
  proxyRequest.setHeader("x-uvp-device-base-url", base ?? "");
}

export function createDeviceProxyRules(
  target: string,
  companionPort: () => number | undefined = () => undefined
): Record<string, DeviceProxyRule> {
  return Object.fromEntries(
    deviceProxyPrefixes.map(prefix => [
      prefix,
      {
        target,
        changeOrigin: false,
        xfwd: false,
        configure(proxy: ViteProxy) {
          proxy.on("proxyReq", (proxyRequest, request) => setDeviceAccessHeaders(proxyRequest, request, companionPort()));
        }
      }
    ])
  );
}

function listen(server: Server, port: number, host: string): Promise<void> {
  return new Promise((resolveListen, reject) => {
    const onError = (error: Error) => {
      server.off("listening", onListening);
      reject(error);
    };
    const onListening = () => {
      server.off("error", onError);
      resolveListen();
    };
    server.once("error", onError);
    server.once("listening", onListening);
    server.listen({ port, host });
  });
}

function close(server: Server): Promise<void> {
  if (!server.listening) return Promise.resolve();
  return new Promise((resolveClose, reject) => {
    server.close(error => (error ? reject(error) : resolveClose()));
  });
}

function proxyRequest(target: URL, request: IncomingMessage, response: ServerResponse, companionPort: number) {
  const host = requestHost(request);
  const base = deviceBaseURL(host, companionPort);
  const headers = {
    ...request.headers,
    host: host ?? "",
    "x-forwarded-host": host ?? "",
    "x-forwarded-proto": "http",
    "x-forwarded-for": requestAddress(request),
    "x-uvp-device-base-url": base ?? ""
  };
  const send = target.protocol === "https:" ? httpsRequest : httpRequest;
  const upstream = send(
    {
      protocol: target.protocol,
      hostname: target.hostname,
      port: target.port || (target.protocol === "https:" ? 443 : 80),
      method: request.method,
      path: request.url,
      headers
    },
    upstreamResponse => {
      response.writeHead(upstreamResponse.statusCode ?? 502, upstreamResponse.headers);
      upstreamResponse.pipe(response);
    }
  );
  upstream.on("error", error => {
    if (response.headersSent) response.destroy(error);
    else response.writeHead(502).end("device access proxy unavailable");
  });
  request.on("aborted", () => upstream.destroy());
  request.pipe(upstream);
}

export async function startDeviceHttpCompanion({
  target,
  host,
  port
}: {
  target: string;
  host: string;
  port: number;
}): Promise<DeviceHttpCompanion> {
  const upstream = new URL(target);
  if (upstream.protocol !== "http:" && upstream.protocol !== "https:")
    throw new Error(`unsupported device proxy target: ${target}`);
  let companionPort = port;
  const server = createServer((request, response) => {
    if (!isDeviceProxyPath(request.url)) {
      response.writeHead(404).end();
      return;
    }
    proxyRequest(upstream, request, response, companionPort);
  });
  await listen(server, port, host);
  const address = server.address();
  if (!address || typeof address === "string") {
    await close(server);
    throw new Error("device access companion did not bind a TCP port");
  }
  companionPort = address.port;
  return {
    server,
    address: () => {
      const bound = server.address();
      return bound && typeof bound !== "string" ? bound : undefined;
    },
    close: () => close(server)
  };
}

function viteHost(host: string | boolean | undefined): string {
  return host === true ? "0.0.0.0" : typeof host === "string" && host ? host : "localhost";
}

export function createDeviceAccessProxyPlugin(
  target: string,
  setCompanionPort: (port: number | undefined) => void = () => undefined
): Plugin {
  return {
    name: "uvp-device-access-proxy",
    configureServer(server) {
      const listenVite = server.listen.bind(server);
      const closeVite = server.close.bind(server);
      let companion: DeviceHttpCompanion | undefined;

      server.listen = async (port?: number, isRestart?: boolean) => {
        const started = await listenVite(port, isRestart);
        if (!server.config.server.https || !server.httpServer) return started;
        const address = server.httpServer.address();
        if (!address || typeof address === "string") throw new Error("Vite HTTPS server did not bind a TCP port");
        const companionPort = address.port + 1;
        try {
          companion = await startDeviceHttpCompanion({ target, host: viteHost(server.config.server.host), port: companionPort });
          setCompanionPort(companionPort);
        } catch (error) {
          await closeVite();
          throw new Error(`设备 HTTP 入口 ${companionPort} 无法绑定: ${error instanceof Error ? error.message : String(error)}`);
        }
        return started;
      };
      server.close = async () => {
        setCompanionPort(undefined);
        await companion?.close();
        companion = undefined;
        await closeVite();
      };
    }
  };
}

export function createDeviceAccessProxyRuntime(target: string): DeviceAccessProxyRuntime {
  let companionPort: number | undefined;
  return {
    plugin: createDeviceAccessProxyPlugin(target, port => {
      companionPort = port;
    }),
    proxy: createDeviceProxyRules(target, () => companionPort)
  };
}
