#!/usr/bin/env python3
"""对接示例：连平台、查设备列表、再查一个通道。

跑之前先确认三件事，否则一定 401 —— 这三个坑占了接入问题的九成：

1. base_url 用**后端端口**（绿色包是 51002，纯 HTTP）。
   不是浏览器访问的 51000：nginx 只转发 /api/、/gb28181/、/public/，
   /openapi/v1 不在其中，打到 51000 会拿到前端 HTML（HTTP 200），
   看起来像"接口挂了"。
2. audience 必须等于平台 config.yml 里的 openapi.audience。
   它参与签名，填错不会提示，只会稳定 401。
3. 本机时钟与平台差不能超过 300 秒。

    export UVP_BASE_URL=http://192.168.1.220:51002
    export UVP_ACCESS_KEY=uvp_xxxxxxxx
    export UVP_SECRET_KEY=xxxxxxxx
    export UVP_AUDIENCE=uvp-xxxxxxxx
    python3 sdk/python/demo_devices.py
"""

from __future__ import annotations

import json
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from uvp_openapi import UvpApiError, UvpClient  # noqa: E402


def required(name: str) -> str:
    value = os.environ.get(name, "").strip()
    if not value:
        sys.exit("缺少环境变量 %s" % name)
    return value


def main() -> int:
    client = UvpClient(
        base_url=required("UVP_BASE_URL"),
        access_key=required("UVP_ACCESS_KEY"),
        secret_key=required("UVP_SECRET_KEY"),
        audience=required("UVP_AUDIENCE"),
    )

    try:
        page = client.get("/openapi/v1/devices",
                          query={"page": 1, "pageSize": 20})
        print(json.dumps(page, ensure_ascii=False, indent=2)[:2000])

        devices = (page.get("data") or {}).get("items") or page.get("data") or []
        if not devices:
            print("\n平台里还没有设备，先接入一台再来。")
            return 0

        device_id = devices[0].get("deviceId") or devices[0].get("id")
        channels = client.get("/openapi/v1/devices/%s/channels" % device_id)
        print("\n设备 %s 的通道：" % device_id)
        print(json.dumps(channels, ensure_ascii=False, indent=2)[:1200])
        return 0
    except UvpApiError as error:
        # requestId 是唯一能在平台日志里反查的线索，务必保留。
        print("请求失败: status=%s code=%s requestId=%s message=%s"
              % (error.status, error.code, error.request_id, error.message),
              file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
