import { http } from "@/utils/http";
import { baseUrlApi } from "@/api/utils";
import type { BaseResult } from "@/api/types";

export interface HTTPSCertificate {
  subject: string;
  dnsNames: string[];
  notBefore: string;
  notAfter: string;
  fingerprint: string;
}
export interface HTTPSOperation {
  id: string;
  state: "running" | "succeeded" | "failed" | "partial" | "recovery_required";
  nginx: string;
  zlm: string;
  error?: string;
}
export interface HTTPSStatus {
  supported: boolean;
  reason?: string;
  settings: { enabled: boolean; port: number };
  httpPort: number;
  certificate?: HTTPSCertificate;
  nginxHealthy: boolean;
  nginxFingerprint?: string;
  zlmFingerprint?: string;
  probeError?: string;
  operation?: HTTPSOperation;
}
export interface HTTPSOptions {
  enabled: boolean;
  port: number;
  syncZlm: boolean;
}
const material = (certificate?: File, privateKey?: File) => {
  const data = new FormData();
  if (certificate) data.append("certificate", certificate);
  if (privateKey) data.append("privateKey", privateKey);
  return data;
};
const uploadOptions = { headers: { "Content-Type": "multipart/form-data" }, timeout: 30000 };
export const getHTTPSStatus = () => http.request<BaseResult<HTTPSStatus>>("get", baseUrlApi("system/https/status"));
export const checkHTTPSPort = (port: number) =>
  http.request<BaseResult<{ available: boolean }>>("post", baseUrlApi("system/https/check-port"), { data: { port } });
export const validateHTTPSCertificate = (certificate: File, privateKey: File) =>
  http.request<BaseResult<HTTPSCertificate>>("post", baseUrlApi("system/https/certificate"), {
    data: material(certificate, privateKey),
    ...uploadOptions
  });
export const applyHTTPS = (options: HTTPSOptions, certificate?: File, privateKey?: File) => {
  const data = material(certificate, privateKey);
  data.append("options", JSON.stringify(options));
  return http.request<BaseResult<HTTPSOperation>>("post", baseUrlApi("system/https/apply"), { data, ...uploadOptions });
};
