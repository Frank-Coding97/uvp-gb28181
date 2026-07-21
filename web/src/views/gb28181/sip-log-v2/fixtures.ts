/**
 * SIP 日志新静态原型数据
 * 覆盖场景:注册成功 / 注册失败(401 循环)/ 心跳 / 点播卡住 / 订阅
 */

export type Direction = "inbound" | "outbound";

export interface TraceMessage {
    eventId: string;
    occurredAt: string;
    direction: Direction;
    transport: string;
    localAddr: string;
    remoteAddr: string;
    deviceId: string;
    method: string;
    statusCode: number;
    callId: string;
    cseq: number;
    cseqMethod: string;
    malformed: boolean;
    payload: string;
}

export interface TraceSession {
    callId: string;
    deviceId: string;
    deviceLabel: string;
    firstAt: string;
    lastAt: string;
    durationMs: number;
    messageCount: number;
    inboundCount: number;
    outboundCount: number;
    methodSequence: string[];
    finalStatus: number;
    hasAuthChallenge: boolean;
    anomaly: boolean;
    scenario: "register-ok" | "register-fail" | "keepalive" | "invite-pending" | "subscribe";
}

const now = new Date("2026-07-21T15:00:00Z");

function iso(offsetMs: number): string {
    return new Date(now.getTime() + offsetMs).toISOString();
}

function registerAuthPayload(cseq: number, deviceId: string, callId: string, withAuth: boolean): string {
    const authLine = withAuth
        ? `Authorization: Digest username="${deviceId}",realm="3402000000",nonce="0a1b2c3d4e5f",uri="sip:3402000000@192.168.1.100:5060",response="a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6",algorithm=MD5\r\n`
        : "";
    return `REGISTER sip:3402000000@192.168.1.100:5060 SIP/2.0\r
Via: SIP/2.0/UDP 192.168.1.55:5060;rport;branch=z9hG4bK${callId.slice(0, 12)}\r
From: <sip:${deviceId}@3402000000>;tag=${callId.slice(0, 8)}\r
To: <sip:${deviceId}@3402000000>\r
Call-ID: ${callId}\r
CSeq: ${cseq} REGISTER\r
Contact: <sip:${deviceId}@192.168.1.55:5060>\r
Max-Forwards: 70\r
User-Agent: HIKVISION DS-2CD3T47EWDA3-L V5.7.15\r
${authLine}Expires: 3600\r
Content-Length: 0\r
\r
`;
}

function response401Payload(cseq: number, deviceId: string, callId: string): string {
    return `SIP/2.0 401 Unauthorized\r
Via: SIP/2.0/UDP 192.168.1.55:5060;rport=5060;received=192.168.1.55;branch=z9hG4bK${callId.slice(0, 12)}\r
From: <sip:${deviceId}@3402000000>;tag=${callId.slice(0, 8)}\r
To: <sip:${deviceId}@3402000000>;tag=platform-${callId.slice(0, 6)}\r
Call-ID: ${callId}\r
CSeq: ${cseq} REGISTER\r
WWW-Authenticate: Digest realm="3402000000",nonce="0a1b2c3d4e5f",algorithm=MD5\r
Content-Length: 0\r
\r
`;
}

function response200Payload(cseq: number, cseqMethod: string, deviceId: string, callId: string): string {
    return `SIP/2.0 200 OK\r
Via: SIP/2.0/UDP 192.168.1.55:5060;rport=5060;received=192.168.1.55;branch=z9hG4bK${callId.slice(0, 12)}\r
From: <sip:${deviceId}@3402000000>;tag=${callId.slice(0, 8)}\r
To: <sip:${deviceId}@3402000000>;tag=platform-${callId.slice(0, 6)}\r
Call-ID: ${callId}\r
CSeq: ${cseq} ${cseqMethod}\r
Contact: <sip:${deviceId}@192.168.1.55:5060>\r
Expires: 3600\r
Content-Length: 0\r
\r
`;
}

function keepaliveXmlPayload(cseq: number, deviceId: string, callId: string, isRequest: boolean): string {
    const sn = 1000 + cseq;
    const body = `<?xml version="1.0" encoding="GB2312"?>\r
<Notify>\r
<CmdType>Keepalive</CmdType>\r
<SN>${sn}</SN>\r
<DeviceID>${deviceId}</DeviceID>\r
<Status>OK</Status>\r
</Notify>\r
`;
    if (isRequest) {
        return `MESSAGE sip:3402000000@192.168.1.100:5060 SIP/2.0\r
Via: SIP/2.0/UDP 192.168.1.55:5060;rport;branch=z9hG4bK${callId.slice(0, 12)}\r
From: <sip:${deviceId}@3402000000>;tag=${callId.slice(0, 8)}\r
To: <sip:3402000000@3402000000>\r
Call-ID: ${callId}\r
CSeq: ${cseq} MESSAGE\r
Max-Forwards: 70\r
User-Agent: HIKVISION\r
Content-Type: Application/MANSCDP+xml\r
Content-Length: ${body.length}\r
\r
${body}`;
    }
    return response200Payload(cseq, "MESSAGE", deviceId, callId);
}

function inviteWithSdpPayload(cseq: number, deviceId: string, callId: string): string {
    const sdp = `v=0\r
o=${deviceId} 0 0 IN IP4 192.168.1.100\r
s=Play\r
c=IN IP4 192.168.1.100\r
t=0 0\r
m=video 30000 RTP/AVP 96 98 97\r
a=recvonly\r
a=rtpmap:96 PS/90000\r
a=rtpmap:98 H264/90000\r
a=rtpmap:97 MPEG4/90000\r
y=0100000001\r
f=v/2/4///a///\r
`;
    return `INVITE sip:${deviceId}@192.168.1.55:5060 SIP/2.0\r
Via: SIP/2.0/UDP 192.168.1.100:5060;rport;branch=z9hG4bK${callId.slice(0, 12)}\r
From: <sip:3402000000@3402000000>;tag=${callId.slice(0, 8)}\r
To: <sip:${deviceId}@3402000000>\r
Call-ID: ${callId}\r
CSeq: ${cseq} INVITE\r
Contact: <sip:3402000000@192.168.1.100:5060>\r
Max-Forwards: 70\r
Subject: ${deviceId}:0100000001,3402000000:0\r
Content-Type: application/sdp\r
Content-Length: ${sdp.length}\r
\r
${sdp}`;
}

function trying100Payload(cseq: number, deviceId: string, callId: string): string {
    return `SIP/2.0 100 Trying\r
Via: SIP/2.0/UDP 192.168.1.100:5060;rport=5060;received=192.168.1.100;branch=z9hG4bK${callId.slice(0, 12)}\r
From: <sip:3402000000@3402000000>;tag=${callId.slice(0, 8)}\r
To: <sip:${deviceId}@3402000000>\r
Call-ID: ${callId}\r
CSeq: ${cseq} INVITE\r
Content-Length: 0\r
\r
`;
}

function subscribeCatalogPayload(cseq: number, deviceId: string, callId: string): string {
    const body = `<?xml version="1.0" encoding="GB2312"?>\r
<Query>\r
<CmdType>Catalog</CmdType>\r
<SN>${1200 + cseq}</SN>\r
<DeviceID>${deviceId}</DeviceID>\r
</Query>\r
`;
    return `SUBSCRIBE sip:${deviceId}@192.168.1.55:5060 SIP/2.0\r
Via: SIP/2.0/UDP 192.168.1.100:5060;rport;branch=z9hG4bK${callId.slice(0, 12)}\r
From: <sip:3402000000@3402000000>;tag=${callId.slice(0, 8)}\r
To: <sip:${deviceId}@3402000000>\r
Call-ID: ${callId}\r
CSeq: ${cseq} SUBSCRIBE\r
Contact: <sip:3402000000@192.168.1.100:5060>\r
Max-Forwards: 70\r
Event: Catalog;id=${1000 + cseq}\r
Expires: 3600\r
Content-Type: Application/MANSCDP+xml\r
Content-Length: ${body.length}\r
\r
${body}`;
}

function notifyCatalogPayload(cseq: number, deviceId: string, callId: string): string {
    const body = `<?xml version="1.0" encoding="GB2312"?>\r
<Notify>\r
<CmdType>Catalog</CmdType>\r
<SN>${1400 + cseq}</SN>\r
<DeviceID>${deviceId}</DeviceID>\r
<SumNum>2</SumNum>\r
<DeviceList Num="2">\r
<Item>\r
<DeviceID>${deviceId.slice(0, 10)}0132000001</DeviceID>\r
<Name>大门口</Name>\r
<Status>ON</Status>\r
</Item>\r
<Item>\r
<DeviceID>${deviceId.slice(0, 10)}0132000002</DeviceID>\r
<Name>后门</Name>\r
<Status>ON</Status>\r
</Item>\r
</DeviceList>\r
</Notify>\r
`;
    return `MESSAGE sip:3402000000@192.168.1.100:5060 SIP/2.0\r
Via: SIP/2.0/UDP 192.168.1.55:5060;rport;branch=z9hG4bK${callId.slice(0, 12)}\r
From: <sip:${deviceId}@3402000000>;tag=${callId.slice(0, 8)}\r
To: <sip:3402000000@3402000000>\r
Call-ID: ${callId}\r
CSeq: ${cseq} MESSAGE\r
Max-Forwards: 70\r
Content-Type: Application/MANSCDP+xml\r
Content-Length: ${body.length}\r
\r
${body}`;
}

// ============ 场景 1:注册成功 ============
const S1_CID = "dfa5372e3c3d2d76@192.168.1.55";
const S1_DID = "34020000001320000001";
const S1_LABEL = "海康 - 大门监控";
const session1: TraceSession = {
    callId: S1_CID, deviceId: S1_DID, deviceLabel: S1_LABEL,
    firstAt: iso(-720_000), lastAt: iso(-716_000), durationMs: 4_000,
    messageCount: 4, inboundCount: 2, outboundCount: 2,
    methodSequence: ["REGISTER", "401", "REGISTER", "200"],
    finalStatus: 200, hasAuthChallenge: true, anomaly: false, scenario: "register-ok"
};
const session1Messages: TraceMessage[] = [
    { eventId: "m-1-1", occurredAt: iso(-720_000), direction: "inbound", transport: "UDP", localAddr: "192.168.1.100:5060", remoteAddr: "192.168.1.55:5060", deviceId: S1_DID, method: "REGISTER", statusCode: 0, callId: S1_CID, cseq: 2863, cseqMethod: "REGISTER", malformed: false, payload: registerAuthPayload(2863, S1_DID, S1_CID, false) },
    { eventId: "m-1-2", occurredAt: iso(-719_800), direction: "outbound", transport: "UDP", localAddr: "192.168.1.100:5060", remoteAddr: "192.168.1.55:5060", deviceId: S1_DID, method: "", statusCode: 401, callId: S1_CID, cseq: 2863, cseqMethod: "REGISTER", malformed: false, payload: response401Payload(2863, S1_DID, S1_CID) },
    { eventId: "m-1-3", occurredAt: iso(-719_500), direction: "inbound", transport: "UDP", localAddr: "192.168.1.100:5060", remoteAddr: "192.168.1.55:5060", deviceId: S1_DID, method: "REGISTER", statusCode: 0, callId: S1_CID, cseq: 2864, cseqMethod: "REGISTER", malformed: false, payload: registerAuthPayload(2864, S1_DID, S1_CID, true) },
    { eventId: "m-1-4", occurredAt: iso(-716_000), direction: "outbound", transport: "UDP", localAddr: "192.168.1.100:5060", remoteAddr: "192.168.1.55:5060", deviceId: S1_DID, method: "", statusCode: 200, callId: S1_CID, cseq: 2864, cseqMethod: "REGISTER", malformed: false, payload: response200Payload(2864, "REGISTER", S1_DID, S1_CID) }
];

// ============ 场景 2:注册失败(401 循环) ============
const S2_CID = "fail-loop-a3c8d2@192.168.1.62";
const S2_DID = "34020000001320000005";
const S2_LABEL = "大华 - 车库入口";
const session2: TraceSession = {
    callId: S2_CID, deviceId: S2_DID, deviceLabel: S2_LABEL,
    firstAt: iso(-540_000), lastAt: iso(-510_000), durationMs: 30_000,
    messageCount: 12, inboundCount: 6, outboundCount: 6,
    methodSequence: ["REGISTER", "401", "REGISTER", "401", "REGISTER", "401", "..."],
    finalStatus: 401, hasAuthChallenge: true, anomaly: true, scenario: "register-fail"
};
const session2Messages: TraceMessage[] = Array.from({ length: 12 }, (_, i) => {
    const isRequest = i % 2 === 0;
    const cseq = 100 + Math.floor(i / 2);
    return {
        eventId: `m-2-${i + 1}`, occurredAt: iso(-540_000 + i * 2500),
        direction: isRequest ? "inbound" : "outbound", transport: "UDP",
        localAddr: "192.168.1.100:5060", remoteAddr: "192.168.1.62:5060",
        deviceId: S2_DID, method: isRequest ? "REGISTER" : "",
        statusCode: isRequest ? 0 : 401, callId: S2_CID, cseq, cseqMethod: "REGISTER", malformed: false,
        payload: isRequest ? registerAuthPayload(cseq, S2_DID, S2_CID, i > 0) : response401Payload(cseq, S2_DID, S2_CID)
    };
});

// ============ 场景 3:心跳流 ============
const S3_CID = "keepalive-b7d2e4-192.168.1.55";
const session3: TraceSession = {
    callId: S3_CID, deviceId: S1_DID, deviceLabel: S1_LABEL,
    firstAt: iso(-600_000), lastAt: iso(-60_000), durationMs: 540_000,
    messageCount: 20, inboundCount: 10, outboundCount: 10,
    methodSequence: ["MESSAGE", "200", "MESSAGE", "200", "..."],
    finalStatus: 200, hasAuthChallenge: false, anomaly: false, scenario: "keepalive"
};
const session3Messages: TraceMessage[] = Array.from({ length: 20 }, (_, i) => {
    const isRequest = i % 2 === 0;
    const cseq = 15200 + Math.floor(i / 2);
    return {
        eventId: `m-3-${i + 1}`, occurredAt: iso(-600_000 + i * 28_000),
        direction: isRequest ? "inbound" : "outbound", transport: "UDP",
        localAddr: "192.168.1.100:5060", remoteAddr: "192.168.1.55:5060",
        deviceId: S1_DID, method: isRequest ? "MESSAGE" : "",
        statusCode: isRequest ? 0 : 200, callId: S3_CID, cseq, cseqMethod: "MESSAGE", malformed: false,
        payload: keepaliveXmlPayload(cseq, S1_DID, S3_CID, isRequest)
    };
});

// ============ 场景 4:点播卡住 ============
const S4_CID = "invite-stuck-c4e6f8-192.168.1.62";
const session4: TraceSession = {
    callId: S4_CID, deviceId: S2_DID, deviceLabel: S2_LABEL,
    firstAt: iso(-180_000), lastAt: iso(-179_500), durationMs: 500,
    messageCount: 2, inboundCount: 1, outboundCount: 1,
    methodSequence: ["INVITE", "100"],
    finalStatus: 100, hasAuthChallenge: false, anomaly: true, scenario: "invite-pending"
};
const session4Messages: TraceMessage[] = [
    { eventId: "m-4-1", occurredAt: iso(-180_000), direction: "outbound", transport: "UDP", localAddr: "192.168.1.100:5060", remoteAddr: "192.168.1.62:5060", deviceId: S2_DID, method: "INVITE", statusCode: 0, callId: S4_CID, cseq: 5, cseqMethod: "INVITE", malformed: false, payload: inviteWithSdpPayload(5, S2_DID, S4_CID) },
    { eventId: "m-4-2", occurredAt: iso(-179_500), direction: "inbound", transport: "UDP", localAddr: "192.168.1.100:5060", remoteAddr: "192.168.1.62:5060", deviceId: S2_DID, method: "", statusCode: 100, callId: S4_CID, cseq: 5, cseqMethod: "INVITE", malformed: false, payload: trying100Payload(5, S2_DID, S4_CID) }
];

// ============ 场景 5:Catalog 订阅 ============
const S5_CID = "subscription-178445834033-catalog";
const S5_DID = "34020000001320000002";
const S5_LABEL = "宇视 - 停车场";
const session5: TraceSession = {
    callId: S5_CID, deviceId: S5_DID, deviceLabel: S5_LABEL,
    firstAt: iso(-360_000), lastAt: iso(-358_500), durationMs: 1_500,
    messageCount: 4, inboundCount: 2, outboundCount: 2,
    methodSequence: ["SUBSCRIBE", "200", "MESSAGE", "200"],
    finalStatus: 200, hasAuthChallenge: false, anomaly: false, scenario: "subscribe"
};
const session5Messages: TraceMessage[] = [
    { eventId: "m-5-1", occurredAt: iso(-360_000), direction: "outbound", transport: "UDP", localAddr: "192.168.1.100:5060", remoteAddr: "192.168.1.75:5060", deviceId: S5_DID, method: "SUBSCRIBE", statusCode: 0, callId: S5_CID, cseq: 30, cseqMethod: "SUBSCRIBE", malformed: false, payload: subscribeCatalogPayload(30, S5_DID, S5_CID) },
    { eventId: "m-5-2", occurredAt: iso(-359_800), direction: "inbound", transport: "UDP", localAddr: "192.168.1.100:5060", remoteAddr: "192.168.1.75:5060", deviceId: S5_DID, method: "", statusCode: 200, callId: S5_CID, cseq: 30, cseqMethod: "SUBSCRIBE", malformed: false, payload: response200Payload(30, "SUBSCRIBE", S5_DID, S5_CID) },
    { eventId: "m-5-3", occurredAt: iso(-359_000), direction: "inbound", transport: "UDP", localAddr: "192.168.1.100:5060", remoteAddr: "192.168.1.75:5060", deviceId: S5_DID, method: "MESSAGE", statusCode: 0, callId: S5_CID, cseq: 12, cseqMethod: "MESSAGE", malformed: false, payload: notifyCatalogPayload(12, S5_DID, S5_CID) },
    { eventId: "m-5-4", occurredAt: iso(-358_500), direction: "outbound", transport: "UDP", localAddr: "192.168.1.100:5060", remoteAddr: "192.168.1.75:5060", deviceId: S5_DID, method: "", statusCode: 200, callId: S5_CID, cseq: 12, cseqMethod: "MESSAGE", malformed: false, payload: response200Payload(12, "MESSAGE", S5_DID, S5_CID) }
];

export const MOCK_SESSIONS: TraceSession[] = [session4, session1, session2, session5, session3];

export const MOCK_MESSAGES: Record<string, TraceMessage[]> = {
    [S1_CID]: session1Messages,
    [S2_CID]: session2Messages,
    [S3_CID]: session3Messages,
    [S4_CID]: session4Messages,
    [S5_CID]: session5Messages
};

export const MOCK_HEALTH = {
    state: "ready" as const,
    lastSuccessAt: iso(-5_000)
};

export const MOCK_DEVICES = [
    { id: 1, deviceId: S1_DID, label: S1_LABEL },
    { id: 2, deviceId: S5_DID, label: S5_LABEL },
    { id: 3, deviceId: S2_DID, label: S2_LABEL }
];
