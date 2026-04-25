package packid

import (
	"regexp"
	"strings"
)

var slugRegexp = regexp.MustCompile(`[^a-z0-9]+`)

func Slug(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = slugRegexp.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "pack"
	}
	return s
}
