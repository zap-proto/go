# SPDX-License-Identifier: BSD-3-Clause-Eco
# The X-chain wire, as a schema. Offsets from node2's
# chains/rust/xvm/src/wire/containers.rs.
#
# An X vector on the wire carries two bytes before the ZAP message — a type
# byte and a shape byte — so a reader strips them before parsing. That
# framing is the chain's, not ZAP's, and it stays where it belongs: in the
# caller.

package xchain

type id32 = bytes_fixed[32]

# The outer envelope: the bytes that were signed, and the credentials over
# them. Unsigned is a whole ZAP message of its own — a Tx — carrying no
# framing byte, because the framing said Signed and a signature covers what
# it covers.
struct Signed {
    Unsigned        bytes @0
    CredentialCount u32   @8
    CredentialBytes bytes @12
}

# The transaction that was signed: which of the five kinds it is, and the
# spending envelope all five carry. Offsets from node2's
# chains/rust/xvm/src/txs/mod.rs, where the kind byte and the envelope sit at
# the same place in every kind.
#
# BaseTx does carry the two framing bytes, so a reader strips them before
# wrapping a Base. Signed's Unsigned does not. That asymmetry is the chain's
# and it is why one of the two is stripped and the other is not.
struct Tx {
    Kind   u8    @0
    BaseTx bytes @8
}

# The multi-asset spending envelope every X transaction carries.
#
# The outputs and the inputs are runs of four-byte offsets, each aiming at a
# container written earlier in the same buffer. The containers carry a tail,
# so they cannot ride in the run itself.
struct Base {
    NetworkID    u32                     @0
    BlockchainID id32                    @8
    Outs         list<ptr<TransferableOut>> @40
    Ins          list<ptr<TransferableIn>>  @48
    Memo         bytes                   @56
}

# One output going out: the asset it moves, and the fx envelope that says how.
# The X-chain settles many assets, so the container names the asset and the
# family byte travels on the inner envelope, where the polymorphism is.
struct TransferableOut {
    AssetID id32  @0
    Output  bytes @32
}

# One input coming in: the UTXO it spends, the asset, and the fx envelope.
struct TransferableIn {
    TxID        id32  @0
    OutputIndex u32   @32
    AssetID     id32  @36
    Input       bytes @68
}

# One X block. Offsets from chains/rust/xvm/src/block/mod.rs.
#
# TxLengths is a run of lengths, not of aims: the transactions are packed end
# to end in TxBlob and each length says how far the next one starts. Four
# bytes an element either way, which is why one name for both was survivable
# and wrong.
struct Block {
    Parent    id32      @0
    Height    u64       @32
    Time      u64       @40
    Root      id32      @48
    TxLengths list<u32> @80
    TxBlob    bytes     @88
}
