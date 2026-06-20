# Whitespace-significant twin of testdata/basetx.zap.
# No braces, no @N offsets: blocks are opened by indentation and field
# byte-offsets are auto-assigned from each type's slot width. Desugars to
# the SAME brace source as basetx.zap and must generate identical Go.

package xvm

type id32 = bytes_fixed[32]

struct BaseTx
    NetworkID    u32
    BlockchainID id32
    Outs         list<TransferableOutput>
    Ins          list<TransferableInput>
    Memo         bytes
