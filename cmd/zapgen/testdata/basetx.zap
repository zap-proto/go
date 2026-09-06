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

# What one entry of those lists is. The X wire holds its outputs and inputs
# as a run of relative offsets into the same buffer, four bytes each; a list
# element has to be declared to have a width at all.
struct TransferableOutput {
    Offset u32 @0
}

struct TransferableInput {
    Offset u32 @0
}
