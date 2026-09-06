# The P-chain spending envelope, as the corpus at
# node2/conformance/corpus/vectors.tsv carries it: the root object of the
# unsigned message that leads every signed P transaction.
#
# Offsets are the ones the hand-written readers use (node2
# chains/cpp/platformvm txs.hpp kOff*, chains/rust platformvm txs.rs). The
# fixed section is 77 bytes.
#
# The four list fields are records of a fixed stride laid down back to back —
# 72 bytes per output, 96 per input, 20 per address, 4 per signature index.
# The dialect cannot say a stride yet, so `list<u8>` here carries what it can
# say: the pointer and the element count. Reading an element is the gap
# reported alongside this backend, and it is a gap in the DIALECT, in both
# languages at once, not in either emitter.

package pchain

type id32 = bytes_fixed[32]

struct Spend {
    Kind         u8        @0
    NetworkID    u32       @1
    BlockchainID id32      @5
    Outs         list<u8>  @37
    OwnerAddrs   list<u8>  @45
    Ins          list<u8>  @53
    SigIndices   list<u8>  @61
    Memo         bytes     @69
}
