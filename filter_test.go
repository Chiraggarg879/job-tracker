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

func TestFilterGenericTitles(t *testing.T) {
	f := NewFilter(FilterConfig{
		TitleInclude:        []string{"software engineer", "software development engineer", "sde", "forward deployed engineer"},
		TitleExclude:        []string{"senior"},
		TitleIncludeGeneric: []string{"developer", "engineer ii", `re:^\W*engineer\b`},
		TitleExcludeGeneric: []string{"data", "sap", "mechanical", "verification"},
	})
	tests := []struct {
		title string
		match bool
	}{
		{"Java Kafka Developer", true},
		{"Engineer II - Software", true},
		{"Engineer", true},
		{"Forward Deployed Engineer", true},
		{"SAP Developer", false},    // unwanted domain
		{"Data Engineer II", false}, // unwanted domain
		{"Engineer - Mechanical Component", false},
		{"Senior Developer", false},       // seniority still excluded
		{"Platform Engineer Lead", false}, // not a core or generic title
		{"Design Verification Engineer", false},
		// core titles ignore the generic domain list
		{"Software Development Engineer - II, Verification Services", true},
		{"SDE II - Data Platform", true},
	}
	for _, tt := range tests {
		if got := f.Match(Job{Title: tt.title, Location: "India"}); got != tt.match {
			t.Errorf("%q: got %v, want %v", tt.title, got, tt.match)
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
