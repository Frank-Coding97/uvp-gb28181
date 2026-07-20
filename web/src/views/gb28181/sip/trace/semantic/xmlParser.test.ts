import { describe, it, expect } from "vitest";
import { parseGbXml, type CatalogPayload, type AlarmPayload } from "./xmlParser";

const catalogXml = `<?xml version="1.0" encoding="UTF-8"?>
<Response>
  <CmdType>Catalog</CmdType>
  <SN>10</SN>
  <DeviceID>34020000001320000001</DeviceID>
  <SumNum>2</SumNum>
  <DeviceList Num="2">
    <Item>
      <DeviceID>34020000001310000001</DeviceID>
      <Name>前门</Name>
      <Manufacturer>Hikvision</Manufacturer>
      <Model>DS-2CD</Model>
      <Status>ON</Status>
      <Parental>0</Parental>
      <ParentID>34020000001320000001</ParentID>
      <CivilCode>340200</CivilCode>
    </Item>
    <Item>
      <DeviceID>34020000001310000002</DeviceID>
      <Name>后门</Name>
      <Manufacturer>Dahua</Manufacturer>
      <Model>IPC-HFW</Model>
      <Status>OFF</Status>
      <Parental>0</Parental>
      <ParentID>34020000001320000001</ParentID>
      <CivilCode>340200</CivilCode>
    </Item>
  </DeviceList>
</Response>`;

const deviceStatusXml = `<?xml version="1.0"?>
<Response>
  <CmdType>DeviceStatus</CmdType>
  <SN>15</SN>
  <DeviceID>34020000001320000001</DeviceID>
  <Online>ONLINE</Online>
  <Status>OK</Status>
  <Result>OK</Result>
  <Encode>ON</Encode>
  <Record>OFF</Record>
  <DeviceTime>2026-07-20T15:00:00</DeviceTime>
</Response>`;

const alarmXml = `<?xml version="1.0"?>
<Notify>
  <CmdType>Alarm</CmdType>
  <SN>99</SN>
  <DeviceID>34020000001320000001</DeviceID>
  <AlarmPriority>1</AlarmPriority>
  <AlarmMethod>2</AlarmMethod>
  <AlarmTime>2026-07-20T15:12:34</AlarmTime>
  <AlarmDescription>玻璃破碎</AlarmDescription>
</Notify>`;

const keepaliveXml = `<?xml version="1.0"?>
<Notify>
  <CmdType>Keepalive</CmdType>
  <SN>1</SN>
  <DeviceID>34020000001320000001</DeviceID>
  <Status>OK</Status>
</Notify>`;

const deviceInfoXml = `<?xml version="1.0"?>
<Response>
  <CmdType>DeviceInfo</CmdType>
  <SN>7</SN>
  <DeviceID>34020000001320000001</DeviceID>
  <Manufacturer>Hikvision</Manufacturer>
  <Model>DS-9600</Model>
  <Firmware>V4.30</Firmware>
  <Channel>16</Channel>
</Response>`;

describe("parseGbXml — 5 CmdType 正常样本", () => {
    it("Catalog", () => {
        const r = parseGbXml(catalogXml);
        expect(r.ok).toBe(true);
        expect(r.cmdType).toBe("Catalog");
        expect(r.sn).toBe("10");
        expect(r.deviceId).toBe("34020000001320000001");
        const payload = r.payload as CatalogPayload;
        expect(payload.kind).toBe("Catalog");
        expect(payload.sumNum).toBe(2);
        expect(payload.deviceList).toHaveLength(2);
        expect(payload.deviceList[0]).toMatchObject({
            deviceId: "34020000001310000001",
            name: "前门",
            manufacturer: "Hikvision",
            status: "ON",
            civilCode: "340200"
        });
    });

    it("DeviceStatus", () => {
        const r = parseGbXml(deviceStatusXml);
        expect(r.ok).toBe(true);
        expect(r.cmdType).toBe("DeviceStatus");
        if (r.payload.kind !== "DeviceStatus") throw new Error("wrong kind");
        expect(r.payload.online).toBe(true);
        expect(r.payload.status).toBe("OK");
    });

    it("Alarm", () => {
        const r = parseGbXml(alarmXml);
        expect(r.ok).toBe(true);
        expect(r.cmdType).toBe("Alarm");
        const p = r.payload as AlarmPayload;
        expect(p.alarmPriority).toBe("1");
        expect(p.alarmDescription).toBe("玻璃破碎");
    });

    it("Keepalive", () => {
        const r = parseGbXml(keepaliveXml);
        expect(r.ok).toBe(true);
        expect(r.cmdType).toBe("Keepalive");
        if (r.payload.kind !== "Keepalive") throw new Error("wrong kind");
        expect(r.payload.status).toBe("OK");
    });

    it("DeviceInfo", () => {
        const r = parseGbXml(deviceInfoXml);
        expect(r.ok).toBe(true);
        expect(r.cmdType).toBe("DeviceInfo");
        if (r.payload.kind !== "DeviceInfo") throw new Error("wrong kind");
        expect(r.payload.firmware).toBe("V4.30");
    });
});

describe("parseGbXml — 畸形/降级样本", () => {
    it("empty string", () => {
        const r = parseGbXml("");
        expect(r.ok).toBe(false);
        expect(r.error).toBe("xml-parse-failed");
        expect(r.cmdType).toBe("Unknown");
    });

    it("null", () => {
        const r = parseGbXml(null);
        expect(r.ok).toBe(false);
        expect(r.error).toBe("xml-parse-failed");
    });

    it("non-xml text", () => {
        const r = parseGbXml("hello world not xml at all");
        expect(r.ok).toBe(false);
        expect(r.error).toBe("xml-parse-failed");
    });

    it("unknown CmdType falls back to Raw payload", () => {
        const xml = `<?xml version="1.0"?>
<Query>
  <CmdType>Broadcast</CmdType>
  <SN>42</SN>
  <DeviceID>D001</DeviceID>
  <Custom>hi</Custom>
</Query>`;
        const r = parseGbXml(xml);
        expect(r.ok).toBe(true);
        expect(r.cmdType).toBe("Unknown");
        expect(r.sn).toBe("42");
        if (r.payload.kind !== "Raw") throw new Error("expected Raw payload");
        expect(r.payload.body.Custom).toBe("hi");
    });
});
