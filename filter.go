package main

import (
	"regexp"
	"strings"
)

type Filter struct {
	include        []*regexp.Regexp
	exclude        []*regexp.Regexp
	keep           []*regexp.Regexp
	includeGeneric []*regexp.Regexp
	excludeGeneric []*regexp.Regexp
	locations      []*regexp.Regexp
}

// wordRegexps turns keywords into case-insensitive whole-word matchers, so
// "sde" matches "SDE II" but not "Insider", and "sr" doesn't match "Src".
// An entry starting with "re:" is used as a case-insensitive regular
// expression instead, e.g. "re:^\W*engineer\b" for titles that start with
// "Engineer".
func wordRegexps(words []string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, 0, len(words))
	for _, w := range words {
		w = strings.TrimSpace(w)
		if w == "" {
			continue
		}
		if pat, ok := strings.CutPrefix(w, "re:"); ok {
			out = append(out, regexp.MustCompile(`(?i)`+pat))
			continue
		}
		out = append(out, regexp.MustCompile(`(?i)(^|[^a-z0-9])`+regexp.QuoteMeta(w)+`($|[^a-z0-9])`))
	}
	return out
}

func NewFilter(fc FilterConfig) *Filter {
	return &Filter{
		include:        wordRegexps(fc.TitleInclude),
		exclude:        wordRegexps(fc.TitleExclude),
		keep:           wordRegexps(fc.TitleExcludeExceptions),
		includeGeneric: wordRegexps(fc.TitleIncludeGeneric),
		excludeGeneric: wordRegexps(fc.TitleExcludeGeneric),
		locations:      wordRegexps(fc.Locations),
	}
}

func anyMatch(res []*regexp.Regexp, s string) bool {
	for _, re := range res {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

// Match reports whether a job fits the title and location filters.
// An empty list for a filter means "don't filter on this". Jobs with no
// location at all are kept (e.g. Atlassian's APAC postings sometimes leave
// it blank), so a missing field never hides a job.
func (f *Filter) Match(j Job) bool {
	if len(f.include) > 0 && !anyMatch(f.include, j.Title) {
		// Not a core title: a broad one counts only outside unwanted domains.
		if !anyMatch(f.includeGeneric, j.Title) || anyMatch(f.excludeGeneric, j.Title) {
			return false
		}
	}
	// Exceptions are blanked out before the exclude check, so "Member of
	// Technical Staff" isn't dropped for containing "staff".
	title := j.Title
	for _, re := range f.keep {
		title = re.ReplaceAllString(title, " ")
	}
	if anyMatch(f.exclude, title) {
		return false
	}
	if len(f.locations) > 0 && strings.TrimSpace(j.Location) != "" && !anyMatch(f.locations, j.Location) {
		return false
	}
	return true
}
