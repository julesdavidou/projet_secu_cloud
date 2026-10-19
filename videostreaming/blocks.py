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
    block_size: int = BLOCK_SIZE

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
                block_size=block_size,
            )
        )
    return blocks

def reassemble_segment(blocks: list[BlockEnvelope]) -> bytes:
    """Restore order, reject ambiguous/missing blocks, and remove only zero padding.

    Cryptographic checks are performed against the trusted manifest/SIS before
    calling this function; structural validation alone cannot detect changed data.
    """
    if not blocks:
        raise ValueError("missing blocks")
    first = blocks[0]
    length, size = first.segment_byte_length, first.block_size
    count = block_count_for_segment(length, size)
    if length <= 0 or count != first.block_count or len(blocks) != count:
        raise ValueError("incomplete block set")
    identity = (first.video_id, first.segment_name, first.segment_index, count, length, size)
    ordered = sorted(blocks, key=lambda block: block.block_index)
    output = []
    for i, block in enumerate(ordered):
        if (block.video_id, block.segment_name, block.segment_index, block.block_count, block.segment_byte_length, block.block_size) != identity or block.block_index != i:
            raise ValueError("duplicate index or inconsistent block identity")
        expected = min(size, length-i*size)
        if len(block.payload) not in {expected, size} or any(block.payload[expected:]):
            raise ValueError("invalid block length or nonzero padding")
        output.append(block.payload[:expected])
    return b"".join(output)
