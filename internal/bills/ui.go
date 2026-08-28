package bills

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"pecunia/internal/accounts"
	"pecunia/internal/cards"
	"pecunia/internal/core"
)

// statusColor follows the rule the cards table is pinned to: no green anywhere
// on a credit card, ever. A bill that is settled is out of play, so it is dim;
// one still owing money is the only thing worth colouring.
func statusColor(b Bill) string {
	switch b.Status {
	case StatusClosed, StatusPartial:
		return core.ColorByName("red").Hex
	default:
		return core.DimColor
	}
}

func styledStatus(b Bill) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(statusColor(b))).Render(b.Status)
}

// left is what the bill still owes, or nothing at all when it owes nothing —
// "R$0.00" on every settled row, and a figure beside every open one, are both
// noise the eye has to read past.
func left(b Bill) string {
	if b.Owed() == 0 {
		return core.DimStyle.Render("—")
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(core.ColorByName("red").Hex)).
		Render(b.Fmt(b.Owed()))
}

// Table is the static list output — no alt screen, so `pecunia cc bill | grep`
// works. The card column earns its place even on one card's bills: it is what
// makes the all-cards list read the same as the filtered one.
func Table(bs []Bill) string {
	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(core.DimStyle).
		Headers("CARD", "MONTH", "CLOSES", "DUE", "TOTAL", "LEFT", "STATUS").
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return core.HeaderStyle.Padding(0, 1)
			}
			// The two money columns are the ones the eye runs down.
			if col == 4 || col == 5 {
				return lipgloss.NewStyle().Padding(0, 1).Align(lipgloss.Right)
			}
			return lipgloss.NewStyle().Padding(0, 1)
		})
	for _, b := range bs {
		t.Row(cards.Label(b.Card), b.Month(),
			core.DimStyle.Render(b.ClosesOn), core.DimStyle.Render(b.DueOn),
			b.Fmt(b.Total), left(b), styledStatus(b))
	}
	return t.Render()
}

// chargeLine is one row inside the detail card: when, what, and how much.
func chargeLine(b Bill, ch Charge) string {
	title := ch.Title
	if ch.Count > 1 {
		title += " " + core.DimStyle.Render("("+
			strconv.FormatInt(ch.Seq, 10)+"/"+strconv.FormatInt(ch.Count, 10)+")")
	}
	// A card income is a credit against the bill, so it is the one thing here
	// that reads with a sign.
	amount := b.Fmt(ch.Value)
	if ch.Kind == "income" {
		amount = "-" + amount
	}
	return core.DimStyle.Render(ch.Date) + "  " + title + "  " +
		core.DimStyle.Render("#"+strconv.FormatInt(ch.ID, 10)) + "  " + amount
}

// cardWidth is the narrowest the detail card gets; longer lines widen it.
const cardWidth = 44

// Details renders one bill as a card in its credit card's colour. live is what
// the bill's period sums to right now: on a closed bill the total is a frozen
// snapshot, and when the two disagree the card says so rather than quietly
// showing a number the ledger no longer agrees with.
func Details(b Bill, charges []Charge, live int64) string {
	accent := lipgloss.Color(b.Card.Col().Hex)
	from, to := b.Period()

	lines := []string{
		cards.Label(b.Card),
		core.DimStyle.Render(from + " → " + to),
		"",
		lipgloss.NewStyle().Bold(true).Render(b.Fmt(b.Total)) + "  " + styledStatus(b),
	}
	if b.Paid > 0 {
		lines = append(lines, core.DimStyle.Render(b.Fmt(b.Paid)+" paid"))
	}
	if b.Owed() > 0 {
		lines = append(lines, left(b)+" "+core.DimStyle.Render("left"))
	}
	lines = append(lines, core.DimStyle.Render("due "+b.DueOn))

	if live != b.Total {
		// The trade a stored total buys: it stops moving when the cycle closes,
		// so an edit to an old transaction leaves it behind. Better said out loud
		// than discovered.
		lines = append(lines, "", core.DimStyle.Render("≠ the ledger now sums "+b.Fmt(live)))
	}

	lines = append(lines, "")
	if len(charges) == 0 {
		lines = append(lines, core.DimStyle.Render("nothing charged in this period"))
	}
	for _, ch := range charges {
		lines = append(lines, chargeLine(b, ch))
	}

	body := lipgloss.JoinVertical(lipgloss.Left, lines...)
	// Width covers the padding too, so the content gets it back — without the +4
	// the longest line wraps.
	w := lipgloss.Width(body) + 4
	if w < cardWidth {
		w = cardWidth
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accent).
		Padding(0, 2).
		Width(w).
		Render(body) + "\n"
}

// Payment is what the pay form comes back with: which account pays, how much,
// and when.
type Payment struct {
	AccountID int64
	Value     int64
	Date      string
}

// PayForm asks how to settle one bill. The bill itself is chosen before the form
// opens, so the amount can start at what is actually still owed — a partial
// payment is then a matter of typing over it.
func PayForm(b Bill, accs []accounts.Account) (Payment, error) {
	var p Payment

	var opts []huh.Option[int64]
	for _, a := range accs {
		// A frozen account is out of play, the same as it is in the transaction
		// form.
		if a.IsFrozen {
			continue
		}
		opts = append(opts, huh.NewOption(
			accounts.Label(a)+"  "+core.DimStyle.Render(a.Cur().Symbol+a.Amount()), a.ID))
	}
	if len(opts) == 0 {
		return p, errors.New("no account to pay from — create one with: pecunia ac n")
	}

	cur := b.Card.Cur()
	account := opts[0].Value
	amount := core.FormatAmount(b.Owed(), cur)
	date := time.Now().Format(dateLayout)

	err := huh.NewForm(huh.NewGroup(
		huh.NewSelect[int64]().Title("Pay from").Options(opts...).Value(&account),
		huh.NewInput().Title("Amount").
			Description("pre-filled with what is left — type over it to pay part of it").
			Value(&amount).Validate(func(v string) error {
			n, err := core.ParseAmount(v, cur)
			if err != nil {
				return err
			}
			if n <= 0 {
				return errors.New("amount must be more than zero")
			}
			return nil
		}),
		huh.NewInput().Title("Date").Description(dateLayout).Value(&date).
			Validate(func(v string) error {
				_, err := time.Parse(dateLayout, strings.TrimSpace(v))
				if err != nil {
					return errors.New("date must be YYYY-MM-DD")
				}
				return nil
			}),
	).Title("Pay bill " + b.ClosesOn + " · " + b.Fmt(b.Owed()) + " left")).
		WithTheme(huh.ThemeCharm()).Run()
	if err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return p, core.ErrCancelled
		}
		return p, err
	}

	// huh skips its validators when stdin ends mid-form, so both of these have to
	// hold on their own rather than trusting what came back.
	value, err := core.ParseAmount(amount, cur)
	if err != nil {
		return p, err
	}
	d, err := time.Parse(dateLayout, strings.TrimSpace(date))
	if err != nil {
		return p, err
	}
	return Payment{AccountID: account, Value: value, Date: d.Format(dateLayout)}, nil
}

// pickerRow is how one bill reads in the picker, and in the pay form's select.
func pickerRow(b Bill) core.Choice {
	return core.Choice{
		Label:  b.ClosesOn + "  " + b.Fmt(b.Owed()) + " left",
		Desc:   "due " + b.DueOn + "  " + b.Status,
		Filter: strings.Join([]string{b.Card.Code, b.ClosesOn, b.DueOn, b.Status}, " "),
	}
}

// Pick shows the list used when a command needs a bill and was given no date.
func Pick(bs []Bill, title string) (Bill, error) { return core.Pick(bs, title, pickerRow) }
