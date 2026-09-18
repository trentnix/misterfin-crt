"""Check heartbeat framing and process sampling used by sustained browser tests."""

import os
import unittest

from tools.ghostty.endurance import resources
from tools.ghostty.fixtures.browser import websocket_frames


def masked(opcode, payload):
    mask = b'1234'
    length = bytes([len(payload) | 128]) if len(payload) < 126 else b'\xfe' + len(payload).to_bytes(2, 'big')
    return bytes([0x80 | opcode]) + length + mask + bytes(b ^ mask[i % 4] for i, b in enumerate(payload))


class EnduranceSupportTests(unittest.TestCase):
    def test_partial_and_coalesced_heartbeat_frames(self):
        ping = masked(9, b'ping')
        buffer = bytearray(ping[:5])
        self.assertEqual(list(websocket_frames(buffer)), [])
        buffer.extend(ping[5:] + masked(1, b'x' * 130) + masked(8, b''))
        self.assertEqual(list(websocket_frames(buffer)), [(9, b'ping'), (1, b'x' * 130), (8, b'')])
        self.assertEqual(buffer, b'')

    def test_oversized_or_unmasked_frames_are_rejected(self):
        for payload in (b'\x89\x01x', b'\x81\xff' + (2 << 20).to_bytes(8, 'big')):
            with self.subTest(payload=payload), self.assertRaises(ValueError):
                list(websocket_frames(bytearray(payload)))

    def test_resource_sample_records_identity_and_absent_process(self):
        sample = resources(os.getpid())
        self.assertEqual(sample['pid'], os.getpid())
        self.assertGreater(sample['rss_kib'], 0)
        self.assertGreater(sample['fds'], 0)
        self.assertTrue(sample['start'].isdigit())
        self.assertIsNone(resources(-1))
