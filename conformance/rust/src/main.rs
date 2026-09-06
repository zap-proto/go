// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//! The Rust half of the cross-language proof. It reads the same corpus as
//! `go run ./conformance`, runs the same schemas through the code zapgen
//! emitted for Rust, and writes the same report. The report IS the proof:
//! the two files are compared byte for byte.
//!
//! ```text
//! go run ./conformance > /tmp/go.tsv
//! cargo run --manifest-path conformance/rust/Cargo.toml > /tmp/rust.tsv
//! cmp /tmp/go.tsv /tmp/rust.tsv
//! ```

mod digest;

// Emitted by zapgen. Regenerate with conformance/run.sh; do not edit.
mod echo_zap;
mod kitchen_zap;
mod pchain_zap;
mod rpc;
mod xchain_zap;
mod zap;

use digest::{hex, unhex};
use echo_zap as echo;
use kitchen_zap as kitchen;
use std::cell::RefCell;
use std::io::{BufWriter, Write};
use std::rc::Rc;

/// Where every line goes. The lines are collected rather than written as
/// they are made because the channel below writes lines from inside a client
/// call, and the order of the report is part of what is compared.
type Sink = Rc<RefCell<Vec<String>>>;

fn main() {
    let mut corpus = String::from("conformance/corpus/vectors.tsv");
    let mut args = std::env::args().skip(1);
    while let Some(a) = args.next() {
        if a == "-corpus" {
            corpus = args.next().unwrap_or(corpus);
        }
    }

    let sink: Sink = Rc::new(RefCell::new(Vec::new()));
    built(&sink);
    envelope(&sink);
    client(&sink);
    let failure = corpus_run(&sink, &corpus).err();

    let out = std::io::stdout();
    let mut w = BufWriter::new(out.lock());
    for row in sink.borrow().iter() {
        let _ = writeln!(w, "{row}");
    }
    let _ = w.flush();

    if let Some(e) = failure {
        eprintln!("zapgen proof: {e}");
        std::process::exit(1);
    }
}

fn line(w: &Sink, kind: &str, id: &str, body: &str) {
    w.borrow_mut().push(format!("{kind}\t{id}\t{body}"));
}

// --- built: a fixed input through the emitted builder -----------------------

fn built(w: &Sink) {
    let leaf = kitchen::new_leaf(&kitchen::LeafInput {
        tag: 7,
        note: "leaf note",
    });
    line(w, "BUILD", "kitchen.Leaf", &hex(&leaf));
    line(w, "READ", "kitchen.Leaf", &digest::leaf_of(&leaf));

    let one = kitchen::new_leaf(&kitchen::LeafInput { tag: 1, note: "one" });
    let two = kitchen::new_leaf(&kitchen::LeafInput { tag: 2, note: "" });
    let three = kitchen::new_leaf(&kitchen::LeafInput {
        tag: 0xffffffff,
        note: "three",
    });
    let items: [&[u8]; 3] = [&one, &two, &three];
    let all = kitchen::new_all(&kitchen::AllInput {
        flag: true,
        a8: 0x7f,
        a16: 0xbeef,
        a32: 0xdeadbeef,
        a64: 0x0123456789abcdef,
        s8: -8,
        s16: -300,
        s32: -70000,
        s64: -5000000000,
        f32: 0.15625,
        f64: -2.718281828459045,
        name: "a name with ünïcödé",
        blob: &[0x00, 0x01, 0xfe, 0xff],
        id: &[1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16],
        items: &items,
        inner: &leaf,
    });
    line(w, "BUILD", "kitchen.All", &hex(&all));
    line(w, "READ", "kitchen.All", &digest::all_of(&all));

    // The empty case: every field left at its zero.
    let zero = kitchen::new_all(&kitchen::AllInput::default());
    line(w, "BUILD", "kitchen.All.zero", &hex(&zero));
    line(w, "READ", "kitchen.All.zero", &digest::all_of(&zero));

    let ping = echo::new_ping(&echo::PingInput { seq: 0xfeedface });
    line(w, "BUILD", "echo.Ping", &hex(&ping));
}

// --- envelope: the framing a generated client and dispatch ride on ----------

fn envelope(w: &Sink) {
    let payload = echo::new_ping(&echo::PingInput { seq: 42 });
    let req = rpc::build_request(&rpc::Call {
        method: echo::ECHO_PING_ORDINAL,
        promise_id: 1,
        target: rpc::NO_TARGET,
        cap: &[0xca, 0xfe],
        payload: &payload,
    });
    line(w, "BUILD", "rpc.request", &hex(&req));

    match rpc::parse_request(&req) {
        Ok(call) => line(
            w,
            "READ",
            "rpc.request",
            &format!(
                "method={};promise={};target={};cap={};payload={}",
                call.method,
                call.promise_id,
                call.target,
                hex(call.cap),
                hex(call.payload)
            ),
        ),
        Err(e) => line(w, "READ", "rpc.request", &format!("err={e}")),
    }

    let body = echo::new_pong(&echo::PongInput { seq: 43 });
    let resp = rpc::build_response(rpc::STATUS_OK, 1, &body);
    line(w, "BUILD", "rpc.response", &hex(&resp));

    // The generated dispatch, over the generated handler contract.
    match echo::dispatch_echo(&mut digest::Handler, &req) {
        Ok(out) => line(w, "BUILD", "echo.dispatch.ping", &hex(&out)),
        Err(e) => line(w, "BUILD", "echo.dispatch.ping", &format!("err={e}")),
    }

    let unknown = rpc::build_request(&rpc::Call {
        method: 99,
        promise_id: 5,
        ..Default::default()
    });
    match echo::dispatch_echo(&mut digest::Handler, &unknown) {
        Ok(out) => line(w, "BUILD", "echo.dispatch.unknown", &hex(&out)),
        Err(e) => line(w, "BUILD", "echo.dispatch.unknown", &format!("err={e}")),
    }

    match echo::dispatch_echo(&mut digest::Faulty, &req) {
        Ok(out) => line(w, "BUILD", "echo.dispatch.fault", &hex(&out)),
        Err(e) => line(w, "BUILD", "echo.dispatch.fault", &format!("err={e}")),
    }
}

// --- client: the emitted client, over a channel that dispatches in place --

/// A channel that carries a call straight into the dispatch, so the emitted
/// client and the emitted server meet with no transport in between.
struct Loop {
    w: Sink,
    n: usize,
}

impl echo::EchoChannel for Loop {
    fn call(&mut self, envelope: &[u8]) -> Result<Vec<u8>, rpc::CallError> {
        line(&self.w, "CALL", &self.n.to_string(), &hex(envelope));
        self.n += 1;
        echo::dispatch_echo(&mut digest::Handler, envelope).map_err(rpc::CallError::Wire)
    }
}

fn client(w: &Sink) {
    let mut c = echo::EchoClient::new(
        Loop {
            w: Rc::clone(w),
            n: 0,
        },
        &[0xca, 0xfe],
    );

    let req = echo::new_ping(&echo::PingInput { seq: 100 });
    let (p, body) = c.ping(&req);
    line(w, "CLIENT", "ping", &answer(p, body));

    // The pipelined form: its payload is the earlier call's answer, supplied
    // server-side, so it ships with no payload of its own.
    let (p2, body2) = c.ping_on(p);
    line(w, "CLIENT", "ping_on", &answer(p2, body2));

    let notice = echo::new_ping(&echo::PingInput { seq: 1 });
    let (p3, sent) = c.notify(&notice);
    line(w, "CLIENT", "notify", &answer(p3, sent.map(|()| Vec::new())));

    let (p4, body4) = c.health();
    line(w, "CLIENT", "health", &answer(p4, body4));
}

/// Render one call's outcome the same way in both languages.
///
/// The refusal is rendered as the bare word, not as the error's text: the Go
/// client answers a formatted error and the Rust client a typed one, and the
/// wording of a refusal is idiom, not wire. That a call was refused, and the
/// bytes of every envelope that carried it, are compared.
fn answer(p: rpc::Promise, body: Result<Vec<u8>, rpc::CallError>) -> String {
    match body {
        Ok(b) => format!("promise={};body={}", p.id, hex(&b)),
        Err(_) => format!("promise={};refused", p.id),
    }
}

// --- corpus -----------------------------------------------------------------

fn corpus_run(w: &Sink, path: &str) -> std::io::Result<()> {
    let text = std::fs::read_to_string(path)?;
    for row in text.lines() {
        if row.starts_with('#') || row.trim().is_empty() {
            continue;
        }
        let col: Vec<&str> = row.split('\t').collect();
        if col.len() < 5 || col[0] != "V" {
            continue;
        }
        let (id, chain, op, wire) = (col[1], col[2], col[3], col[4]);
        if wire == "-" || wire.is_empty() {
            line(w, "V", id, "nowire");
            continue;
        }
        match unhex(wire) {
            Some(b) => vector(w, id, chain, op, &b),
            None => line(w, "V", id, "badhex"),
        }
    }
    Ok(())
}

fn vector(w: &Sink, id: &str, chain: &str, op: &str, b: &[u8]) {
    let head = format!("{chain}/{op}");
    // What the runtime says about the bytes, before any schema is applied.
    match zap::Message::parse(zap_body(chain, op, b)) {
        Err(e) => {
            line(w, "V", id, &format!("{head};parse=err:{e}"));
            return;
        }
        Ok(m) => line(
            w,
            "V",
            id,
            &format!(
                "{head};parse=ok;version={};flags={};size={}",
                m.version(),
                m.flags(),
                m.size()
            ),
        ),
    }

    match (chain, op) {
        ("P", "tx") => {
            line_err(w, "R", id, digest::spend_of(b));
            let out = match digest::rebuild_spend(b) {
                Ok(o) => o,
                Err(e) => {
                    line(w, "W", id, &format!("err={e}"));
                    return;
                }
            };
            line(w, "W", id, &hex(&out));
            line_err(w, "RR", id, digest::spend_of(&out));
        }
        ("P", "block") => line_err(w, "R", id, digest::block_of(b)),
        ("X", "tx") => {
            line_err(w, "R", id, digest::signed_of(b));
            let out = match digest::rebuild_signed(b) {
                Ok(o) => o,
                Err(e) => {
                    line(w, "W", id, &format!("err={e}"));
                    return;
                }
            };
            line(w, "W", id, &hex(&out));
            line_err(w, "RR", id, digest::signed_of(&prefixed(&out)));
        }
        ("X", "block") => line_err(w, "R", id, digest::x_block_of(b)),
        _ => {}
    }
}

/// Strip the chain's own framing. An X TRANSACTION carries a type byte and a
/// shape byte ahead of the ZAP message; an X block and every P vector carry
/// none.
fn zap_body<'a>(chain: &str, op: &str, b: &'a [u8]) -> &'a [u8] {
    if chain == "X" && op == "tx" && b.len() >= digest::PREFIX {
        return &b[digest::PREFIX..];
    }
    b
}

/// Put the two X framing bytes back so a rebuilt envelope is read by the
/// same reader that read the original.
fn prefixed(b: &[u8]) -> Vec<u8> {
    let mut out = vec![0u8, 0u8];
    out.extend_from_slice(b);
    out
}

fn line_err(w: &Sink, kind: &str, id: &str, r: Result<String, zap::Error>) {
    match r {
        Ok(s) => line(w, kind, id, &s),
        Err(e) => line(w, kind, id, &format!("err={e}")),
    }
}
