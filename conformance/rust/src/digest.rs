// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//! The Rust half of the proof: the same fields, read through the code zapgen
//! emitted for Rust, rendered as the same line of text the Go half writes.
//!
//! The generated Rust exports its offsets and takes a view of an object
//! already in a message, so this reads a list element as a typed view from
//! outside the generated module. The Go backend keeps both unexported, which
//! is why its half of the proof sits inside the generated package.

use crate::echo_zap as echo;
use crate::kitchen_zap as kitchen;
use crate::pchain_zap as p;
use crate::xchain_zap as x;
use crate::zap;

/// Lowercase hex, the shape Go's %x writes.
pub fn hex(b: &[u8]) -> String {
    let mut s = String::with_capacity(b.len() * 2);
    for byte in b {
        s.push_str(&format!("{byte:02x}"));
    }
    s
}

pub fn unhex(s: &str) -> Option<Vec<u8>> {
    if s.len() % 2 != 0 {
        return None;
    }
    let b = s.as_bytes();
    let mut out = Vec::with_capacity(s.len() / 2);
    for pair in b.chunks(2) {
        let hi = (pair[0] as char).to_digit(16)?;
        let lo = (pair[1] as char).to_digit(16)?;
        out.push((hi * 16 + lo) as u8);
    }
    Some(out)
}

// --- kitchen ---------------------------------------------------------------

pub fn leaf_of(b: &[u8]) -> String {
    match kitchen::Leaf::wrap(b) {
        Ok(t) => leaf(&t),
        Err(e) => format!("err={e}"),
    }
}

fn leaf(t: &kitchen::Leaf<'_>) -> String {
    format!("tag={};note=<{}>", t.tag(), t.note())
}

pub fn all_of(b: &[u8]) -> String {
    let t = match kitchen::All::wrap(b) {
        Ok(t) => t,
        Err(e) => return format!("err={e}"),
    };
    let mut w = String::new();
    w.push_str(&format!(
        "flag={};a8={};a16={};a32={};a64={}",
        t.flag(),
        t.a8(),
        t.a16(),
        t.a32(),
        t.a64()
    ));
    w.push_str(&format!(
        ";s8={};s16={};s32={};s64={}",
        t.s8(),
        t.s16(),
        t.s32(),
        t.s64()
    ));
    // Bit patterns, not printed decimals: a float's text is the one thing two
    // languages are certain to render differently.
    w.push_str(&format!(
        ";f32={:08x};f64={:016x}",
        t.f32().to_bits(),
        t.f64().to_bits()
    ));
    w.push_str(&format!(
        ";name=<{}>;blob={};id={}",
        t.name(),
        hex(t.blob()),
        hex(t.id())
    ));

    let items = t.items();
    w.push_str(&format!(";items={}[", items.len()));
    for i in 0..items.len() {
        match kitchen::Leaf::wrap(items.bytes_at(i)) {
            Ok(e) => w.push_str(&format!("{i}:{};", leaf(&e))),
            Err(_) => w.push_str(&format!("{i}:err;")),
        }
    }
    w.push(']');
    w.push_str(&format!(";inner={}", leaf(&t.inner())));
    w
}

// --- P chain ---------------------------------------------------------------

pub fn spend_of(b: &[u8]) -> Result<String, zap::Error> {
    let t = p::Spend::wrap(b)?;
    let mut w = String::new();
    w.push_str(&format!(
        "kind={};net={};chain={};memo={}",
        t.kind(),
        t.network_id(),
        hex(t.blockchain_id()),
        hex(t.memo())
    ));

    let outs = t.outs();
    w.push_str(&format!(";outs={}[", outs.len()));
    for i in 0..outs.len() {
        let o = p::Out::new(outs.object(i, p::OUT_SIZE));
        w.push_str(&format!(
            "{i}:asset={},slock={},amt={},thr={},olock={},astart={},acount={};",
            hex(o.asset()),
            o.stake_lock(),
            o.amount(),
            o.threshold(),
            o.owner_lock(),
            o.addr_start(),
            o.addr_count()
        ));
    }
    w.push(']');

    let addrs = t.owner_addrs();
    w.push_str(&format!(";addrs={}[", addrs.len()));
    for i in 0..addrs.len() {
        let a = p::Addr::new(addrs.object(i, p::ADDR_SIZE));
        w.push_str(&format!("{i}:{};", hex(a.bytes())));
    }
    w.push(']');

    let ins = t.ins();
    w.push_str(&format!(";ins={}[", ins.len()));
    for i in 0..ins.len() {
        let v = p::In::new(ins.object(i, p::IN_SIZE));
        w.push_str(&format!(
            "{i}:txid={},idx={},asset={},slock={},amt={},sstart={},scount={};",
            hex(v.tx_id()),
            v.output_index(),
            hex(v.asset()),
            v.stake_lock(),
            v.amount(),
            v.sig_start(),
            v.sig_count()
        ));
    }
    w.push(']');

    let sigs = t.sig_indices();
    w.push_str(&format!(";sigs={}[", sigs.len()));
    for i in 0..sigs.len() {
        let s = p::Sig::new(sigs.object(i, p::SIG_SIZE));
        w.push_str(&format!("{i}:{};", s.index()));
    }
    w.push(']');
    Ok(w)
}

pub fn block_of(b: &[u8]) -> Result<String, zap::Error> {
    let t = p::Block::wrap(b)?;
    let mut w = String::new();
    w.push_str(&format!(
        "kind={};parent={};height={};time={};blob={};proposal={}",
        t.kind(),
        hex(t.parent()),
        t.height(),
        t.time(),
        hex(t.tx_blob()),
        hex(t.proposal_tx())
    ));
    let lens = t.tx_lengths();
    w.push_str(&format!(";txlens={}[", lens.len()));
    for i in 0..lens.len() {
        let s = p::Sig::new(lens.object(i, p::SIG_SIZE));
        w.push_str(&format!("{i}:{};", s.index()));
    }
    w.push(']');
    Ok(w)
}

/// Write the transaction back out through the generated builder, carrying
/// every field the builder can carry.
pub fn rebuild_spend(b: &[u8]) -> Result<Vec<u8>, zap::Error> {
    let t = p::Spend::wrap(b)?;
    let outs = records(t.outs(), p::OUT_SIZE);
    let addrs = records(t.owner_addrs(), p::ADDR_SIZE);
    let ins = records(t.ins(), p::IN_SIZE);
    let sigs = records(t.sig_indices(), p::SIG_SIZE);
    Ok(p::new_spend(&p::SpendInput {
        kind: t.kind(),
        network_id: t.network_id(),
        blockchain_id: t.blockchain_id(),
        outs: &outs,
        owner_addrs: &addrs,
        ins: &ins,
        sig_indices: &sigs,
        memo: t.memo(),
    }))
}

/// Slice a stride list into one byte run per element.
fn records<'a>(l: zap::List<'a>, stride: usize) -> Vec<&'a [u8]> {
    let mut out = Vec::with_capacity(l.len());
    for i in 0..l.len() {
        out.push(l.object(i, stride).bytes_fixed(0, stride));
    }
    out
}

// --- X chain ---------------------------------------------------------------

/// The two bytes an X transaction carries ahead of its ZAP message.
pub const PREFIX: usize = 2;

pub fn signed_of(b: &[u8]) -> Result<String, zap::Error> {
    if b.len() < PREFIX {
        return Err(zap::Error::BufferTooSmall);
    }
    let t = x::Signed::wrap(&b[PREFIX..])?;
    let mut w = String::new();
    w.push_str(&format!(
        "type={};shape={};unsigned={};creds={};credbytes={}",
        b[0],
        b[1],
        t.unsigned().len(),
        t.credential_count(),
        t.credential_bytes().len()
    ));

    let inner = t.unsigned();
    if inner.len() > PREFIX {
        match x::Base::wrap(&inner[PREFIX..]) {
            Ok(base) => {
                let outs = base.outs();
                w.push_str(&format!(
                    ";base{{itype={};ishape={};net={};chain={};memo={};outs={}[",
                    inner[0],
                    inner[1],
                    base.network_id(),
                    hex(base.blockchain_id()),
                    hex(base.memo()),
                    outs.len()
                ));
                for i in 0..outs.len() {
                    let ptr = x::Ptr::new(outs.object(i, x::PTR_SIZE));
                    w.push_str(&format!("{i}:{};", ptr.offset()));
                }
                let ins = base.ins();
                w.push_str(&format!("];ins={}[", ins.len()));
                for i in 0..ins.len() {
                    let ptr = x::Ptr::new(ins.object(i, x::PTR_SIZE));
                    w.push_str(&format!("{i}:{};", ptr.offset()));
                }
                w.push_str("]}");
            }
            Err(_) => w.push_str(";base{unreadable}"),
        }
    } else {
        w.push_str(";base{absent}");
    }
    Ok(w)
}

pub fn rebuild_signed(b: &[u8]) -> Result<Vec<u8>, zap::Error> {
    if b.len() < PREFIX {
        return Err(zap::Error::BufferTooSmall);
    }
    let t = x::Signed::wrap(&b[PREFIX..])?;
    Ok(x::new_signed(&x::SignedInput {
        unsigned: t.unsigned(),
        credential_count: t.credential_count(),
        credential_bytes: t.credential_bytes(),
    }))
}

pub fn x_block_of(b: &[u8]) -> Result<String, zap::Error> {
    let t = x::Block::wrap(b)?;
    let mut w = String::new();
    w.push_str(&format!(
        "parent={};height={};time={};root={};blob={}",
        hex(t.parent()),
        t.height(),
        t.time(),
        hex(t.root()),
        hex(t.tx_blob())
    ));
    let lens = t.tx_lengths();
    w.push_str(&format!(";txlens={}[", lens.len()));
    for i in 0..lens.len() {
        let ptr = x::Ptr::new(lens.object(i, x::PTR_SIZE));
        w.push_str(&format!("{i}:{};", ptr.offset()));
    }
    w.push(']');
    Ok(w)
}

// --- the echo service ------------------------------------------------------

/// Answers a ping with a pong of the next sequence number.
pub struct Handler;

impl echo::EchoHandler for Handler {
    fn ping(&mut self, req: &[u8]) -> Result<Vec<u8>, crate::rpc::Fault> {
        let p = echo::Ping::wrap(req).map_err(|_| crate::rpc::Fault)?;
        Ok(echo::new_pong(&echo::PongInput { seq: p.seq() + 1 }))
    }
    fn notify(&mut self, _req: &[u8]) -> Result<(), crate::rpc::Fault> {
        Ok(())
    }
    fn health(&mut self) -> Result<Vec<u8>, crate::rpc::Fault> {
        Ok(echo::new_pong(&echo::PongInput { seq: 0 }))
    }
    fn shutdown(&mut self) -> Result<(), crate::rpc::Fault> {
        Ok(())
    }
}

/// Refuses every call, which is how the internal status gets exercised.
pub struct Faulty;

impl echo::EchoHandler for Faulty {
    fn ping(&mut self, _req: &[u8]) -> Result<Vec<u8>, crate::rpc::Fault> {
        Err(crate::rpc::Fault)
    }
    fn notify(&mut self, _req: &[u8]) -> Result<(), crate::rpc::Fault> {
        Err(crate::rpc::Fault)
    }
    fn health(&mut self) -> Result<Vec<u8>, crate::rpc::Fault> {
        Err(crate::rpc::Fault)
    }
    fn shutdown(&mut self) -> Result<(), crate::rpc::Fault> {
        Err(crate::rpc::Fault)
    }
}
