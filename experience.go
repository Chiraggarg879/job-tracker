package main

import (
	"html"
	"regexp"
	"strconv"
	"strings"
)

var (
	tagRe   = regexp.MustCompile(`<[^>]*>`)
	spaceRe = regexp.MustCompile(`\s+`)

	// Matches "3 years", "3+ years", "3 plus yrs", "2-4 years", "2 to 4 years",
	// "three years", "0 to 12 months", "3-6 Y". Group 1 is the minimum, group 2 the unit.
	yearsRe = regexp.MustCompile(`\b(\d{1,2}|one|two|three|four|five|six|seven|eight|nine|ten)\s*(?:\+|plus)?\s*(?:(?:-|–|—|to)\s*\d{1,2}\s*)?\+?\s*(years?|yrs?|y|months?)\b`)

	numberWords = map[string]int{
		"one": 1, "two": 2, "three": 3, "four": 4, "five": 5,
		"six": 6, "seven": 7, "eight": 8, "nine": 9, "ten": 10,
	}

	// Headings that start a nice-to-have section...
	preferredMarkers = []string{
		"preferred qualifications", "preferred skills", "preferred experience", "preferred:",
		"preferred requirements", "desired qualifications", "desired skills", "desired experience",
		"desirable", "nice to have", "nice-to-have", "good to have", "good-to-have",
		"bonus points", "bonus if", "pluses", "would be a plus",
	}
	// ...and headings that start (or return to) the actual requirements.
	// Only heading-like phrases: a bare "you have" also appears in sentences
	// like "if you have 5+ years with Kafka" inside a preferred section.
	requiredMarkers = []string{
		"minimum", "basic qualifications", "requirement", "required",
		"must have", "must-have", "you'll need", "you’ll need", "you need",
		"looking for", "what you bring", "what you have", "you have:",
	}

	// "experience" as a word: not "experiences" (marketing copy such as
	// "creating innovative experiences") or "experienced".
	experienceRe = regexp.MustCompile(`\bexperience\b|\bexp\b`)
	// "4 year degree", "4 years of college": schooling, not work experience.
	schoolingRe = regexp.MustCompile(`^[\s)\]]*(?:of\s+)?(?:a\s+)?(?:\w+\s+)?(?:degree|bachelor|college|university|undergraduate|engineering degree|course|program)`)
	// "Bachelor's degree (4 years)": the schooling word comes right before.
	schoolingBeforeRe = regexp.MustCompile(`(?:degree|bachelor'?s?|college|university|program)[\s(\[,:–-]{0,3}$`)
)

// lastIndexAny returns the position of the last occurrence of any marker.
func lastIndexAny(s string, markers []string) int {
	last := -1
	for _, m := range markers {
		last = max(last, strings.LastIndex(s, m))
	}
	return last
}

// htmlToText turns (possibly double-escaped) HTML into plain lowercase text.
func htmlToText(s string) string {
	s = html.UnescapeString(s)
	s = tagRe.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	return strings.ToLower(spaceRe.ReplaceAllString(s, " "))
}

// RequiredYears extracts the minimum years of experience a posting asks for.
// It only counts "N years" mentions that have "experience" close by, so
// phrases like "20 years of history" are ignored. When several are found
// it returns the largest: "5+ years Java, 2+ years cloud" needs 5.
func RequiredYears(title, description string) (years int, found bool) {
	// In a title, "N years" can only mean experience, so it skips the
	// "experience nearby" check below.
	titleLen := len(htmlToText(title))
	text := htmlToText(title + " . " + description)

	for _, loc := range yearsRe.FindAllStringSubmatchIndex(text, -1) {
		before := text[max(0, loc[0]-50):loc[0]]
		// Only look for "experience" within the same sentence, so company
		// history like "for more than 25 years. Creating innovative
		// experiences..." isn't read as a requirement. After "yrs" a "." is
		// an abbreviation ("2 yrs. experience"), not a sentence end.
		after := text[loc[1]:min(len(text), loc[1]+100)]
		if strings.HasPrefix(text[loc[4]:loc[5]], "yr") {
			after = strings.TrimPrefix(after, ".")
		}
		if end := strings.Index(after, ". "); end >= 0 {
			after = after[:end]
		}
		if loc[0] >= titleLen && !experienceRe.MatchString(after) && !experienceRe.MatchString(before) {
			continue
		}
		if schoolingRe.MatchString(after) || schoolingBeforeRe.MatchString(before) {
			continue
		}
		// Skip mentions under a "preferred" heading, unless a "required"
		// heading came after it. Checking per mention (rather than cutting
		// the text at the first marker) handles intros like "the preferred
		// qualifications are a bonus" that precede the real requirements.
		head := text[:loc[0]]
		if lastIndexAny(head, preferredMarkers) > lastIndexAny(head, requiredMarkers) {
			continue
		}
		raw := text[loc[2]:loc[3]]
		n, ok := numberWords[raw]
		if !ok {
			n, _ = strconv.Atoi(raw)
		}
		if strings.HasPrefix(text[loc[4]:loc[5]], "month") {
			n /= 12
		}
		if n > 25 { // not a plausible requirement
			continue
		}
		if !found || n > years {
			years = n
		}
		found = true
	}
	return years, found
}
