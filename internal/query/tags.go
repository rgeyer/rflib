package query

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ryan/rflib/internal/domain"
)

// tagFields are the searches a tag can repeat: a tag that is a value of
// one of these, and that the channel matches, says nothing search can't
// already find.
var tagFields = []string{"service", "band", "mode", "is"}

// tagAliases are tags that name a field value without being one.
var tagAliases = map[string]string{"ham": "service:amateur"}

// TagRepeats returns a warning for each of the channel's own tags that
// repeats one of its first-class fields, such as cb on a CB channel or
// repeater on a repeater. Such tags are allowed on private channels; the
// shared list's curated tags are never field values (see IsFieldValue).
func TagRepeats(c domain.Channel) []string {
	var out []string
	for _, t := range domain.NormalizeTags(c.Tags) {
		for _, q := range searches(t) {
			if m, err := CompileText(q, Env{}); err == nil && m(c) {
				field, value, _ := strings.Cut(q, ":")
				out = append(out, fmt.Sprintf("tag %q repeats one of the channel's fields: %s:%s finds it without the tag", t, field, strings.Trim(value, `"`)))
				break
			}
		}
	}
	return out
}

// IsFieldValue reports whether a tag is a value of a first-class field for
// any channel, which a shared tag must never be.
func IsFieldValue(tag string) bool {
	for _, q := range searches(tag) {
		if _, err := CompileText(q, Env{}); err == nil {
			return true
		}
	}
	return false
}

// searches are the field searches a tag could stand for.
func searches(tag string) []string {
	var out []string
	if q, ok := tagAliases[tag]; ok {
		out = append(out, q)
	}
	for _, f := range tagFields {
		out = append(out, f+":"+strconv.Quote(tag))
	}
	return out
}
