import argparse
import json
from pathlib import Path

import joblib
from sklearn.feature_extraction.text import TfidfVectorizer
from sklearn.linear_model import LogisticRegression
from sklearn.pipeline import FeatureUnion
from sklearn.pipeline import Pipeline


def load_dataset(path: Path):
    texts = []
    labels = []
    weights = []
    default_weight = 4.0 if "hard_negative" in path.name or "hard_negatives" in path.name else 1.0
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
                weight = row.get("weight", default_weight)
                try:
                    weights.append(float(weight))
                except Exception:
                    weights.append(default_weight)
    return texts, labels, weights


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--input", action="append", required=True)
    parser.add_argument("--output", required=True)
    args = parser.parse_args()

    texts = []
    labels = []
    weights = []
    for raw in args.input:
        ds_texts, ds_labels, ds_weights = load_dataset(Path(raw))
        texts.extend(ds_texts)
        labels.extend(ds_labels)
        weights.extend(ds_weights)
    features = FeatureUnion(
        [
            (
                "word_tfidf",
                TfidfVectorizer(
                    analyzer="word",
                    ngram_range=(1, 2),
                    min_df=2,
                    max_features=50000,
                    sublinear_tf=True,
                ),
            ),
            (
                "char_tfidf",
                TfidfVectorizer(
                    analyzer="char_wb",
                    ngram_range=(3, 5),
                    min_df=2,
                    max_features=30000,
                    sublinear_tf=True,
                ),
            ),
        ]
    )
    pipeline = Pipeline(
        [
            ("features", features),
            ("clf", LogisticRegression(max_iter=3000, class_weight="balanced", C=1.5)),
        ]
    )
    fit_kwargs = {}
    if weights:
        fit_kwargs["clf__sample_weight"] = weights
    pipeline.fit(texts, labels, **fit_kwargs)
    Path(args.output).parent.mkdir(parents=True, exist_ok=True)
    joblib.dump(pipeline, args.output)


if __name__ == "__main__":
    main()
