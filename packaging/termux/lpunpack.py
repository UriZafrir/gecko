# lpunpack.py IMAGE NAME OUT: writes the logical partition NAME of an Android
# super image (dynamic partitions, linear extents) to OUT. IMAGE is the super
# partition itself or a GPT disk image holding it (the emulator's
# system.img). For linker-smoke.sh.
import struct
import sys

src, name, out = sys.argv[1:4]
f = open(src, 'rb')


def read(off, n):
    f.seek(off)
    return f.read(n)


def super_offsets():
    yield 0
    gpt = read(512, 92)
    if gpt[:8] != b'EFI PART':
        return
    lba, count, size = struct.unpack_from('<QII', gpt, 72)
    for i in range(count):
        first = struct.unpack_from('<Q', read(lba * 512 + i * size, size), 32)[0]
        if first:
            yield first * 512


start = next((o for o in super_offsets() if read(o + 4096, 4) == b'gDla'), None)
if start is None:
    sys.exit('no super partition in ' + src)
# Two geometry copies after 4096 reserved bytes, then the primary metadata.
base = start + 4096 + 2 * 4096
h = read(base, 256)
if h[:4] != b'0PLA':
    sys.exit('no metadata header')
header_size = struct.unpack_from('<I', h, 8)[0]
tables = read(base + header_size, struct.unpack_from('<I', h, 44)[0])


def table(off):
    o, n, sz = struct.unpack_from('<III', h, off)
    return [tables[o + i * sz:o + (i + 1) * sz] for i in range(n)]


extents = [struct.unpack_from('<QIQI', e) for e in table(92)]
for p in table(80):
    if p[:36].rstrip(b'\0').decode() != name:
        continue
    first, num = struct.unpack_from('<II', p, 40)
    with open(out, 'wb') as w:
        for sectors, kind, sector, _ in extents[first:first + num]:
            if kind != 0:
                sys.exit('not a linear extent')
            f.seek(start + sector * 512)
            left = sectors * 512
            while left:
                b = f.read(min(left, 1 << 20))
                w.write(b)
                left -= len(b)
    sys.exit(0)
sys.exit('no partition ' + name)
