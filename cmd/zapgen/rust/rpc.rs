// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
//
// zapgen emits this file verbatim as rpc.rs when a schema declares an
// interface. Like zap.rs it is maintained here, in the generator, so every
// Rust consumer gets one call envelope rather than one per crate.

//! The ZAP call envelope: the framing that carries one interface method
//! call and its answer over a ZAP transport.
//!
//! These are the same offsets the Go runtime's `rpc` package writes, so a
//! request built here is parsed there and back. Both envelopes carry the
//! router message type in the header flags, which is how a transport knows
//! to hand them to a service rather than to a schema reader.
//!
//! ```text
//! Request  (fixed 28)          Response (fixed 20)
//!   method     u32   @0          status     u32   @0
//!   promise_id u32   @4          promise_id u32   @4
//!   target     u32   @8          body       bytes @12
//!   cap        bytes @12
//!   payload    bytes @20
//! ```

#![allow(dead_code)]

use crate::zap;

/// The service's message-type slot, carried in the header flags' high byte.
pub const MSG_TYPE_ROUTER_BASE: u16 = 200;

/// The target of a call that pipelines off nothing.
pub const NO_TARGET: u32 = 0;

pub const STATUS_OK: u32 = 200;
pub const STATUS_BAD_REQUEST: u32 = 400;
pub const STATUS_UNAUTHORIZED: u32 = 401;
pub const STATUS_FORBIDDEN: u32 = 403;
pub const STATUS_NOT_FOUND: u32 = 404;
pub const STATUS_INTERNAL: u32 = 500;

const REQ_METHOD: usize = 0;
const REQ_PROMISE_ID: usize = 4;
const REQ_TARGET: usize = 8;
const REQ_CAP: usize = 12;
const REQ_PAYLOAD: usize = 20;
const REQ_SIZE: usize = 28;

const RESP_STATUS: usize = 0;
const RESP_PROMISE_ID: usize = 4;
const RESP_BODY: usize = 12;
const RESP_SIZE: usize = 20;

/// One outbound request.
#[derive(Clone, Copy, Debug)]
pub struct Call<'a> {
    pub method: u32,
    pub promise_id: u32,
    pub target: u32,
    pub cap: &'a [u8],
    pub payload: &'a [u8],
}

impl Default for Call<'_> {
    fn default() -> Self {
        Call {
            method: 0,
            promise_id: 0,
            target: NO_TARGET,
            cap: &[],
            payload: &[],
        }
    }
}

/// Write a request envelope.
pub fn build_request(c: &Call<'_>) -> Vec<u8> {
    let mut b = zap::Builder::new_v2(c.cap.len() + c.payload.len() + REQ_SIZE + 64);
    let mut ob = b.start_object(REQ_SIZE);
    ob.set_u32(&mut b, REQ_METHOD, c.method);
    ob.set_u32(&mut b, REQ_PROMISE_ID, c.promise_id);
    ob.set_u32(&mut b, REQ_TARGET, c.target);
    ob.set_bytes(&mut b, REQ_CAP, c.cap);
    ob.set_bytes(&mut b, REQ_PAYLOAD, c.payload);
    ob.finish_as_root(&mut b);
    b.finish_with_flags(MSG_TYPE_ROUTER_BASE << 8)
}

/// Read a request envelope. `cap` and `payload` borrow `msg`.
pub fn parse_request(msg: &[u8]) -> Result<Call<'_>, zap::Error> {
    let r = zap::Message::parse(msg)?.root();
    Ok(Call {
        method: r.u32(REQ_METHOD),
        promise_id: r.u32(REQ_PROMISE_ID),
        target: r.u32(REQ_TARGET),
        cap: r.bytes(REQ_CAP),
        payload: r.bytes(REQ_PAYLOAD),
    })
}

/// One answer.
#[derive(Clone, Copy, Debug)]
pub struct Response<'a> {
    pub status: u32,
    pub promise_id: u32,
    pub body: &'a [u8],
}

/// Write a response envelope.
pub fn build_response(status: u32, promise_id: u32, body: &[u8]) -> Vec<u8> {
    let mut b = zap::Builder::new_v2(body.len() + RESP_SIZE + 64);
    let mut ob = b.start_object(RESP_SIZE);
    ob.set_u32(&mut b, RESP_STATUS, status);
    ob.set_u32(&mut b, RESP_PROMISE_ID, promise_id);
    ob.set_bytes(&mut b, RESP_BODY, body);
    ob.finish_as_root(&mut b);
    b.finish_with_flags(MSG_TYPE_ROUTER_BASE << 8)
}

/// Read a response envelope. `body` borrows `msg`.
pub fn parse_response(msg: &[u8]) -> Result<Response<'_>, zap::Error> {
    let r = zap::Message::parse(msg)?.root();
    Ok(Response {
        status: r.u32(RESP_STATUS),
        promise_id: r.u32(RESP_PROMISE_ID),
        body: r.bytes(RESP_BODY),
    })
}

/// A handle to the answer of a call still in flight. Its id is what a
/// dependent call names as its target.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Promise {
    pub id: u32,
}

/// The client half of pipelining: promise ids, unique and non-zero, for one
/// connection. Rust's ownership does the locking the Go session does by hand.
#[derive(Debug, Default)]
pub struct Session {
    next: u32,
}

impl Session {
    pub fn new() -> Self {
        Session { next: 0 }
    }

    /// A fresh promise. Never [`NO_TARGET`], even across a wrap.
    pub fn next(&mut self) -> Promise {
        self.next = self.next.wrapping_add(1);
        if self.next == NO_TARGET {
            self.next = self.next.wrapping_add(1);
        }
        Promise { id: self.next }
    }
}

/// What a handler answers when it cannot. Dispatch turns it into
/// [`STATUS_INTERNAL`], the same answer the Go dispatch gives an error.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Fault;

impl core::fmt::Display for Fault {
    fn fmt(&self, f: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        f.write_str("handler fault")
    }
}

impl std::error::Error for Fault {}

/// What a call answers when the transport fails or the service says no.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum CallError {
    /// The channel could not carry the call.
    Transport,
    /// The envelope came back malformed.
    Wire(zap::Error),
    /// The service answered with a status other than [`STATUS_OK`].
    Status(u32),
}

impl core::fmt::Display for CallError {
    fn fmt(&self, f: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        match self {
            CallError::Transport => f.write_str("call: transport failed"),
            CallError::Wire(e) => write!(f, "call: {e}"),
            CallError::Status(s) => write!(f, "call: status {s}"),
        }
    }
}

impl std::error::Error for CallError {}

impl From<zap::Error> for CallError {
    fn from(e: zap::Error) -> Self {
        CallError::Wire(e)
    }
}
