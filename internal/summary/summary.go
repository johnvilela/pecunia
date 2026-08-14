// Package summary is the one screen that answers "where do I stand": what moved
// today, what needs paying, what the accounts hold and where the goals are.
//
// It owns no table and writes no SQL of its own — every figure here comes from
// a store that already exists, which is why a summary can never disagree with
// the command it summarises.
package summary

import (
	"database/sql"
	"time"

	"kakei/internal/accounts"
	"kakei/internal/bills"
	"kakei/internal/budgets"
	"kakei/internal/cards"
	"kakei/internal/goals"
	"kakei/internal/recurring"
	"kakei/internal/transactions"
)

// Period is the window a summary covers, both ends inclusive. A day is a period
// whose ends are the same date, which is all anything downstream needs to know
// about the difference — there is no month one day long.
type Period struct{ From, To string } // YYYY-MM-DD

func (p Period) Day() bool { return p.From == p.To }

// Contains is whether a date falls in the window. Dates are compared as text
// everywhere in kakei: YYYY-MM-DD sorts the same way it reads, and a string
// carries no timezone to disagree about.
func (p Period) Contains(date string) bool { return p.From <= date && date <= p.To }

// Board is what falls in one window: the recurring bills and the credit-card
// statements together. They are two tables and the same worry.
type Board struct {
	Bills      []recurring.Bill
	Statements []bills.Bill
}

func (b Board) Empty() bool { return len(b.Bills) == 0 && len(b.Statements) == 0 }

// Summary is one screen of everything. Every field is what some existing
// renderer already takes, so nothing here is worked out twice.
type Summary struct {
	Period Period
	Title  string    // "Thursday, 13 August 2026", or "August 2026"
	Today  time.Time // what every status is asked against, never the wall clock

	Ledger []transactions.Transaction // the window's rows, newest first

	// In and Out are keyed by currency code and both hold positive figures —
	// the direction is the field, not the sign. There is no combined total,
	// here or anywhere: centavos and satoshis do not add up, and there is no
	// rate in kakei to make them.
	In, Out map[string]int64
	// MTD is what has gone out since the 1st. Nil on a month summary, where the
	// totals above already are the month.
	MTD map[string]int64

	Due  Board // payable now, or already late
	Soon Board // lands inside the next seven days, today excluded

	Accounts []accounts.Account // frozen ones left out, as `kakei ac` leaves them
	Cards    []cards.Card
	Goals    []goals.Goal
	// Budgets are read for the month the window falls in, whatever the window
	// is: a budget is a cap on a month, so a day summary shows the month its day
	// belongs to rather than a day's worth of one. Archived ones are left out,
	// as `kakei bg` leaves them.
	Budgets []budgets.Budget
}

// empty is a database nobody has put anything in yet, as opposed to a quiet
// day. A quiet day still has balances to show.
func (s Summary) empty() bool {
	return len(s.Ledger) == 0 && s.Due.Empty() && s.Soon.Empty() &&
		len(s.Accounts) == 0 && len(s.Cards) == 0 && len(s.Goals) == 0 &&
		len(s.Budgets) == 0
}

// live is whether the window takes in today. What is due is a fact about now,
// so a window that is already over has nothing to say about it — which is not
// the same as having nothing due, and must not read as it.
func (s Summary) live() bool {
	return s.Period.Contains(s.Today.Format(transactions.DateLayout))
}

// week is how far ahead "what is coming" looks.
const week = 7

// periodTitle is what the window is called at the top of the screen: the
// weekday and the date for a day, the month alone for a month.
func periodTitle(p Period) string {
	d, err := time.Parse(transactions.DateLayout, p.From)
	if err != nil {
		return p.From
	}
	if p.Day() {
		return d.Format("Monday, 2 January 2006")
	}
	return d.Format("January 2006")
}

// Collect reads one window of everything.
//
// today is handed in rather than asked for, so every status on the screen is
// judged against the same date — a card statement worked out from the wall
// clock while the bills were worked out from a given day is the one answer that
// would disagree with the rest of the screen.
func Collect(conn *sql.DB, p Period, today time.Time) (Summary, error) {
	s := Summary{Period: p, Title: periodTitle(p), Today: today}
	if err := s.collectLedger(conn); err != nil {
		return Summary{}, err
	}
	if err := s.collectBalances(conn); err != nil {
		return Summary{}, err
	}

	// Skipping this skips the per-card statement walk with it, which is the
	// expensive half of a summary. The balances and the goals stay as they are
	// now either way — they have no history to read.
	if !s.live() {
		return s, nil
	}
	if err := s.collectDue(conn); err != nil {
		return Summary{}, err
	}
	return s, nil
}

// collectLedger reads the window's transactions and totals them. On a day
// summary it reads from the 1st instead, which is what makes the month-to-date
// figure free: the rows are already here, and there is no aggregate query
// anywhere in kakei to ask for it a second way.
func (s *Summary) collectLedger(conn *sql.DB) error {
	from := s.Period.From
	if s.Period.Day() {
		from = transactions.CycleOf(s.Period.From) + "-01"
		s.MTD = map[string]int64{}
	}
	rows, err := transactions.NewStore(conn).List(transactions.Filter{From: from, To: s.Period.To})
	if err != nil {
		return err
	}

	s.In, s.Out = map[string]int64{}, map[string]int64{}
	for _, tr := range rows {
		out := tr.Kind == transactions.KindOutcome
		// A transfer is money moving between two accounts you own: nothing was
		// earned and nothing was consumed, and counting its legs would inflate
		// both directions at once. It is still listed — both legs really moved —
		// but it totals to nothing, because that is what it is.
		if tr.IsTransfer() {
			if s.Period.Contains(tr.Date) {
				s.Ledger = append(s.Ledger, tr)
			}
			continue
		}
		if out && s.MTD != nil {
			s.MTD[tr.Currency] += tr.Value
		}
		if !s.Period.Contains(tr.Date) {
			continue
		}
		s.Ledger = append(s.Ledger, tr)
		// The direction is the map, not the sign: both hold what they hold as
		// a positive figure, and only the net has a sign to carry.
		if out {
			s.Out[tr.Currency] += tr.Value
		} else {
			s.In[tr.Currency] += tr.Value
		}
	}
	return nil
}

func (s *Summary) collectBalances(conn *sql.DB) error {
	accs, err := accounts.NewStore(conn).List()
	if err != nil {
		return err
	}
	for _, a := range accs {
		// Frozen accounts are out of play, and `kakei ac` hides them too.
		if !a.IsFrozen {
			s.Accounts = append(s.Accounts, a)
		}
	}
	if s.Cards, err = cards.NewStore(conn).List(); err != nil {
		return err
	}
	if s.Goals, err = goals.NewStore(conn).List(); err != nil {
		return err
	}
	// A budget caps a month, so the window's own month is what it is read for —
	// and unlike what is due, it is a fact about that month rather than about
	// now, so a window that is already over still has one worth reading.
	s.Budgets, err = budgets.NewStore(conn).List(transactions.CycleOf(s.Period.From), false)
	return err
}

// collectDue splits what is owed into what needs paying now and what lands
// inside the week. The two are worked out from the same reads, because a bill
// in one must never be in the other.
func (s *Summary) collectDue(conn *sql.DB) error {
	day := s.Today.Format(transactions.DateLayout)
	ahead := s.Today.AddDate(0, 0, week).Format(transactions.DateLayout)

	all, err := recurring.NewStore(conn).List(false)
	if err != nil {
		return err
	}
	owing := make(map[int64]bool, len(all))
	for _, b := range all {
		if st := b.Current(s.Today).Status; st == recurring.StatusOpen || st == recurring.StatusOverdue {
			s.Due.Bills = append(s.Due.Bills, b)
			owing[b.ID] = true
		}
	}

	// Current stands at the oldest cycle still unpaid, so a bill already
	// settled for this month says nothing about the one opening in five days —
	// and rent is exactly the bill nobody wants surprised by. Seven days can
	// never span more than one month boundary, so this month and the next are
	// every cycle the window can reach.
	next := time.Date(s.Today.Year(), s.Today.Month()+1, 1, 0, 0, 0, 0, s.Today.Location())
	cycles := []string{s.Today.Format(recurring.CycleLayout), next.Format(recurring.CycleLayout)}
	for _, b := range all {
		if owing[b.ID] {
			continue // one bill, one section: pay it now beats it opens again soon
		}
		for _, c := range cycles {
			occ := b.Occurrence(c, s.Today)
			if occ.Status == recurring.StatusUpcoming && day < occ.OpenOn && occ.OpenOn <= ahead {
				s.Soon.Bills = append(s.Soon.Bills, b)
				break
			}
		}
	}

	// ponytail: one Unpaid per card, and every one of them writes — statements
	// are generated on read. `kakei cc bill` already walks every card the same
	// way, so a summary is no more expensive than a command that ships.
	store := bills.NewStoreAt(conn, func() time.Time { return s.Today })
	for _, c := range s.Cards {
		owed, err := store.Unpaid(c)
		if err != nil {
			return err
		}
		for _, b := range owed {
			// Unpaid already leaves the still-open cycle out, so everything
			// here is a total that has stopped moving. The two dates partition:
			// nothing can be both past its due date and inside the week ahead.
			switch {
			case b.DueOn <= day:
				s.Due.Statements = append(s.Due.Statements, b)
			case b.DueOn <= ahead:
				s.Soon.Statements = append(s.Soon.Statements, b)
			}
		}
	}
	return nil
}
