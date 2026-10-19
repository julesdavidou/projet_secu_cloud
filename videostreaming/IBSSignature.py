"""
Implémentation de IBSSignature avec un pairing ASYMÉTRIQUE (Type 3),
via py_ecc (courbe BN254 / alt_bn128) — adaptation du schéma de Hess
(originellement Type 1 symétrique) utilisé dans l'article.

Installation : pip install py_ecc      (pure Python, pas de dépendance C)

--------------------------------------------------------------------------
CHOIX DE (G0, G1, p, e) DU PAPIER, ADAPTÉS AU TYPE 3
--------------------------------------------------------------------------
Le "G0" unique du papier est éclaté en deux groupes distincts, avec un
rôle FIXE pour chacun (c'est cette discipline de placement qui remplace
la propriété de symétrie e(aP,bP)=e(bP,aP), qui n'existe qu'en Type 1) :

    G1_ecc (py_ecc.bn128.G1) : identités hachées H1(ID), points aléatoires
                               P1, clés secrètes S_w / DK_d, signatures w2
    G2_ecc (py_ecc.bn128.G2) : générateur système P, clés publiques
                               PK_s / PK' / PK_d
    GT     (py_ecc.bn128.FQ12) : résultat du pairing, "G1" dans le papier
    p (papier) = curve_order de py_ecc (ordre des groupes -- PAS le
                 field_modulus, qui est un nombre différent et plus grand)

    e(X, Y) avec X in G1_ecc, Y in G2_ecc  ->  pairing(Y, X) côté py_ecc
    (py_ecc attend l'argument G2 en premier, d'où l'inversion dans le
    wrapper ci-dessous)

Conséquence pratique : AUCUNE formule mathématique du papier n'a besoin
d'être changée (vérifié par simulation numérique séparée). Seuls deux
détails d'implémentation diffèrent :
    - "-PK'" est réalisé par neg() (négation de point elliptique),
      jamais par une inversion dans GT
    - H1 cible explicitement G1_ecc, jamais "G0" en général
--------------------------------------------------------------------------
"""

from __future__ import annotations
import hashlib
from dataclasses import dataclass
from typing import Any

from py_ecc.bn128 import (
    G1 as G1_GEN,
    G2 as G2_GEN,
    add,
    multiply,
    neg,
    pairing,
    curve_order,
)

# Import du format des paramètres SIS défini dans security_protocol.py
# (à garder dans le même dossier / package)
from SISIntegrity import SISParams


G1Point = tuple      # point de G1_ecc (identités, clés secrètes, signatures)
G2Point = tuple      # point de G2_ecc (générateur, clés publiques)
GTElement = Any       # élément de FQ12, résultat d'un pairing


class IBSSignature:
    """
    Signature identity-based (Hess), adaptée à un pairing asymétrique
    Type 3 (BN254 via py_ecc).
    """

    def __init__(self):
        self.r_order = curve_order      # "p" du papier : ordre des groupes
        self.P: G2Point = G2_GEN        # générateur système, dans G2_ecc

    # -- Pairing et primitives de base ----------------------------------------

    def pairing_fn(self, X_g1: G1Point, Y_g2: G2Point) -> GTElement:
        """
        Calcule e(X, Y) avec X in G1_ecc, Y in G2_ecc.

        Entrée  : X_g1 (point de G1_ecc), Y_g2 (point de G2_ecc)
        Sortie  : élément de GT
        Note    : py_ecc.pairing() attend (G2, G1) dans cet ordre, d'où
                  l'inversion ci-dessous pour coller à la convention e(X,Y).
        """
        return pairing(Y_g2, X_g1)

    def random_scalar(self) -> int:
        """Tire un scalaire aléatoire dans Z*_p (p = self.r_order)."""
        import random
        return random.randrange(1, self.r_order)

    def H1(self, identity: str) -> G1Point:
        """
        Hache une identité vers un point de G1_ecc.

        Entrée  : identity (ex. ID_w ou ID_d)
        Sortie  : point de G1_ecc
        Formule : hash-then-multiply (simplification pour prototype :
                  H1(ID) = SHA256(ID) mod p * G1_GEN). Suffisant pour ce
                  projet, mais à noter comme simplification dans le
                  rapport (un vrai hash-to-curve évite tout lien connu
                  avec le logarithme discret du générateur).
        """
        digest = hashlib.sha256(identity.encode()).digest()
        scalar = int.from_bytes(digest, "big") % self.r_order
        return multiply(G1_GEN, scalar)

    def H2(self, V: Any, paramA: SISParams, r_elt: GTElement) -> int:
        """
        Hache (V, paramA, r) vers un scalaire de Z*_p.

        Entrée  : V (empreinte d'intégrité), paramA, r_elt (élément de GT)
        Sortie  : w1, entier dans Z*_p
        """
        payload = repr(V).encode() + repr(paramA).encode() + repr(r_elt).encode()
        digest = hashlib.sha256(payload).digest()
        return int.from_bytes(digest, "big") % self.r_order

    # -- Setup de zone / identification ----------------------------------------

    def setup_zone_keys(self) -> tuple[int, G2Point]:
        """
        Génère la clé maîtresse et la clé publique d'un serveur d'identification.

        Sortie  : (t_s, PK_s)
        Formule : t_s aléatoire dans Z*_p ; PK_s = t_s * P   (P dans G2_ecc)
        """
        t_s = self.random_scalar()
        PK_s = multiply(self.P, t_s)
        return t_s, PK_s

    def issue_secret_share(self, t_s: int, ID_w: str) -> G1Point:
        """
        Émet une part secrète pour un owner s'identifiant dans une zone.

        Entrée  : t_s (clé maîtresse de la zone), ID_w
        Sortie  : S_w^s, point de G1_ecc
        Formule : S_w^s = t_s * H1(ID_w)
        """
        return multiply(self.H1(ID_w), t_s)

    def compute_owner_key(self, S_w_prev: G1Point | None, S_w_s: G1Point) -> G1Point:
        """
        Met à jour la clé secrète cumulative de l'owner.

        Entrée  : S_w_prev (None si première identification), S_w_s
        Sortie  : S_w mise à jour, point de G1_ecc
        Formule : S_w = S_w_prev + S_w_s
        """
        if S_w_prev is None:
            return S_w_s
        return add(S_w_prev, S_w_s)

    def compute_visited_PK(self, PK_v: G2Point | None, PK_s: G2Point) -> G2Point:
        """
        Met à jour la clé publique cumulative des zones visitées.

        Entrée  : PK_v (None si première zone), PK_s
        Sortie  : PK mise à jour, point de G2_ecc
        Formule : PK = PK_v + PK_s
        """
        if PK_v is None:
            return PK_s
        return add(PK_v, PK_s)

    # -- Génération / vérification de signature --------------------------------

    def generate_signature(
        self, V: Any, paramA: SISParams, S_w: G1Point
    ) -> tuple[int, G1Point]:
        """
        Génère la signature identity-based d'un owner sur l'empreinte V.

        Entrée  : V, paramA, S_w (clé secrète de l'owner, dans G1_ecc)
        Sortie  : sigma = (w1, w2)
        Formule : P1 aléatoire dans G1_ecc, k aléatoire dans Z*_p
                  r = e(P1, P)^k
                  w1 = H2(V, paramA, r)
                  w2 = w1 * S_w + k * P1     (dans G1_ecc)
        """
        k = self.random_scalar()
        P1 = multiply(G1_GEN, self.random_scalar())
        r_val = self.pairing_fn(P1, self.P) ** k
        w1 = self.H2(V, paramA, r_val)
        w2 = add(multiply(S_w, w1), multiply(P1, k))
        return w1, w2

    def verify_signature(
        self,
        ID_w: str,
        PK_v: G2Point,
        PK_s: G2Point,
        sigma: tuple[int, G1Point],
        V_prime: Any,
        paramA: SISParams,
    ) -> bool:
        """
        Vérifie une signature d'owner sur les données reconstruites.

        Entrée  : ID_w, PK_v, PK_s, sigma=(w1, w2), V_prime, paramA
        Sortie  : True si la signature est valide
        Formule : PK' = PK_v + PK_s
                  r' = e(w2, P) * e(H1(ID_w), -PK')^w1
                  valide si w1 == H2(V_prime, paramA, r')
        """
        w1, w2 = sigma
        PK_prime = self.compute_visited_PK(PK_v, PK_s)
        term1 = self.pairing_fn(w2, self.P)
        term2 = self.pairing_fn(self.H1(ID_w), neg(PK_prime)) ** w1
        r_prime = term1 * term2
        return w1 == self.H2(V_prime, paramA, r_prime)

    # -- Délégation de signature -------------------------------------------------

    def compute_delegation_key(self, S_w: G1Point, y: int, ID_d: str) -> G1Point:
        """
        Calcule la clé de délégation transmise par l'owner au délégué.

        Entrée  : S_w, y (aléatoire dans Z*_p), ID_d
        Sortie  : DK_d, point de G1_ecc
        Formule : DK_d = S_w + y * H1(ID_d)
        """
        return add(S_w, multiply(self.H1(ID_d), y))

    def generate_delegated_signature(
        self, V: Any, paramA: SISParams, DK_d: G1Point
    ) -> tuple[int, G1Point]:
        """
        Génère une signature au nom du délégué.

        Entrée  : V (empreinte après modification), paramA, DK_d
        Sortie  : sigma1 = (w1, w2)
        Formule : identique à generate_signature(), DK_d à la place de S_w
        """
        return self.generate_signature(V, paramA, DK_d)

    def verify_delegated_signature(
        self,
        ID_w: str,
        ID_d: str,
        PK_v: G2Point,
        PK_s: G2Point,
        PK_d: G2Point,
        sigma1: tuple[int, G1Point],
        V_prime: Any,
        paramA: SISParams,
    ) -> bool:
        """
        Vérifie une signature déléguée.

        Entrée  : ID_w, ID_d, PK_v, PK_s, PK_d, sigma1=(w1, w2), V_prime, paramA
        Sortie  : True si la signature déléguée est valide
        Formule : PK' = PK_v + PK_s
                  r' = e(w2, P) * e(H1(ID_w), -PK')^w1 * e(H1(ID_d), -PK_d)^w1
                  valide si w1 == H2(V_prime, paramA, r')
        """
        w1, w2 = sigma1
        PK_prime = self.compute_visited_PK(PK_v, PK_s)
        term1 = self.pairing_fn(w2, self.P)
        term2 = self.pairing_fn(self.H1(ID_w), neg(PK_prime)) ** w1
        term3 = self.pairing_fn(self.H1(ID_d), neg(PK_d)) ** w1
        r_prime = term1 * term2 * term3
        return w1 == self.H2(V_prime, paramA, r_prime)