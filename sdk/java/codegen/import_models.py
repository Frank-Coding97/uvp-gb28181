#!/usr/bin/env python3
"""Regenerate the model layer from the OpenAPI contract.

The models are generated, then normalised:

* ``@javax.annotation.*`` annotations are dropped so the SDK does not need jsr305 or
  javax.annotation-api on the classpath.
* The per-model ``JsonNullable`` helpers are dropped: the contract models no ``nullable`` fields,
  so those helpers are dead code that would otherwise drag in jackson-databind.

Everything else is left exactly as generated. Running this script is safe and idempotent; the
result is committed so SDK consumers never need a JDK-based generator.

Usage:
    python3 sdk/java/codegen/import_models.py [--jar path/to/openapi-generator-cli.jar]
"""

from __future__ import annotations

import argparse
import glob
import re
import re
import os
import shutil
import subprocess
import sys
import tempfile
import urllib.request

import yaml

import yaml

REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", ".."))
SPEC = os.path.join(REPO_ROOT, "api", "openapi-v1.yaml")
MODEL_DIR = os.path.join(
    REPO_ROOT, "sdk", "java", "uvp-openapi-sdk", "src", "main", "java", "com", "uvp", "openapi", "model"
)
CACHE_DIR = os.path.join(REPO_ROOT, "sdk", "java", ".cache")
GENERATOR_VERSION = "7.10.0"
GENERATOR_URL = ("https://repo1.maven.org/maven2/org/openapitools/openapi-generator-cli/"
                 "%s/openapi-generator-cli-%s.jar" % (GENERATOR_VERSION, GENERATOR_VERSION))


def find_java():
    candidates = [
        "/opt/homebrew/opt/openjdk@17/bin/java",
        "/usr/lib/jvm/temurin-17-jdk/bin/java",
        shutil.which("java"),
    ]
    for candidate in candidates:
        if candidate and os.path.exists(candidate):
            return candidate
    raise SystemExit("no java binary found; install JDK 17 or pass --java")


def ensure_jar(explicit):
    if explicit:
        if not os.path.exists(explicit):
            raise SystemExit("generator jar not found: %s" % explicit)
        return explicit
    cached = os.path.join(CACHE_DIR, "openapi-generator-cli-%s.jar" % GENERATOR_VERSION)
    if os.path.exists(cached) and os.path.getsize(cached) > 1_000_000:
        return cached
    os.makedirs(CACHE_DIR, exist_ok=True)
    print("downloading generator %s ..." % GENERATOR_VERSION)
    urllib.request.urlretrieve(GENERATOR_URL, cached)
    return cached


def strip_javax_annotations(path):
    """Removes ``@javax.annotation.*`` annotations without touching the rest of the line.

    The generator puts annotations on their own line *and* inline with method signatures
    (``public Foo bar(@javax.annotation.Nonnull String x)``). Dropping whole lines would delete the
    signature and produce a file that only fails at compile time, so the annotation text is
    removed in place and a line is dropped only when nothing else is left on it.
    """
    annotation = re.compile(r"@javax\.annotation\.[A-Za-z]+(\((?:[^()]*)\))?")
    with open(path, encoding="utf-8") as handle:
        lines = handle.read().split("\n")

    kept = []
    changed = False
    for line in lines:
        if "@javax.annotation." in line:
            cleaned = annotation.sub("", line)
            changed = True
            if cleaned.strip() == "":
                continue
            kept.append(cleaned.rstrip())
        else:
            kept.append(line)
    if changed:
        with open(path, "w", encoding="utf-8") as handle:
            handle.write("\n".join(kept))
        return True
    return False


def drop_json_nullable(path):
    """Removes the two JsonNullable helper methods and the import that only they used.

    Done by brace counting rather than a regex: a lazy regex with DOTALL can swallow the next
    method's signature, which produces code that looks fine until it fails to compile.
    """
    with open(path, encoding="utf-8") as handle:
        lines = handle.read().split("\n")

    out = []
    index = 0
    removed = False
    while index < len(lines):
        line = lines[index]
        stripped = line.strip()
        is_helper = (stripped.startswith("private static <T> boolean equalsNullable(JsonNullable<T>")
                     or stripped.startswith("private static <T> int hashCodeNullable(JsonNullable<T>"))
        if not is_helper:
            out.append(line)
            index += 1
            continue

        # Drop a blank separator and the javadoc block immediately above, if present.
        if out and out[-1].strip() == "":
            out.pop()
        if out and out[-1].strip().endswith("*/"):
            while out and not out[-1].strip().startswith("/**"):
                out.pop()
            if out:
                out.pop()

        depth = 0
        while index < len(lines):
            depth += lines[index].count("{") - lines[index].count("}")
            index += 1
            if depth <= 0 and lines[index - 1].strip().endswith("}"):
                break
        removed = True
        # Collapse the blank line the helper left behind.
        if out and out[-1].strip() == "":
            out.pop()
        out.append("")

    text = "\n".join(out)
    if "import org.openapitools.jackson.nullable.JsonNullable;" in text:
        text = text.replace("import org.openapitools.jackson.nullable.JsonNullable;\n", "")
        removed = True
    if removed:
        with open(path, "w", encoding="utf-8") as handle:
            handle.write(text)
    return removed


def retarget_json_helper(path):
    """Points the generated toJson/fromJson helpers at UvpJson instead of the generator's JSON."""
    with open(path, encoding="utf-8") as handle:
        text = handle.read()
    if "com.uvp.openapi.JSON" not in text:
        return False
    updated = (text.replace("import com.uvp.openapi.JSON;", "import com.uvp.openapi.UvpJson;")
                   .replace("JSON.getGson()", "UvpJson.getGson()"))
    with open(path, "w", encoding="utf-8") as handle:
        handle.write(updated)
    return True


def retarget_json_helper(path):
    """Points the generated toJson/fromJson helpers at UvpJson instead of the generator's JSON."""
    with open(path, encoding="utf-8") as handle:
        text = handle.read()
    if "com.uvp.openapi.JSON" not in text:
        return False
    updated = (text.replace("import com.uvp.openapi.JSON;", "import com.uvp.openapi.UvpJson;")
                   .replace("JSON.getGson()", "UvpJson.getGson()"))
    with open(path, "w", encoding="utf-8") as handle:
        handle.write(updated)
    return True


EMPTY_MODEL_TEMPLATE = """package com.uvp.openapi.model;

/**
 * %(name)s - the contract defines this body as an empty JSON object.
 *
 * <p>Written explicitly because the generator skips object schemas that declare no properties,
 * even when an operation references them, which would leave the API classes referring to a type
 * that does not exist.</p>
 */
public class %(name)s {

  @Override
  public boolean equals(Object other) {
    return other != null && getClass() == other.getClass();
  }

  @Override
  public int hashCode() {
    return getClass().getName().hashCode();
  }

  @Override
  public String toString() {
    return "%(name)s{}";
  }
}
"""


def emit_empty_models():
    """Creates classes for object schemas that declare no properties."""
    with open(SPEC, encoding="utf-8") as handle:
        doc = yaml.safe_load(handle)
    created = []
    for name, schema in (doc.get("components", {}).get("schemas") or {}).items():
        if schema.get("type") != "object" or schema.get("properties"):
            continue
        path = os.path.join(MODEL_DIR, name + ".java")
        if os.path.exists(path):
            continue
        with open(path, "w", encoding="utf-8") as handle:
            handle.write(EMPTY_MODEL_TEMPLATE % {"name": name})
        created.append(name)
    return created


EMPTY_MODEL_TEMPLATE = """package com.uvp.openapi.model;

/**
 * %(name)s - the contract defines this body as an empty JSON object.
 *
 * <p>Written explicitly because the generator skips object schemas that declare no properties,
 * even when an operation references them, which would leave the API classes referring to a type
 * that does not exist.</p>
 */
public class %(name)s {

  @Override
  public boolean equals(Object other) {
    return other != null && getClass() == other.getClass();
  }

  @Override
  public int hashCode() {
    return getClass().getName().hashCode();
  }

  @Override
  public String toString() {
    return "%(name)s{}";
  }
}
"""


def emit_empty_models():
    """Creates classes for object schemas that declare no properties."""
    with open(SPEC, encoding="utf-8") as handle:
        doc = yaml.safe_load(handle)
    created = []
    for name, schema in (doc.get("components", {}).get("schemas") or {}).items():
        if schema.get("type") != "object" or schema.get("properties"):
            continue
        path = os.path.join(MODEL_DIR, name + ".java")
        if os.path.exists(path):
            continue
        with open(path, "w", encoding="utf-8") as handle:
            handle.write(EMPTY_MODEL_TEMPLATE % {"name": name})
        created.append(name)
    return created


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--jar")
    parser.add_argument("--java")
    args = parser.parse_args()

    jar = ensure_jar(args.jar)
    java = args.java or find_java()

    workdir = tempfile.mkdtemp(prefix="uvp-models-")
    try:
        cmd = [java, "-jar", jar, "generate",
               "-i", SPEC, "-g", "java", "-o", workdir, "--library", "okhttp-gson",
               "--global-property", "models,modelDocs=false,modelTests=false",
               "--additional-properties",
               "java8=true,dateLibrary=java8,useJakartaEe=false,hideGenerationTimestamp=true,"
               "modelPackage=com.uvp.openapi.model"]
        print("generating models ...")
        subprocess.run(cmd, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.STDOUT)

        generated = glob.glob(os.path.join(workdir, "src", "main", "java", "com", "uvp", "openapi",
                                           "model", "*.java"))
        if not generated:
            raise SystemExit("generator produced no models")

        if os.path.isdir(MODEL_DIR):
            shutil.rmtree(MODEL_DIR)
        os.makedirs(MODEL_DIR, exist_ok=True)
        for source in generated:
            shutil.copy2(source, os.path.join(MODEL_DIR, os.path.basename(source)))

        annotation_clean = 0
        nullable_clean = 0
        json_clean = 0
        for path in sorted(glob.glob(os.path.join(MODEL_DIR, "*.java"))):
            if strip_javax_annotations(path):
                annotation_clean += 1
            if drop_json_nullable(path):
                nullable_clean += 1
            if retarget_json_helper(path):
                json_clean += 1

        empties = emit_empty_models()

        print("models: %d generated + %d empty-body classes" % (len(generated), len(empties)))
        print("cleanup: javax annotations in %d, JsonNullable helpers in %d, JSON helper in %d"
              % (annotation_clean, nullable_clean, json_clean))
    finally:
        shutil.rmtree(workdir, ignore_errors=True)


if __name__ == "__main__":
    sys.exit(main())
