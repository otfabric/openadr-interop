#!/usr/bin/env python3
"""Validate every fixture: the JSON Schema, then the cross-field rules.

Usage: validate-fixtures.py [--require-schema]

The cross-field rules are those of the adapters (adapters/*/openadr_interop/
fixture.py), so what passes here is what a reference server accepts. Without
the jsonschema package the schema check is skipped, unless --require-schema
is given.
"""

import importlib.util
import json
import pathlib
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
SCHEMA = ROOT / "fixtures" / "schema" / "openadr-fixture.schema.json"


def adapter_rules():
    path = ROOT / "adapters" / "openleadr" / "openadr_interop" / "fixture.py"
    spec = importlib.util.spec_from_file_location("fixture_rules", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def main() -> int:
    require_schema = "--require-schema" in sys.argv[1:]
    try:
        import jsonschema
    except ImportError:
        jsonschema = None
        if require_schema:
            print("FAIL the jsonschema package is not installed")
            return 1
        print("(jsonschema not installed: schema check skipped)")

    rules = adapter_rules()
    schema = json.loads(SCHEMA.read_text(encoding="utf-8"))
    if jsonschema is not None:
        jsonschema.Draft202012Validator.check_schema(schema)

    paths = sorted((ROOT / "fixtures").glob("*/fixture.json"))
    if not paths:
        print("FAIL no fixture found")
        return 1
    status = 0
    for path in paths:
        label = path.relative_to(ROOT)
        try:
            doc = json.loads(path.read_text(encoding="utf-8"))
            if jsonschema is not None:
                errors = sorted(jsonschema.Draft202012Validator(schema).iter_errors(doc), key=lambda e: list(e.path))
                if errors:
                    for error in errors:
                        print(f"FAIL {label}: /{'/'.join(map(str, error.path))}: {error.message}")
                    status = 1
                    continue
            rules.load(str(path))
            print(f"ok   {label}")
        except (ValueError, rules.FixtureError) as exc:
            print(f"FAIL {label}: {exc}")
            status = 1
    return status


if __name__ == "__main__":
    sys.exit(main())
