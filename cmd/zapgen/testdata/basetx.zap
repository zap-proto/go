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
