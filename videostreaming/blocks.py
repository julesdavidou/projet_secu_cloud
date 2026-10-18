from __future__ import annotations

import math
from dataclasses import dataclass

BLOCK_SIZE = 125000


@dataclass(frozen=True, slots=True)
class BlockEnvelope:
    video_id: str
    segment_name: str
    segment_index: int
    block_index: int
    block_count: int
    segment_byte_length: int
    payload: bytes

    def to_wire_dict(self) -> dict:
        return {
            "video_id": self.video_id,
            "segment_name": self.segment_name,
            "segment_index": self.segment_index,
            "block_index": self.block_index,
            "block_count": self.block_count,
            "segment_byte_length": self.segment_byte_length,
            "block_size": len(self.payload),
            "payload_hex": self.payload.hex(),
        }


def block_count_for_segment(segment_len: int, block_size: int = BLOCK_SIZE) -> int:
    if segment_len < 0:
        raise ValueError("segment_len must be >= 0")
    if block_size <= 0:
        raise ValueError("block_size must be > 0")
    if segment_len == 0:
        return 0
    return math.ceil(segment_len / block_size)


def pad_block(data: bytes, block_size: int = BLOCK_SIZE) -> bytes:
    if block_size <= 0:
        raise ValueError("block_size must be > 0")
    if len(data) > block_size:
        raise ValueError("block larger than block_size")
    if len(data) == block_size:
        return data
    return data + b"\x00" * (block_size - len(data))


def segment_to_blocks(
    video_id: str,
    segment_name: str,
    segment_index: int,
    data: bytes,
    block_size: int = BLOCK_SIZE,
) -> list[BlockEnvelope]:
    n = block_count_for_segment(len(data), block_size)
    if n == 0:
        return []

    blocks: list[BlockEnvelope] = []
    for i in range(n):
        start = i * block_size
        chunk = data[start : start + block_size]
        blocks.append(
            BlockEnvelope(
                video_id=video_id,
                segment_name=segment_name,
                segment_index=segment_index,
                block_index=i,
                block_count=n,
                segment_byte_length=len(data),
                payload=pad_block(chunk, block_size),
            )
        )
    return blocks

def reassemble_segment(blocks: list[BlockEnvelope]) -> bytes:
    """Reconstitue les données du segment à partir d'une liste d'enveloppes de blocs.

    Trie les blocs par block_index, concatène les payloads et tronque le
    padding à la taille d'origine (segment_byte_length).
    """
    if not blocks:
        return b""

    # Trie les blocs dans l'ordre croissant de leur index
    sorted_blocks = sorted(blocks, key=lambda b: b.block_index)

    # Récupère la taille initiale attendue du segment (stockée dans les métadonnées)
    expected_length = sorted_blocks[0].segment_byte_length

    # Concatène tous les payloads
    full_payload = b"".join(b.payload for b in sorted_blocks)

    # Retire le bourrage (padding) en tronquant la taille
    return full_payload[:expected_length]