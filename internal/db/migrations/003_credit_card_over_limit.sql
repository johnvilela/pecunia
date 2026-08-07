-- Whether the issuer lets this card be used past its limit. Off by default:
-- a card declines at the limit unless it says otherwise.
ALTER TABLE credit_cards ADD COLUMN over_limit_allowed INTEGER NOT NULL DEFAULT 0;
