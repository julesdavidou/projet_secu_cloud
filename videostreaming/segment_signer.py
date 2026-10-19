"""Signature et vérification SIS/IBS d'un segment, partagées par :
l'upload VOD (sendBlocks.py), l'ingestion live (live_watcher.py), l'agent
client (edge_client_agent.py) et live_verify.py.

Ce qui est réellement signé n'est pas V seul mais [video_id, segment_name, V] :
la signature lie ainsi le contenu du segment à son identité. Sans ça, un
stockage malveillant pourrait servir le segment 5 (valide, signé) à la place
du segment 7, ou rejouer un segment d'une autre vidéo du même owner.
IBSSignature traite V comme un objet opaque (haché via repr), donc aucun
changement n'est nécessaire dans vos fichiers SISIntegrity/IBSSignature.

Coût mesuré (py_ecc, PC de bureau) : signature ~2,7 s, vérification ~4,6 s
par segment, quasi indépendant du nombre de blocs (le coût est dans les 2
pairings). Sur ARM (Jetson/Raspberry) compter plusieurs fois plus.
"""
from __future__ import annotations

import random

from crypto_serialize import deserialize_g2, deserialize_sigma, serialize_g2, serialize_sigma
from IBSSignature import IBSSignature
from SISIntegrity import SecurityParams, SISIntegrity, SISParams


def signed_value(video_id: str, segment_name: str, V: list) -> list:
    """Valeur passée à generate_signature / verify_signature à la place de V."""
    return [video_id, segment_name, V]


class SegmentSigner:
    """Clés IBS + matrice SIS générées une fois, réutilisées pour toute une vidéo/un live."""

    def __init__(self, owner_id: str):
        self.owner_id = owner_id
        self.sec_params = SecurityParams()
        self.sis = SISIntegrity(self.sec_params)
        q = self.sec_params.q
        self.sis_params = SISParams(
            I0=random.randrange(1, q), a=random.randrange(1, q), C0=random.randrange(1, q)
        )
        self.A = self.sis.generate_matrix_A(self.sis_params)

        self.ibs = IBSSignature()
        t_s, self.PK_s = self.ibs.setup_zone_keys()
        share = self.ibs.issue_secret_share(t_s, owner_id)
        self.S_w = self.ibs.compute_owner_key(None, share)

    def security_header(self) -> dict:
        """Partie publique, à publier avec le placement (et à épingler côté client)."""
        sp = self.sec_params
        return {
            "sec_params": {"l": sp.l, "m": sp.m, "q": sp.q, "lambda_bits": sp.lambda_bits},
            "sis_params": {"I0": self.sis_params.I0, "a": self.sis_params.a, "C0": self.sis_params.C0},
            "owner_id": self.owner_id,
            "PK_s": serialize_g2(self.PK_s),
        }

    def trust_anchor(self) -> dict:
        """Ce que le client doit recevoir par un canal de confiance (pas depuis le stockage)."""
        return {"owner_id": self.owner_id, "PK_s": serialize_g2(self.PK_s)}

    def sign_segment(self, video_id: str, segment_name: str, payloads: list[bytes], include_v: bool = False) -> dict:
        X = self.sis.build_data_matrix(payloads)
        V = self.sis.compute_V(self.A, X)
        sigma = self.ibs.generate_signature(signed_value(video_id, segment_name, V), self.sis_params, self.S_w)
        entry = {"sigma": serialize_sigma(sigma)}
        if include_v:  # utile pour localiser un bloc altéré, mais volumineux (l entiers par bloc)
            entry["V"] = V
        return entry


def verify_segment(
    security: dict, entry: dict, video_id: str, segment_name: str, payloads: list[bytes]
) -> tuple[bool, list[int]]:
    """Fonction de niveau module (picklable) : exécutable dans un ProcessPoolExecutor.

    Retourne (ok, blocs_en_défaut). blocs_en_défaut n'est rempli que si la
    vérification échoue ET que V a été stocké dans `entry`.
    """
    sis = SISIntegrity(SecurityParams(**security["sec_params"]))
    sis_params = SISParams(**security["sis_params"])
    A = sis.reconstruct_A(sis_params)

    V_prime = sis.compute_V(A, sis.build_data_matrix(payloads))
    ibs = IBSSignature()
    ok = ibs.verify_signature(
        security["owner_id"],
        None,
        deserialize_g2(security["PK_s"]),
        deserialize_sigma(entry["sigma"]),
        signed_value(video_id, segment_name, V_prime),
        sis_params,
    )

    bad: list[int] = []
    if not ok and entry.get("V"):
        bad = [i for i, (p, v) in enumerate(zip(payloads, entry["V"])) if not sis.verify_block(p, A, v)]
    return ok, bad
