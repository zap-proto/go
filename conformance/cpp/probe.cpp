// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.
//
// probe.cpp is the C++ half of the two-backend proof. It exercises the code
// the C++ backend emitted, and prints what it built and what it read, in the
// format probe.go prints. The proof is that the two outputs are the same file.
//
//   probe write               one line per build case: name and the hex bytes
//   probe back  CASES         read those bytes back: one line of fields per case
//   probe read  VECTORS.TSV   one line per corpus vector: the fields read
//
// `back` is the crossing point: run the Go probe's write output through this
// probe's back, and the C++ reader is reading bytes the Go builder wrote.
//
// Nothing here decides anything. Every value comes from a generated accessor
// or a generated builder.

#include <array>
#include <bit>
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <expected>
#include <fstream>
#include <span>
#include <string>
#include <string_view>
#include <vector>

#include "basetx_zap.hpp"
#include "echo_zap.hpp"
#include "pchain_zap.hpp"
#include "probe_zap.hpp"

namespace {

using Bytes = std::vector<std::uint8_t>;
using View = std::span<const std::uint8_t>;

std::string hex_of(View b) {
    static const char* d = "0123456789abcdef";
    std::string out;
    out.reserve(b.size() * 2);
    for (std::uint8_t c : b) {
        out.push_back(d[c >> 4]);
        out.push_back(d[c & 0x0F]);
    }
    return out;
}

void emit(const char* name, View b) {
    std::printf("W\t%s\t%s\n", name, hex_of(b).c_str());
}

Bytes long_run(int n) {
    Bytes out(static_cast<std::size_t>(n));
    for (int i = 0; i < n; ++i) out[static_cast<std::size_t>(i)] = static_cast<std::uint8_t>(i);
    return out;
}

std::array<std::uint8_t, 32> id32(std::uint8_t fill) {
    std::array<std::uint8_t, 32> out{};
    out.fill(fill);
    return out;
}

// ── write: every builder, from the same fixed inputs probe.go uses ──────────

probe::WideInput wide_full(View child) {
    std::array<std::uint8_t, 16> id{};
    for (std::size_t i = 0; i < id.size(); ++i) id[i] = static_cast<std::uint8_t>(i);
    probe::WideInput in;
    in.Flag = true;
    in.A8 = 0xAB;
    in.A16 = 0xBEEF;
    in.A32 = 0xDEADBEEF;
    in.A64 = 0x0123456789ABCDEFull;
    in.S8 = -3;
    in.S16 = -300;
    in.S32 = -70000;
    in.S64 = -5000000000000ll;
    in.F32 = -1.5f;
    in.F64 = 3.141592653589793;
    in.Id = id;
    in.Name = "zap";
    in.Blob = View(reinterpret_cast<const std::uint8_t*>("\x00\x01\x02\x03\x04"), 5);
    in.Child = child;
    return in;
}

void write_echo() {
    const std::uint8_t cap_bytes[] = {0xCA, 0xFE};
    const std::uint8_t payload_bytes[] = {0x11, 0x22, 0x33};
    const View cap(cap_bytes, 2);
    const View payload(payload_bytes, 3);

    // A channel that keeps the envelope the generated client shipped and
    // answers with a fixed response.
    class Recorder : public echo::EchoChannel {
      public:
        Bytes env;
        std::expected<zap::rpc::Response, std::string> Call(View envelope) override {
            env.assign(envelope.begin(), envelope.end());
            static const std::uint8_t body[] = {0x10, 0x20};
            zap::rpc::Response r;
            r.status = zap::rpc::kStatusOK;
            r.promise_id = 1;
            r.body = View(body, 2);
            return r;
        }
    };

    // One fresh client per case, so each request carries promise id 1.
    {
        Recorder r;
        echo::EchoClient(&r, cap).Ping(payload);
        emit("echo/req/ping", r.env);
    }
    {
        Recorder r;
        echo::EchoClient(&r, cap).Notify(payload);
        emit("echo/req/notify", r.env);
    }
    {
        Recorder r;
        echo::EchoClient(&r, cap).Health();
        emit("echo/req/health", r.env);
    }
    {
        Recorder r;
        echo::EchoClient(&r, cap).Shutdown();
        emit("echo/req/shutdown", r.env);
    }
    {
        Recorder r;
        echo::EchoClient c(&r, cap);
        auto first = c.Ping(payload);
        c.PingOn(first->promise);
        emit("echo/req/ping-on", r.env);
    }

    class Handler : public echo::EchoHandler {
      public:
        std::expected<Bytes, std::string> Ping(View) override { return Bytes{0x01, 0x02, 0x03}; }
        std::expected<void, std::string> Notify(View) override { return {}; }
        std::expected<Bytes, std::string> Health() override { return Bytes{0x04}; }
        std::expected<void, std::string> Shutdown() override { return {}; }
    };

    Handler h;
    const char* names[] = {"ping", "notify", "health", "shutdown"};
    for (const char* name : names) {
        Recorder r;
        echo::EchoClient c(&r, cap);
        if (std::strcmp(name, "ping") == 0) {
            c.Ping(payload);
        } else if (std::strcmp(name, "notify") == 0) {
            c.Notify(payload);
        } else if (std::strcmp(name, "health") == 0) {
            c.Health();
        } else {
            c.Shutdown();
        }
        auto resp = echo::DispatchEcho(h, r.env);
        Bytes out = resp ? *resp : Bytes{};
        emit((std::string("echo/resp/") + name).c_str(), out);
    }

    zap::rpc::Call unknown;
    unknown.method = 99;
    unknown.promise_id = 1;
    unknown.target = zap::rpc::kNoTarget;
    unknown.cap = cap;
    const Bytes unknown_env = zap::rpc::build_request(unknown);
    auto refused = echo::DispatchEcho(h, unknown_env);
    emit("echo/resp/unknown", refused ? *refused : Bytes{});
}

void write() {
    emit("inner/zero", probe::NewInner(probe::InnerInput{}));
    {
        probe::InnerInput in;
        in.Tag = 0xA1B2C3D4;
        in.Val = 0x0102030405060708ull;
        emit("inner/set", probe::NewInner(in));
    }

    emit("wide/zero", probe::NewWide(probe::WideInput{}));
    {
        probe::InnerInput child_in;
        child_in.Tag = 7;
        child_in.Val = 9;
        const Bytes child = probe::NewInner(child_in);
        probe::WideInput in = wide_full(child);
        const std::uint8_t e0[] = {0xAA};
        const std::uint8_t e1[] = {0xBB, 0xCC};
        in.Items = {View(e0, 1), View(e1, 2), View()};
        emit("wide/full", probe::NewWide(in));
    }
    {
        probe::WideInput in;
        const std::uint8_t e1[] = {0x01};
        const Bytes e2 = long_run(70);
        in.Items = {View(), View(e1, 1), View(e2)};
        emit("wide/lists", probe::NewWide(in));
    }
    {
        probe::WideInput in;
        in.Name = "zero-copy application protocol";
        emit("wide/text", probe::NewWide(in));
    }

    emit("spend/zero", pchain::NewSpend(pchain::SpendInput{}));
    {
        const Bytes out0 = long_run(72), out1 = long_run(72);
        const Bytes owner = long_run(20), in0 = long_run(96);
        const std::uint8_t sig[] = {0, 0, 0, 0};
        const char* memo = "conformance memo";
        pchain::SpendInput in;
        in.Kind = 3;
        in.NetworkID = 1;
        in.BlockchainID = id32(0x09);
        in.Outs = {View(out0), View(out1)};
        in.OwnerAddrs = {View(owner)};
        in.Ins = {View(in0)};
        in.SigIndices = {View(sig, 4)};
        in.Memo = View(reinterpret_cast<const std::uint8_t*>(memo), std::strlen(memo));
        emit("spend/full", pchain::NewSpend(in));
    }

    emit("basetx/zero", xvm::NewBaseTx(xvm::BaseTxInput{}));
    {
        probe::InnerInput a, b;
        a.Tag = 1;
        a.Val = 2;
        b.Tag = 3;
        b.Val = 4;
        const Bytes outer = probe::NewInner(a), inner = probe::NewInner(b);
        const std::uint8_t memo[] = {0xDE, 0xAD, 0xBE, 0xEF};
        xvm::BaseTxInput in;
        in.NetworkID = 0xFFFFFFFF;
        in.BlockchainID = id32(0xAB);
        in.Outs = {View(outer)};
        in.Ins = {View(inner)};
        in.Memo = View(memo, 4);
        emit("basetx/full", xvm::NewBaseTx(in));
    }

    write_echo();
}

// ── back: every reader, over bytes the other language may have written ─────

bool unhex(std::string_view s, Bytes& out);

std::string wide_line(View raw) {
    auto v = probe::WrapWide(raw);
    if (!v) return "parse=err";
    const auto child = v->Child();
    char buf[1024];
    std::snprintf(buf, sizeof buf,
                  "flag=%s a8=%u a16=%u a32=%u a64=%llu s8=%d s16=%d s32=%d s64=%lld "
                  "f32=%08x f64=%016llx id=%s name=%.*s blob=%s items=%lld child=%u:%llu",
                  v->Flag() ? "true" : "false", static_cast<unsigned>(v->A8()),
                  static_cast<unsigned>(v->A16()), v->A32(),
                  static_cast<unsigned long long>(v->A64()), static_cast<int>(v->S8()),
                  static_cast<int>(v->S16()), v->S32(), static_cast<long long>(v->S64()),
                  std::bit_cast<std::uint32_t>(v->F32()),
                  static_cast<unsigned long long>(std::bit_cast<std::uint64_t>(v->F64())),
                  hex_of(v->Id()).c_str(), static_cast<int>(v->Name().size()), v->Name().data(),
                  hex_of(v->Blob()).c_str(), static_cast<long long>(v->Items().size()),
                  child.Tag(), static_cast<unsigned long long>(child.Val()));
    return buf;
}

std::string spend_fields(View raw);

std::string back_line(std::string_view name, View raw) {
    char buf[1024];
    if (name.rfind("inner/", 0) == 0) {
        auto v = probe::WrapInner(raw);
        if (!v) return "parse=err";
        std::snprintf(buf, sizeof buf, "tag=%u val=%llu", v->Tag(),
                      static_cast<unsigned long long>(v->Val()));
        return buf;
    }
    if (name.rfind("wide/", 0) == 0) return wide_line(raw);
    if (name.rfind("spend/", 0) == 0) return spend_fields(raw);
    if (name.rfind("basetx/", 0) == 0) {
        auto v = xvm::WrapBaseTx(raw);
        if (!v) return "parse=err";
        std::snprintf(buf, sizeof buf, "net=%u chain=%s outs=%lld ins=%lld memo=%s", v->NetworkID(),
                      hex_of(v->BlockchainID()).c_str(), static_cast<long long>(v->Outs().size()),
                      static_cast<long long>(v->Ins().size()), hex_of(v->Memo()).c_str());
        return buf;
    }
    if (name.rfind("echo/req/", 0) == 0) {
        auto c = zap::rpc::parse_request(raw);
        if (!c) return "parse=err";
        std::snprintf(buf, sizeof buf, "method=%u promise=%u target=%u cap=%s payload=%s", c->method,
                      c->promise_id, c->target, hex_of(c->cap).c_str(), hex_of(c->payload).c_str());
        return buf;
    }
    if (name.rfind("echo/resp/", 0) == 0) {
        auto r = zap::rpc::parse_response(raw);
        if (!r) return "parse=err";
        std::snprintf(buf, sizeof buf, "status=%u promise=%u body=%s", r->status, r->promise_id,
                      hex_of(r->body).c_str());
        return buf;
    }
    return "case=unknown";
}

std::vector<std::string_view> columns(std::string_view line) {
    std::vector<std::string_view> col;
    while (true) {
        const std::size_t tab = line.find('\t');
        if (tab == std::string_view::npos) {
            col.push_back(line);
            return col;
        }
        col.push_back(line.substr(0, tab));
        line = line.substr(tab + 1);
    }
}

int back(const char* path) {
    std::ifstream in(path);
    if (!in) {
        std::fprintf(stderr, "probe back: cannot open %s\n", path);
        return 1;
    }
    std::string line;
    while (std::getline(in, line)) {
        const auto col = columns(line);
        if (col.size() < 3 || col[0] != "W") continue;
        Bytes raw;
        const std::string name(col[1]);
        if (!unhex(col[2], raw)) {
            std::printf("B\t%s\twire=unhex\n", name.c_str());
            continue;
        }
        std::printf("B\t%s\t%s\n", name.c_str(), back_line(name, raw).c_str());
    }
    return 0;
}

// ── read: the generated reader over the corpus ─────────────────────────────

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

std::string spend_line(std::string_view wire) {
    Bytes raw;
    if (!unhex(wire, raw)) return "wire=unhex";
    return spend_fields(raw);
}

std::string spend_fields(View raw) {
    auto s = pchain::WrapSpend(raw);
    if (!s) return "parse=err";
    char buf[512];
    std::snprintf(buf, sizeof buf,
                  "parse=ok kind=%u net=%u chain=%s outs=%lld owners=%lld ins=%lld sigs=%lld memo=%s",
                  static_cast<unsigned>(s->Kind()), static_cast<unsigned>(s->NetworkID()),
                  hex_of(s->BlockchainID()).c_str(), static_cast<long long>(s->Outs().size()),
                  static_cast<long long>(s->OwnerAddrs().size()),
                  static_cast<long long>(s->Ins().size()),
                  static_cast<long long>(s->SigIndices().size()), hex_of(s->Memo()).c_str());
    return buf;
}

int read(const char* path) {
    std::ifstream in(path);
    if (!in) {
        std::fprintf(stderr, "probe read: cannot open %s\n", path);
        return 1;
    }
    std::string line;
    while (std::getline(in, line)) {
        if (line.rfind("V\t", 0) != 0) continue;
        const auto col = columns(line);
        if (col.size() < 5 || col[2] != "P" || col[3] != "tx") continue;
        std::printf("R\t%.*s\t%s\n", static_cast<int>(col[1].size()), col[1].data(),
                    spend_line(col[4]).c_str());
    }
    return 0;
}

}  // namespace

int main(int argc, char** argv) {
    if (argc < 2) {
        std::fprintf(stderr, "usage: probe write | probe back CASES | probe read VECTORS.TSV\n");
        return 2;
    }
    if (std::strcmp(argv[1], "write") == 0) {
        write();
        return 0;
    }
    if (std::strcmp(argv[1], "back") == 0) {
        if (argc < 3) {
            std::fprintf(stderr, "probe back: need a write-output path\n");
            return 2;
        }
        return back(argv[2]);
    }
    if (std::strcmp(argv[1], "read") == 0) {
        if (argc < 3) {
            std::fprintf(stderr, "probe read: need a vectors.tsv path\n");
            return 2;
        }
        return read(argv[2]);
    }
    std::fprintf(stderr, "probe: unknown mode %s\n", argv[1]);
    return 2;
}
