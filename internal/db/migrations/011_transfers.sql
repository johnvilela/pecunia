-- Money moving between two accounts you own: an outcome on the source and an
-- income on the destination, sharing this group. It is not income and it is not
-- an expense -- nothing was earned and nothing was consumed -- which is why
-- recording it as an ordinary pair of transactions inflated both totals and
-- made a month read worse and better than it was.
--
-- The group is the id of the outcome row, the leg the money left from, so the
-- origin of a transfer is a fact of the data rather than a convention about
-- which row was written first.
--
-- Two rows rather than one column pair because that is what keeps every
-- existing rule intact: each leg still names exactly one target, still carries a
-- positive value with the direction in its kind, and still moves one balance.
-- A single row with from/to columns would have broken the
-- (account_id IS NULL) <> (card_id IS NULL) CHECK, Signed(), CardDelta() and
-- every renderer that assumes one target.
--
-- It also gives the two legs independent amounts, which is what carries a
-- cross-currency transfer -- R$500.00 out, $92.00 in, and no rate stored
-- anywhere -- and a fee, which is the same thing in one currency.
--
-- NULL on every transaction that is not a transfer, which is nearly all of them.
-- Nothing here constrains a group to exactly two rows; SQLite cannot say it, so
-- the store is the only thing that ever writes one and it writes both or
-- neither.
ALTER TABLE transactions ADD COLUMN transfer_group INTEGER;

-- Every read of a transfer is the sibling lookup: same group, other id.
CREATE INDEX transactions_transfer_group ON transactions (transfer_group);
