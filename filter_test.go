package main

import "testing"

func TestFilterMatch(t *testing.T) {
	f := NewFilter(FilterConfig{
		TitleInclude:           []string{"software engineer", "software dev engineer", "sde", "member of technical staff"},
		TitleExclude:           []string{"senior", "staff"},
		TitleExcludeExceptions: []string{"member of technical staff"},
		Locations:              []string{"India", "Bengaluru"},
	})
	tests := []struct {
		name  string
		job   Job
		match bool
	}{
		{"india", Job{Title: "Software Engineer", Location: "Bengaluru - India"}, true},
		{"empty location kept", Job{Title: "Software Engineer", Location: ""}, true},
		{"other country", Job{Title: "Software Engineer", Location: "Sydney - Australia"}, false},
		{"excluded", Job{Title: "Senior Software Engineer", Location: "India"}, false},
		{"amazon dev engineer", Job{Title: "Software Dev Engineer II, Amazon Leo", Location: "Bengaluru, Karnataka, IND"}, true},
		{"sde-1", Job{Title: "SDE-1", Location: "Hyderabad, India"}, true},
		{"mts kept despite staff", Job{Title: "Member of Technical Staff", Location: "India"}, true},
		{"staff engineer excluded", Job{Title: "Staff Software Engineer", Location: "India"}, false},
		{"senior mts excluded", Job{Title: "Senior Member of Technical Staff", Location: "India"}, false},
	}
	for _, tt := range tests {
		if got := f.Match(tt.job); got != tt.match {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.match)
		}
	}
}

func TestWorkdayReqID(t *testing.T) {
	for _, id := range []string{"JR2025205", "R-294544", "REF088312W", "1205422", "PT-JR039956", "R0109057"} {
		if !workdayReqIDRe.MatchString(id) {
			t.Errorf("%q should be a requisition ID", id)
		}
	}
	for _, s := range []string{"Bangalore, India", "Posted Today", "3 Locations", ""} {
		if workdayReqIDRe.MatchString(s) {
			t.Errorf("%q should not be a requisition ID", s)
		}
	}
}
