# SPDX-License-Identifier: BSD-3-Clause-Eco
# Every type the schema dialect has, in one struct, so the proof exercises
# each one rather than the handful a real chain happens to use.

package kitchen

struct Leaf {
    Tag  u32  @0
    Note text @4
}

struct All {
    Flag  bool            @0
    A8    u8              @1
    A16   u16             @2
    A32   u32             @4
    A64   u64             @8
    S8    i8              @16
    S16   i16             @18
    S32   i32             @20
    S64   i64             @24
    F32   f32             @32
    F64   f64             @40
    Name  text            @48
    Blob  bytes           @56
    Id    bytes_fixed[16] @64
    Items list<Leaf>      @80
    Inner Leaf            @88
}
