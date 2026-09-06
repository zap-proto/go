# SPDX-License-Identifier: BSD-3-Clause-Eco
# The P-chain wire, as a schema.
#
# These offsets are not invented here: they are the consensus offsets the
# node2 P-chain states by hand, in chains/rust/platformvm/src/txs.rs and
# block.rs, and again in chains/cpp. Written once as a schema, both the Go
# and the Rust readers below come out of zapgen instead of out of a person.
#
# The four-byte pads are real. A record's stride is eight-aligned, so the
# last field of Out ends at 68 and the next record starts at 72; naming the
# gap is what makes the emitted SIZE the stride the wire actually uses.

package pchain

type id32 = bytes_fixed[32]

# Every P transaction opens with this: a kind byte, the chain it is for,
# what it spends and what it makes.
struct Spend {
    Kind         u8         @0
    NetworkID    u32        @1
    BlockchainID id32       @5
    Outs         list<Out>  @37
    OwnerAddrs   list<Addr> @45
    Ins          list<In>   @53
    SigIndices   list<Sig>  @61
    Memo         bytes      @69
}

struct Out {
    Asset     id32           @0
    StakeLock u64            @32
    Amount    u64            @40
    Threshold u32            @48
    OwnerLock u64            @52
    AddrStart u32            @60
    AddrCount u32            @64
    Pad       bytes_fixed[4] @68
}

struct In {
    TxID        id32           @0
    OutputIndex u32            @32
    Asset       id32           @36
    StakeLock   u64            @68
    Amount      u64            @76
    SigStart    u32            @84
    SigCount    u32            @88
    Pad         bytes_fixed[4] @92
}

struct Addr {
    Bytes bytes_fixed[20] @0
}

struct Sig {
    Index u32 @0
}

# A P block. Decided blocks stop at 49 bytes and standard ones at 65; a
# field past the end of the object reads zero, which is what both runtimes
# answer and what makes one view serve all three shapes.
struct Block {
    Kind       u8        @0
    Parent     id32      @1
    Height     u64       @33
    Time       u64       @41
    TxLengths  list<Sig> @49
    TxBlob     bytes     @57
    ProposalTx bytes     @65
}
