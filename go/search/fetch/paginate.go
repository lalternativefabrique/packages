package fetch

// Paginate splits Page.Text into pages of at most runesPerPage runes, so a
// caller can walk the rest of a long article instead of losing it to a hard
// truncation.
func (p *Page) Paginate(runesPerPage int) []string {
	if runesPerPage <= 0 {
		return []string{p.Text}
	}
	runes := []rune(p.Text)
	if len(runes) == 0 {
		return nil
	}
	pages := make([]string, 0, len(runes)/runesPerPage+1)
	for start := 0; start < len(runes); start += runesPerPage {
		end := start + runesPerPage
		if end > len(runes) {
			end = len(runes)
		}
		pages = append(pages, string(runes[start:end]))
	}
	return pages
}
