package main

// Fetchers for career sites beyond the big ATS platforms in sources.go:
// Eightfold, Jibe (iCIMS), Workable, Rippling and a few custom company sites.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func init() {
	for k, v := range map[string]fetchFunc{
		"eightfold": fetchEightfold,
		"jibe":      fetchJibe,
		"workable":  fetchWorkable,
		"rippling":  fetchRippling,
		"apple":     fetchApple,
		"goldman":   fetchGoldman,
		"bofa":      fetchBofA,
	} {
		fetchers[k] = v
	}
	describers["eightfold"] = describeEightfold
	describers["workable"] = describeWorkable
}

func unixDate(ts int64) string {
	if ts <= 0 {
		return ""
	}
	return time.Unix(ts, 0).Format("2006-01-02")
}

// ---------- Eightfold ----------

// Eightfold-hosted career sites (Qualcomm, Ericsson, NetApp...). Host is the
// career site ("careers.qualcomm.com"), Slug the company domain
// ("qualcomm.com"). Tenants serve one of two APIs: the newer PCSX search
// (Site "pcsx", the same API as Microsoft's) or the older apply/v2 ("v2").
func fetchEightfold(ctx context.Context, c Company) ([]Job, error) {
	const pageSize = 10
	var jobs []Job
	for page := range pagesOr(c, 5) {
		q := url.Values{}
		q.Set("domain", c.Slug)
		q.Set("query", c.Query)
		q.Set("location", c.Location)
		q.Set("start", strconv.Itoa(page*pageSize))
		q.Set("num", strconv.Itoa(pageSize))
		q.Set("sort_by", "timestamp")
		var n int
		if c.Site == "v2" {
			var resp struct {
				Positions []struct {
					ID          json.Number `json:"id"`
					Name        string      `json:"name"`
					Location    string      `json:"location"`
					Locations   []string    `json:"locations"`
					TCreate     int64       `json:"t_create"`
					PositionURL string      `json:"canonicalPositionUrl"`
				} `json:"positions"`
			}
			if err := getJSON(ctx, "https://"+c.Host+"/api/apply/v2/jobs?"+q.Encode(), &resp); err != nil {
				return nil, err
			}
			n = len(resp.Positions)
			for _, j := range resp.Positions {
				jobs = append(jobs, Job{
					ID:       jobID(c, j.ID.String()),
					Company:  c.Name,
					Title:    j.Name,
					Location: strings.Join(append([]string{j.Location}, j.Locations...), "; "),
					URL:      j.PositionURL,
					Posted:   unixDate(j.TCreate),
					RawID:    j.ID.String(),
				})
			}
		} else {
			var resp struct {
				Data struct {
					Positions []struct {
						ID          json.Number `json:"id"`
						Name        string      `json:"name"`
						Locations   []string    `json:"locations"`
						PostedTs    int64       `json:"postedTs"`
						PositionURL string      `json:"positionUrl"`
					} `json:"positions"`
				} `json:"data"`
			}
			if err := getJSON(ctx, "https://"+c.Host+"/api/pcsx/search?"+q.Encode(), &resp); err != nil {
				return nil, err
			}
			n = len(resp.Data.Positions)
			for _, j := range resp.Data.Positions {
				jobs = append(jobs, Job{
					ID:       jobID(c, j.ID.String()),
					Company:  c.Name,
					Title:    j.Name,
					Location: strings.Join(j.Locations, "; "),
					URL:      "https://" + c.Host + j.PositionURL,
					Posted:   unixDate(j.PostedTs),
					RawID:    j.ID.String(),
				})
			}
		}
		if n < pageSize {
			break
		}
	}
	return jobs, nil
}

func describeEightfold(ctx context.Context, c Company, j Job) (string, error) {
	if c.Site == "v2" {
		var resp struct {
			JobDescription string `json:"job_description"`
		}
		err := getJSON(ctx, "https://"+c.Host+"/api/apply/v2/jobs/"+j.RawID+"?domain="+url.QueryEscape(c.Slug), &resp)
		return resp.JobDescription, err
	}
	var resp struct {
		Data struct {
			JobDescription string `json:"jobDescription"`
		} `json:"data"`
	}
	q := url.Values{}
	q.Set("position_id", j.RawID)
	q.Set("domain", c.Slug)
	q.Set("hl", "en")
	err := getJSON(ctx, "https://"+c.Host+"/api/pcsx/position_details?"+q.Encode(), &resp)
	return resp.Data.JobDescription, err
}

// ---------- Jibe (iCIMS career sites) ----------

// Google Jibe front ends over iCIMS (AMD, Booking.com, ZS, MSCI...). Host is
// the career site; results come newest first with the description included.
func fetchJibe(ctx context.Context, c Company) ([]Job, error) {
	const pageSize = 100
	var jobs []Job
	for page := 1; page <= pagesOr(c, 3); page++ {
		q := url.Values{}
		q.Set("keywords", c.Query)
		q.Set("location", c.Location)
		q.Set("page", strconv.Itoa(page))
		q.Set("limit", strconv.Itoa(pageSize))
		q.Set("sortBy", "posted_date")
		q.Set("descending", "true")
		var resp struct {
			TotalCount int `json:"totalCount"`
			Jobs       []struct {
				Data struct {
					ReqID          string `json:"req_id"`
					Title          string `json:"title"`
					City           string `json:"city"`
					State          string `json:"state"`
					Country        string `json:"country"`
					FullLocation   string `json:"full_location"`
					PostedDate     string `json:"posted_date"`
					ApplyURL       string `json:"apply_url"`
					Description    string `json:"description"`
					Qualifications string `json:"qualifications"`
				} `json:"data"`
			} `json:"jobs"`
		}
		if err := getJSON(ctx, "https://"+c.Host+"/api/jobs?"+q.Encode(), &resp); err != nil {
			return nil, err
		}
		for _, j := range resp.Jobs {
			d := j.Data
			jobs = append(jobs, Job{
				ID:          jobID(c, d.ReqID),
				Company:     c.Name,
				Title:       d.Title,
				Location:    strings.Join([]string{d.City, d.State, d.Country, d.FullLocation}, "; "),
				URL:         "https://" + c.Host + "/jobs/" + d.ReqID,
				Posted:      shortDate(d.PostedDate),
				Description: d.Description + "\n" + d.Qualifications,
			})
		}
		if len(resp.Jobs) < pageSize || page*pageSize >= resp.TotalCount {
			break
		}
	}
	return jobs, nil
}

// ---------- Workable ----------

func fetchWorkable(ctx context.Context, c Company) ([]Job, error) {
	var resp struct {
		Jobs []struct {
			Title       string `json:"title"`
			Shortcode   string `json:"shortcode"`
			City        string `json:"city"`
			State       string `json:"state"`
			Country     string `json:"country"`
			URL         string `json:"url"`
			PublishedOn string `json:"published_on"`
			Locations   []struct {
				City    string `json:"city"`
				Country string `json:"country"`
			} `json:"locations"`
		} `json:"jobs"`
	}
	if err := getJSON(ctx, "https://apply.workable.com/api/v1/widget/accounts/"+url.PathEscape(c.Slug), &resp); err != nil {
		return nil, err
	}
	jobs := make([]Job, 0, len(resp.Jobs))
	for _, j := range resp.Jobs {
		locs := []string{j.City, j.State, j.Country}
		for _, l := range j.Locations {
			locs = append(locs, l.City+", "+l.Country)
		}
		jobs = append(jobs, Job{
			ID:       jobID(c, j.Shortcode),
			Company:  c.Name,
			Title:    j.Title,
			Location: strings.Join(locs, "; "),
			URL:      j.URL,
			Posted:   j.PublishedOn,
			RawID:    j.Shortcode,
		})
	}
	return jobs, nil
}

func describeWorkable(ctx context.Context, c Company, j Job) (string, error) {
	var resp struct {
		Description  string `json:"description"`
		Requirements string `json:"requirements"`
	}
	err := getJSON(ctx, "https://apply.workable.com/api/v2/accounts/"+url.PathEscape(c.Slug)+"/jobs/"+j.RawID, &resp)
	return resp.Description + "\n" + resp.Requirements, err
}

// ---------- Rippling ----------

func fetchRippling(ctx context.Context, c Company) ([]Job, error) {
	var resp []struct {
		UUID         string `json:"uuid"`
		Name         string `json:"name"`
		URL          string `json:"url"`
		WorkLocation struct {
			Label string `json:"label"`
		} `json:"workLocation"`
	}
	if err := getJSON(ctx, "https://api.rippling.com/platform/api/ats/v1/board/"+url.PathEscape(c.Slug)+"/jobs", &resp); err != nil {
		return nil, err
	}
	jobs := make([]Job, 0, len(resp))
	for _, j := range resp {
		jobs = append(jobs, Job{
			ID:       jobID(c, j.UUID),
			Company:  c.Name,
			Title:    j.Name,
			Location: j.WorkLocation.Label,
			URL:      j.URL,
		})
	}
	return jobs, nil
}

// ---------- Apple ----------

// jobs.apple.com needs a CSRF token plus the session cookie that comes with
// it. Location is Apple's location code ("postLocation-INDC" for India).
func fetchApple(ctx context.Context, c Company) ([]Job, error) {
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Timeout: httpClient.Timeout, Jar: jar}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://jobs.apple.com/api/v1/CSRFToken", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	resp.Body.Close()
	token := resp.Header.Get("X-Apple-CSRF-Token")
	if token == "" {
		return nil, fmt.Errorf("apple: no CSRF token (HTTP %d)", resp.StatusCode)
	}

	var jobs []Job
	for page := 1; page <= pagesOr(c, 10); page++ {
		body, _ := json.Marshal(map[string]any{
			"query":   c.Query,
			"filters": map[string]any{"locations": []string{c.Location}},
			"page":    page,
			"locale":  "en-us",
			"sort":    "newest",
			"format":  map[string]string{"longDate": "MMMM D, YYYY", "mediumDate": "MMM D, YYYY"},
		})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://jobs.apple.com/api/v1/search", strings.NewReader(string(body)))
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Apple-CSRF-Token", token)
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		var out struct {
			Res struct {
				TotalRecords  int `json:"totalRecords"`
				SearchResults []struct {
					PositionID string `json:"positionId"`
					Title      string `json:"postingTitle"`
					Slug       string `json:"transformedPostingTitle"`
					PostDate   string `json:"postDateInGMT"`
					JobSummary string `json:"jobSummary"`
					Locations  []struct {
						City    string `json:"city"`
						Country string `json:"countryName"`
					} `json:"locations"`
				} `json:"searchResults"`
			} `json:"res"`
		}
		err = json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("apple search: HTTP %d", resp.StatusCode)
		}
		if err != nil {
			return nil, err
		}
		for _, j := range out.Res.SearchResults {
			var locs []string
			for _, l := range j.Locations {
				locs = append(locs, strings.Trim(l.City+", "+l.Country, ", "))
			}
			jobs = append(jobs, Job{
				ID:          jobID(c, j.PositionID),
				Company:     c.Name,
				Title:       j.Title,
				Location:    strings.Join(locs, "; "),
				URL:         "https://jobs.apple.com/en-in/details/" + j.PositionID + "/" + j.Slug,
				Posted:      shortDate(j.PostDate),
				Description: j.JobSummary,
			})
		}
		if len(out.Res.SearchResults) == 0 || page*20 >= out.Res.TotalRecords {
			break
		}
	}
	return jobs, nil
}

// ---------- Goldman Sachs ----------

// higher.gs.com is backed by a public GraphQL API. The corporate title
// (Analyst / Associate / Vice President) is appended to the job title so
// the title filters can act on seniority.
func fetchGoldman(ctx context.Context, c Company) ([]Job, error) {
	const pageSize = 20
	const query = `query GetRoles($searchQueryInput: RoleSearchQueryInput!) { roleSearch(searchQueryInput: $searchQueryInput) { totalCount items { roleId jobTitle corporateTitle locations { city country } } } }`
	var jobs []Job
	for page := range pagesOr(c, 5) {
		body := map[string]any{
			"operationName": "GetRoles",
			"query":         query,
			"variables": map[string]any{"searchQueryInput": map[string]any{
				"page":        map[string]int{"pageSize": pageSize, "pageNumber": page},
				"sort":        map[string]string{"sortStrategy": "POSTED_DATE", "sortOrder": "DESC"},
				"filters":     []any{map[string]any{"filterCategoryType": "LOCATION", "filters": []any{map[string]any{"filter": c.Location, "subFilters": []any{}}}}},
				"experiences": []string{"EARLY_CAREER", "PROFESSIONAL"},
				"searchTerm":  c.Query,
			}},
		}
		data, err := doRequest(ctx, http.MethodPost, "https://api-higher.gs.com/gateway/api/v1/graphql", body)
		if err != nil {
			return nil, err
		}
		var resp struct {
			Data struct {
				RoleSearch struct {
					TotalCount int `json:"totalCount"`
					Items      []struct {
						RoleID         string `json:"roleId"`
						JobTitle       string `json:"jobTitle"`
						CorporateTitle string `json:"corporateTitle"`
						Locations      []struct {
							City    string `json:"city"`
							Country string `json:"country"`
						} `json:"locations"`
					} `json:"items"`
				} `json:"roleSearch"`
			} `json:"data"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			return nil, err
		}
		rs := resp.Data.RoleSearch
		for _, j := range rs.Items {
			var locs []string
			for _, l := range j.Locations {
				locs = append(locs, l.City+", "+l.Country)
			}
			jobs = append(jobs, Job{
				ID:       jobID(c, j.RoleID),
				Company:  c.Name,
				Title:    j.JobTitle + " (" + j.CorporateTitle + ")",
				Location: strings.Join(locs, "; "),
				URL:      "https://higher.gs.com/roles/" + j.RoleID,
			})
		}
		if len(rs.Items) < pageSize || (page+1)*pageSize >= rs.TotalCount {
			break
		}
	}
	return jobs, nil
}

// ---------- Bank of America ----------

// careers.bankofamerica.com's search servlet; keyword parameters are
// ignored, so it lists every job for the location and the title filters do
// the rest.
func fetchBofA(ctx context.Context, c Company) ([]Job, error) {
	const pageSize = 200
	var jobs []Job
	for page := range pagesOr(c, 3) {
		q := url.Values{}
		q.Set("search", "jobsByLocation")
		q.Set("searchstring", c.Location)
		q.Set("rows", strconv.Itoa(pageSize))
		q.Set("start", strconv.Itoa(page*pageSize))
		var resp struct {
			TotalMatches int `json:"totalMatches"`
			JobsList     []struct {
				ID         string `json:"jobRequisitionId"`
				Title      string `json:"postingTitle"`
				City       string `json:"city"`
				Country    string `json:"country"`
				PostedDate string `json:"postedDate"`
				Path       string `json:"jcrURL"`
			} `json:"jobsList"`
		}
		if err := getJSON(ctx, "https://careers.bankofamerica.com/services/jobssearchservlet?"+q.Encode(), &resp); err != nil {
			return nil, err
		}
		for _, j := range resp.JobsList {
			jobs = append(jobs, Job{
				ID:       jobID(c, j.ID),
				Company:  c.Name,
				Title:    j.Title,
				Location: j.City + ", " + j.Country,
				URL:      "https://careers.bankofamerica.com" + j.Path,
				Posted:   j.PostedDate,
			})
		}
		if len(resp.JobsList) < pageSize || (page+1)*pageSize >= resp.TotalMatches {
			break
		}
	}
	return jobs, nil
}
