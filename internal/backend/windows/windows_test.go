//go:build windows

package windows

import (
	"testing"

	"github.com/kang-sw/gotto-hando/internal/backend"
	"github.com/kang-sw/gotto-hando/internal/ir"
)

// sampleWindows is a small z-ordered fixture spanning the four selector
// kinds (id/pid/app/title) with mixed case, mirroring darwin's
// windows_test.go fixture (App carries an image basename here, e.g.
// "notepad.exe", not a bundle name).
func sampleWindows() []backend.Window {
	return []backend.Window{
		{ID: 10, PID: 501, App: "notepad.exe", Title: "Untitled - Notepad"},
		{ID: 11, PID: 502, App: "explorer.exe", Title: "This PC"},
		{ID: 12, PID: 501, App: "notepad.exe", Title: "notes.txt - Notepad"},
	}
}

// TestSelectorMatching checks filterWindows across every WINDOW SELECTOR
// kind (help.txt WINDOW SELECTORS :286-297): id/pid exact, app/title
// case-insensitive substring, title RE2 regex, and the no-match case. Pure
// (no FFI), so it runs off the synthetic list.
func TestSelectorMatching(t *testing.T) {
	wins := sampleWindows()
	cases := []struct {
		name    string
		sel     ir.Selector
		wantIDs []int
	}{
		{"id exact", ir.Selector{Kind: "id", Value: "11"}, []int{11}},
		{"id no match", ir.Selector{Kind: "id", Value: "99"}, nil},
		{"pid exact matches all owned", ir.Selector{Kind: "pid", Value: "501"}, []int{10, 12}},
		{"app substring case-insensitive", ir.Selector{Kind: "app", Value: "NOTEPAD"}, []int{10, 12}},
		{"title substring case-insensitive", ir.Selector{Kind: "title", Value: "notes"}, []int{12}},
		{"title regex anchored", ir.Selector{Kind: "title", Value: "^This", Regex: true}, []int{11}},
		{"title regex alternation", ir.Selector{Kind: "title", Value: "Untitled|notes", Regex: true}, []int{10, 12}},
		{"empty title matches every window", ir.Selector{Kind: "title", Value: ""}, []int{10, 11, 12}},
		{"bad regex matches nothing", ir.Selector{Kind: "title", Value: "(", Regex: true}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := filterWindows(wins, c.sel)
			var ids []int
			for _, w := range got {
				ids = append(ids, w.ID)
			}
			if !equalInts(ids, c.wantIDs) {
				t.Fatalf("ids = %v, want %v", ids, c.wantIDs)
			}
		})
	}
}

// TestFilterPreservesZOrder confirms filterWindows keeps the input z-order
// (front-to-back, the order EnumWindows already visits them in), so the
// engine's frontmost-wins rule (help.txt :294) reads element 0.
func TestFilterPreservesZOrder(t *testing.T) {
	got := filterWindows(sampleWindows(), ir.Selector{Kind: "app", Value: "notepad"})
	if len(got) != 2 || got[0].ID != 10 || got[1].ID != 12 {
		t.Fatalf("got ids in order %v, want [10 12]", ids(got))
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func ids(wins []backend.Window) []int {
	var out []int
	for _, w := range wins {
		out = append(out, w.ID)
	}
	return out
}
