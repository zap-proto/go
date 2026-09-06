// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.
//
// The C++ third of the cross-language proof. This program, its Go twin
// (conformance) and its Rust twin (conformance/rust) read the same corpus,
// run the same schemas through the code zapgen emitted for each language, and
// write the same report. The report IS the proof: three files, compared byte
// for byte.
//
//   probe-cpp -corpus conformance/corpus/vectors.tsv
//
// Nothing here decides anything. Every value comes from a generated accessor
// or a generated builder.

#include <algorithm>
#include <array>
#include <bit>
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <format>
#include <fstream>
#include <span>
#include <string>
#include <string_view>
#include <vector>

#include "echo_zap.hpp"
#include "kitchen_zap.hpp"
#include "pchain_zap.hpp"
#include "xchain_zap.hpp"

namespace {

using Bytes = std::vector<std::uint8_t>;
using View = std::span<const std::uint8_t>;

// Where every line goes. The lines are collected rather than written as they
// are made because the channel below writes lines from inside a client call,
// and the order of the report is part of what is compared.
std::vector<std::string> report;

void line(std::string_view kind, std::string_view id, std::string_view body) {
    report.emplace_back(std::format("{}\t{}\t{}", kind, id, body));
}

std::string hex(View b) {
    static const char* d = "0123456789abcdef";
    std::string out;
    out.reserve(b.size() * 2);
    for (std::uint8_t c : b) {
        out.push_back(d[c >> 4]);
        out.push_back(d[c & 0x0F]);
    }
    return out;
}

bool unhex(std::string_view s, Bytes& out) {
    if (s.size() % 2 != 0) return false;
    out.clear();
    out.reserve(s.size() / 2);
    auto nib = [](char c) -> int {
        if (c >= '0' && c <= '9') return c - '0';
        if (c >= 'a' && c <= 'f') return c - 'a' + 10;
        if (c >= 'A' && c <= 'F') return c - 'A' + 10;
        return -1;
    };
    for (std::size_t i = 0; i < s.size(); i += 2) {
        const int hi = nib(s[i]), lo = nib(s[i + 1]);
        if (hi < 0 || lo < 0) return false;
        out.push_back(static_cast<std::uint8_t>((hi << 4) | lo));
    }
    return true;
}

View view(const Bytes& b) { return View(b.data(), b.size()); }

// --- the schemas, rendered ---------------------------------------------------

std::string leaf(const kitchen::Leaf& t) {
    return std::format("tag={};note=<{}>", t.Tag(), t.Note());
}

std::string leaf_of(View b) {
    auto t = kitchen::WrapLeaf(b);
    if (!t) return std::format("err={}", zap::describe(t.error()));
    return leaf(*t);
}

std::string all_of(View b) {
    auto t = kitchen::WrapAll(b);
    if (!t) return std::format("err={}", zap::describe(t.error()));
    std::string w = std::format("flag={};a8={};a16={};a32={};a64={}", t->Flag() ? "true" : "false",
                                t->A8(), t->A16(), t->A32(), t->A64());
    w += std::format(";s8={};s16={};s32={};s64={}", t->S8(), t->S16(), t->S32(), t->S64());
    // Bit patterns, not printed decimals: a float's text is the one thing
    // three languages are certain to render differently.
    w += std::format(";f32={:08x};f64={:016x}", std::bit_cast<std::uint32_t>(t->F32()),
                     std::bit_cast<std::uint64_t>(t->F64()));
    w += std::format(";name=<{}>;blob={};id={}", t->Name(), hex(t->Blob()), hex(t->Id()));

    const auto items = t->Items();
    w += std::format(";items={}[", items.size());
    for (std::int64_t i = 0; i < items.size(); ++i) {
        w += std::format("{}:{};", i, leaf(items.at(i)));
    }
    w += "]";
    w += std::format(";inner={}", leaf(t->Inner()));
    return w;
}

std::string spend_of(View b) {
    auto t = pchain::WrapSpend(b);
    if (!t) return std::format("err={}", zap::describe(t.error()));
    std::string w = std::format("kind={};net={};chain={};memo={}", t->Kind(), t->NetworkID(),
                                hex(t->BlockchainID()), hex(t->Memo()));

    const auto outs = t->Outs();
    w += std::format(";outs={}[", outs.size());
    for (std::int64_t i = 0; i < outs.size(); ++i) {
        const auto o = outs.at(i);
        w += std::format("{}:asset={},slock={},amt={},thr={},olock={},astart={},acount={};", i,
                         hex(o.Asset()), o.StakeLock(), o.Amount(), o.Threshold(), o.OwnerLock(),
                         o.AddrStart(), o.AddrCount());
    }
    w += "]";

    const auto addrs = t->OwnerAddrs();
    w += std::format(";addrs={}[", addrs.size());
    for (std::int64_t i = 0; i < addrs.size(); ++i) {
        w += std::format("{}:{};", i, hex(addrs.at(i).Bytes()));
    }
    w += "]";

    const auto ins = t->Ins();
    w += std::format(";ins={}[", ins.size());
    for (std::int64_t i = 0; i < ins.size(); ++i) {
        const auto in = ins.at(i);
        w += std::format("{}:txid={},idx={},asset={},slock={},amt={},sstart={},scount={};", i,
                         hex(in.TxID()), in.OutputIndex(), hex(in.Asset()), in.StakeLock(),
                         in.Amount(), in.SigStart(), in.SigCount());
    }
    w += "]";

    const auto sigs = t->SigIndices();
    w += std::format(";sigs={}[", sigs.size());
    for (std::int64_t i = 0; i < sigs.size(); ++i) {
        w += std::format("{}:{};", i, sigs.at(i).Index());
    }
    w += "]";
    return w;
}

std::string p_block_of(View b) {
    auto t = pchain::WrapBlock(b);
    if (!t) return std::format("err={}", zap::describe(t.error()));
    std::string w =
        std::format("kind={};parent={};height={};time={};blob={};proposal={}", t->Kind(),
                    hex(t->Parent()), t->Height(), t->Time(), hex(t->TxBlob()), hex(t->ProposalTx()));
    const auto lens = t->TxLengths();
    w += std::format(";txlens={}[", lens.size());
    for (std::int64_t i = 0; i < lens.size(); ++i) {
        w += std::format("{}:{};", i, lens.at(i).Index());
    }
    w += "]";
    return w;
}

// The record runs a rebuild puts back. No stride is named: the element
// answers its bytes because it knows how wide it is.
template <typename L>
std::vector<View> records(const L& l) {
    std::vector<View> out;
    out.reserve(static_cast<std::size_t>(l.size()));
    for (std::int64_t i = 0; i < l.size(); ++i) out.push_back(l.at(i).Record());
    return out;
}

std::expected<Bytes, zap::Error> rebuild_spend(View b) {
    auto t = pchain::WrapSpend(b);
    if (!t) return std::unexpected(t.error());
    pchain::SpendInput in;
    in.Kind = t->Kind();
    in.NetworkID = t->NetworkID();
    std::copy_n(t->BlockchainID().begin(), in.BlockchainID.size(), in.BlockchainID.begin());
    const auto outs = records(t->Outs());
    const auto addrs = records(t->OwnerAddrs());
    const auto ins = records(t->Ins());
    const auto sigs = records(t->SigIndices());
    in.Outs = outs;
    in.OwnerAddrs = addrs;
    in.Ins = ins;
    in.SigIndices = sigs;
    in.Memo = t->Memo();
    return pchain::NewSpend(in);
}

// The two bytes an X transaction carries ahead of its ZAP message.
constexpr std::size_t kPrefix = 2;

std::string signed_of(View b) {
    if (b.size() < kPrefix) return "err=short envelope";
    auto t = xchain::WrapSigned(b.subspan(kPrefix));
    if (!t) return std::format("err={}", zap::describe(t.error()));
    std::string w = std::format("type={};shape={};unsigned={};creds={};credbytes={}", b[0], b[1],
                                t->Unsigned().size(), t->CredentialCount(),
                                t->CredentialBytes().size());
    const auto inner = t->Unsigned();
    if (inner.size() > kPrefix) {
        auto base = xchain::WrapBase(inner.subspan(kPrefix));
        if (base) {
            w += std::format(";base{{itype={};ishape={};net={};chain={};memo={};outs={}[", inner[0],
                             inner[1], base->NetworkID(), hex(base->BlockchainID()),
                             hex(base->Memo()), base->Outs().size());
            const auto outs = base->Outs();
            for (std::int64_t i = 0; i < outs.size(); ++i) {
                w += std::format("{}:{};", i, outs.at(i).Offset());
            }
            const auto ins = base->Ins();
            w += std::format("];ins={}[", ins.size());
            for (std::int64_t i = 0; i < ins.size(); ++i) {
                w += std::format("{}:{};", i, ins.at(i).Offset());
            }
            w += "]}";
        } else {
            w += ";base{unreadable}";
        }
    } else {
        w += ";base{absent}";
    }
    return w;
}

std::expected<Bytes, zap::Error> rebuild_signed(View b) {
    if (b.size() < kPrefix) return std::unexpected(zap::Error::BufferTooSmall);
    auto t = xchain::WrapSigned(b.subspan(kPrefix));
    if (!t) return std::unexpected(t.error());
    xchain::SignedInput in;
    in.Unsigned = t->Unsigned();
    in.CredentialCount = t->CredentialCount();
    in.CredentialBytes = t->CredentialBytes();
    return xchain::NewSigned(in);
}

std::string x_block_of(View b) {
    auto t = xchain::WrapBlock(b);
    if (!t) return std::format("err={}", zap::describe(t.error()));
    std::string w = std::format("parent={};height={};time={};root={};blob={}", hex(t->Parent()),
                                t->Height(), t->Time(), hex(t->Root()), hex(t->TxBlob()));
    const auto lens = t->TxLengths();
    w += std::format(";txlens={}[", lens.size());
    for (std::int64_t i = 0; i < lens.size(); ++i) {
        w += std::format("{}:{};", i, lens.at(i).Offset());
    }
    w += "]";
    return w;
}

// --- built: a fixed input through the emitted builder ------------------------

void built() {
    kitchen::LeafInput leaf_in;
    leaf_in.Tag = 7;
    leaf_in.Note = "leaf note";
    const Bytes leaf_msg = kitchen::NewLeaf(leaf_in);
    line("BUILD", "kitchen.Leaf", hex(view(leaf_msg)));
    line("READ", "kitchen.Leaf", leaf_of(view(leaf_msg)));

    kitchen::LeafInput one, two, three;
    one.Tag = 1;
    one.Note = "one";
    two.Tag = 2;
    two.Note = "";
    three.Tag = 0xffffffff;
    three.Note = "three";
    const Bytes m1 = kitchen::NewLeaf(one), m2 = kitchen::NewLeaf(two), m3 = kitchen::NewLeaf(three);

    kitchen::AllInput all_in;
    all_in.Flag = true;
    all_in.A8 = 0x7f;
    all_in.A16 = 0xbeef;
    all_in.A32 = 0xdeadbeef;
    all_in.A64 = 0x0123456789abcdefull;
    all_in.S8 = -8;
    all_in.S16 = -300;
    all_in.S32 = -70000;
    all_in.S64 = -5000000000ll;
    all_in.F32 = 0.15625f;
    all_in.F64 = -2.718281828459045;
    all_in.Name = "a name with ünïcödé";
    const std::array<std::uint8_t, 4> blob{0x00, 0x01, 0xfe, 0xff};
    all_in.Blob = View(blob);
    all_in.Id = {1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16};
    all_in.Items = {view(m1), view(m2), view(m3)};
    all_in.Inner = view(leaf_msg);
    const Bytes all = kitchen::NewAll(all_in);
    line("BUILD", "kitchen.All", hex(view(all)));
    line("READ", "kitchen.All", all_of(view(all)));

    // The empty case: every field left at its zero.
    const Bytes zero = kitchen::NewAll(kitchen::AllInput{});
    line("BUILD", "kitchen.All.zero", hex(view(zero)));
    line("READ", "kitchen.All.zero", all_of(view(zero)));

    echo::PingInput ping;
    ping.Seq = 0xfeedface;
    line("BUILD", "echo.Ping", hex(view(echo::NewPing(ping))));
}

// --- the server the proof calls ---------------------------------------------

// A handler that answers a ping with a pong of the next sequence number.
class Handler : public echo::EchoHandler {
  public:
    std::expected<Bytes, std::string> Ping(View req) override {
        auto p = echo::WrapPing(req);
        if (!p) return std::unexpected(std::string(zap::describe(p.error())));
        echo::PongInput out;
        out.Seq = p->Seq() + 1;
        return echo::NewPong(out);
    }
    std::expected<void, std::string> Notify(View) override { return {}; }
    std::expected<Bytes, std::string> Health() override {
        return echo::NewPong(echo::PongInput{});
    }
    std::expected<void, std::string> Shutdown() override { return {}; }
};

// A handler that refuses every call, which is how the internal status gets
// exercised.
class Faulty : public echo::EchoHandler {
  public:
    std::expected<Bytes, std::string> Ping(View) override { return std::unexpected("no"); }
    std::expected<void, std::string> Notify(View) override { return std::unexpected("no"); }
    std::expected<Bytes, std::string> Health() override { return std::unexpected("no"); }
    std::expected<void, std::string> Shutdown() override { return std::unexpected("no"); }
};

// --- envelope: the framing a generated client and dispatch ride on ----------

void envelope() {
    const std::array<std::uint8_t, 2> cap{0xca, 0xfe};
    echo::PingInput seq42;
    seq42.Seq = 42;
    const Bytes payload = echo::NewPing(seq42);

    zap::rpc::Call c;
    c.method = echo::kEchoPingOrdinal;
    c.promise_id = 1;
    c.target = zap::rpc::kNoTarget;
    c.cap = View(cap);
    c.payload = view(payload);
    const Bytes req = zap::rpc::build_request(c);
    line("BUILD", "rpc.request", hex(view(req)));

    if (auto call = zap::rpc::parse_request(view(req))) {
        line("READ", "rpc.request",
             std::format("method={};promise={};target={};cap={};payload={}", call->method,
                         call->promise_id, call->target, hex(call->cap), hex(call->payload)));
    } else {
        line("READ", "rpc.request", std::format("err={}", zap::describe(call.error())));
    }

    echo::PongInput seq43;
    seq43.Seq = 43;
    const Bytes body = echo::NewPong(seq43);
    line("BUILD", "rpc.response", hex(view(zap::rpc::build_response(zap::rpc::kStatusOK, 1, view(body)))));

    Handler h;
    if (auto out = echo::DispatchEcho(h, view(req))) {
        line("BUILD", "echo.dispatch.ping", hex(view(*out)));
    } else {
        line("BUILD", "echo.dispatch.ping", std::format("err={}", out.error()));
    }

    zap::rpc::Call other;
    other.method = 99;
    other.promise_id = 5;
    const Bytes unknown = zap::rpc::build_request(other);
    if (auto out = echo::DispatchEcho(h, view(unknown))) {
        line("BUILD", "echo.dispatch.unknown", hex(view(*out)));
    } else {
        line("BUILD", "echo.dispatch.unknown", std::format("err={}", out.error()));
    }

    Faulty f;
    if (auto out = echo::DispatchEcho(f, view(req))) {
        line("BUILD", "echo.dispatch.fault", hex(view(*out)));
    } else {
        line("BUILD", "echo.dispatch.fault", std::format("err={}", out.error()));
    }
}

// --- client: the emitted client, over a channel that dispatches in place ----

// A channel that carries a call straight into the dispatch, so the emitted
// client and the emitted server meet with no transport in between.
//
// It also keeps the promise id of the envelope it shipped: the C++ client
// answers a refusal without one, and the report says which call was refused.
class Loop : public echo::EchoChannel {
  public:
    int n = 0;
    std::uint32_t promise = 0;
    Bytes body;

    std::expected<zap::rpc::Response, std::string> Call(View envelope) override {
        line("CALL", std::format("{}", n), hex(envelope));
        ++n;
        if (auto c = zap::rpc::parse_request(envelope)) promise = c->promise_id;
        auto out = echo::DispatchEcho(handler_, envelope);
        if (!out) return std::unexpected(out.error());
        body = *out;
        auto r = zap::rpc::parse_response(view(body));
        if (!r) return std::unexpected(std::string(zap::describe(r.error())));
        return *r;
    }

  private:
    Handler handler_;
};

// One call's outcome, rendered the same way in every language.
//
// The refusal is rendered as the bare word, not as the error's text: each
// client answers a refusal in its own idiom, and the wording of a refusal is
// idiom, not wire. That a call was refused, and the bytes of every envelope
// that carried it, are compared.
std::string answer(std::uint32_t promise, bool ok, View body) {
    if (!ok) return std::format("promise={};refused", promise);
    return std::format("promise={};body={}", promise, hex(body));
}

void client() {
    const std::array<std::uint8_t, 2> cap{0xca, 0xfe};
    Loop loop;
    echo::EchoClient c(&loop, View(cap));

    echo::PingInput seq100;
    seq100.Seq = 100;
    const Bytes req = echo::NewPing(seq100);
    auto p = c.Ping(view(req));
    line("CLIENT", "ping",
         answer(p ? p->promise.id : loop.promise, p.has_value(), p ? view(p->body) : View()));

    // The pipelined form: its payload is the earlier call's answer, supplied
    // server-side, so it ships with no payload of its own.
    zap::rpc::Promise on{};
    if (p) on = p->promise;
    auto p2 = c.PingOn(on);
    line("CLIENT", "ping_on",
         answer(p2 ? p2->promise.id : loop.promise, p2.has_value(), p2 ? view(p2->body) : View()));

    echo::PingInput seq1;
    seq1.Seq = 1;
    const Bytes notice = echo::NewPing(seq1);
    auto p3 = c.Notify(view(notice));
    line("CLIENT", "notify", answer(p3 ? p3->id : loop.promise, p3.has_value(), View()));

    auto p4 = c.Health();
    line("CLIENT", "health",
         answer(p4 ? p4->promise.id : loop.promise, p4.has_value(), p4 ? view(p4->body) : View()));
}

// --- corpus ------------------------------------------------------------------

// The message size the ZAP header states, or 0 for bytes that do not open one.
std::size_t declared(View b) {
    if (b.size() < zap::kHeaderSize) return 0;
    return zap::load_u32(b.data() + 12);
}

// Whether what the generated builder wrote is what the chain wrote.
//
// A chain vector may carry more than one message — a P transaction is its
// unsigned bytes with a credential message concatenated — so the comparison
// is against the FIRST message, whose length its own header declares. "no"
// carries where the two part, because a byte offset is the only useful thing
// to say about a disagreement of bytes.
std::string same(View built, View wire) {
    const std::size_t n = declared(wire);
    if (n == 0 || n > wire.size()) return "no;the vector declares no message";
    const auto head = wire.subspan(0, n);
    if (built.size() != head.size()) {
        return std::format("no;size={};chain={}", built.size(), head.size());
    }
    for (std::size_t i = 0; i < built.size(); ++i) {
        if (built[i] != head[i]) return std::format("no;at={}", i);
    }
    return "yes";
}

// Strip the chain's own framing. An X TRANSACTION carries a type byte and a
// shape byte ahead of the ZAP message; an X block and every P vector carry
// none.
View zap_body(std::string_view chain, std::string_view op, View b) {
    if (chain == "X" && op == "tx" && b.size() >= kPrefix) return b.subspan(kPrefix);
    return b;
}

// Put the two X framing bytes back so a rebuilt envelope is read by the same
// reader that read the original.
Bytes prefixed(View b) {
    Bytes out{0, 0};
    out.insert(out.end(), b.begin(), b.end());
    return out;
}

void vector_line(std::string_view id, std::string_view chain, std::string_view op, View b) {
    const std::string head = std::format("{}/{}", chain, op);
    // What the runtime says about the bytes, before any schema is applied.
    const auto m = zap::Message::parse(zap_body(chain, op, b));
    if (!m) {
        line("V", id, std::format("{};parse=err:{}", head, zap::describe(m.error())));
        return;
    }
    line("V", id,
         std::format("{};parse=ok;version={};flags={};size={}", head, m->version(), m->flags(),
                     m->size()));

    if (chain == "P" && op == "tx") {
        line("R", id, spend_of(b));
        auto out = rebuild_spend(b);
        if (!out) {
            line("W", id, std::format("err={}", zap::describe(out.error())));
            return;
        }
        line("W", id, hex(view(*out)));
        line("EQ", id, same(view(*out), b));
        line("RR", id, spend_of(view(*out)));
        return;
    }
    if (chain == "P" && op == "block") {
        line("R", id, p_block_of(b));
        return;
    }
    if (chain == "X" && op == "tx") {
        line("R", id, signed_of(b));
        auto out = rebuild_signed(b);
        if (!out) {
            line("W", id, std::format("err={}", zap::describe(out.error())));
            return;
        }
        line("W", id, hex(view(*out)));
        line("EQ", id, same(view(*out), b.subspan(kPrefix)));
        const Bytes back = prefixed(view(*out));
        line("RR", id, signed_of(view(back)));
        return;
    }
    if (chain == "X" && op == "block") {
        line("R", id, x_block_of(b));
    }
}

std::vector<std::string_view> columns(std::string_view row) {
    std::vector<std::string_view> col;
    while (true) {
        const std::size_t tab = row.find('\t');
        if (tab == std::string_view::npos) {
            col.push_back(row);
            return col;
        }
        col.push_back(row.substr(0, tab));
        row = row.substr(tab + 1);
    }
}

bool blank(std::string_view s) {
    return std::all_of(s.begin(), s.end(), [](char c) { return c == ' ' || c == '\t' || c == '\r'; });
}

int corpus(const char* path) {
    std::ifstream in(path);
    if (!in) {
        std::fprintf(stderr, "zapgen proof: cannot open %s\n", path);
        return 1;
    }
    std::string row;
    while (std::getline(in, row)) {
        if (!row.empty() && row.back() == '\r') row.pop_back();
        if (row.starts_with('#') || blank(row)) continue;
        const auto col = columns(row);
        if (col.size() < 5 || col[0] != "V") continue;
        const auto id = col[1], chain = col[2], op = col[3], wire = col[4];
        if (wire == "-" || wire.empty()) {
            line("V", id, "nowire");
            continue;
        }
        Bytes b;
        if (!unhex(wire, b)) {
            line("V", id, "badhex");
            continue;
        }
        vector_line(id, chain, op, view(b));
    }
    return 0;
}

}  // namespace

int main(int argc, char** argv) {
    const char* path = "conformance/corpus/vectors.tsv";
    for (int i = 1; i < argc; ++i) {
        if (std::strcmp(argv[i], "-corpus") == 0 && i + 1 < argc) path = argv[++i];
    }

    built();
    envelope();
    client();
    const int status = corpus(path);

    for (const auto& row : report) std::printf("%s\n", row.c_str());
    return status;
}
