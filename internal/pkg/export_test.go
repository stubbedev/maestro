package pkg

import "strconv"

// Test hooks for the external tests.

// LinkList returns the PHP list [$l0, $l1, ...] (int keys 0, 1, ...).
func LinkList(links ...*Link) Links {
	if len(links) == 0 {
		return Links{}
	}

	keys := make([]string, len(links))
	for i := range keys {
		keys[i] = strconv.Itoa(i)
	}

	return Links{&linksData{links: append([]*Link(nil), links...), keys: keys}}
}
