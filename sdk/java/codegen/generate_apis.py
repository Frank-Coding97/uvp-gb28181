#!/usr/bin/env python3
"""Generate the typed ``*Api`` classes from the OpenAPI contract.

Why generate instead of hand-writing: the contract has 69 operations and will keep growing. The
generated files are committed, so a consumer of the SDK never runs this script; it exists so that
adding an operation to ``api/openapi-v1.yaml`` is a one-command change instead of a manual edit that
drifts.

What this script deliberately does NOT generate: the five signature headers. Those are injected by
``ApiInvoker``, so no public method ever takes a signature argument.

Usage:
    python3 sdk/java/codegen/generate_apis.py
"""

from __future__ import annotations

import json
import os
import re
import sys
import textwrap

import yaml

REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", ".."))
SPEC = os.path.join(REPO_ROOT, "api", "openapi-v1.yaml")
OUT_DIR = os.path.join(
    REPO_ROOT, "sdk", "java", "uvp-openapi-sdk", "src", "main", "java", "com", "uvp", "openapi", "api"
)
MODEL_DIR = os.path.join(
    REPO_ROOT, "sdk", "java", "uvp-openapi-sdk", "src", "main", "java", "com", "uvp", "openapi", "model"
)

SIGNATURE_PARAMS = {"SignVersion", "AccessKey", "Timestamp", "Nonce", "Signature", "IdempotencyKey"}

# Operations that wait on the device even though they are reads.
SLOW_READS = {"deviceRecordQuery"}

JAVA_KEYWORDS = {
    "abstract", "assert", "boolean", "break", "byte", "case", "catch", "char", "class", "const",
    "continue", "default", "do", "double", "else", "enum", "extends", "final", "finally", "float",
    "for", "goto", "if", "implements", "import", "instanceof", "int", "interface", "long", "native",
    "new", "package", "private", "protected", "public", "return", "short", "static", "strictfp",
    "super", "switch", "synchronized", "this", "throw", "throws", "transient", "try", "void",
    "volatile", "while", "true", "false", "null", "var", "record", "yield",
}

# Domain split. Order matters: the first matching rule wins, so put the narrow paths first.
DOMAINS = [
    ("PtzApi", ("ptz",)),
    ("LiveApi", ("/live",)),
    ("PlaybackApi", ("playback", "download", "/recordings")),
    ("TalkApi", ("talk", "broadcast")),
    ("AlarmApi", ("alarms", "status-events", "subscriptions", "events", "positions")),
    ("ConfigApi", ("configs", "video-params", "config-operations", "firmware-upgrades")),
    ("ControlApi", ("recording", "alarm-guard", "maintenance", "storage-cards")),
]

CLASS_DOC = {
    "DeviceApi": "Devices, channels, live status and storage cards.",
    "LiveApi": "Live preview. Returns playable URLs; the media plane is not handled by this SDK.",
    "PlaybackApi": "Record search, playback sessions and download sessions.",
    "PtzApi": "PTZ direction control, presets, cruise tracks and precise positioning.",
    "TalkApi": "Voice talk and broadcast. SDP is passed through; media stays outside the SDK.",
    "AlarmApi": "Alarms, status events, subscriptions and reported positions.",
    "ConfigApi": "Device configuration, video parameters and firmware upgrade tasks.",
    "ControlApi": "Recording control, alarm guard, reboot, storage card format, task polling.",
}


def ref_name(schema):
    if not schema:
        return None
    ref = schema.get("$ref")
    if not ref:
        return None
    return ref.rsplit("/", 1)[-1]


def java_type(schema):
    if not schema:
        return "Object"
    name = ref_name(schema)
    if name:
        return name
    kind = schema.get("type")
    if kind == "string":
        return "String"
    if kind == "integer":
        return "Long" if schema.get("format") == "int64" else "Integer"
    if kind == "number":
        return "java.math.BigDecimal"
    if kind == "boolean":
        return "Boolean"
    if kind == "array":
        item = java_type(schema.get("items") or {})
        return "java.util.List<%s>" % item
    return "Object"


def safe(name):
    return name + "Value" if name in JAVA_KEYWORDS else name


def load_operations():
    with open(SPEC, encoding="utf-8") as handle:
        doc = yaml.safe_load(handle)
    parameters = doc.get("components", {}).get("parameters", {})
    models = model_names()
    ops = []
    for path, item in doc["paths"].items():
        for method in ("get", "post", "put", "delete", "patch"):
            op = item.get(method)
            if not op:
                continue
            params = []
            for raw in op.get("parameters", []):
                ref = raw["$ref"].rsplit("/", 1)[-1] if "$ref" in raw else None
                if ref in SIGNATURE_PARAMS:
                    continue
                param = parameters[ref] if ref else raw
                if param.get("in") == "header" and param["name"] == "Idempotency-Key":
                    # Injected by ApiInvoker like the other transport headers.
                    continue
                params.append({
                    "name": param["name"],
                    "in": param.get("in"),
                    "required": bool(param.get("required")),
                    "java_type": java_type(param.get("schema")),
                })
            request_body = op.get("requestBody") or {}
            body_schema = ((request_body.get("content") or {}).get("application/json") or {}).get("schema")
            body_type = java_type(body_schema) if body_schema else None
            body_required = bool(request_body.get("required"))
            response = ((op.get("responses", {}).get("200", {}).get("content") or {})
                        .get("application/json") or {}).get("schema")
            response_type = ref_name(response) or "Object"
            if response_type == "Object" and response:
                # Inline 200 body: openapi-generator emits <Operation>200Response for it.
                guess = op["operationId"][:1].upper() + op["operationId"][1:] + "200Response"
                if guess in models:
                    response_type = guess
            ops.append({
                "id": op["operationId"],
                "method": method.upper(),
                "path": path,
                "summary": (op.get("summary") or "").strip(),
                "scope": op.get("x-uvp-scope") or "",
                "idempotency": op.get("x-uvp-idempotency-mode") or "none",
                "params": params,
                "body_type": body_type,
                "body_schema": body_schema,
                "body_required": body_required,
                "response_type": response_type,
            })
    resolve_inline_bodies(ops)
    return ops


def model_names():
    try:
        return {name[:-5] for name in os.listdir(MODEL_DIR) if name.endswith(".java")}
    except OSError:
        return set()


def shape_key(schema):
    """Identity of an inline body, ignoring prose so identical shapes collapse to one type."""
    trimmed = {key: value for key, value in schema.items() if key not in ("description", "title")}
    return json.dumps(trimmed, sort_keys=True, ensure_ascii=False)


def resolve_inline_bodies(ops):
    """Gives inline request bodies their generated model class instead of ``Object``.

    The contract writes most bodies inline (``type: object`` plus properties) rather than through
    ``components.schemas``. openapi-generator names those classes ``<Operation>Request`` but my first
    pass mapped every inline body to ``Object``, which forces callers to hand-build maps and loses
    every compile-time check. Types are recoverable, so recover them.
    """
    models = model_names()
    by_shape = {}
    for op in ops:
        schema = op.get("body_schema")
        if not schema or "$ref" in schema or schema.get("type") != "object":
            continue
        if not (schema.get("properties") or {}):
            # Empty body: the contract still requires JSON, so send an explicit empty object.
            op["body_type"] = "EmptyObject"
            continue
        guess = op["id"][:1].upper() + op["id"][1:] + "Request"
        if guess in models:
            op["body_type"] = guess
            by_shape[shape_key(schema)] = guess
    for op in ops:
        schema = op.get("body_schema")
        if not schema or "$ref" in schema or op.get("body_type") != "Object":
            continue
        if not (schema.get("properties") or {}):
            op["body_type"] = "EmptyObject"
            continue
        # Second pass: identical inline shapes that the generator de-duplicated (talk vs broadcast).
        shared = by_shape.get(shape_key(schema))
        if shared:
            op["body_type"] = shared


def domain_of(path):
    for name, needles in DOMAINS:
        for needle in needles:
            if needle in path:
                return name
    return "DeviceApi"


def is_slow(op):
    return op["method"] != "GET" or op["id"] in SLOW_READS


def render_method(op):
    response_type = op["response_type"]
    params = op["params"]
    path_params = [p for p in params if p["in"] == "path"]
    query_params = [p for p in params if p["in"] == "query"]
    header_params = [p for p in params if p["in"] == "header"]
    if header_params:
        raise SystemExit("unsupported header parameter on %s" % op["id"])

    all_args = []
    for p in path_params:
        all_args.append("%s %s" % (p["java_type"], safe(p["name"])))
    for p in query_params:
        all_args.append("%s %s" % (p["java_type"], safe(p["name"])))
    if op["body_type"]:
        all_args.append("%s body" % op["body_type"])

    call = "Call.%s(\"%s\")" % (op["method"].lower(), op["path"])
    chain = [call]
    for p in path_params:
        chain.append('.path("%s", %s)' % (p["name"], safe(p["name"])))
    for p in query_params:
        chain.append('.query("%s", %s)' % (p["name"], safe(p["name"])))
    if op["body_type"]:
        chain.append(".body(body)")
    if op["idempotency"] == "required":
        chain.append(".idempotency(Call.Idempotency.REQUIRED)")
    elif op["idempotency"] == "optional":
        chain.append(".idempotency(Call.Idempotency.OPTIONAL)")
    if is_slow(op):
        chain.append(".slow()")
    chain.append(".as(%s.class)" % response_type)

    body = ("return invoker.execute(Call." + op["method"].lower() + "(\"%s\")" % op["path"]
            + "\n" + "\n".join("        " + link for link in chain[1:])
            + ");") if len(chain) > 1 else 'return invoker.execute(%s);' % chain[0]

    doc = ["/**", " * %s" % (op["summary"] or op["id"]), " *"]
    doc.append(" * <p>{@code %s %s}</p>" % (op["method"], op["path"]))
    doc.append(" *")
    doc.append(" * <p>Required scope: {@code %s}</p>" % (op["scope"] or "unspecified"))
    if op["idempotency"] == "required":
        doc.append(" * <p>The platform requires an {@code Idempotency-Key} for this operation. One is")
        doc.append(" * generated automatically; pin it with {@link com.uvp.openapi.client.UvpIdempotency}")
        doc.append(" * when you retry after an unknown outcome.</p>")
    doc.append(" */")

    signature = "public %s %s(%s) {" % (response_type, op["id"], ", ".join(all_args))
    lines = doc + [signature, "    " + body, "}"]

    # Convenience overload: only the mandatory arguments, when that is actually shorter.
    required = [p for p in params if p["required"]]
    if len(required) < len(params) or (op["body_type"] and not op["body_required"]):
        short_args = ["%s %s" % (p["java_type"], safe(p["name"])) for p in required]
        pass_args = []
        required_names = {p["name"] for p in required}
        for p in path_params:
            pass_args.append(safe(p["name"]))
        for p in query_params:
            pass_args.append(safe(p["name"]) if p["name"] in required_names else "null")
        if op["body_type"]:
            pass_args.append("body" if op["body_required"] else "null")
        if op["body_type"]:
            short_args.append("%s body" % op["body_type"])
        lines.append("")
        lines.append("/** Same as {@link #%s} with optional arguments left unset. */" % op["id"])
        lines.append("public %s %s(%s) {" % (response_type, op["id"], ", ".join(short_args)))
        lines.append("    return %s(%s);" % (op["id"], ", ".join(pass_args)))
        lines.append("}")
    return "\n".join(lines)


def main():
    ops = load_operations()
    grouped = {}
    for op in ops:
        grouped.setdefault(domain_of(op["path"]), []).append(op)

    os.makedirs(OUT_DIR, exist_ok=True)
    for class_name, class_ops in sorted(grouped.items()):
        methods = "\n\n".join(render_method(op) for op in class_ops)
        source = """// GENERATED FILE - DO NOT EDIT.
// Source: api/openapi-v1.yaml via sdk/java/codegen/generate_apis.py
package com.uvp.openapi.api;

import com.uvp.openapi.internal.ApiInvoker;
import com.uvp.openapi.internal.Call;
import com.uvp.openapi.model.*;

/**
 * %s
 *
 * <p>Signing, retries and error mapping happen inside {@link ApiInvoker}; no method here takes a
 * signature argument.</p>
 */
public final class %s {

    private final ApiInvoker invoker;

    public %s(ApiInvoker invoker) {
        this.invoker = invoker;
    }

%s
}
""" % (CLASS_DOC.get(class_name, class_name), class_name, class_name,
       textwrap.indent(methods, "    ").rstrip())
        path = os.path.join(OUT_DIR, class_name + ".java")
        with open(path, "w", encoding="utf-8") as handle:
            handle.write(source)
        print("wrote %-16s %2d operations" % (class_name + ".java", len(class_ops)))
    print("total %d operations" % len(ops))


if __name__ == "__main__":
    sys.exit(main())
