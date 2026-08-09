package categories

import (
	"testing"

	"kakei/internal/core"
)

func TestCol(t *testing.T) {
	cases := []struct {
		name  string
		color string
		want  string
	}{
		{"a palette name resolves", "teal", "teal"},
		{"an unknown color falls back", "puce", core.Palette[0].Name},
		{"an empty color falls back", "", core.Palette[0].Name},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := (Category{Color: tc.color}).Col().Name; got != tc.want {
				t.Fatalf("Col() = %q; want %q", got, tc.want)
			}
		})
	}
}
