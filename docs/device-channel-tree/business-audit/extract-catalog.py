"""Extract Catalog XML from a local Ethernet/IPv4 pcap, including UDP fragments.

Usage: python3 extract-catalog.py input.pcap output-directory
Writes XML bodies only; SIP authentication headers are never exported.
"""
import struct
import sys
from pathlib import Path
import xml.etree.ElementTree as ET


def extract(path):
    data = Path(path).read_bytes()
    endian = "<" if data[:4] in (b"\xd4\xc3\xb2\xa1", b"\x4d\x3c\xb2\xa1") else ">"
    link = struct.unpack_from(endian + "I", data, 20)[0]
    if link != 1:
        raise ValueError("expected Ethernet pcap")
    offset = 24
    pending = {}
    while offset + 16 <= len(data):
        size = struct.unpack_from(endian + "I", data, offset + 8)[0]
        frame = data[offset+16:offset+16+size]
        offset += 16 + size
        if len(frame) < 34 or frame[12:14] != b"\x08\x00":
            continue
        ip = frame[14:]
        header_length = (ip[0] & 15) * 4
        if ip[9] != 17:
            continue
        length, ident, flags = struct.unpack_from("!HHH", ip, 2)
        key = (ip[12:20], ident)
        fragments, total = pending.setdefault(key, ({}, None))
        fragment_offset = (flags & 8191) * 8
        payload = ip[header_length:length]
        fragments[fragment_offset] = payload
        if not flags & 8192:
            total = fragment_offset + len(payload)
            pending[key] = (fragments, total)
        if total is None:
            continue
        parts, cursor = [], 0
        for start in sorted(fragments):
            if start != cursor:
                break
            parts.append(fragments[start])
            cursor += len(fragments[start])
        if cursor != total:
            continue
        del pending[key]
        udp = b"".join(parts)
        sip = udp[8:]
        separator = sip.find(b"\r\n\r\n")
        if separator < 0:
            continue
        body = sip[separator+4:]
        if b"<CmdType>Catalog</CmdType>" in body:
            yield body


if __name__ == "__main__":
    output = Path(sys.argv[2])
    output.mkdir(parents=True, exist_ok=True)
    count = 0
    for count, body in enumerate(extract(sys.argv[1]), 1):
        # Decode only to print non-sensitive IDs/counts; preserve original bytes.
        root = ET.fromstring(body.decode("gb18030"))
        filename = output / f"catalog-{count}-sn-{root.findtext('SN')}.xml"
        filename.write_bytes(body)
        print(filename.name, "items=", len(root.findall(".//DeviceList/Item")))
    print("catalog_messages=", count)
