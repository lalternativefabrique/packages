package fetch

import "testing"

func TestPaginateSplitsIntoEvenPages(t *testing.T) {
	p := &Page{Text: "0123456789"}
	pages := p.Paginate(4)
	want := []string{"0123", "4567", "89"}
	if len(pages) != len(want) {
		t.Fatalf("got %d pages, want %d: %v", len(pages), len(want), pages)
	}
	for i, w := range want {
		if pages[i] != w {
			t.Errorf("page %d = %q, want %q", i, pages[i], w)
		}
	}
}

func TestPaginateEmptyTextYieldsNoPages(t *testing.T) {
	p := &Page{Text: ""}
	if pages := p.Paginate(10); len(pages) != 0 {
		t.Errorf("got %d pages, want 0", len(pages))
	}
}

func TestPaginateZeroSizeReturnsWholeText(t *testing.T) {
	p := &Page{Text: "hello"}
	pages := p.Paginate(0)
	if len(pages) != 1 || pages[0] != "hello" {
		t.Errorf("got %v, want a single page with the whole text", pages)
	}
}
