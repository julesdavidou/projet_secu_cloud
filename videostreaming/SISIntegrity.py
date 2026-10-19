"""
Protocole de vérification d'intégrité distribuée
(basé sur Sellami, Imine, Gallais - FGCS 2024)

SISIntegrity : construction de la matrice A, hachage des blocs,
    calcul et vérification de l'empreinte V (algèbre modulaire pure).

Dépendances prévues (à installer) :
    numpy        -> algèbre matricielle pour SISIntegrity
    hashlib      -> stdlib, pour H3
"""

from __future__ import annotations
from dataclasses import dataclass
from typing import Any
import numpy as np
import hashlib as hl


@dataclass
class SecurityParams:
    """Paramètres de sécurité partagés (l, m, q) pour le module SIS."""
    l: int = 64
    m: int = 1536
    q: int = 4099
    lambda_bits: int = m // 48
    # ATTENTION : lambda_bits est calculé à partir du m PAR DÉFAUT (1536)


@dataclass
class SISParams:
    """Graine publique du générateur uniforme I (paramA dans le papier)."""
    I0: int
    a: int
    C0: int

class SISIntegrity:
    """
    Construction et vérification de l'empreinte d'intégrité des données
    via le problème SIS (Short Integer Solution).

    Ne dépend que d'algèbre modulaire (numpy + arithmétique sur Z_q).
    """

    def __init__(self, params: SecurityParams):
        self.params = params  # l, m, q, lambda_bits

    # -- Setup / génération de la matrice A ---------------------------------

    def lcg_next(self, I_n: int, C_n: int, a: int, q: int) -> tuple[int, int]:
        """
        Une itération du générateur linéaire congruentiel (section 3.4.1 §5).

        Entrée  : I_n, C_n (état courant), a (multiplicateur), q (modulo/base)
        Sortie  : (I_n+1, C_n+1)
        Formule : I_n+1 = (a*I_n + C_n) mod q
                  C_n+1 = (a*I_n + C_n) div q
        """
        I_np1 = (a * I_n + C_n) % q
        C_np1 = (a * I_n + C_n) // q
        return (I_np1, C_np1)

    def generate_matrix_A(self, sis_params: SISParams) -> Any:
        """
        Génère la matrice A (l x m) à partir de la graine publique.

        Entrée  : sis_params (I0, a, C0)
        Sortie  : matrice A de dimensions (l, m) sur Z_q
        Formule : applique lcg_next() en boucle l*m fois, reshape en (l, m)
        """
        A = []
        I = sis_params.I0
        C = sis_params.C0
        for hauteur in range(self.params.l):
            A.append([])
            for largeur in range(self.params.m):
                # BUG corrigé : "lgc_next" -> "lcg_next", et suppression
                # du "self" en trop (self.lcg_next est déjà une méthode liée)
                I, C = self.lcg_next(I, C, sis_params.a, self.params.q)
                A[-1].append(I)
        return A

    def reconstruct_A(self, sis_params: SISParams) -> Any:
        """
        Reconstruit A côté vérifieur à partir de paramA uniquement
        (évite de stocker/transmettre toute la matrice — cf. section 3.4.3.3).

        Entrée  : sis_params (I0, a, C0)
        Sortie  : matrice A identique à generate_matrix_A()
        """
        return self.generate_matrix_A(sis_params)

    # -- Construction des vecteurs / matrice de données ----------------------

    def hash_block(self, D_i: bytes, lambda_bits: int) -> str:
        """
        Hache un bloc de donnée (fonction H3 du papier, résistante aux collisions).

        Entrée  : D_i (bloc de données brutes), lambda_bits (taille de sortie)
        Sortie  : h_i, chaîne de bits ('0'/'1') de longueur lambda_bits
        Formule : H3 : {0,1}* -> {0,1}^lambda (SHA-256 tronqué)
        """
        digest = hl.sha256(D_i).digest()
        h_i = int.from_bytes(digest, byteorder="big")
        h_i >>= (256 - lambda_bits)
        return format(h_i, f"0{lambda_bits}b")

    def build_vector_x(self, h_i: str, m: int, lambda_bits: int) -> str:
        """
        Construit le vecteur x_i en répétant h_i pour remplir m bits (Fig. 2).

        Entrée  : h_i (hash du bloc), m (taille cible du vecteur)
        Sortie  : x_i, chaîne de {0,1}^m
        """
        x_i = ""
        for longueur in range(m // lambda_bits):
            x_i += h_i
        return x_i[:m]

    def build_data_matrix(self, blocks: list[bytes]) -> Any:
        """
        Empile les vecteurs x_i de chaque bloc en une matrice X.

        Entrée  : blocks = [D_1, ..., D_N]
        Sortie  : matrice X : liste de N chaînes binaires de longueur m
        Formule : X = [x_1, x_2, ..., x_N]
        """
        X = []
        for Di in blocks:
            h_i = self.hash_block(Di, self.params.lambda_bits)
            x_i = self.build_vector_x(h_i, self.params.m, self.params.lambda_bits)
            X.append(x_i)
        return X

    # -- Calcul et vérification de l'empreinte V ------------------------------

    def _x_to_vector(self, x_i: str) -> list[int]:
        """Convertit une chaîne binaire '0101...' en liste d'entiers [0,1,0,1,...]."""
        return [int(bit) for bit in x_i]

    def compute_V(self, A: Any, X: Any) -> Any:
        """
        Calcule l'empreinte d'intégrité globale.

        Entrée  : A (l, m), X (liste de N chaînes binaires de longueur m)
        Sortie  : V, liste de N vecteurs de longueur l   [v_1, ..., v_N]
        Formule : v_i = A * x_i mod q   pour chaque bloc i
        """
        V = []
        for x_i in X:
            x_i_vec = self._x_to_vector(x_i)
            v_i = [int(np.dot(row, x_i_vec)) % self.params.q for row in A]
            V.append(v_i)
        return V

    def verify_block(self, D_i: bytes, A: Any, v_i_expected: Any) -> bool:
        """
        Vérifie qu'un bloc reçu correspond bien au vecteur v_i stocké dans V.

        Entrée  : D_i (bloc reçu), A (matrice reconstruite), v_i_expected
        Sortie  : True si le bloc est intègre, False sinon
        Formule : recalcule x_i puis v_i' = A * x_i mod q, compare à v_i_expected
        """
        h_i = self.hash_block(D_i, self.params.lambda_bits)
        x_i = self.build_vector_x(h_i, self.params.m, self.params.lambda_bits)
        x_i_vec = self._x_to_vector(x_i)
        v_i_computed = [int(np.dot(row, x_i_vec)) % self.params.q for row in A]
        return v_i_computed == v_i_expected