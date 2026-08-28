package transactions

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"pecunia/internal/db"
)

// newTestDB gives the caller its own SQLite file in its own temp dir, so no two
// cases ever share state. Call it inside the subtest, not the parent.
//
// A real file rather than :memory: — the CHECK constraints and the foreign keys
// are most of what the schema is worth, and only the migration path builds them.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	t.Setenv("PECUNIA_DB", filepath.Join(t.TempDir(), "pecunia.db"))
	conn, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// fixtures puts one account, one card and one category in the database and
// returns their ids. The schema cases below need something real to point at.
func fixtures(t *testing.T, conn *sql.DB) (account, card, category int64) {
	t.Helper()
	exec := func(query string, args ...any) int64 {
		res, err := conn.Exec(query, args...)
		if err != nil {
			t.Fatal(err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	account = exec(`INSERT INTO accounts (code, name, color, balance, currency)
		VALUES ('INTER', 'Banco Inter', 'orange', 100000, 'BRL')`)
	card = exec(`INSERT INTO credit_cards (code, name, color, credit_limit, balance, currency, closing_day, due_day)
		VALUES ('NUCRD', 'Nubank', 'violet', 500000, 0, 'BRL', 15, 22)`)
	category = exec(`INSERT INTO categories (code, name, color) VALUES ('FOOD1', 'Food', 'lime')`)
	return account, card, category
}

// insert writes one row straight through the schema, so what comes back is the
// constraint's own verdict and not a Go guard standing in front of it.
func insert(conn *sql.DB, cols string, args ...any) error {
	marks := strings.TrimSuffix(strings.Repeat("?, ", len(args)), ", ")
	_, err := conn.Exec(`INSERT INTO transactions (`+cols+`) VALUES (`+marks+`)`, args...)
	return err
}

func TestSchemaTargets(t *testing.T) {
	t.Run("an account target is accepted", func(t *testing.T) {
		conn := newTestDB(t)
		acc, _, _ := fixtures(t, conn)
		if err := insert(conn, `title, account_id, value, kind, date`,
			"Groceries", acc, 12000, "outcome", "2026-08-08"); err != nil {
			t.Fatalf("insert with an account = %v", err)
		}
	})

	t.Run("a card target is accepted", func(t *testing.T) {
		conn := newTestDB(t)
		_, card, _ := fixtures(t, conn)
		if err := insert(conn, `title, card_id, value, kind, date`,
			"Groceries", card, 12000, "outcome", "2026-08-08"); err != nil {
			t.Fatalf("insert with a card = %v", err)
		}
	})

	t.Run("both targets at once is rejected", func(t *testing.T) {
		conn := newTestDB(t)
		acc, card, _ := fixtures(t, conn)
		if err := insert(conn, `title, account_id, card_id, value, kind, date`,
			"Groceries", acc, card, 12000, "outcome", "2026-08-08"); err == nil {
			t.Fatal("insert with both an account and a card succeeded; want the CHECK to reject it")
		}
	})

	t.Run("no target at all is rejected", func(t *testing.T) {
		conn := newTestDB(t)
		fixtures(t, conn)
		if err := insert(conn, `title, value, kind, date`,
			"Groceries", 12000, "outcome", "2026-08-08"); err == nil {
			t.Fatal("insert with neither an account nor a card succeeded; want the CHECK to reject it")
		}
	})
}

func TestSchemaColumns(t *testing.T) {
	cases := []struct {
		name  string
		value int64
		kind  string
		date  string
	}{
		{"an unknown kind", 12000, "refund", "2026-08-08"},
		{"a zero value", 0, "outcome", "2026-08-08"},
		{"a negative value", -12000, "outcome", "2026-08-08"},
		{"a malformed date", 12000, "outcome", "08/08/2026"},
	}
	for _, tc := range cases {
		t.Run(tc.name+" is rejected", func(t *testing.T) {
			conn := newTestDB(t)
			acc, _, _ := fixtures(t, conn)
			if err := insert(conn, `title, account_id, value, kind, date`,
				"Groceries", acc, tc.value, tc.kind, tc.date); err == nil {
				t.Fatalf("insert with %s succeeded; want the CHECK to reject it", tc.name)
			}
		})
	}

	t.Run("the description and the category are optional", func(t *testing.T) {
		conn := newTestDB(t)
		acc, _, _ := fixtures(t, conn)
		if err := insert(conn, `title, account_id, value, kind, date`,
			"Groceries", acc, 12000, "outcome", "2026-08-08"); err != nil {
			t.Fatal(err)
		}
		var desc string
		var category sql.NullInt64
		if err := conn.QueryRow(`SELECT description, category_id FROM transactions`).
			Scan(&desc, &category); err != nil {
			t.Fatal(err)
		}
		if desc != "" {
			t.Fatalf("description defaulted to %q; want empty", desc)
		}
		if category.Valid {
			t.Fatalf("category_id defaulted to %d; want NULL", category.Int64)
		}
	})

	t.Run("the timestamps fill themselves in", func(t *testing.T) {
		conn := newTestDB(t)
		acc, _, _ := fixtures(t, conn)
		if err := insert(conn, `title, account_id, value, kind, date`,
			"Groceries", acc, 12000, "outcome", "2026-08-08"); err != nil {
			t.Fatal(err)
		}
		var created, updated string
		if err := conn.QueryRow(`SELECT created_at, updated_at FROM transactions`).
			Scan(&created, &updated); err != nil {
			t.Fatal(err)
		}
		if created == "" || updated == "" {
			t.Fatalf("timestamps = %q / %q; want both filled in", created, updated)
		}
	})
}

func TestSchemaReferences(t *testing.T) {
	t.Run("deleting a category leaves the transaction without one", func(t *testing.T) {
		conn := newTestDB(t)
		acc, _, cat := fixtures(t, conn)
		if err := insert(conn, `title, account_id, category_id, value, kind, date`,
			"Groceries", acc, cat, 12000, "outcome", "2026-08-08"); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(`DELETE FROM categories WHERE id = ?`, cat); err != nil {
			t.Fatalf("delete category = %v; want it allowed", err)
		}
		var category sql.NullInt64
		if err := conn.QueryRow(`SELECT category_id FROM transactions`).Scan(&category); err != nil {
			t.Fatal(err)
		}
		if category.Valid {
			t.Fatalf("category_id = %d after the category was deleted; want NULL", category.Int64)
		}
	})

	for _, tc := range []struct{ name, table, column string }{
		{"account", "accounts", "account_id"},
		{"credit card", "credit_cards", "card_id"},
	} {
		t.Run("deleting a referenced "+tc.name+" is refused", func(t *testing.T) {
			conn := newTestDB(t)
			acc, card, _ := fixtures(t, conn)
			target := map[string]int64{"account_id": acc, "card_id": card}[tc.column]
			if err := insert(conn, `title, `+tc.column+`, value, kind, date`,
				"Groceries", target, 12000, "outcome", "2026-08-08"); err != nil {
				t.Fatal(err)
			}
			if _, err := conn.Exec(`DELETE FROM `+tc.table+` WHERE id = ?`, target); err == nil {
				t.Fatalf("delete %s = nil; want the foreign key to refuse it", tc.name)
			}
		})
	}

	t.Run("deleting a transaction takes its tags with it", func(t *testing.T) {
		conn := newTestDB(t)
		acc, _, _ := fixtures(t, conn)
		if err := insert(conn, `title, account_id, value, kind, date`,
			"Groceries", acc, 12000, "outcome", "2026-08-08"); err != nil {
			t.Fatal(err)
		}
		var id int64
		if err := conn.QueryRow(`SELECT id FROM transactions`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(`INSERT INTO transaction_tags (transaction_id, tag) VALUES (?, 'food')`, id); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(`DELETE FROM transactions WHERE id = ?`, id); err != nil {
			t.Fatal(err)
		}
		var n int
		if err := conn.QueryRow(`SELECT count(*) FROM transaction_tags`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("%d tag(s) left behind; want the cascade to clear them", n)
		}
	})

	t.Run("the same tag may not be listed twice on one transaction", func(t *testing.T) {
		conn := newTestDB(t)
		acc, _, _ := fixtures(t, conn)
		if err := insert(conn, `title, account_id, value, kind, date`,
			"Groceries", acc, 12000, "outcome", "2026-08-08"); err != nil {
			t.Fatal(err)
		}
		var id int64
		if err := conn.QueryRow(`SELECT id FROM transactions`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(`INSERT INTO transaction_tags (transaction_id, tag) VALUES (?, 'food')`, id); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(`INSERT INTO transaction_tags (transaction_id, tag) VALUES (?, 'food')`, id); err == nil {
			t.Fatal("the same tag inserted twice succeeded; want the primary key to reject it")
		}
	})
}

func TestSchemaAdjustmentKind(t *testing.T) {
	t.Run("an adjustment is accepted", func(t *testing.T) {
		conn := newTestDB(t)
		acc, _, _ := fixtures(t, conn)
		if err := insert(conn, `title, account_id, value, kind, date`,
			"Balance adjustment", acc, 5000, "adjustment", "2026-08-27"); err != nil {
			t.Fatalf("insert an adjustment = %v", err)
		}
	})

	t.Run("an adjustment may be negative", func(t *testing.T) {
		conn := newTestDB(t)
		acc, _, _ := fixtures(t, conn)
		if err := insert(conn, `title, account_id, value, kind, date`,
			"Balance adjustment", acc, -5000, "adjustment", "2026-08-27"); err != nil {
			t.Fatalf("insert a negative adjustment = %v", err)
		}
	})

	t.Run("an adjustment of zero is rejected", func(t *testing.T) {
		conn := newTestDB(t)
		acc, _, _ := fixtures(t, conn)
		if err := insert(conn, `title, account_id, value, kind, date`,
			"Balance adjustment", acc, 0, "adjustment", "2026-08-27"); err == nil {
			t.Fatal("an adjustment of zero was written; want the CHECK to refuse it")
		}
	})

	t.Run("income and outcome still refuse zero and negative", func(t *testing.T) {
		conn := newTestDB(t)
		acc, _, _ := fixtures(t, conn)
		for _, kind := range []string{"income", "outcome"} {
			for _, value := range []int64{0, -5000} {
				if err := insert(conn, `title, account_id, value, kind, date`,
					"Groceries", acc, value, kind, "2026-08-27"); err == nil {
					t.Fatalf("a %s of %d was written; want the CHECK to refuse it", kind, value)
				}
			}
		}
	})

	t.Run("an unknown kind is still refused", func(t *testing.T) {
		conn := newTestDB(t)
		acc, _, _ := fixtures(t, conn)
		if err := insert(conn, `title, account_id, value, kind, date`,
			"Groceries", acc, 5000, "refund", "2026-08-27"); err == nil {
			t.Fatal("an unknown kind was written; want the CHECK to refuse it")
		}
	})
}
