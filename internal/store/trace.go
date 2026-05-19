package store

import (
	"context"
	"regexp"
	"strings"
)

// FindCallers returns chunks whose Text contains the call-site pattern
// `symbol(`, excluding the definition chunk itself.
//
// Note: chunks where r.Name == symbol are excluded regardless of file, so
// same-named symbols across files are also skipped. Acceptable for v1.
func FindCallers(ctx context.Context, st Store, symbol string, limit int) ([]SearchResult, error) {
	// ListByPath with empty prefix scans every chunk in the index.
	// Pull more than `limit` so we have room to filter.
	raw, err := st.ListByPath(ctx, "", limit*10+100)
	if err != nil {
		return nil, err
	}
	needle := symbol + "("
	var out []SearchResult
	for _, r := range raw {
		if r.Name == symbol {
			continue
		}
		if strings.Contains(r.Text, needle) {
			out = append(out, r)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

// goKeywords is the set of Go built-ins and keywords that look like function
// calls in source text but are not user-defined symbols.
var goKeywords = map[string]bool{
	"if": true, "for": true, "switch": true, "func": true, "return": true,
	"defer": true, "go": true, "make": true, "len": true, "cap": true,
	"append": true, "copy": true, "new": true, "panic": true, "recover": true,
	"print": true, "println": true, "range": true, "select": true,
	"chan": true, "map": true, "struct": true, "interface": true, "type": true,
	"var": true, "const": true, "package": true, "import": true,
	"break": true, "continue": true, "else": true, "fallthrough": true, "goto": true,
}

// identCallRe matches an identifier immediately followed by '('.
// Note: for qualified calls like pkg.Foo(, only "Foo" is captured (the part
// after the dot). This is a known v1 limitation.
var identCallRe = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)\(`)

// FindCallees returns chunks corresponding to symbols referenced as function
// calls in symbol's body. Resolution is best-effort — unresolvable names are
// silently dropped.
func FindCallees(ctx context.Context, st Store, symbol string, limit int) ([]SearchResult, error) {
	// Find the definition chunk for this symbol.
	defs, err := st.SearchStructural(ctx, symbol, "", "", 1)
	if err != nil {
		return nil, err
	}
	if len(defs) == 0 {
		return nil, nil
	}
	body := defs[0].Text
	seen := map[string]bool{}
	var out []SearchResult
	for _, m := range identCallRe.FindAllStringSubmatch(body, -1) {
		name := m[1]
		if goKeywords[name] || seen[name] || name == symbol {
			continue
		}
		seen[name] = true
		hits, err := st.SearchStructural(ctx, name, "", "", 1)
		if err != nil {
			// Best-effort: skip unresolvable names.
			continue
		}
		if len(hits) > 0 {
			out = append(out, hits[0])
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}
