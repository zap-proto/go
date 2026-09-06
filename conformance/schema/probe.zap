# Every type the schema dialect has, in one struct, so the two backends are
# compared on all of them at once: each field is written by both builders from
# the same input and read back by both readers from the same bytes.
#
# The offsets are hand-placed, as the dialect requires: @N is a byte offset.

package probe

type id16 = bytes_fixed[16]

struct Inner {
    Tag u32 @0
    Val u64 @4
}

struct Wide {
    Flag  bool      @0
    A8    u8        @1
    A16   u16       @2
    A32   u32       @4
    A64   u64       @8
    S8    i8        @16
    S16   i16       @18
    S32   i32       @20
    S64   i64       @24
    F32   f32       @32
    F64   f64       @40
    Id    id16      @48
    Name  text      @64
    Blob  bytes     @72
    Items list<u8>  @80
    Child Inner     @88
}
