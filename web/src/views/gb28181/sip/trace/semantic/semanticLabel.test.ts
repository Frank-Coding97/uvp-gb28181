import { describe, it, expect } from "vitest";
import { classifyMessage } from "./semanticLabel";

describe("semanticLabel — method 快捷路径", () => {
    it("REGISTER", () => {
        const r = classifyMessage({ method: "REGISTER" });
        expect(r.category).toBe("register");
        expect(r.label).toBe("设备注册");
    });

    it("BYE", () => {
        const r = classifyMessage({ method: "BYE" });
        expect(r.category).toBe("bye");
    });

    it("SUBSCRIBE", () => {
        const r = classifyMessage({ method: "SUBSCRIBE" });
        expect(r.category).toBe("subscribe");
    });
});

describe("semanticLabel — MESSAGE/NOTIFY + XML CmdType", () => {
    it("MESSAGE Keepalive", () => {
        const body = `<?xml version="1.0"?>
<Notify>
  <CmdType>Keepalive</CmdType>
  <SN>1</SN>
  <DeviceID>D001</DeviceID>
  <Status>OK</Status>
</Notify>`;
        const r = classifyMessage({ method: "MESSAGE", body });
        expect(r.category).toBe("keepalive");
        expect(r.cmdType).toBe("Keepalive");
    });

    it("MESSAGE Catalog", () => {
        const body = `<?xml version="1.0"?><Response><CmdType>Catalog</CmdType><SN>10</SN></Response>`;
        const r = classifyMessage({ method: "MESSAGE", body });
        expect(r.category).toBe("catalog");
    });

    it("MESSAGE DeviceInfo", () => {
        const body = `<Response><CmdType>DeviceInfo</CmdType><SN>7</SN></Response>`;
        const r = classifyMessage({ method: "MESSAGE", body });
        expect(r.category).toBe("device-info");
    });

    it("MESSAGE Alarm", () => {
        const body = `<Notify><CmdType>Alarm</CmdType><SN>99</SN></Notify>`;
        const r = classifyMessage({ method: "MESSAGE", body });
        expect(r.category).toBe("alarm");
    });

    it("MESSAGE DeviceControl → ptz", () => {
        const body = `<Control><CmdType>DeviceControl</CmdType><SN>5</SN></Control>`;
        const r = classifyMessage({ method: "MESSAGE", body });
        expect(r.category).toBe("ptz");
    });

    it("MESSAGE RecordInfo", () => {
        const body = `<Response><CmdType>RecordInfo</CmdType><SN>8</SN></Response>`;
        const r = classifyMessage({ method: "MESSAGE", body });
        expect(r.category).toBe("record-info");
    });

    it("NOTIFY with unknown CmdType → notify fallback", () => {
        const body = `<Notify><CmdType>Broadcast</CmdType><SN>42</SN></Notify>`;
        const r = classifyMessage({ method: "NOTIFY", body });
        expect(r.category).toBe("notify");
    });
});

describe("semanticLabel — INVITE/ACK + SDP s=", () => {
    it("INVITE s=Play → invite-play", () => {
        const body = `v=0\r
o=- 0 0 IN IP4 127.0.0.1\r
s=Play\r
c=IN IP4 0.0.0.0\r
t=0 0\r
m=video 0 RTP/AVP 96\r
`;
        const r = classifyMessage({ method: "INVITE", body });
        expect(r.category).toBe("invite-play");
        expect(r.sessionKind).toBe("Play");
    });

    it("INVITE s=Playback → invite-playback", () => {
        const body = `v=0\r\ns=Playback\r\nm=video 0 RTP/AVP 96\r\n`;
        const r = classifyMessage({ method: "INVITE", body });
        expect(r.category).toBe("invite-playback");
        expect(r.sessionKind).toBe("Playback");
    });

    it("INVITE s=Download → invite-download", () => {
        const body = `v=0\r\ns=Download\r\nm=video 0 RTP/AVP 96\r\n`;
        const r = classifyMessage({ method: "INVITE", body });
        expect(r.category).toBe("invite-download");
        expect(r.sessionKind).toBe("Download");
    });

    it("INVITE 无 s= 默认 Play 兜底", () => {
        const body = `v=0\r\nm=video 0 RTP/AVP 96\r\n`;
        const r = classifyMessage({ method: "INVITE", body });
        expect(r.category).toBe("invite-play");
        expect(r.sessionKind).toBeUndefined();
    });

    it("ACK s=Playback", () => {
        const body = `v=0\ns=Playback\nm=video 0 RTP/AVP 96`;
        const r = classifyMessage({ method: "ACK", body });
        expect(r.category).toBe("invite-playback");
    });
});

describe("semanticLabel — fallback other", () => {
    it("OPTIONS → other", () => {
        const r = classifyMessage({ method: "OPTIONS" });
        expect(r.category).toBe("other");
        expect(r.label).toBe("OPTIONS");
    });

    it("no method → other", () => {
        const r = classifyMessage({});
        expect(r.category).toBe("other");
    });
});
