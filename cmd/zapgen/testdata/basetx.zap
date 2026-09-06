# Test schema for the zapgen golden test.
#
# The X-chain BaseTx, at the offsets the Lux reference actually writes, plus
# the two records it points at and the owner group every fx output carries.
# Between them they cover all four list shapes the wire has:
#
#   Outs / Ins   pointer   — the element carries a tail, so the run is pointers
#   Addrs        fixed     — bytes_fixed[20] back to back
#   Sigs         number    — a run of u32
#   Entries      inline    — a record with no tail, back to back at its width

package xvm

type id32 = bytes_fixed[32]
type addr20 = bytes_fixed[20]

struct BaseTx {
    NetworkID    u32               @0
    BlockchainID id32              @8
    Outs         list<Transfer>    @40
    Ins          list<Spend>       @48
    Memo         bytes             @56
}

struct Transfer {
    AssetID id32  @0
    Output  bytes @32
}

struct Spend {
    TxID        id32  @0
    OutputIndex u32   @32
    AssetID     id32  @36
    Input       bytes @68
}

struct Owner {
    Locktime  u64          @0
    Threshold u32          @8
    Addrs     list<addr20> @12
    Sigs      list<u32>    @20
    Entries   list<Entry>  @28
}

struct Entry {
    Amount u64    @0
    Asset  id32   @8
    Flags  u32    @40
}
