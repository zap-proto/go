// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
//
// zapgen emits this file verbatim as zap.rs. It is the Rust side of the ZAP
// wire, and it is the ONLY hand-maintained Rust in the toolchain: everything
// else a schema needs is emitted from the schema. Edit it here, in the
// generator, and every language consumer gets the fix at once.

//! ZAP — the wire, read without parsing.
//!
//! A 16-byte header names the root object's offset; every field is either
//! inline at a fixed offset or a relative pointer into the same buffer.
//!
//! ```text
//! +--------------------------------------------+
//! | Header (16 bytes)                          |
//! |  +- Magic   4B "ZAP\0"                     |
//! |  +- Version 2B  1 or 2                     |
//! |  +- Flags   2B                             |
//! |  +- Root    4B  offset of the root object  |
//! |  +- Size    4B  total message size         |
//! +--------------------------------------------+
//! | Data segment: objects, lists, byte tails   |
//! +--------------------------------------------+
//! ```
//!
//! Every integer is little-endian. Every pointer is relative to the field
//! that holds it, so a message can be copied anywhere without rewriting it.
//!
//! The reader never panics. Bytes arrive from whoever wants to send them, so
//! every accessor is bounds-checked and answers the zero value on a short or
//! crooked buffer. Two rules beyond plain bounds carry weight: a byte tail's
//! pointer is UNSIGNED, so a crafted offset cannot alias back into the fixed
//! section, and no pointer of any kind may land inside the 16-byte header,
//! whose contents are not a legitimate payload.

#![allow(dead_code)]

/// The header, in bytes.
pub const HEADER_SIZE: usize = 16;

/// The four bytes every message starts with.
pub const MAGIC: [u8; 4] = [b'Z', b'A', b'P', 0];

/// The original layout.
pub const VERSION1: u16 = 1;
/// Adds a one-byte discriminator at struct byte 0.
pub const VERSION2: u16 = 2;
/// What [`Builder::new`] stamps. Both versions are accepted on read.
pub const VERSION: u16 = VERSION1;

/// Objects and lists start on this boundary.
pub const ALIGNMENT: usize = 8;

pub const FLAG_NONE: u16 = 0;
pub const FLAG_COMPRESSED: u16 = 1 << 0;
pub const FLAG_ENCRYPTED: u16 = 1 << 1;
pub const FLAG_SIGNED: u16 = 1 << 2;

/// What a buffer can be refused for.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Error {
    InvalidMagic,
    InvalidVersion,
    BufferTooSmall,
}

impl core::fmt::Display for Error {
    fn fmt(&self, f: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        match self {
            Error::InvalidMagic => write!(f, "zap: invalid magic bytes"),
            Error::InvalidVersion => write!(f, "zap: unsupported version"),
            Error::BufferTooSmall => write!(f, "zap: buffer too small"),
        }
    }
}

impl std::error::Error for Error {}

fn u16_at(b: &[u8], at: usize) -> u16 {
    u16::from_le_bytes([b[at], b[at + 1]])
}

fn u32_at(b: &[u8], at: usize) -> u32 {
    u32::from_le_bytes([b[at], b[at + 1], b[at + 2], b[at + 3]])
}

// ------------------------------------------------------------------ read --

/// A parsed message: the buffer, trimmed to its declared size.
#[derive(Clone, Copy, Debug)]
pub struct Message<'a> {
    data: &'a [u8],
}

impl<'a> Message<'a> {
    /// Read the header and take the message. Nothing is copied and nothing is
    /// decoded; a message that passes here is one whose magic, version and
    /// declared size are sane, no more.
    pub fn parse(data: &'a [u8]) -> Result<Self, Error> {
        if data.len() < HEADER_SIZE {
            return Err(Error::BufferTooSmall);
        }
        if data[0..4] != MAGIC {
            return Err(Error::InvalidMagic);
        }
        let version = u16_at(data, 4);
        if version != VERSION1 && version != VERSION2 {
            return Err(Error::InvalidVersion);
        }
        let size = u32_at(data, 12) as usize;
        if size < HEADER_SIZE || size > data.len() {
            return Err(Error::BufferTooSmall);
        }
        Ok(Message {
            data: &data[..size],
        })
    }

    /// The message's own bytes.
    pub fn bytes(&self) -> &'a [u8] {
        self.data
    }

    pub fn size(&self) -> usize {
        self.data.len()
    }

    pub fn version(&self) -> u16 {
        u16_at(self.data, 4)
    }

    pub fn flags(&self) -> u16 {
        u16_at(self.data, 6)
    }

    /// The root object.
    pub fn root(&self) -> Object<'a> {
        Object {
            data: self.data,
            offset: u32_at(self.data, 8) as usize,
        }
    }
}

/// A view onto one object. Field offsets are relative to the object.
#[derive(Clone, Copy, Debug)]
pub struct Object<'a> {
    data: &'a [u8],
    offset: usize,
}

impl<'a> Object<'a> {
    /// The absent object: every field reads zero.
    pub fn null() -> Self {
        Object {
            data: &[],
            offset: 0,
        }
    }

    pub fn is_null(&self) -> bool {
        self.offset == 0
    }

    /// This object's absolute offset in the message.
    pub fn offset(&self) -> usize {
        self.offset
    }

    /// The message this object lives in.
    pub fn message(&self) -> Message<'a> {
        Message { data: self.data }
    }

    pub fn bool(&self, field: usize) -> bool {
        self.u8(field) != 0
    }

    pub fn u8(&self, field: usize) -> u8 {
        let pos = self.offset + field;
        if pos >= self.data.len() {
            return 0;
        }
        self.data[pos]
    }

    pub fn u16(&self, field: usize) -> u16 {
        let pos = self.offset + field;
        if pos + 2 > self.data.len() {
            return 0;
        }
        u16_at(self.data, pos)
    }

    pub fn u32(&self, field: usize) -> u32 {
        let pos = self.offset + field;
        if pos + 4 > self.data.len() {
            return 0;
        }
        u32_at(self.data, pos)
    }

    pub fn u64(&self, field: usize) -> u64 {
        let pos = self.offset + field;
        if pos + 8 > self.data.len() {
            return 0;
        }
        let mut w = [0u8; 8];
        w.copy_from_slice(&self.data[pos..pos + 8]);
        u64::from_le_bytes(w)
    }

    pub fn i8(&self, field: usize) -> i8 {
        self.u8(field) as i8
    }

    pub fn i16(&self, field: usize) -> i16 {
        self.u16(field) as i16
    }

    pub fn i32(&self, field: usize) -> i32 {
        self.u32(field) as i32
    }

    pub fn i64(&self, field: usize) -> i64 {
        self.u64(field) as i64
    }

    pub fn f32(&self, field: usize) -> f32 {
        f32::from_bits(self.u32(field))
    }

    pub fn f64(&self, field: usize) -> f64 {
        f64::from_bits(self.u64(field))
    }

    /// `n` bytes inline at `field` — an id, a signature, a public key.
    /// Empty if the span falls outside the buffer.
    pub fn bytes_fixed(&self, field: usize, n: usize) -> &'a [u8] {
        if n == 0 {
            return &[];
        }
        let pos = self.offset + field;
        if pos + n > self.data.len() {
            return &[];
        }
        &self.data[pos..pos + n]
    }

    /// The variable-length tail `field` points at.
    ///
    /// The pointer is UNSIGNED — a forward offset from the field's own
    /// position. A negative bit pattern arrives as a huge positive one and
    /// dies on the bounds check, which is what keeps a crafted tail from
    /// aliasing the fixed section or the header.
    pub fn bytes(&self, field: usize) -> &'a [u8] {
        let pos = self.offset + field;
        if pos + 4 > self.data.len() {
            return &[];
        }
        let rel = u32_at(self.data, pos) as usize;
        if rel == 0 {
            return &[];
        }
        let len_pos = pos + 4;
        if len_pos + 4 > self.data.len() {
            return &[];
        }
        let length = u32_at(self.data, len_pos) as usize;
        let abs = pos + rel;
        if abs < HEADER_SIZE {
            return &[];
        }
        if abs + length > self.data.len() {
            return &[];
        }
        &self.data[abs..abs + length]
    }

    /// A text tail as bytes. UTF-8 is not checked here; see [`Object::text`].
    pub fn text_bytes(&self, field: usize) -> &'a [u8] {
        self.bytes(field)
    }

    /// A text tail as a string. Bytes that are not UTF-8 read as "".
    pub fn text(&self, field: usize) -> &'a str {
        core::str::from_utf8(self.bytes(field)).unwrap_or("")
    }

    /// The nested object `field` points at.
    ///
    /// This pointer is SIGNED: a builder may finalize a nested object before
    /// its parent, so the payload can live earlier in the buffer. Nothing may
    /// point inside the header, whose bytes are not a payload.
    pub fn object(&self, field: usize) -> Object<'a> {
        let pos = self.offset + field;
        if pos + 4 > self.data.len() {
            return Object::null();
        }
        let rel = u32_at(self.data, pos) as i32;
        if rel == 0 {
            return Object::null();
        }
        let abs = pos as i64 + rel as i64;
        if abs < HEADER_SIZE as i64 || abs >= self.data.len() as i64 {
            return Object::null();
        }
        Object {
            data: self.data,
            offset: abs as usize,
        }
    }

    /// The list `field` points at. The pointer is SIGNED, as for an object.
    /// The declared length is clamped to the message size: a forged count
    /// must not hand a caller a four-billion-iteration loop.
    pub fn list(&self, field: usize) -> List<'a> {
        let pos = self.offset + field;
        if pos + 8 > self.data.len() {
            return List::null();
        }
        let rel = u32_at(self.data, pos) as i32;
        if rel == 0 {
            return List::null();
        }
        let length = u32_at(self.data, pos + 4) as usize;
        if length > self.data.len() {
            return List::null();
        }
        let abs = pos as i64 + rel as i64;
        if abs < HEADER_SIZE as i64 || abs >= self.data.len() as i64 {
            return List::null();
        }
        List {
            data: self.data,
            offset: abs as usize,
            length,
            present: true,
        }
    }

    /// The list `field` points at, whose elements are `stride` bytes wide.
    ///
    /// [`Object::list`] can do no better than "the count is no larger than the
    /// message", because the wire carries a start and a count and nothing
    /// about how wide an element is. A caller generated from a schema DOES
    /// know the width, and the tight bound is the one worth checking: a count
    /// that cannot fit `stride` bytes per element in what is left of the
    /// message is refused once, here, rather than surviving to be the trip
    /// count of every loop over it.
    ///
    /// A stride of 0 means "not stated", and answers exactly [`Object::list`].
    pub fn list_stride(&self, field: usize, stride: usize) -> List<'a> {
        let l = self.list(field);
        if stride == 0 || !l.present {
            return l;
        }
        if l.length > (self.data.len() - l.offset) / stride {
            return List::null();
        }
        l
    }
}

/// A view onto one list. What an element IS depends on which accessor is
/// asked — the wire carries a start, a count, and nothing else.
#[derive(Clone, Copy, Debug)]
pub struct List<'a> {
    data: &'a [u8],
    offset: usize,
    length: usize,
    present: bool,
}

impl<'a> List<'a> {
    /// The absent list: no elements, and `is_null`.
    pub fn null() -> Self {
        List {
            data: &[],
            offset: 0,
            length: 0,
            present: false,
        }
    }

    /// The wire-declared element count.
    pub fn len(&self) -> usize {
        self.length
    }

    pub fn is_empty(&self) -> bool {
        self.length == 0
    }

    /// True when the field held a null pointer or failed its bounds check.
    pub fn is_null(&self) -> bool {
        !self.present
    }

    pub fn u8(&self, i: usize) -> u8 {
        if i >= self.length {
            return 0;
        }
        let pos = self.offset + i;
        if pos >= self.data.len() {
            return 0;
        }
        self.data[pos]
    }

    pub fn u32(&self, i: usize) -> u32 {
        if i >= self.length {
            return 0;
        }
        let pos = self.offset + i * 4;
        if pos + 4 > self.data.len() {
            return 0;
        }
        u32_at(self.data, pos)
    }

    pub fn u64(&self, i: usize) -> u64 {
        if i >= self.length {
            return 0;
        }
        let pos = self.offset + i * 8;
        if pos + 8 > self.data.len() {
            return 0;
        }
        let mut w = [0u8; 8];
        w.copy_from_slice(&self.data[pos..pos + 8]);
        u64::from_le_bytes(w)
    }

    /// Element `i` of a fixed-stride list, as an object.
    pub fn object(&self, i: usize, elem_size: usize) -> Object<'a> {
        if i >= self.length {
            return Object::null();
        }
        Object {
            data: self.data,
            offset: self.offset + i * elem_size,
        }
    }

    /// The list's whole byte run — the shape a fixed-stride list is written in.
    pub fn bytes(&self) -> &'a [u8] {
        if !self.present || self.offset + self.length > self.data.len() {
            return &[];
        }
        &self.data[self.offset..self.offset + self.length]
    }

    /// Element `i` of a variable-element list: entries are a 4-byte
    /// little-endian length followed by that many bytes. O(i) — walking the
    /// whole list by index is O(n^2); use [`List::each_bytes`] for that.
    pub fn bytes_at(&self, i: usize) -> &'a [u8] {
        if i >= self.length {
            return &[];
        }
        let mut p = self.offset;
        for _ in 0..i {
            if p + 4 > self.data.len() {
                return &[];
            }
            p += 4 + u32_at(self.data, p) as usize;
        }
        if p + 4 > self.data.len() {
            return &[];
        }
        let sz = u32_at(self.data, p) as usize;
        let start = p + 4;
        let end = start + sz;
        if end > self.data.len() {
            return &[];
        }
        &self.data[start..end]
    }

    /// Element `i` of a variable-element list, parsed as its own message.
    pub fn object_at(&self, i: usize) -> Object<'a> {
        let b = self.bytes_at(i);
        match Message::parse(b) {
            Ok(m) => m.root(),
            Err(_) => Object::null(),
        }
    }

    /// Walk a variable-element list once, in order. `f` answers false to stop.
    /// The walk stops at the declared count or at the first entry that would
    /// read past the buffer, whichever comes first — the same place
    /// [`List::bytes_at`] starts answering empty.
    pub fn each_bytes<F: FnMut(usize, &'a [u8]) -> bool>(&self, mut f: F) {
        if !self.present {
            return;
        }
        let mut p = self.offset;
        for i in 0..self.length {
            if p + 4 > self.data.len() {
                return;
            }
            let sz = u32_at(self.data, p) as usize;
            let start = p + 4;
            let end = start + sz;
            if end > self.data.len() {
                return;
            }
            if !f(i, &self.data[start..end]) {
                return;
            }
            p = end;
        }
    }

    /// Walk a variable-element list once, each entry parsed as a message. An
    /// entry that does not parse yields the null object and the walk goes on.
    pub fn each<F: FnMut(usize, Object<'a>) -> bool>(&self, mut f: F) {
        self.each_bytes(|i, b| {
            let o = match Message::parse(b) {
                Ok(m) => m.root(),
                Err(_) => Object::null(),
            };
            f(i, o)
        });
    }
}

// ----------------------------------------------------------------- write --

/// Writes a message. Offsets handed back by [`Builder::start_object`] and
/// [`Builder::start_list`] are absolute positions in the buffer under
/// construction; a pointer field turns one into a relative offset when it is
/// set.
#[derive(Debug)]
pub struct Builder {
    buf: Vec<u8>,
    pos: usize,
    root_offset: usize,
}

impl Default for Builder {
    fn default() -> Self {
        Builder::new(256)
    }
}

impl Builder {
    /// A builder over a buffer of at least `capacity` bytes, header written,
    /// stamped [`VERSION`].
    pub fn new(capacity: usize) -> Self {
        Builder::with_version(capacity, VERSION)
    }

    /// A builder that stamps [`VERSION2`] — what the call envelope and the
    /// transport framing use.
    pub fn new_v2(capacity: usize) -> Self {
        Builder::with_version(capacity, VERSION2)
    }

    fn with_version(capacity: usize, version: u16) -> Self {
        let cap = if capacity < HEADER_SIZE {
            256
        } else {
            capacity
        };
        let mut buf = vec![0u8; cap];
        buf[0..4].copy_from_slice(&MAGIC);
        buf[4..6].copy_from_slice(&version.to_le_bytes());
        Builder {
            buf,
            pos: HEADER_SIZE,
            root_offset: 0,
        }
    }

    /// Start over on the same buffer.
    pub fn reset(&mut self) {
        self.pos = HEADER_SIZE;
        self.root_offset = 0;
    }

    /// Where the next byte lands.
    pub fn pos(&self) -> usize {
        self.pos
    }

    fn grow(&mut self, n: usize) {
        if self.pos + n <= self.buf.len() {
            return;
        }
        let mut new_cap = self.buf.len() * 2;
        if new_cap < self.pos + n {
            new_cap = self.pos + n;
        }
        self.buf.resize(new_cap, 0);
    }

    fn align(&mut self, alignment: usize) {
        let padding = (alignment - (self.pos % alignment)) % alignment;
        self.grow(padding);
        for _ in 0..padding {
            self.buf[self.pos] = 0;
            self.pos += 1;
        }
    }

    /// Extend the cursor to `end`, zero-filling. This is what makes a grown
    /// buffer emit the same bytes as one that never grew.
    fn ensure(&mut self, end: usize) {
        if end > self.pos {
            self.grow(end - self.pos);
            for i in self.pos..end {
                self.buf[i] = 0;
            }
            self.pos = end;
        }
    }

    /// Open an object whose fixed section is `data_size` bytes. The whole
    /// fixed section is reserved now, so a tail written afterwards lands past
    /// every fixed field instead of on top of one that has not been set yet.
    pub fn start_object<'a>(&mut self, data_size: usize) -> ObjectBuilder<'a> {
        self.align(ALIGNMENT);
        let start = self.pos;
        self.ensure(start + data_size);
        ObjectBuilder {
            start,
            size: data_size,
            deferred: Vec::new(),
        }
    }

    /// Open a list. What an element is depends on which `add_*` is called.
    pub fn start_list(&mut self) -> ListBuilder {
        self.align(ALIGNMENT);
        ListBuilder {
            start: self.pos,
            count: 0,
        }
    }

    /// Copy raw bytes into the buffer at the alignment boundary and answer
    /// where they landed.
    pub fn write_bytes(&mut self, data: &[u8]) -> usize {
        if data.is_empty() {
            return 0;
        }
        self.align(ALIGNMENT);
        let at = self.pos;
        self.grow(data.len());
        self.buf[at..at + data.len()].copy_from_slice(data);
        self.pos = at + data.len();
        at
    }

    /// Copy a finished message into this buffer and answer the offset of its
    /// ROOT OBJECT — which is what a nested-struct pointer must name, not
    /// where the copy begins.
    ///
    /// The distinction is the whole point. A finished message opens with a
    /// 16-byte header, so a pointer aimed at the start of the copy lands on
    /// the magic and a reader answers "ZAP" where the first field should be.
    /// Every pointer INSIDE the message is relative to the field that holds
    /// it, so the copy needs no rewriting: only the root has to be found, and
    /// it is written in the header the copy carries.
    ///
    /// Answers 0 — the null pointer — for anything that is not a message, so
    /// a caller may embed an absent field without asking first.
    pub fn embed(&mut self, msg: &[u8]) -> usize {
        if msg.len() < HEADER_SIZE {
            return 0;
        }
        let root = u32_at(msg, 8) as usize;
        if root < HEADER_SIZE || root >= msg.len() {
            return 0;
        }
        let at = self.write_bytes(msg);
        if at == 0 {
            return 0;
        }
        at + root
    }

    /// Name `offset` as the message root.
    pub fn set_root(&mut self, offset: usize) {
        self.root_offset = offset;
    }

    /// Seal the message: write the root offset and the total size.
    pub fn finish(mut self) -> Vec<u8> {
        let root = self.root_offset as u32;
        let size = self.pos as u32;
        self.buf[8..12].copy_from_slice(&root.to_le_bytes());
        self.buf[12..16].copy_from_slice(&size.to_le_bytes());
        self.buf.truncate(self.pos);
        self.buf
    }

    /// Seal the message with `flags` in the header.
    pub fn finish_with_flags(mut self, flags: u16) -> Vec<u8> {
        self.buf[6..8].copy_from_slice(&flags.to_le_bytes());
        self.finish()
    }

    fn put_u8(&mut self, at: usize, v: u8) {
        self.ensure(at + 1);
        self.buf[at] = v;
    }

    fn put_u16(&mut self, at: usize, v: u16) {
        self.ensure(at + 2);
        self.buf[at..at + 2].copy_from_slice(&v.to_le_bytes());
    }

    fn put_u32(&mut self, at: usize, v: u32) {
        self.ensure(at + 4);
        self.buf[at..at + 4].copy_from_slice(&v.to_le_bytes());
    }

    fn put_u64(&mut self, at: usize, v: u64) {
        self.ensure(at + 8);
        self.buf[at..at + 8].copy_from_slice(&v.to_le_bytes());
    }
}

/// A handle on one open object. Field offsets are relative to the object;
/// the buffer is passed in per call, so a list may be opened while the object
/// is still open — which is exactly what a schema with a list field needs.
#[derive(Debug)]
pub struct ObjectBuilder<'a> {
    start: usize,
    size: usize,
    deferred: Vec<(usize, &'a [u8])>,
}

impl<'a> ObjectBuilder<'a> {
    /// The object's absolute offset — what a pointer field or a root names.
    pub fn offset(&self) -> usize {
        self.start
    }

    pub fn set_bool(&mut self, b: &mut Builder, field: usize, v: bool) {
        self.set_u8(b, field, if v { 1 } else { 0 });
    }

    pub fn set_u8(&mut self, b: &mut Builder, field: usize, v: u8) {
        b.put_u8(self.start + field, v);
    }

    pub fn set_u16(&mut self, b: &mut Builder, field: usize, v: u16) {
        b.put_u16(self.start + field, v);
    }

    pub fn set_u32(&mut self, b: &mut Builder, field: usize, v: u32) {
        b.put_u32(self.start + field, v);
    }

    pub fn set_u64(&mut self, b: &mut Builder, field: usize, v: u64) {
        b.put_u64(self.start + field, v);
    }

    pub fn set_i8(&mut self, b: &mut Builder, field: usize, v: i8) {
        self.set_u8(b, field, v as u8);
    }

    pub fn set_i16(&mut self, b: &mut Builder, field: usize, v: i16) {
        self.set_u16(b, field, v as u16);
    }

    pub fn set_i32(&mut self, b: &mut Builder, field: usize, v: i32) {
        self.set_u32(b, field, v as u32);
    }

    pub fn set_i64(&mut self, b: &mut Builder, field: usize, v: i64) {
        self.set_u64(b, field, v as u64);
    }

    pub fn set_f32(&mut self, b: &mut Builder, field: usize, v: f32) {
        self.set_u32(b, field, v.to_bits());
    }

    pub fn set_f64(&mut self, b: &mut Builder, field: usize, v: f64) {
        self.set_u64(b, field, v.to_bits());
    }

    /// Copy `v` inline at `field` — a fixed-width byte slot. Empty is a
    /// no-op, leaving the slot's zeros.
    pub fn set_bytes_fixed(&mut self, b: &mut Builder, field: usize, v: &[u8]) {
        if v.is_empty() {
            return;
        }
        let at = self.start + field;
        b.ensure(at + v.len());
        b.buf[at..at + v.len()].copy_from_slice(v);
    }

    /// Point `field` at `v`. The length is written now and the bytes are
    /// written by [`ObjectBuilder::finish`], after the fixed section and
    /// after anything opened in between — so the layout does not depend on
    /// the order the fields happen to be set in.
    pub fn set_bytes(&mut self, b: &mut Builder, field: usize, v: &'a [u8]) {
        let field_abs = self.start + field;
        if v.is_empty() {
            b.put_u32(field_abs, 0);
            b.put_u32(field_abs + 4, 0);
            return;
        }
        b.put_u32(field_abs + 4, v.len() as u32);
        self.deferred.push((field, v));
    }

    /// Point `field` at `v`'s bytes.
    pub fn set_text(&mut self, b: &mut Builder, field: usize, v: &'a str) {
        self.set_bytes(b, field, v.as_bytes());
    }

    /// Point `field` at an object already written at `obj_offset`. Zero
    /// writes the null pointer.
    pub fn set_object(&mut self, b: &mut Builder, field: usize, obj_offset: usize) {
        let field_abs = self.start + field;
        if obj_offset == 0 {
            b.put_u32(field_abs, 0);
            return;
        }
        let rel = obj_offset as i64 - field_abs as i64;
        b.put_u32(field_abs, rel as i32 as u32);
    }

    /// Point `field` at a list already written at `list_offset` holding
    /// `length` elements. Either being zero writes the null pair.
    pub fn set_list(&mut self, b: &mut Builder, field: usize, list_offset: usize, length: usize) {
        let field_abs = self.start + field;
        if list_offset == 0 || length == 0 {
            b.put_u32(field_abs, 0);
            b.put_u32(field_abs + 4, 0);
            return;
        }
        let rel = list_offset as i64 - field_abs as i64;
        b.put_u32(field_abs, rel as i32 as u32);
        b.put_u32(field_abs + 4, length as u32);
    }

    /// Seal the object: write the tails and patch their pointers. Answers the
    /// object's offset.
    pub fn finish(&mut self, b: &mut Builder) -> usize {
        b.ensure(self.start + self.size);
        for (field, data) in self.deferred.drain(..) {
            let at = b.pos;
            b.grow(data.len());
            b.buf[at..at + data.len()].copy_from_slice(data);
            b.pos = at + data.len();
            let field_abs = self.start + field;
            let rel = at as i64 - field_abs as i64;
            b.put_u32(field_abs, rel as i32 as u32);
        }
        self.start
    }

    /// Seal the object and name it the message root.
    pub fn finish_as_root(&mut self, b: &mut Builder) -> usize {
        let at = self.finish(b);
        b.set_root(at);
        at
    }
}

/// A handle on one open list.
#[derive(Clone, Copy, Debug)]
pub struct ListBuilder {
    start: usize,
    count: usize,
}

impl ListBuilder {
    pub fn add_u8(&mut self, b: &mut Builder, v: u8) {
        b.grow(1);
        let p = b.pos;
        b.buf[p] = v;
        b.pos = p + 1;
        self.count += 1;
    }

    pub fn add_u32(&mut self, b: &mut Builder, v: u32) {
        b.grow(4);
        let p = b.pos;
        b.buf[p..p + 4].copy_from_slice(&v.to_le_bytes());
        b.pos = p + 4;
        self.count += 1;
    }

    pub fn add_u64(&mut self, b: &mut Builder, v: u64) {
        b.grow(8);
        let p = b.pos;
        b.buf[p..p + 8].copy_from_slice(&v.to_le_bytes());
        b.pos = p + 8;
        self.count += 1;
    }

    /// Append raw bytes. The count advances by the BYTE length — the element
    /// kind of a byte run or a fixed-stride record written whole.
    pub fn add_bytes(&mut self, b: &mut Builder, data: &[u8]) {
        b.grow(data.len());
        let p = b.pos;
        b.buf[p..p + data.len()].copy_from_slice(data);
        b.pos = p + data.len();
        self.count += data.len();
    }

    /// Append one variable-length entry: a 4-byte little-endian length, then
    /// the bytes. The count advances by ONE — the element kind of a list of
    /// nested objects or of variable byte payloads.
    pub fn add_object_bytes(&mut self, b: &mut Builder, data: &[u8]) {
        b.grow(4 + data.len());
        let p = b.pos;
        b.buf[p..p + 4].copy_from_slice(&(data.len() as u32).to_le_bytes());
        b.buf[p + 4..p + 4 + data.len()].copy_from_slice(data);
        b.pos = p + 4 + data.len();
        self.count += 1;
    }

    /// The list's start offset and its element count.
    pub fn finish(&self) -> (usize, usize) {
        (self.start, self.count)
    }

    /// The list's start offset, for a caller tracking the count itself.
    pub fn finish_offset(&self) -> usize {
        self.start
    }

    /// How many elements have been added.
    pub fn count(&self) -> usize {
        self.count
    }
}
