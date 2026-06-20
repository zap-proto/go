package echo

struct Ping {
    Seq u64 @0
}

struct Pong {
    Seq u64 @0
}

interface Echo {
    ping(req: Ping) returns (resp: Pong)
    notify(req: Ping)
    health() returns (resp: Pong)
    shutdown()
}
