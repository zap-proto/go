// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap

import (
	"testing"
)

func TestBuilder(t *testing.T) {
	b := NewBuilder(256)

	// Write some text data first
	textOffset := b.WriteText("hello world")

	// Build a simple object
	ob := b.StartObject(24) // 24 bytes for our fields
	ob.SetUint32(0, 42)
	ob.SetUint64(8, 0xDEADBEEF)
	ob.SetBool(16, true)
	ob.FinishAsRoot()

	data := b.Finish()

	// Parse it back
	msg, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	root := msg.Root()

	if got := root.Uint32(0); got != 42 {
		t.Errorf("Uint32(0) = %d, want 42", got)
	}

	if got := root.Uint64(8); got != 0xDEADBEEF {
		t.Errorf("Uint64(8) = %x, want DEADBEEF", got)
	}

	if got := root.Bool(16); !got {
		t.Errorf("Bool(16) = %v, want true", got)
	}

	_ = textOffset
}

func TestPrimitives(t *testing.T) {
	b := NewBuilder(256)

	ob := b.StartObject(64)
	ob.SetInt8(0, -42)
	ob.SetInt16(2, -1000)
	ob.SetInt32(4, -100000)
	ob.SetInt64(8, -1000000000)
	ob.SetUint8(16, 255)
	ob.SetUint16(18, 65535)
	ob.SetUint32(20, 4294967295)
	ob.SetUint64(24, 18446744073709551615)
	ob.SetFloat32(32, 3.14)
	ob.SetFloat64(40, 2.718281828)
	ob.FinishAsRoot()

	data := b.Finish()
	msg, _ := Parse(data)
	root := msg.Root()

	if got := root.Int8(0); got != -42 {
		t.Errorf("Int8 = %d, want -42", got)
	}
	if got := root.Int16(2); got != -1000 {
		t.Errorf("Int16 = %d, want -1000", got)
	}
	if got := root.Int32(4); got != -100000 {
		t.Errorf("Int32 = %d, want -100000", got)
	}
	if got := root.Int64(8); got != -1000000000 {
		t.Errorf("Int64 = %d, want -1000000000", got)
	}
	if got := root.Uint8(16); got != 255 {
		t.Errorf("Uint8 = %d, want 255", got)
	}
	if got := root.Uint16(18); got != 65535 {
		t.Errorf("Uint16 = %d, want 65535", got)
	}
	if got := root.Uint32(20); got != 4294967295 {
		t.Errorf("Uint32 = %d, want 4294967295", got)
	}
	if got := root.Uint64(24); got != 18446744073709551615 {
		t.Errorf("Uint64 = %d, want max uint64", got)
	}
}

func TestList(t *testing.T) {
	b := NewBuilder(256)

	// Write a list of uint32s
	lb := b.StartList(4)
	lb.AddUint32(100)
	lb.AddUint32(200)
	lb.AddUint32(300)
	listOffset, listLen := lb.Finish()

	// Build object referencing the list
	ob := b.StartObject(16)
	ob.SetUint32(0, 999)
	ob.SetList(4, listOffset, listLen)
	ob.FinishAsRoot()

	data := b.Finish()
	msg, _ := Parse(data)
	root := msg.Root()

	if got := root.Uint32(0); got != 999 {
		t.Errorf("Uint32(0) = %d, want 999", got)
	}

	list := root.List(4)
	if list.Len() != 3 {
		t.Errorf("List.Len() = %d, want 3", list.Len())
	}

	if got := list.Uint32(0); got != 100 {
		t.Errorf("List[0] = %d, want 100", got)
	}
	if got := list.Uint32(1); got != 200 {
		t.Errorf("List[1] = %d, want 200", got)
	}
	if got := list.Uint32(2); got != 300 {
		t.Errorf("List[2] = %d, want 300", got)
	}
}

func TestByteList(t *testing.T) {
	b := NewBuilder(256)

	lb := b.StartList(1)
	lb.AddBytes([]byte("hello"))
	listOffset, listLen := lb.Finish()

	ob := b.StartObject(16)
	ob.SetList(0, listOffset, listLen)
	ob.FinishAsRoot()

	data := b.Finish()
	msg, _ := Parse(data)
	root := msg.Root()

	list := root.List(0)
	if got := string(list.Bytes()); got != "hello" {
		t.Errorf("List.Bytes() = %q, want %q", got, "hello")
	}
}

func TestNestedObject(t *testing.T) {
	b := NewBuilder(256)

	// Build inner object
	inner := b.StartObject(8)
	inner.SetUint32(0, 111)
	inner.SetUint32(4, 222)
	innerOffset := inner.Finish()

	// Build outer object
	outer := b.StartObject(16)
	outer.SetUint32(0, 333)
	outer.SetObject(4, innerOffset)
	outer.FinishAsRoot()

	data := b.Finish()
	msg, _ := Parse(data)
	root := msg.Root()

	if got := root.Uint32(0); got != 333 {
		t.Errorf("outer.Uint32(0) = %d, want 333", got)
	}

	innerObj := root.Object(4)
	if innerObj.IsNull() {
		t.Fatal("inner object is null")
	}

	if got := innerObj.Uint32(0); got != 111 {
		t.Errorf("inner.Uint32(0) = %d, want 111", got)
	}
	if got := innerObj.Uint32(4); got != 222 {
		t.Errorf("inner.Uint32(4) = %d, want 222", got)
	}
}

func TestTextRoundTrip(t *testing.T) {
	b := NewBuilder(256)

	// Build object with text fields using SetText
	ob := b.StartObject(24) // id(uint32=4) + name(text=8) + age(int32=4) => 16, aligned to 24
	ob.SetUint32(0, 42)
	ob.SetText(4, "Alice")
	ob.SetInt32(12, 30)
	ob.FinishAsRoot()

	data := b.Finish()
	msg, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	root := msg.Root()

	if got := root.Uint32(0); got != 42 {
		t.Errorf("Uint32(0) = %d, want 42", got)
	}
	if got := root.Text(4); got != "Alice" {
		t.Errorf("Text(4) = %q, want %q", got, "Alice")
	}
	if got := root.Int32(12); got != 30 {
		t.Errorf("Int32(12) = %d, want 30", got)
	}
}

func TestMultipleTextFields(t *testing.T) {
	b := NewBuilder(256)

	ob := b.StartObject(24) // 3 text fields * 8 bytes = 24
	ob.SetText(0, "hello")
	ob.SetText(8, "world")
	ob.SetText(16, "!")
	ob.FinishAsRoot()

	data := b.Finish()
	msg, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	root := msg.Root()

	if got := root.Text(0); got != "hello" {
		t.Errorf("Text(0) = %q, want %q", got, "hello")
	}
	if got := root.Text(8); got != "world" {
		t.Errorf("Text(8) = %q, want %q", got, "world")
	}
	if got := root.Text(16); got != "!" {
		t.Errorf("Text(16) = %q, want %q", got, "!")
	}
}

func TestNestedObjectWithText(t *testing.T) {
	b := NewBuilder(512)

	// Build inner object with text
	inner := b.StartObject(16) // text(8) + uint32(4) = 12, aligned to 16
	inner.SetText(0, "inner-text")
	inner.SetUint32(8, 999)
	innerOffset := inner.Finish()

	// Build outer object with text + nested
	outer := b.StartObject(16) // text(8) + object(4) = 12, aligned to 16
	outer.SetText(0, "outer-text")
	outer.SetObject(8, innerOffset)
	outer.FinishAsRoot()

	data := b.Finish()
	msg, _ := Parse(data)
	root := msg.Root()

	if got := root.Text(0); got != "outer-text" {
		t.Errorf("outer.Text(0) = %q, want %q", got, "outer-text")
	}

	innerObj := root.Object(8)
	if innerObj.IsNull() {
		t.Fatal("inner object is null")
	}
	if got := innerObj.Text(0); got != "inner-text" {
		t.Errorf("inner.Text(0) = %q, want %q", got, "inner-text")
	}
	if got := innerObj.Uint32(8); got != 999 {
		t.Errorf("inner.Uint32(8) = %d, want 999", got)
	}
}

func TestInvalidMagic(t *testing.T) {
	data := []byte("INVALID_MAGIC___")
	_, err := Parse(data)
	if err != ErrInvalidMagic {
		t.Errorf("expected ErrInvalidMagic, got %v", err)
	}
}

func TestBufferTooSmall(t *testing.T) {
	_, err := Parse([]byte{1, 2, 3})
	if err != ErrBufferTooSmall {
		t.Errorf("expected ErrBufferTooSmall, got %v", err)
	}
}

func TestSchema(t *testing.T) {
	// Define a schema
	schema := NewSchema("test")

	person := NewStructBuilder("Person").
		Uint32("id").
		Text("name").
		Int32("age").
		Bool("active").
		Build()

	schema.AddStruct(person)

	// Verify the struct
	if person.Size != 24 { // Aligned to 8
		t.Errorf("Person.Size = %d, want 24", person.Size)
	}

	if len(person.Fields) != 4 {
		t.Errorf("Person has %d fields, want 4", len(person.Fields))
	}

	// Check field offsets
	expected := map[string]int{
		"id":     0,
		"name":   4,
		"age":    12,
		"active": 16,
	}

	for _, f := range person.Fields {
		if exp, ok := expected[f.Name]; ok {
			if f.Offset != exp {
				t.Errorf("Field %s offset = %d, want %d", f.Name, f.Offset, exp)
			}
		}
	}
}

func BenchmarkParse(b *testing.B) {
	builder := NewBuilder(256)
	ob := builder.StartObject(24)
	ob.SetUint64(0, 12345)
	ob.SetUint64(8, 67890)
	ob.SetUint64(16, 11111)
	ob.FinishAsRoot()
	data := builder.Finish()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		msg, _ := Parse(data)
		root := msg.Root()
		_ = root.Uint64(0)
		_ = root.Uint64(8)
		_ = root.Uint64(16)
	}
}

func BenchmarkBuild(b *testing.B) {
	b.ReportAllocs()

	builder := NewBuilder(256)
	for i := 0; i < b.N; i++ {
		builder.Reset()
		ob := builder.StartObject(24)
		ob.SetUint64(0, 12345)
		ob.SetUint64(8, 67890)
		ob.SetUint64(16, 11111)
		ob.FinishAsRoot()
		_ = builder.Finish()
	}
}

// TestAbsentObjectReadsZero — the absent object is what Object() and List()
// answer for a pointer they refuse, so a hostile message routes every
// downstream accessor onto it. Reading one must answer the zero value, not
// dereference a message that is not there.
func TestAbsentObjectReadsZero(t *testing.T) {
	var o Object
	if !o.IsNull() {
		t.Fatal("the zero Object is the absent one")
	}
	if o.Bool(0) || o.Uint8(0) != 0 || o.Uint16(0) != 0 || o.Uint32(0) != 0 || o.Uint64(0) != 0 {
		t.Error("an absent object's scalars are not zero")
	}
	if o.Int8(0) != 0 || o.Int16(0) != 0 || o.Int32(0) != 0 || o.Int64(0) != 0 {
		t.Error("an absent object's signed scalars are not zero")
	}
	if o.Float32(0) != 0 || o.Float64(0) != 0 {
		t.Error("an absent object's floats are not zero")
	}
	if o.Text(0) != "" || o.Bytes(0) != nil || o.BytesFixed(0, 4) != nil {
		t.Error("an absent object's tails are not empty")
	}
	if !o.Object(0).IsNull() || o.List(0).Len() != 0 {
		t.Error("an absent object's pointers do not lead to absent things")
	}
}

// TestRefusedPointerReadsZero walks the path a real message takes there: a
// nested-object pointer the reader refuses, whose fields are then read.
func TestRefusedPointerReadsZero(t *testing.T) {
	b := NewBuilder(64)
	ob := b.StartObject(8)
	ob.SetUint32(0, 0) // a null nested pointer
	ob.FinishAsRoot()
	msg, err := Parse(b.Finish())
	if err != nil {
		t.Fatal(err)
	}
	nested := msg.Root().Object(0)
	if !nested.IsNull() {
		t.Fatal("a zero pointer must lead to the absent object")
	}
	if nested.Uint32(0) != 0 || nested.Text(4) != "" {
		t.Error("reading through a refused pointer must answer zero")
	}
}

// TestEmbedNamesTheRoot — a message copied into another buffer keeps every
// internal pointer, because they are relative; what has to be found again is
// its root. A pointer to the head of the copy names the copy's header, and a
// reader would answer the magic bytes where the first field belongs.
func TestEmbedNamesTheRoot(t *testing.T) {
	inner := NewBuilder(64)
	io := inner.StartObject(8)
	io.SetUint32(0, 0xcafebabe)
	io.SetUint32(4, 7)
	io.FinishAsRoot()
	sub := inner.Finish()

	outer := NewBuilder(128)
	oo := outer.StartObject(4)
	oo.SetObject(0, outer.Embed(sub))
	oo.FinishAsRoot()

	msg, err := Parse(outer.Finish())
	if err != nil {
		t.Fatal(err)
	}
	nested := msg.Root().Object(0)
	if nested.IsNull() {
		t.Fatal("the embedded message is not reachable")
	}
	if got := nested.Uint32(0); got != 0xcafebabe {
		t.Errorf("first field of the embedded message = %#x, want 0xcafebabe", got)
	}
	if got := nested.Uint32(4); got != 7 {
		t.Errorf("second field of the embedded message = %d, want 7", got)
	}
}

// TestEmbedRefusesWhatIsNotAMessage — an absent or malformed field embeds as
// the null pointer, so a caller need not ask first.
func TestEmbedRefusesWhatIsNotAMessage(t *testing.T) {
	b := NewBuilder(64)
	for _, bad := range [][]byte{nil, {}, []byte("short"), make([]byte, HeaderSize)} {
		if at := b.Embed(bad); at != 0 {
			t.Errorf("Embed(%d bytes) = %d, want 0", len(bad), at)
		}
	}
}
