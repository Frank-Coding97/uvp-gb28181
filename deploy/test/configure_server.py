#!/usr/bin/env python3
import base64
import json
import os
import re
import secrets
import subprocess
from pathlib import Path

BASE = Path("/opt/uvp-gb28181")
CONFIG_TEMPLATE = BASE / "config" / "config.example.yml"
CONFIG_PATH = BASE / "config" / "config.yml"
TRACE_BACKEND_ENV_PATH = BASE / "config" / "sip-trace-backend.env"
DATABASE_NAME = "uvp_gb28181"
DATABASE_USER = "uvp_gb28181"
TRUSTED_PROXIES = ["127.0.0.1", "::1", "172.18.0.3"]


def container_env(name: str) -> dict[str, str]:
    raw = subprocess.check_output(["docker", "inspect", name], text=True)
    env_list = json.loads(raw)[0]["Config"].get("Env", [])
    return dict(item.split("=", 1) for item in env_list if "=" in item)


def zlm_value(section_name: str, key_name: str) -> str:
    section = ""
    config_path = Path("/opt/wvp_docker_compose/zlmediakit/config.ini")
    for raw_line in config_path.read_text(encoding="utf-8-sig").splitlines():
        line = raw_line.strip()
        if line.startswith("[") and line.endswith("]"):
            section = line[1:-1]
            continue
        if section == section_name and "=" in line:
            key, value = line.split("=", 1)
            if key.strip() == key_name:
                return value.strip()
    raise RuntimeError(f"Missing ZLMediaKit setting: [{section_name}] {key_name}")


def run_mysql(sql: str, *, database: str | None = None, capture: bool = False) -> str:
    suffix = f" --default-character-set=utf8mb4 {database}" if database else ""
    command = f'exec mysql -uroot -p"$MYSQL_ROOT_PASSWORD" -N -B{suffix}'
    result = subprocess.run(
        ["docker", "exec", "-i", "wvp-mysql", "sh", "-lc", command],
        input=sql,
        text=True,
        check=True,
        stdout=subprocess.PIPE if capture else subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    return result.stdout.strip() if capture else ""


def yaml_scalar(value: object) -> str:
    if isinstance(value, bool):
        return "true" if value else "false"
    if isinstance(value, int):
        return str(value)
    if isinstance(value, list):
        return json.dumps(value, ensure_ascii=False)
    return json.dumps(str(value), ensure_ascii=False)


def render_config(template: str, replacements: dict[tuple[str, ...], object]) -> str:
    rendered: list[str] = []
    stack: list[tuple[int, str]] = []
    key_pattern = re.compile(r"^(\s*)([A-Za-z0-9_]+)\s*:(.*)$")

    for line in template.splitlines():
        match = key_pattern.match(line)
        if not match:
            rendered.append(line)
            continue

        indent_text, key, remainder = match.groups()
        indent = len(indent_text)
        while stack and stack[-1][0] >= indent:
            stack.pop()
        path = tuple(item[1] for item in stack) + (key,)

        if path in replacements:
            rendered.append(f"{indent_text}{key}: {yaml_scalar(replacements[path])}")
            continue

        rendered.append(line)
        stripped_remainder = remainder.strip()
        if not stripped_remainder or stripped_remainder.startswith("#"):
            stack.append((indent, key))

    missing = [".".join(path) for path in replacements if not any(
        re.match(rf"^\s*{re.escape(path[-1])}\s*:", line) for line in rendered
    )]
    if missing:
        raise RuntimeError(f"Config keys not found: {', '.join(missing)}")
    return "\n".join(rendered) + "\n"


def read_env(path: Path) -> dict[str, str]:
    if not path.exists():
        return {}
    values: dict[str, str] = {}
    for raw_line in path.read_text(encoding="utf-8").splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        values[key.strip()] = value.strip()
    return values


def write_private_env(path: Path, values: dict[str, str]) -> None:
    for key, value in values.items():
        if not key or "\n" in value or "\r" in value:
            raise RuntimeError(f"Invalid environment value for {key}")
    temporary_path = path.with_suffix(path.suffix + ".tmp")
    temporary_path.write_text(
        "".join(f"{key}={value}\n" for key, value in values.items()),
        encoding="utf-8",
    )
    os.chmod(temporary_path, 0o600)
    temporary_path.replace(path)
    os.chmod(path, 0o600)


def ensure_trace_env() -> None:
    backend = read_env(TRACE_BACKEND_ENV_PATH)
    encryption_key = backend.get("UVP_SIP_TRACE_ENCRYPTION_KEY") or base64.b64encode(
        secrets.token_bytes(32)
    ).decode("ascii")

    write_private_env(TRACE_BACKEND_ENV_PATH, {
        "UVP_SIP_TRACE_ENCRYPTION_KEY": encryption_key,
    })


def main() -> None:
    BASE.joinpath("data", "logs").mkdir(parents=True, exist_ok=True)
    BASE.joinpath("data", "uploads").mkdir(parents=True, exist_ok=True)
    ensure_trace_env()

    mysql_env = container_env("wvp-mysql")
    redis_env = container_env("wvp-redis")
    if not mysql_env.get("MYSQL_ROOT_PASSWORD"):
        raise RuntimeError("wvp-mysql does not expose MYSQL_ROOT_PASSWORD")

    database_password = secrets.token_urlsafe(32)
    jwt_secret = secrets.token_urlsafe(48)
    zlm_secret = zlm_value("api", "secret")
    zlm_rtp_port = int(zlm_value("rtp_proxy", "port"))

    escaped_password = database_password.replace("'", "''")
    run_mysql(
        f"""
        CREATE DATABASE IF NOT EXISTS `{DATABASE_NAME}`
          CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
        CREATE USER IF NOT EXISTS '{DATABASE_USER}'@'%' IDENTIFIED BY '{escaped_password}';
        ALTER USER '{DATABASE_USER}'@'%' IDENTIFIED BY '{escaped_password}';
        GRANT ALL PRIVILEGES ON `{DATABASE_NAME}`.* TO '{DATABASE_USER}'@'%';
        FLUSH PRIVILEGES;
        """
    )

    table_count = int(run_mysql(
        f"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='{DATABASE_NAME}';",
        capture=True,
    ) or "0")
    if table_count == 0:
        schema_path = BASE / "backend" / "resource" / "database" / "uvp-gb28181.sql"
        schema = schema_path.read_text(encoding="utf-8-sig")
        run_mysql(schema, database=DATABASE_NAME)
        initialized = True
    else:
        initialized = False

    replacements: dict[tuple[str, ...], object] = {
        ("server", "appdebug"): True,
        ("server", "cachetype"): "redis",
        ("server", "notcheckuser"): [1],
        ("system", "systemname"): "UVP GB28181",
        ("httpserver", "port"): ":18978",
        ("httpserver", "trustedproxies"): TRUSTED_PROXIES,
        ("token", "jwttokensignkey"): jwt_secret,
        ("redis", "host"): "wvp-redis",
        ("redis", "port"): 6379,
        ("redis", "password"): redis_env.get("REDIS_PASSWORD", ""),
        ("logs", "ginlogname"): "./resource/logs/gin.log",
        ("logs", "zaplogname"): "./resource/logs/uvp-gb28181.log",
        ("logs", "level"): "info",
        ("gormv2", "usedbtype"): "mysql",
        ("gormv2", "mysql", "write", "host"): "wvp-mysql",
        ("gormv2", "mysql", "write", "database"): DATABASE_NAME,
        ("gormv2", "mysql", "write", "port"): 3306,
        ("gormv2", "mysql", "write", "user"): DATABASE_USER,
        ("gormv2", "mysql", "write", "pass"): database_password,
        ("gormv2", "mysql", "read", "host"): "wvp-mysql",
        ("gormv2", "mysql", "read", "database"): DATABASE_NAME,
        ("gormv2", "mysql", "read", "port"): 3306,
        ("gormv2", "mysql", "read", "user"): DATABASE_USER,
        ("gormv2", "mysql", "read", "pass"): database_password,
        ("upload", "local_path"): "./resource/public/uploads",
        ("scheduler", "log", "dir"): "./resource/logs/scheduler",
        ("gb28181", "enabled"): True,
        ("gb28181", "trace", "enabled"): True,
        ("gb28181", "zlm", "host"): "wvp-zlmediakit",
        ("gb28181", "zlm", "httpport"): 80,
        ("gb28181", "zlm", "secret"): zlm_secret,
        ("gb28181", "zlm", "rtpport"): zlm_rtp_port,
        ("gb28181", "media", "hookhost"): "wvp-backend",
        ("gb28181", "media", "hookport"): 18978,
    }

    template = CONFIG_TEMPLATE.read_text(encoding="utf-8")
    config = render_config(template, replacements)
    temporary_path = CONFIG_PATH.with_suffix(".yml.tmp")
    temporary_path.write_text(config, encoding="utf-8")
    os.chmod(temporary_path, 0o600)
    temporary_path.replace(CONFIG_PATH)
    os.chmod(CONFIG_PATH, 0o600)

    print(f"Database: {DATABASE_NAME} ({'initialized' if initialized else 'preserved'})")
    print(f"Config: {CONFIG_PATH} mode=600")
    print(f"SIP Trace encryption env: {TRACE_BACKEND_ENV_PATH} mode=600")
    print(f"ZLMediaKit: wvp-zlmediakit:80 RTP={zlm_rtp_port}")


if __name__ == "__main__":
    main()
