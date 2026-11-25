package main

import "testing"

func TestRequiredYears(t *testing.T) {
	tests := []struct {
		name, title, desc string
		want              int
		wantFound         bool
	}{
		{"plus", "SDE", "3+ years of non-internship professional software development experience", 3, true},
		{"range takes minimum", "SWE", "<li>2-4 years of experience in backend</li>", 2, true},
		{"to range", "SWE", "Looking for 1 to 3 yrs of relevant experience", 1, true},
		{"words", "SWE", "Three years of experience with Go", 3, true},
		{"largest of several", "SWE", "5+ years Java experience. 2+ years of cloud experience.", 5, true},
		{"experience before number", "SWE", "Experience: 4 years minimum", 4, true},
		{"years in title", "Software Engineer | C/C++ | 4-8 years", "", 4, true},
		{"escaped html", "SWE", "&lt;li&gt;&lt;strong&gt;7+ years of experience&lt;/strong&gt;&lt;/li&gt;", 7, true},
		{"unrelated years ignored", "SWE", "We have been building payments for 20 years. Join us!", 0, false},
		{"none stated", "SWE", "Strong DSA skills and Go knowledge.", 0, false},
		{
			"preferred section ignored", "SWE",
			"Minimum qualifications: Bachelor's degree. 1 year of experience with software development in Python. " +
				"Some more filler text describing the team and the role so the marker is far enough in. " +
				"Preferred qualifications: 5 years of experience with distributed systems.",
			1, true,
		},
		{
			"preferred intro before real requirements", "SWE",
			"The preferred qualifications are a bonus, not a requirement. Minimum requirements: 8+ years of experience designing APIs. " +
				"Preferred qualifications: experience with financial systems.",
			8, true,
		},
		{
			"required section after preferred", "SWE",
			"Preferred: 6 years of experience in Rust. Requirements: 2+ years of experience in Go.",
			2, true,
		},
		{"months", "SWE", "0 to 12 months of experience in the software engineering field", 0, true},
		{"18 months", "SWE", "18 months of professional experience", 1, true},
		{"company history in next sentence", "Software Engineer",
			"PayPal has been revolutionizing commerce globally for more than 25 years. Creating innovative experiences that make moving money simple. Minimum Qualifications: 1+ years relevant experience and a Bachelor's degree",
			1, true},
		{"abbreviation dot", "SWE", "Need 2 yrs. experience with Go", 2, true},
		{"degree length", "SWE", "Bachelor's degree (4 years) or equivalent experience. 1+ year of experience with Go.", 1, true},
		{"four year degree", "SWE", "A 4 year degree in CS or equivalent practical experience", 0, false},
		{"plural experiences", "SWE", "For 30 years we have built delightful customer experiences.", 0, false},
		{"if you have in preferred", "SWE", "Requirements: 1+ years of experience in Go. Preferred qualifications: if you have 5+ years of experience with Kafka", 1, true},
		{"desired section", "SWE", "Minimum: 2 years of experience. Desired qualifications: 6 years of experience leading teams", 2, true},
		{"zero to n", "SWE", "0-5 years of experience in Java", 0, true},
		{"y abbreviation in title", "Full Stack Dev (Angular + C#) 3-6 Y", "", 3, true},
		{"zero to n spaced", "SWE", "Experience: 0 – 3 yrs", 0, true},
		{"zero to n in title", "Software Engineer (0-2 Years)", "", 0, true},
	}
	for _, tt := range tests {
		got, found := RequiredYears(tt.title, tt.desc)
		if got != tt.want || found != tt.wantFound {
			t.Errorf("%s: got (%d, %v), want (%d, %v)", tt.name, got, found, tt.want, tt.wantFound)
		}
	}
}
