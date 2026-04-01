#!/usr/bin/env python3
import argparse
import json
import re
import subprocess
import sys
from pathlib import Path


def load_json_indexes(path: Path) -> list[dict]:
    data = json.loads(path.read_text(encoding="utf-8"))
    return data.get("indexes", [])


def load_existing_indexes(project_id: str, database: str) -> list[dict]:
    cmd = [
        "gcloud",
        "firestore",
        "indexes",
        "composite",
        "list",
        "--project",
        project_id,
        "--database",
        database,
        "--format=json",
    ]
    out = subprocess.check_output(cmd, text=True)
    return json.loads(out)


def collection_group(idx: dict) -> str:
    cg = idx.get("collectionGroup")
    if cg:
        return str(cg)
    name = str(idx.get("name", ""))
    match = re.search(r"/collectionGroups/([^/]+)/indexes/", name)
    return match.group(1) if match else ""


def field_signature(field: dict) -> tuple[str, str, str]:
    field_path = str(field.get("fieldPath", ""))
    if "arrayConfig" in field:
        return (field_path, "array", str(field["arrayConfig"]))
    if "vectorConfig" in field:
        return (
            field_path,
            "vector",
            json.dumps(field["vectorConfig"], sort_keys=True, separators=(",", ":")),
        )
    return (field_path, "order", str(field.get("order", "")))


def index_signature(idx: dict) -> tuple[str, str, tuple[tuple[str, str, str], ...]]:
    fields = tuple(
        field_signature(field)
        for field in idx.get("fields", [])
        if field.get("fieldPath") != "__name__"
    )
    return (collection_group(idx), str(idx.get("queryScope", "COLLECTION")), fields)


def build_create_command(project_id: str, database: str, idx: dict) -> list[str]:
    cmd = [
        "gcloud",
        "firestore",
        "indexes",
        "composite",
        "create",
        "--project",
        project_id,
        "--database",
        database,
        "--collection-group",
        idx["collectionGroup"],
        "--query-scope",
        idx.get("queryScope", "COLLECTION"),
        "--async",
    ]
    for field in idx.get("fields", []):
        field_path = field["fieldPath"]
        if "arrayConfig" in field:
            cfg = f"field-path={field_path},array-config={field['arrayConfig']}"
        elif "vectorConfig" in field:
            vector_json = json.dumps(field["vectorConfig"], sort_keys=True, separators=(",", ":"))
            cfg = f"field-path={field_path},vector-config={vector_json}"
        else:
            cfg = f"field-path={field_path},order={field['order']}"
        cmd.extend(["--field-config", cfg])
    return cmd


def is_auto_single_field_index(idx: dict) -> bool:
    fields = [field for field in idx.get("fields", []) if field.get("fieldPath") != "__name__"]
    if len(fields) != 1:
        return False
    field = fields[0]
    return "order" in field and "arrayConfig" not in field and "vectorConfig" not in field


def main() -> int:
    parser = argparse.ArgumentParser(description="Sync Firestore composite indexes from web/firestore.indexes.json")
    parser.add_argument("--project", required=True, help="GCP project id")
    parser.add_argument(
        "--json",
        default="web/firestore.indexes.json",
        help="Path to firestore.indexes.json",
    )
    parser.add_argument(
        "--database",
        default="(default)",
        help="Firestore database name",
    )
    parser.add_argument(
        "--apply",
        action="store_true",
        help="Create missing indexes with gcloud firestore indexes composite create --async",
    )
    parser.add_argument(
        "--verbose",
        action="store_true",
        help="Print every missing index and create command",
    )
    args = parser.parse_args()

    json_path = Path(args.json)
    wanted = load_json_indexes(json_path)
    existing = load_existing_indexes(args.project, args.database)

    existing_signatures = {index_signature(idx) for idx in existing}
    missing = [
        idx
        for idx in wanted
        if index_signature(idx) not in existing_signatures and not is_auto_single_field_index(idx)
    ]

    print(f"wanted={len(wanted)} existing={len(existing)} missing={len(missing)}")
    if not missing:
        return 0

    if args.verbose or not args.apply:
        for idx in missing:
            print(
                json.dumps(
                    {
                        "collectionGroup": idx["collectionGroup"],
                        "queryScope": idx.get("queryScope", "COLLECTION"),
                        "fields": idx["fields"],
                    },
                    ensure_ascii=True,
                )
            )

    if not args.apply:
        return 0

    failures = 0
    for idx in missing:
        cmd = build_create_command(args.project, args.database, idx)
        if args.verbose:
            print(f"create {idx['collectionGroup']} -> {' '.join(cmd)}")
        result = subprocess.run(cmd, capture_output=True, text=True)
        if result.returncode != 0:
            stderr = result.stderr.strip()
            stdout = result.stdout.strip()
            text = stderr or stdout
            lowered = text.lower()
            if "already exists" in lowered:
                continue
            if "this index is not necessary" in lowered and "single field index controls" in lowered:
                continue
            failures += 1
            print(text, file=sys.stderr)
    return 1 if failures else 0


if __name__ == "__main__":
    raise SystemExit(main())
