// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"bytes"
	"encoding/binary"
	"testing"

	zap "github.com/zap-proto/go"
)

func TestRequestRoundTrip(t *testing.T) {
	in := Call{
		Method:    3,
		PromiseID: 42,
		Target:    NoTarget,
		Cap:       []byte("capability-bytes"),
		Payload:   []byte{0x01, 0x02, 0x03, 0x04, 0x05},
	}
	msg := BuildRequest(in)
	got, err := ParseRequest(msg)
	if err != nil {
		t.Fatalf("ParseRequest: %v", err)
	}
	if got.Method != in.Method || got.PromiseID != in.PromiseID || got.Target != in.Target {
		t.Errorf("scalar mismatch: got %+v want %+v", got, in)
	}
	if !bytes.Equal(got.Cap, in.Cap) {
		t.Errorf("Cap = %q, want %q", got.Cap, in.Cap)
	}
	if !bytes.Equal(got.Payload, in.Payload) {
		t.Errorf("Payload = %x, want %x", got.Payload, in.Payload)
	}
}

func TestResponseRoundTrip(t *testing.T) {
	body := []byte("result-body-bytes")
	msg := BuildResponse(StatusOK, 42, body)
	got, err := ParseResponse(msg)
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	if got.Status != StatusOK {
		t.Errorf("Status = %d, want %d", got.Status, StatusOK)
	}
	if got.PromiseID != 42 {
		t.Errorf("PromiseID = %d, want 42", got.PromiseID)
	}
	if !bytes.Equal(got.Body, body) {
		t.Errorf("Body = %q, want %q", got.Body, body)
	}
}

func TestEmptyPayloads(t *testing.T) {
	// A void method: empty cap and empty payload must round-trip.
	msg := BuildRequest(Call{Method: 1})
	got, err := ParseRequest(msg)
	if err != nil {
		t.Fatalf("ParseRequest: %v", err)
	}
	if got.Method != 1 {
		t.Errorf("Method = %d, want 1", got.Method)
	}
	if len(got.Cap) != 0 || len(got.Payload) != 0 {
		t.Errorf("expected empty cap/payload, got cap=%x payload=%x", got.Cap, got.Payload)
	}
}

// TestEnvelopeWireShape pins the on-wire envelope so it stays
// byte-compatible with the other language runtimes' transport envelopes
// (the TS envelope.ts mirrors these exact constants):
//   - v2 header (byte 4 == 2)
//   - flags high byte == MsgTypeRouterBase
//   - Method at request struct offset 0; Status at response struct offset 0.
func TestEnvelopeWireShape(t *testing.T) {
	msg := BuildRequest(Call{Method: 7, PromiseID: 9})
	if version := binary.LittleEndian.Uint16(msg[4:6]); version != zap.Version2 {
		t.Errorf("request header version = %d, want Version2", version)
	}
	flags := binary.LittleEndian.Uint16(msg[6:8])
	if flags>>8 != MsgTypeRouterBase {
		t.Errorf("request flags msgType = %d, want %d", flags>>8, MsgTypeRouterBase)
	}
	m, err := zap.Parse(msg)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := m.Root().Uint32(reqMethodOff); got != 7 {
		t.Errorf("Method@0 = %d, want 7", got)
	}

	resp := BuildResponse(StatusForbidden, 9, nil)
	if version := binary.LittleEndian.Uint16(resp[4:6]); version != zap.Version2 {
		t.Errorf("response header version = %d, want Version2", version)
	}
	rm, err := zap.Parse(resp)
	if err != nil {
		t.Fatalf("Parse response: %v", err)
	}
	if got := rm.Root().Uint32(respStatusOff); got != StatusForbidden {
		t.Errorf("Status@0 = %d, want %d", got, StatusForbidden)
	}
}

// TestParseRejectsGarbage confirms the parse functions surface a framing
// error rather than panicking on a non-ZAP buffer.
func TestParseRejectsGarbage(t *testing.T) {
	if _, err := ParseRequest([]byte("not a zap message")); err == nil {
		t.Errorf("expected error from garbage request")
	}
	if _, err := ParseResponse(make([]byte, 4)); err == nil {
		t.Errorf("expected error from short response")
	}
}
