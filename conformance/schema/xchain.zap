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
# them.
struct Signed {
    Unsigned        bytes @0
    CredentialCount u32   @8
    CredentialBytes bytes @12
}

# The multi-asset spending envelope every X transaction carries.
struct Base {
    NetworkID    u32       @0
    BlockchainID id32      @8
    Outs         list<Ptr> @40
    Ins          list<Ptr> @48
    Memo         bytes     @56
}

# An element of an X pointer list: one relative offset into the same buffer.
struct Ptr {
    Offset u32 @0
}

# One X block. Offsets from chains/rust/xvm/src/block/mod.rs.
struct Block {
    Parent    id32      @0
    Height    u64       @32
    Time      u64       @40
    Root      id32      @48
    TxLengths list<Ptr> @80
    TxBlob    bytes     @88
}
