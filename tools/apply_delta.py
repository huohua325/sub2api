import bz2, hashlib, os, pathlib, re, sys, tempfile

source, delta, target, before_hash, after_hash = sys.argv[1:]
old = pathlib.Path(source).read_bytes()
assert hashlib.sha256(old).hexdigest() == before_hash, 'source hash mismatch'
p = pathlib.Path(delta).read_bytes()
assert p[:8] == b'BSDIFF40'
def integer(b):
    value = int.from_bytes(b, 'little') & ((1 << 63)-1)
    return -value if b[7] & 128 else value
control_size, difference_size, result_size = [integer(p[i:i+8]) for i in (8,16,24)]
assert control_size >= 0 and difference_size >= 0 and 0 < result_size < 500_000_000
control = bz2.decompress(p[32:32+control_size])
difference = bz2.decompress(p[32+control_size:32+control_size+difference_size])
extra = bz2.decompress(p[32+control_size+difference_size:])
result = bytearray(result_size)
oldpos = newpos = ctrlpos = diffpos = extrapos = 0
while newpos < result_size:
    assert ctrlpos+24 <= len(control)
    x,y,z = [integer(control[ctrlpos+i:ctrlpos+i+8]) for i in (0,8,16)]
    ctrlpos += 24
    assert x >= 0 and y >= 0 and newpos+x+y <= result_size and diffpos+x <= len(difference) and extrapos+y <= len(extra)
    first, last = max(0, -oldpos), min(x, len(old)-oldpos)
    if first < last:
        result[newpos+first:newpos+last] = old[oldpos+first:oldpos+last]
    # Most unchanged executable bytes have a zero difference. Scan those in C
    # and only add nonzero runs, retaining BSDIFF's byte-wise modulo arithmetic.
    for match in re.finditer(rb'[^\x00]+', memoryview(difference)[diffpos:diffpos+x]):
        position = newpos+match.start()
        for i, value in enumerate(match.group()):
            result[position+i] = (result[position+i]+value) & 255
    newpos += x
    oldpos += x
    diffpos += x
    result[newpos:newpos+y] = extra[extrapos:extrapos+y]
    newpos += y
    extrapos += y
    oldpos += z
assert hashlib.sha256(result).hexdigest() == after_hash, 'target hash mismatch'
dst = pathlib.Path(target)
assert dst.parent.resolve() == pathlib.Path(delta).parent.resolve(), 'target outside release directory'
# Never truncate a predictable path: it may link to the live program or a backup.
descriptor, temporary_name = tempfile.mkstemp(prefix='.'+dst.name+'-', dir=dst.parent)
temporary = pathlib.Path(temporary_name)
try:
    with os.fdopen(descriptor, 'wb') as stream:
        stream.write(result)
    os.chmod(temporary, 0o755)
    os.replace(temporary, dst)
finally:
    temporary.unlink(missing_ok=True)
print('verified_target='+str(dst)+' sha256='+after_hash)
