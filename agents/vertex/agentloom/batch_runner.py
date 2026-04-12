from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
from pathlib import Path


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Run AgentLoom over multiple JSON inputs and save outputs.")
    parser.add_argument("--inputs", nargs="+", required=True, help="Input JSON files")
    parser.add_argument("--output-dir", required=True, help="Directory for output JSON files")
    parser.add_argument("--python-bin", default=sys.executable, help="Python binary to use")
    return parser.parse_args()


def main() -> None:
    args = parse_args()
    output_dir = Path(args.output_dir)
    output_dir.mkdir(parents=True, exist_ok=True)
    runner_path = Path(__file__).with_name("runner.py")
    env = os.environ.copy()
    env["PYTHONPATH"] = str(Path(__file__).resolve().parent.parent)

    for input_path_str in args.inputs:
        input_path = Path(input_path_str).resolve()
        output_path = output_dir / f"{input_path.stem}.out.json"
        proc = subprocess.run(
            [args.python_bin, str(runner_path), "--input-file", str(input_path)],
            check=True,
            capture_output=True,
            text=True,
            env=env,
        )
        output_path.write_text(proc.stdout, encoding="utf-8")
        print(json.dumps({"input": str(input_path), "output": str(output_path)}, ensure_ascii=False))


if __name__ == "__main__":
    main()
