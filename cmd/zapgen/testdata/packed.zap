# The auto-offset twin's brace source.
#
# Nothing here says @N: the field widths pack in declaration order, which is
# what testdata/ws/packed_ws.zap has to reproduce from indentation alone.

package packed

type id32 = bytes_fixed[32]

struct Head {
    Kind    u8            @0
    Amount  u64           @1
    Asset   id32          @9
    Sigs    list<u32>     @41
    Leaves  list<Leaf>    @49
    Memo    bytes         @57
}

struct Leaf {
    Weight u64  @0
    Index  u32  @8
}
