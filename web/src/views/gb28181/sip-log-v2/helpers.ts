import dayjs from "dayjs";
import type { TraceMessage, TraceSession } from "./fixtures";

export function formatTime(value: string): string {
    return dayjs(value).format("HH:mm:ss.SSS");
}

export function formatFullTime(value: string): string {
    return dayjs(value).format("MM-DD HH:mm:ss.SSS");
}

export function formatDuration(ms: number): string {
    if (ms < 1000) return `${ms} ms`;
    if (ms < 60_000) return `${(ms / 1000).toFixed(1)} s`;
    if (ms < 3_600_000) return `${(ms / 60_000).toFixed(1)} min`;
    return `${(ms / 3_600_000).toFixed(1)} h`;
}

export function methodLabel(message: TraceMessage): string {
    if (message.statusCode) return String(message.statusCode);
    return message.method || message.cseqMethod || "?";
}

export type StatusTone = "success" | "warning" | "danger" | "neutral" | "info";

export function statusTone(code: number): StatusTone {
    if (!code) return "info";
    if (code >= 500) return "danger";
    if (code >= 400) return "warning";
    if (code >= 300) return "neutral";
    if (code >= 200) return "success";
    if (code >= 100) return "info";
    return "neutral";
}

export function methodTone(method: string): StatusTone {
    switch (method) {
        case "REGISTER":
        case "SUBSCRIBE":
        case "NOTIFY":
            return "info";
        case "INVITE":
        case "ACK":
            return "success";
        case "BYE":
        case "CANCEL":
            return "warning";
        case "MESSAGE":
            return "neutral";
        default:
            return "neutral";
    }
}

export function methodChainTone(step: string): StatusTone {
    if (/^\d+$/.test(step)) return statusTone(Number(step));
    if (step === "...") return "neutral";
    return methodTone(step);
}

export function sessionStateLabel(session: TraceSession): { label: string; tone: StatusTone } {
    if (session.scenario === "register-fail") return { label: "认证失败", tone: "danger" };
    if (session.scenario === "invite-pending") return { label: "点播卡住", tone: "warning" };
    if (session.finalStatus >= 400) return { label: `异常 ${session.finalStatus}`, tone: "danger" };
    if (session.finalStatus >= 200 && session.finalStatus < 300) return { label: "完成", tone: "success" };
    if (session.finalStatus === 100) return { label: "进行中", tone: "info" };
    return { label: "未知", tone: "neutral" };
}

function splitHeaderBody(payload: string): { header: string; body: string } {
    const marker = payload.indexOf("\r\n\r\n");
    if (marker < 0) return { header: payload, body: "" };
    return { header: payload.slice(0, marker), body: payload.slice(marker + 4) };
}

export interface ParsedPayload {
    startLine: string;
    headers: Array<{ name: string; value: string }>;
    body: string;
    bodyType: "sdp" | "xml" | "text" | "empty";
}

export function parsePayload(payload: string): ParsedPayload {
    const { header, body } = splitHeaderBody(payload);
    const lines = header.split("\r\n");
    const startLine = lines.shift() || "";
    const headers: Array<{ name: string; value: string }> = [];
    for (const line of lines) {
        if (!line.trim()) continue;
        const colon = line.indexOf(":");
        if (colon < 0) continue;
        headers.push({ name: line.slice(0, colon).trim(), value: line.slice(colon + 1).trim() });
    }
    const bodyTrimmed = body.trim();
    let bodyType: ParsedPayload["bodyType"] = "empty";
    if (bodyTrimmed.startsWith("v=0")) bodyType = "sdp";
    else if (bodyTrimmed.startsWith("<?xml") || bodyTrimmed.startsWith("<")) bodyType = "xml";
    else if (bodyTrimmed.length > 0) bodyType = "text";
    return { startLine, headers, body, bodyType };
}

// 关键 SIP header — 运维定位问题最先看的
export const KEY_HEADER_NAMES = new Set([
    "From", "To", "Call-ID", "CSeq", "Via", "Contact", "User-Agent",
    "Expires", "Event", "Subject", "Content-Type", "Content-Length",
    "WWW-Authenticate", "Authorization", "Max-Forwards"
]);

export function isKeyHeader(name: string): boolean {
    return KEY_HEADER_NAMES.has(name);
}
