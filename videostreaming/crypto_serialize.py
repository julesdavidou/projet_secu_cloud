"""Sérialisation JSON des objets py_ecc (points G1/G2) utilisés par IBSSignature,
pour pouvoir stocker PK_s et sigma dans placement.json (JSON-only, pas de pickle).

V et w1 (int) sont déjà des entiers Python natifs — rien à faire pour eux.
Seuls les points elliptiques (PK_s en G2, w2 en G1) ont besoin de ces helpers.
"""
from __future__ import annotations

from py_ecc.bn128 import FQ, FQ2


def serialize_g1(point) -> list | None:
    """Point de G1_ecc (ex: w2 dans sigma) -> [x, y] (entiers)."""
    if point is None:  # point à l'infini
        return None
    x, y = point
    return [int(x.n), int(y.n)]


def deserialize_g1(data: list | None):
    if data is None:
        return None
    x, y = data
    return (FQ(x), FQ(y))


def serialize_g2(point) -> list | None:
    """Point de G2_ecc (ex: PK_s, PK_v) -> [[x0,x1], [y0,y1]] (coeffs FQ2)."""
    if point is None:
        return None
    x, y = point
    return [[int(c) for c in x.coeffs], [int(c) for c in y.coeffs]]


def deserialize_g2(data: list | None):
    if data is None:
        return None
    x, y = data
    return (FQ2(x), FQ2(y))


def serialize_sigma(sigma: tuple[int, tuple]) -> list:
    """sigma = (w1, w2) -> [w1, [x, y]] JSON-friendly."""
    w1, w2 = sigma
    return [w1, serialize_g1(w2)]


def deserialize_sigma(data: list) -> tuple[int, tuple]:
    w1, w2_data = data
    return (w1, deserialize_g1(w2_data))
