# A remark about this file that belongs to nobody. The blank line below
# ends it, so it documents the schema rather than the first struct.

# Ledger is the read side of an account: what a caller may ask about a
# balance without being trusted to compute one.
package ledger

# An amount held by one account, at one height.
#
# The height is carried WITH the amount because a balance without the
# height it was read at is a number that was true once.
struct Balance {
    # The account the amount belongs to, 20 bytes, unhashed.
    Owner  bytes_fixed[20] @0
    # Whole units. Fractions are not representable here on purpose.
    Amount u64             @20  # not a float, and never will be
    Height u64             @28
}

# What a caller asks a balance for.
struct Query {
    Owner bytes_fixed[20] @0
}

# Ledger answers questions about balances. It never moves one.
interface Ledger {
    # Read one account's balance at the chain's last accepted height.
    lookup(req: Query) returns (resp: Balance)

    # Answer the height every lookup would currently be answered at.
    height() returns (resp: Balance)
}
