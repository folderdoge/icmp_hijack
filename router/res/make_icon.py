"""Create the small software-center icon without external dependencies."""
from pathlib import Path
import struct
import zlib

size = 64
rows = []
for y in range(size):
    row = bytearray()
    for x in range(size):
        color = (29, 79, 98, 255)
        # Two endpoint nodes, connected by a channel and an arrow.
        if (x - 15) ** 2 + (y - 32) ** 2 < 58 or (x - 49) ** 2 + (y - 32) ** 2 < 58:
            color = (87, 222, 218, 255)
        if 21 <= x <= 42 and 29 <= y <= 34:
            color = (241, 247, 249, 255)
        if 35 <= x <= 43 and abs(y - 32) <= (43 - x):
            color = (241, 247, 249, 255)
        row.extend(color)
    rows.append(b'\x00' + row)

def chunk(kind, body):
    return struct.pack('!I', len(body)) + kind + body + struct.pack('!I', zlib.crc32(kind + body) & 0xffffffff)

png = b'\x89PNG\r\n\x1a\n'
png += chunk(b'IHDR', struct.pack('!IIBBBBB', size, size, 8, 6, 0, 0, 0))
png += chunk(b'IDAT', zlib.compress(b''.join(rows)))
png += chunk(b'IEND', b'')
Path(__file__).with_name('icon-icmphijack.png').write_bytes(png)
