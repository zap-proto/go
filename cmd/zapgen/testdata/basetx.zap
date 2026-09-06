# Test schema for the zapgen golden test.
# Matches the canonical example in the zapgen spec.

package xvm

type id32 = bytes_fixed[32]

struct BaseTx {
    NetworkID    u32                       @0
    BlockchainID id32                      @4
    Outs         list<TransferableOutput>  @36
    Ins          list<TransferableInput>   @44
    Memo         bytes                     @52
}

# The two the envelope points at. A schema is one closed set of names: a
# `list<T>` whose T is declared somewhere else emits an UNTYPED list and says
# nothing about it, so the element types belong here beside the struct that
# holds them.
struct TransferableOutput {
    Asset  id32  @0
    Output bytes @32
}

struct TransferableInput {
    TxID   id32  @0
    Index  u32   @32
    Asset  id32  @36
    Input  bytes @68
}
