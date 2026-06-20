// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package rpc is the ZAP call envelope: the msgType + method + capability
// framing that carries an interface method call and its response over a
// ZAP transport.
//
// This is the canonical wire contract; the TypeScript runtime's
// envelope.ts mirrors these exact offsets and sizes byte-for-byte, so a
// request built here is decoded by the TS ParseRequest and a response
// built by the TS BuildResponse is decoded by ParseResponse here (and
// vice versa). The generated typed Client and abstract Server (zapgen
// `interface` emission) are thin wrappers over BuildRequest /
// ParseRequest / BuildResponse / ParseResponse.
//
// Request object (fixed size 28):
//
//	Method    u32   @0    which interface method (the .zap ordinal, 1-based)
//	PromiseID u32   @4    caller-assigned id this call's answer resolves to
//	Target    u32   @8    promise this call pipelines off (0 = root)
//	Cap       bytes @12   opaque capability buffer (cap.Cap bytes; may be empty)
//	Payload   bytes @20   ZAP-encoded method params
//
// Response object (fixed size 20):
//
//	Status    u32   @0    200 ok, else an error code
//	PromiseID u32   @4    echoes the request's PromiseID
//	Body      bytes @12   ZAP-encoded results (empty for a void method)
//
// Both envelopes are finished with header flags = MsgTypeRouterBase << 8
// so a ZAP transport routes them to this service's handler
// (msgType = flags >> 8).
package rpc

import zap "github.com/zap-proto/go"

// MsgTypeRouterBase is this service's ZAP message-type slot, carried in
// the high byte of the header flags word.
const MsgTypeRouterBase uint16 = 200

// NoTarget is the Target value for a call that does not pipeline off an
// earlier promise (the call targets the bootstrap object).
const NoTarget uint32 = 0

// Status codes carried in a Response.
const (
	StatusOK           uint32 = 200
	StatusBadRequest   uint32 = 400
	StatusUnauthorized uint32 = 401
	StatusForbidden    uint32 = 403
	StatusNotFound     uint32 = 404
	StatusInternal     uint32 = 500
)

// Request field offsets.
const (
	reqMethodOff    = 0
	reqPromiseIDOff = 4
	reqTargetOff    = 8
	reqCapOff       = 12
	reqPayloadOff   = 20
	reqFixedSize    = 28
)

// Response field offsets.
const (
	respStatusOff    = 0
	respPromiseIDOff = 4
	respBodyOff      = 12
	respFixedSize    = 20
)

// Call is one outbound request's fields.
type Call struct {
	Method    uint32
	PromiseID uint32
	Target    uint32
	Cap       []byte
	Payload   []byte
}

// BuildRequest encodes a Call into a router-tagged ZAP message. The
// header is v2 to match the transport default; the flags carry the
// router msgType so the transport dispatches it to this service.
func BuildRequest(c Call) []byte {
	b := zap.NewBuilderV2(len(c.Cap) + len(c.Payload) + reqFixedSize + 64)
	ob := b.StartObject(reqFixedSize)
	ob.SetUint32(reqMethodOff, c.Method)
	ob.SetUint32(reqPromiseIDOff, c.PromiseID)
	ob.SetUint32(reqTargetOff, c.Target)
	ob.SetBytes(reqCapOff, c.Cap)
	ob.SetBytes(reqPayloadOff, c.Payload)
	ob.FinishAsRoot()
	return b.FinishWithFlags(MsgTypeRouterBase << 8)
}

// ParseRequest decodes a router-tagged request message into a Call. The
// returned Cap and Payload slices alias the input buffer; copy them if
// you retain them past the buffer's lifetime.
func ParseRequest(msg []byte) (Call, error) {
	m, err := zap.Parse(msg)
	if err != nil {
		return Call{}, err
	}
	r := m.Root()
	return Call{
		Method:    r.Uint32(reqMethodOff),
		PromiseID: r.Uint32(reqPromiseIDOff),
		Target:    r.Uint32(reqTargetOff),
		Cap:       r.Bytes(reqCapOff),
		Payload:   r.Bytes(reqPayloadOff),
	}, nil
}

// Response is a decoded response envelope.
type Response struct {
	Status    uint32
	PromiseID uint32
	Body      []byte
}

// BuildResponse encodes a status + body into a router-tagged response.
func BuildResponse(status, promiseID uint32, body []byte) []byte {
	b := zap.NewBuilderV2(len(body) + respFixedSize + 64)
	ob := b.StartObject(respFixedSize)
	ob.SetUint32(respStatusOff, status)
	ob.SetUint32(respPromiseIDOff, promiseID)
	ob.SetBytes(respBodyOff, body)
	ob.FinishAsRoot()
	return b.FinishWithFlags(MsgTypeRouterBase << 8)
}

// ParseResponse decodes a router-tagged response message. The returned
// Body slice aliases the input buffer.
func ParseResponse(msg []byte) (Response, error) {
	m, err := zap.Parse(msg)
	if err != nil {
		return Response{}, err
	}
	r := m.Root()
	return Response{
		Status:    r.Uint32(respStatusOff),
		PromiseID: r.Uint32(respPromiseIDOff),
		Body:      r.Bytes(respBodyOff),
	}, nil
}
