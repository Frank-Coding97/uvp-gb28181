"""Check a 2022 business fixture against Appendix J relationships.

This checks only this task's business scenario, not full GB28181 conformance.
Usage: python3 check-catalog.py captured-catalog.xml
"""
import sys
import re
from pathlib import Path
import xml.etree.ElementTree as ET


def type_code(code):
    return code[10:13] if len(code) == 20 and code.isdecimal() else ""


def check(path):
    data = Path(path).read_bytes()
    encoding = re.search(br"encoding=[\"']([^\"']+)", data[:160])
    root = ET.fromstring(data.decode(encoding.group(1).decode() if encoding else "utf-8"))
    items = root.findall(".//DeviceList/Item")
    by_id = {item.findtext("DeviceID", ""): item for item in items}
    errors = []
    if len(items) != len(by_id):
        errors.append("duplicate directory IDs in scenario")
    groups = []
    orgs = []
    channels = []
    for code, item in by_id.items():
        kind = type_code(code)
        parents = item.findtext("ParentID", "").split("/")
        group = item.findtext("BusinessGroupID", "")
        if kind == "215":
            groups.append(code)
            if len(parents) != 1 or type_code(parents[0]) != "200":
                errors.append(f"{code}: 215 ParentID must identify its defining system (200)")
        if kind == "216":
            orgs.append(code)
            if type_code(group) != "215" or group not in by_id:
                errors.append(f"{code}: missing or unknown BusinessGroupID (215)")
            if parents != [""] and (len(parents) != 1 or type_code(parents[0]) != "216"):
                errors.append(f"{code}: ParentID, when present, must identify a parent virtual organization")
            if parents != [""] and parents[0] not in by_id:
                errors.append(f"{code}: unknown parent virtual organization")
        if kind == "132":
            channels.append(code)
            virtual = [parent for parent in parents if type_code(parent) == "216"]
            if not virtual:
                errors.append(f"{code}: scenario channel has no virtual organization in ParentID")
            physical = [parent for parent in parents if parent and type_code(parent) != "216"]
            if not physical:
                errors.append(f"{code}: ParentID lacks physical parent or system")
            if any(parent not in by_id for parent in virtual):
                errors.append(f"{code}: unknown virtual organization")
    if not groups or not orgs or not channels:
        errors.append("scenario must contain 215, 216 and video resources")
    for code in orgs:
        seen = set()
        current = code
        while current in by_id and type_code(current) == "216":
            if current in seen:
                errors.append(f"{code}: virtual organization parent cycle")
                break
            seen.add(current)
            parent = by_id[current].findtext("ParentID", "")
            if parent in by_id and by_id[parent].findtext("BusinessGroupID", "") != by_id[current].findtext("BusinessGroupID", ""):
                errors.append(f"{code}: nested organization crosses business groups")
            current = parent
    return errors, (len(groups), len(orgs), len(channels))


if __name__ == "__main__":
    errors, counts = check(sys.argv[1])
    print(f"groups={counts[0]} organizations={counts[1]} video_resources={counts[2]}")
    for error in errors:
        print(error)
    sys.exit(bool(errors))
