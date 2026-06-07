// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap

import (
	"testing"
)

// Benchmark ZAP message building
func BenchmarkZAPBuild(b *testing.B) {
	builder := NewBuilder(256)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		builder.Reset()

		obj := builder.StartObject(64)
		obj.SetUint64(0, uint64(i))
		obj.SetUint64(8, 0xDEADBEEF)
		obj.SetUint32(16, 12345)
		obj.SetBool(20, true)
		obj.FinishAsRoot()

		_ = builder.Finish()
	}
}

// Benchmark ZAP message parsing (zero-copy)
func BenchmarkZAPParse(b *testing.B) {
	// Build a message once
	builder := NewBuilder(256)
	obj := builder.StartObject(64)
	obj.SetUint64(0, 12345678)
	obj.SetUint64(8, 0xDEADBEEF)
	obj.SetUint32(16, 12345)
	obj.SetBool(20, true)
	obj.FinishAsRoot()
	data := builder.Finish()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		msg, _ := Parse(data)
		root := msg.Root()
		_ = root.Uint64(0)
		_ = root.Uint64(8)
		_ = root.Uint32(16)
		_ = root.Bool(20)
	}
}

// Benchmark ZAP with text fields
func BenchmarkZAPBuildWithText(b *testing.B) {
	builder := NewBuilder(512)
	text := "Hello, World! This is a test message for benchmarking."

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		builder.Reset()

		textOffset := builder.WriteText(text)

		obj := builder.StartObject(32)
		obj.SetUint64(0, uint64(i))
		obj.SetUint32(8, uint32(len(text)))
		// Store text offset reference
		obj.SetUint32(12, uint32(textOffset))
		obj.FinishAsRoot()

		_ = builder.Finish()
	}
}

// Benchmark ZAP list building
func BenchmarkZAPBuildList(b *testing.B) {
	builder := NewBuilder(1024)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		builder.Reset()

		// Build list of 100 uint64s
		lb := builder.StartList(8)
		for j := 0; j < 100; j++ {
			lb.AddUint64(uint64(j))
		}
		listOffset, listLen := lb.Finish()

		obj := builder.StartObject(16)
		obj.SetList(0, listOffset, listLen)
		obj.FinishAsRoot()

		_ = builder.Finish()
	}
}

// Benchmark ZAP list reading
func BenchmarkZAPParseList(b *testing.B) {
	builder := NewBuilder(1024)

	lb := builder.StartList(8)
	for j := 0; j < 100; j++ {
		lb.AddUint64(uint64(j))
	}
	listOffset, listLen := lb.Finish()

	obj := builder.StartObject(16)
	obj.SetList(0, listOffset, listLen)
	obj.FinishAsRoot()
	data := builder.Finish()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		msg, _ := Parse(data)
		root := msg.Root()
		list := root.List(0)
		sum := uint64(0)
		for j := 0; j < list.Len(); j++ {
			sum += list.Uint64(j)
		}
		_ = sum
	}
}
