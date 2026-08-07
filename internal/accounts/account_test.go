package accounts

import "testing"

func TestAccountAccessors(t *testing.T) {
	a := Account{Color: "teal", Currency: "BTC", Balance: 150000000}
	if a.Cur().Code != "BTC" || a.Col().Name != "teal" || a.Amount() != "1.50000000" {
		t.Fatalf("accessors gave %s / %s / %s", a.Cur().Code, a.Col().Name, a.Amount())
	}
}
