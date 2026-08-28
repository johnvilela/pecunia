package recurring

import (
	"strings"
	"testing"

	"pecunia/internal/transactions"
)

// bills is the board every case here renders: one of each state, deliberately
// out of order so the sort has something to do.
func bills() []Bill {
	netflix := energy()
	netflix.ID, netflix.Code, netflix.Name = 2, "NFLIX", "Netflix"
	netflix.Expected, netflix.OpenDay, netflix.DueDay = 5590, 22, 25
	netflix.CreatedAt = "2026-08-01 09:00:00"

	rent := energy()
	rent.ID, rent.Code, rent.Name = 3, "RENTX", "Rent"
	rent.Expected, rent.OpenDay, rent.DueDay = 180000, 1, 10
	rent.CreatedAt = "2026-08-01 09:00:00"
	rent.Payments = map[string]Tally{"2026-08": {Value: 180000, Count: 1}}

	// ENERG opened on the 5th and was due on the 15th: overdue on the 20th.
	return []Bill{netflix, rent, energy()}
}

// owedRow is the line the total sits on, so a case can check the figure is
// beside its own label instead of adrift somewhere under the table.
func owedRow(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "still owed") {
			return line
		}
	}
	return ""
}

func TestBoard(t *testing.T) {
	out := Board(bills(), on("2026-08-20"))

	t.Run("puts what is late first", func(t *testing.T) {
		energyAt, rentAt := strings.Index(out, "ENERG"), strings.Index(out, "RENTX")
		if energyAt == -1 || rentAt == -1 {
			t.Fatalf("board is missing a bill:\n%s", out)
		}
		if energyAt > rentAt {
			t.Errorf("the paid bill came before the overdue one:\n%s", out)
		}
	})

	t.Run("is a table with a column per thing", func(t *testing.T) {
		for _, want := range []string{"BILL", "AMOUNT", "STATUS", "WHEN"} {
			if !strings.Contains(out, want) {
				t.Errorf("board has no %s column:\n%s", want, out)
			}
		}
	})

	t.Run("says how late", func(t *testing.T) {
		if !strings.Contains(out, "5 days late") {
			t.Errorf("board does not say how late ENERG is:\n%s", out)
		}
	})

	t.Run("names every state", func(t *testing.T) {
		for _, want := range []string{"overdue", "upcoming", "paid"} {
			if !strings.Contains(out, want) {
				t.Errorf("board never says %q:\n%s", want, out)
			}
		}
	})

	t.Run("totals what is still owed", func(t *testing.T) {
		// ENERG overdue at R$214.90, NFLIX not open yet, RENTX paid — so the
		// total is ENERG alone, not the three of them added up.
		row := owedRow(out)
		if row == "" {
			t.Fatalf("board does not total what is owed:\n%s", out)
		}
		if !strings.Contains(row, "R$214.90") {
			t.Errorf("the total is not beside its own label: %q", row)
		}
	})

	t.Run("closes the table with the total rather than floating it below", func(t *testing.T) {
		if row := strings.TrimSpace(owedRow(out)); !strings.HasPrefix(row, "│") {
			t.Errorf("the total hangs under the table instead of closing it: %q", row)
		}
	})

	t.Run("counts amounts per currency", func(t *testing.T) {
		mixed := bills()
		mixed[0].Currency = "USD"
		mixed[0].Payments = nil
		mixed[0].OpenDay, mixed[0].DueDay = 1, 5 // overdue on the 20th too
		got := Board(mixed, on("2026-08-20"))
		if !strings.Contains(got, "$55.90") || !strings.Contains(got, "R$214.90") {
			t.Errorf("two currencies were added up as one:\n%s", got)
		}
	})

	t.Run("an archived bill is owed nothing and says so", func(t *testing.T) {
		withGym := bills()
		gym := energy()
		gym.ID, gym.Code, gym.Name, gym.Expected = 4, "GYMXX", "Academia", 12990
		gym.Active = false
		got := Board(append(withGym, gym), on("2026-08-20"))
		if !strings.Contains(got, "archived") {
			t.Errorf("an archived bill does not say so on the board:\n%s", got)
		}
		// ENERG alone is owed: the archived R$129.90 is not money due.
		if row := owedRow(got); !strings.Contains(row, "R$214.90") {
			t.Errorf("an archived bill was counted as owing: %q", row)
		}
	})

	t.Run("an empty board still renders", func(t *testing.T) {
		if got := Board(nil, on("2026-08-20")); strings.TrimSpace(got) == "" {
			t.Error("an empty board rendered nothing at all")
		}
	})
}

func TestDetails(t *testing.T) {
	b := energy()
	b.Payments = map[string]Tally{"2026-08": {Value: 21490, Count: 1}}
	paid := []transactions.Transaction{
		{Date: "2026-08-08", Value: 21490, Kind: transactions.KindOutcome, Cycle: "2026-08", Currency: "BRL"},
		{Date: "2026-07-09", Value: 19000, Kind: transactions.KindOutcome, Cycle: "2026-07", Currency: "BRL"},
	}
	out := Details(b, paid, on("2026-08-20"))

	t.Run("leads with the bill", func(t *testing.T) {
		for _, want := range []string{"ENERG", "Energy"} {
			if !strings.Contains(out, want) {
				t.Errorf("card is missing %q:\n%s", want, out)
			}
		}
	})

	t.Run("says where the cycle stands", func(t *testing.T) {
		if !strings.Contains(out, "paid") || !strings.Contains(out, "2026-08") {
			t.Errorf("card does not say the cycle is settled:\n%s", out)
		}
	})

	t.Run("shows the average really paid", func(t *testing.T) {
		// (21490 + 19000) / 2
		if !strings.Contains(out, "R$202.45") {
			t.Errorf("card is missing the average:\n%s", out)
		}
	})

	t.Run("lists the payments themselves", func(t *testing.T) {
		for _, want := range []string{"2026-08-08", "R$214.90", "2026-07-09", "R$190.00"} {
			if !strings.Contains(out, want) {
				t.Errorf("card is missing %q:\n%s", want, out)
			}
		}
	})

	t.Run("a bill nobody has paid yet has no history block", func(t *testing.T) {
		fresh := energy()
		got := Details(fresh, nil, on("2026-08-20"))
		if strings.Contains(got, "average") {
			t.Errorf("a bill with no payments showed an average:\n%s", got)
		}
		if !strings.Contains(got, "overdue") {
			t.Errorf("card does not say the bill is overdue:\n%s", got)
		}
	})

	t.Run("says an archived bill is archived", func(t *testing.T) {
		gone := energy()
		gone.Active = false
		if got := Details(gone, nil, on("2026-08-20")); !strings.Contains(got, "archived") {
			t.Errorf("an archived bill does not say so:\n%s", got)
		}
	})
}

func TestLabel(t *testing.T) {
	if got := Label(energy()); !strings.Contains(got, "ENERG") || !strings.Contains(got, "Energy") {
		t.Errorf("label = %q, want the code and the name", got)
	}
}
