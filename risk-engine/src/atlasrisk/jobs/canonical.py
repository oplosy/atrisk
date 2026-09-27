"""Language-neutral JSON encoding used for job and result hashes."""

from __future__ import annotations

import hashlib
import json
import math
from typing import Any


def canonical_json(value: Any) -> bytes:
    """Encode JSON with sorted keys, compact separators, and no non-finite floats."""
    return json.dumps(
        _normalize_numbers(value),
        ensure_ascii=False,
        allow_nan=False,
        sort_keys=True,
        separators=(",", ":"),
    ).encode("utf-8")


def _normalize_numbers(value: Any) -> Any:
    """Use the shared shortest JSON number rule (integral floats become integers)."""
    if isinstance(value, float):
        if not math.isfinite(value):
            raise ValueError("canonical JSON does not permit non-finite numbers")
        return int(value) if value.is_integer() else value
    if isinstance(value, dict):
        return {key: _normalize_numbers(item) for key, item in value.items()}
    if isinstance(value, list):
        return [_normalize_numbers(item) for item in value]
    if isinstance(value, tuple):
        return [_normalize_numbers(item) for item in value]
    return value


def sha256_json(value: Any) -> str:
    return hashlib.sha256(canonical_json(value)).hexdigest()
