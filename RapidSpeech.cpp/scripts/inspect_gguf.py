import struct, sys

path = sys.argv[1]
with open(path, 'rb') as f:
    magic = struct.unpack('<I', f.read(4))[0]
    version = struct.unpack('<I', f.read(4))[0]
    n_tensors = struct.unpack('<Q', f.read(8))[0]
    n_kv = struct.unpack('<Q', f.read(8))[0]
    print(f'Magic: {hex(magic)}, Version: {version}, Tensors: {n_tensors}, KV: {n_kv}')
    for i in range(n_kv):
        key_len = struct.unpack('<Q', f.read(8))[0]
        key = f.read(key_len).decode('utf-8')
        vtype = struct.unpack('<I', f.read(4))[0]
        if vtype == 8:
            val_len = struct.unpack('<Q', f.read(8))[0]
            val = f.read(val_len).decode('utf-8')
            print(f'  KV: {key} = "{val}"')
        elif vtype == 5:
            val = struct.unpack('<i', f.read(4))[0]
            print(f'  KV: {key} = {val}')
    for i in range(min(15, n_tensors)):
        name_len = struct.unpack('<Q', f.read(8))[0]
        name = f.read(name_len).decode('utf-8')
        n_dims = struct.unpack('<I', f.read(4))[0]
        dims = [struct.unpack('<Q', f.read(8))[0] for _ in range(n_dims)]
        dtype = struct.unpack('<I', f.read(4))[0]
        offset = struct.unpack('<Q', f.read(8))[0]
        print(f'  {name}: dims={dims}, dtype={dtype}')
