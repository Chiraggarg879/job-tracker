package main

// Job aggregators: YC startups, Hacker News "Who is hiring?", and the remote
// boards We Work Remotely, Remotive and Remote OK. Each job carries the real
// hiring company plus the board it came from; Remotive and Remote OK ask
// that their listings be credited and linked back, which the URL and the
// "(via ...)" company suffix do.

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strings"
)

func init() {
	fetchers["yc"] = fetchYC
	fetchers["hnhiring"] = fetchHNHiring
	fetchers["wwr"] = fetchWWR
	fetchers["remotive"] = fetchRemotive
	fetchers["remoteok"] = fetchRemoteOK
}

// ---------- Y Combinator jobs ----------

var dataPageRe = regexp.MustCompile(`data-page="([^"]+)"`)

// YC writes India as the bare country code ("IN", "Remote (IN)").
var ycIndiaRe = regexp.MustCompile(`\bIN\b`)

// ycombinator.com/jobs pages embed their listings as JSON. Query is the
// page path ("/jobs/role/software-engineer/india"); use Queries for several.
// The visa field is not used: YC shows "US citizen/visa only" even on jobs
// based in India, and the location filter already drops US-only roles.
func fetchYC(ctx context.Context, c Company) ([]Job, error) {
	page, err := getPage(ctx, "https://www.ycombinator.com"+c.Query)
	if err != nil {
		return nil, err
	}
	m := dataPageRe.FindStringSubmatch(page)
	if m == nil {
		return nil, fmt.Errorf("yc: no job data on %s", c.Query)
	}
	var d struct {
		Props struct {
			JobPostings []struct {
				ID            json.Number `json:"id"`
				Title         string      `json:"title"`
				URL           string      `json:"url"`
				Location      string      `json:"location"`
				MinExperience string      `json:"minExperience"`
				CompanyName   string      `json:"companyName"`
				SalaryRange   string      `json:"salaryRange"`
			} `json:"jobPostings"`
		} `json:"props"`
	}
	if err := json.Unmarshal([]byte(html.UnescapeString(m[1])), &d); err != nil {
		return nil, err
	}
	var jobs []Job
	for _, j := range d.Props.JobPostings {
		loc := j.Location
		if ycIndiaRe.MatchString(loc) {
			loc += "; India"
		}
		desc := ""
		if j.MinExperience != "" {
			desc = j.MinExperience + " of experience."
		}
		title := j.Title
		if j.SalaryRange != "" {
			title += " (" + j.SalaryRange + ")"
		}
		jobs = append(jobs, Job{
			ID:          jobID(c, j.ID.String()),
			Company:     j.CompanyName + " (YC)",
			Title:       title,
			Location:    loc,
			URL:         "https://www.ycombinator.com" + j.URL,
			Description: desc,
		})
	}
	return jobs, nil
}

// ---------- Hacker News "Who is hiring?" ----------

// Each month's thread is a list of comments whose first line reads
// "Company | Role | Location | ...". The whole first line serves as both
// title and location, so the include and location filters match against it.
func fetchHNHiring(ctx context.Context, c Company) ([]Job, error) {
	var search struct {
		Hits []struct {
			ID    string `json:"objectID"`
			Title string `json:"title"`
		} `json:"hits"`
	}
	if err := getJSON(ctx, "https://hn.algolia.com/api/v1/search_by_date?tags=story,author_whoishiring&hitsPerPage=5", &search); err != nil {
		return nil, err
	}
	threadID := ""
	for _, h := range search.Hits {
		if strings.Contains(strings.ToLower(h.Title), "who is hiring") {
			threadID = h.ID
			break
		}
	}
	if threadID == "" {
		return nil, fmt.Errorf("hn: no current Who is hiring thread")
	}
	var thread struct {
		Children []struct {
			ID        json.Number `json:"id"`
			Text      string      `json:"text"`
			CreatedAt string      `json:"created_at"`
		} `json:"children"`
	}
	if err := getJSON(ctx, "https://hn.algolia.com/api/v1/items/"+threadID, &thread); err != nil {
		return nil, err
	}
	var jobs []Job
	for _, p := range thread.Children {
		if p.Text == "" {
			continue // deleted comment
		}
		header := p.Text
		if i := strings.Index(header, "<p>"); i >= 0 {
			header = header[:i]
		}
		header = cleanText(header)
		if !strings.Contains(header, "|") {
			continue // not a job post
		}
		company := strings.TrimSpace(strings.SplitN(header, "|", 2)[0])
		jobs = append(jobs, Job{
			ID:          jobID(c, p.ID.String()),
			Company:     truncate(company, 40) + " (HN)",
			Title:       truncate(header, 200),
			Location:    header,
			URL:         "https://news.ycombinator.com/item?id=" + p.ID.String(),
			Posted:      shortDate(p.CreatedAt),
			Description: cleanText(p.Text),
		})
	}
	return jobs, nil
}

// ---------- We Work Remotely ----------

var (
	wwrRegionRe  = rssFieldRe("region")
	wwrCountryRe = rssFieldRe("country")
	wwrGUIDRe    = rssFieldRe("guid")
)

// We Work Remotely category RSS feeds. Query is the category slug
// ("remote-back-end-programming-jobs"); use Queries for several. Titles
// read "Company: Role". The location is the list of allowed countries when
// there is one (a job can say "Anywhere in the World" yet allow only the US
// and Canada), else the region.
func fetchWWR(ctx context.Context, c Company) ([]Job, error) {
	data, err := getPage(ctx, "https://weworkremotely.com/categories/"+c.Query+".rss")
	if err != nil {
		return nil, err
	}
	var jobs []Job
	for _, item := range rssItemRe.FindAllStringSubmatch(data, -1) {
		field := func(re *regexp.Regexp) string {
			if m := re.FindStringSubmatch(item[1]); m != nil {
				return strings.TrimSpace(html.UnescapeString(m[1]))
			}
			return ""
		}
		company, title, ok := strings.Cut(field(rssTitleRe), ": ")
		if !ok {
			company, title = "", company
		}
		link := field(rssLinkRe)
		id := field(wwrGUIDRe)
		if id == "" {
			id = link
		}
		loc := field(wwrCountryRe)
		if loc == "" {
			loc = field(wwrRegionRe)
		}
		jobs = append(jobs, Job{
			ID:          jobID(c, id),
			Company:     company + " (WWR)",
			Title:       title,
			Location:    loc,
			URL:         link,
			Description: field(rssDescRe),
		})
	}
	return jobs, nil
}

// ---------- Remotive ----------

// Remotive's public API, which asks for at most four calls a day; the
// twice-daily schedule stays under that. Query is the category
// ("software-dev").
func fetchRemotive(ctx context.Context, c Company) ([]Job, error) {
	var resp struct {
		Jobs []struct {
			ID          json.Number `json:"id"`
			Title       string      `json:"title"`
			Company     string      `json:"company_name"`
			Location    string      `json:"candidate_required_location"`
			URL         string      `json:"url"`
			Published   string      `json:"publication_date"`
			Description string      `json:"description"`
		} `json:"jobs"`
	}
	if err := getJSON(ctx, "https://remotive.com/api/remote-jobs?category="+c.Query, &resp); err != nil {
		return nil, err
	}
	jobs := make([]Job, 0, len(resp.Jobs))
	for _, j := range resp.Jobs {
		jobs = append(jobs, Job{
			ID:          jobID(c, j.ID.String()),
			Company:     j.Company + " (via Remotive)",
			Title:       j.Title,
			Location:    j.Location,
			URL:         j.URL,
			Posted:      shortDate(j.Published),
			Description: j.Description,
		})
	}
	return jobs, nil
}

// ---------- Remote OK ----------

// Remote OK's public API. The first array element is a legal notice, not
// a job, and has no id.
func fetchRemoteOK(ctx context.Context, c Company) ([]Job, error) {
	var resp []struct {
		ID          json.Number `json:"id"`
		Position    string      `json:"position"`
		Company     string      `json:"company"`
		Location    string      `json:"location"`
		URL         string      `json:"url"`
		Date        string      `json:"date"`
		Description string      `json:"description"`
	}
	if err := getJSON(ctx, "https://remoteok.com/api", &resp); err != nil {
		return nil, err
	}
	var jobs []Job
	for _, j := range resp {
		if j.ID == "" || j.Position == "" {
			continue
		}
		// A blank location here means "not stated", not worldwide, so it
		// must not pass the filter's keep-unknown-location rule.
		loc := j.Location
		if strings.TrimSpace(loc) == "" {
			loc = "Remote (location not stated)"
		}
		jobs = append(jobs, Job{
			ID:          jobID(c, j.ID.String()),
			Company:     j.Company + " (via Remote OK)",
			Title:       j.Position,
			Location:    loc,
			URL:         j.URL,
			Posted:      shortDate(j.Date),
			Description: j.Description,
		})
	}
	return jobs, nil
}
