package main

import (
	"strconv"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// Parser-based HTML assertions. The suites' page bodies are full documents,
// so checking them with strings.Contains is brittle: a tag split across a
// searched boundary or an entity inside a matched string can fake a match,
// and stray injected tags (the template-edit mangling class) stay invisible.
// These helpers parse the body with a real HTML parser and assert on the
// resulting tree instead.

// parsePage parses a rendered page body into a node tree.
func parsePage(t *testing.T, body string) *html.Node {
	t.Helper()
	root, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parsing rendered HTML: %v", err)
	}
	return root
}

// hasClass reports whether the element's class attribute carries the token.
func hasClass(n *html.Node, c string) bool {
	for _, a := range n.Attr {
		if a.Key == "class" {
			for _, tok := range strings.Fields(a.Val) {
				if tok == c {
					return true
				}
			}
		}
	}
	return false
}

// elemsByClass returns every element carrying the class token, in document
// order.
func elemsByClass(root *html.Node, c string) []*html.Node {
	var out []*html.Node
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && hasClass(n, c) {
			out = append(out, n)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return out
}

// answersTable returns the page's answers table (class answers-table),
// failing if the page carries a different number than one of them.
func answersTable(t *testing.T, root *html.Node) *html.Node {
	t.Helper()
	tbls := elemsByClass(root, "answers-table")
	if len(tbls) != 1 {
		t.Fatalf("page: %d answers tables, want exactly one", len(tbls))
	}
	return tbls[0]
}

// tbodyRows returns the table's body rows in document order.
func tbodyRows(table *html.Node) []*html.Node {
	var out []*html.Node
	for child := table.FirstChild; child != nil; child = child.NextSibling {
		if child.Type != html.ElementNode || child.Data != "tbody" {
			continue
		}
		for row := child.FirstChild; row != nil; row = row.NextSibling {
			if row.Type == html.ElementNode && row.Data == "tr" {
				out = append(out, row)
			}
		}
	}
	return out
}

// colCount is a row's effective column span: the sum of its cells'
// colspan attributes (1 each when unset).
func colCount(row *html.Node) int {
	n := 0
	for cell := row.FirstChild; cell != nil; cell = cell.NextSibling {
		if cell.Type != html.ElementNode || (cell.Data != "td" && cell.Data != "th") {
			continue
		}
		c := 1
		for _, a := range cell.Attr {
			if a.Key == "colspan" {
				if v, err := strconv.Atoi(a.Val); err == nil && v > 0 {
					c = v
				}
			}
		}
		n += c
	}
	return n
}

// tableShape checks that the page's answers table is rectangular: every
// body row spans the same number of columns. A stray closing tag injected
// by a template edit (the known write-file hazard) would drop a cell from
// one row and break this.
func tableShape(t *testing.T, root *html.Node) {
	t.Helper()
	rows := tbodyRows(answersTable(t, root))
	if len(rows) == 0 {
		t.Fatalf("answers table has no body rows")
	}
	want := colCount(rows[0])
	if want < 2 {
		t.Fatalf("answers table's first row spans %d columns, want at least 2", want)
	}
	for i, r := range rows {
		if got := colCount(r); got != want {
			t.Fatalf("answers table: body row %d spans %d columns, want %d — every row must be rectangular", i, got, want)
		}
	}
}

// totalsRows returns the answers table's totals rows, in document order.
func totalsRows(root *html.Node) []*html.Node {
	var out []*html.Node
	for _, tbl := range elemsByClass(root, "answers-table") {
		for _, row := range tbodyRows(tbl) {
			if hasClass(row, "totals-row") {
				out = append(out, row)
			}
		}
	}
	return out
}

// rowTds returns a row's direct <td> cells in document order.
func rowTds(tr *html.Node) []*html.Node {
	var out []*html.Node
	for cell := tr.FirstChild; cell != nil; cell = cell.NextSibling {
		if cell.Type == html.ElementNode && cell.Data == "td" {
			out = append(out, cell)
		}
	}
	return out
}

// nodeText returns an element's text content with collapsed whitespace —
// a stable form to compare against the visible label.
func nodeText(n *html.Node) string {
	var sb strings.Builder
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			sb.WriteString(n.Data)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return strings.Join(strings.Fields(sb.String()), " ")
}

// hotCells counts the row's highlighted (totals-hot) cells.
func hotCells(tr *html.Node) int {
	n := 0
	for _, td := range rowTds(tr) {
		if hasClass(td, "totals-hot") {
			n++
		}
	}
	return n
}
