"""Serialize retries of one action without blocking independent owners/actions."""
from contextlib import contextmanager
import threading

_guard = threading.Lock()
_active = {}

@contextmanager
def serialized(key):
    with _guard:
        if key not in _active:
            if len(_active) >= 1024:
                raise ValueError('too many concurrent actions')
            _active[key] = [threading.RLock(), 0]
        entry = _active[key]
        entry[1] += 1
    try:
        with entry[0]:
            yield
    finally:
        with _guard:
            entry[1] -= 1
            if entry[1] == 0:
                del _active[key]
