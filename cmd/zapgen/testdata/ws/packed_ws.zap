# Whitespace-significant twin of testdata/packed.zap.
# No braces, no @N offsets: blocks are opened by indentation and field
# byte-offsets are auto-assigned from each type's slot width. Desugars to
# the SAME brace source as packed.zap and must generate identical output.

package packed

type id32 = bytes_fixed[32]

struct Head
    Kind    u8
    Amount  u64
    Asset   id32
    Sigs    list<u32>
    Leaves  list<Leaf>
    Memo    bytes

struct Leaf
    Weight u64
    Index  u32
