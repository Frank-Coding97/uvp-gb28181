# 签名向量的来源

`signature-v1.json` 不是手写的，是从主仓同步过来的副本。
它被 Java / Python / Go 三份实现共用，任何一份对不上向量都算回归。

| 项 | 值 |
|---|---|
| 来源 | `server/app/openapi/auth/testdata/signature-v1.json` |
| 同步时主仓 commit | `11c040bc` |
| 同步时间 | 2026-11-09 18:21:49 |
| SHA-256 | `6ba7f337166189df97a4a2ed8188bc6b3f44380ebbb93c453e4d7546d5a1fc05` |

同步命令：`python3 sdk/scripts/sync-fixture.py`

契约（`api/openapi-v1.yaml` 的 `x-uvp-signature-contract`）变了就要重新同步，
然后跑 `python3 sdk/scripts/verify-parity.py --with-java` 确认三份实现都还一致。
