import argparse
import json
from pathlib import Path

import joblib
from sklearn.metrics import classification_report


def load_dataset(path: Path):
    texts = []
    labels = []
    with path.open("r", encoding="utf-8") as handle:
        for line in handle:
            if not line.strip():
                continue
            row = json.loads(line)
            text = "\n".join(
                [
                    str(row.get("sourceHost", "")),
                    str(row.get("title", "")),
                    str(row.get("summary", "")),
                    str(row.get("body", "")),
                    str(row.get("categoryHint", "")),
                ]
            ).strip()
            label = str(row.get("label", "")).strip()
            if text and label:
                texts.append(text)
                labels.append(label)
    return texts, labels


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--input", action="append", required=True)
    parser.add_argument("--model", required=True)
    args = parser.parse_args()
    model = joblib.load(args.model)
    texts = []
    labels = []
    for raw in args.input:
        ds_texts, ds_labels = load_dataset(Path(raw))
        texts.extend(ds_texts)
        labels.extend(ds_labels)
    preds = model.predict(texts)
    print(classification_report(labels, preds, digits=4))


if __name__ == "__main__":
    main()
