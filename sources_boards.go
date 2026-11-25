package main

// Multi-company job boards: Getro (VC portfolio boards) and the remote
// boards Himalayas, Jobicy and Working Nomads. As with the aggregators in
// sources_remote.go, each job names the hiring company plus the board, and
// links to the board's own listing, as Jobicy and Himalayas ask.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func init() {
	fetchers["getro"] = fetchGetro
	fetchers["himalayas"] = fetchHimalayas
	fetchers["jobicy"] = fetchJobicy
	fetchers["workingnomads"] = fetchWorkingNomads
}

// ---------- Getro ----------

// Getro powers many VC portfolio job boards. Slug is the board's
// collection id; Query and Location narrow the search server-side.
func fetchGetro(ctx context.Context, c Company) ([]Job, error) {
	const pageSize = 100
	var jobs []Job
	for page := range pagesOr(c, 10) {
		body := map[string]any{
			"hitsPerPage": pageSize,
			"page":        page,
			"query":       c.Query,
			"filters":     map[string]any{},
		}
		if c.Location != "" {
			body["filters"] = map[string]any{"searchable_locations": []string{c.Location}}
		}
		data, err := doRequest(ctx, http.MethodPost, "https://api.getro.com/api/v2/collections/"+url.PathEscape(c.Slug)+"/search/jobs", body)
		if err != nil {
			return nil, err
		}
		var resp struct {
			Results struct {
				Count int `json:"count"`
				Jobs  []struct {
					ID           json.Number `json:"id"`
					Title        string      `json:"title"`
					URL          string      `json:"url"`
					CreatedAt    int64       `json:"created_at"`
					Locations    []string    `json:"searchable_locations"`
					Organization struct {
						Name string `json:"name"`
					} `json:"organization"`
				} `json:"jobs"`
			} `json:"results"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			return nil, err
		}
		for _, j := range resp.Results.Jobs {
			jobs = append(jobs, Job{
				ID:       jobID(c, j.ID.String()),
				Company:  j.Organization.Name + " (via " + c.Name + ")",
				Title:    j.Title,
				Location: strings.Join(j.Locations, "; "),
				URL:      j.URL,
				Posted:   unixDate(j.CreatedAt),
			})
		}
		if len(resp.Results.Jobs) < pageSize || (page+1)*pageSize >= resp.Results.Count {
			break
		}
	}
	return jobs, nil
}

// ---------- Himalayas ----------

// Himalayas' public feed, newest first, paged by cursor. An empty
// location restriction means the job is open worldwide.
func fetchHimalayas(ctx context.Context, c Company) ([]Job, error) {
	var jobs []Job
	cursor := ""
	for range pagesOr(c, 5) {
		u := "https://himalayas.app/jobs/api?limit=100"
		if cursor != "" {
			u += "&cursor=" + url.QueryEscape(cursor)
		}
		var resp struct {
			NextCursor string `json:"nextCursor"`
			Jobs       []struct {
				Title       string   `json:"title"`
				CompanyName string   `json:"companyName"`
				Locations   []string `json:"locationRestrictions"`
				GUID        string   `json:"guid"`
				PubDate     int64    `json:"pubDate"`
				Description string   `json:"description"`
			} `json:"jobs"`
		}
		if err := getJSON(ctx, u, &resp); err != nil {
			return nil, err
		}
		for _, j := range resp.Jobs {
			loc := strings.Join(j.Locations, "; ")
			if loc == "" {
				loc = "Worldwide"
			}
			jobs = append(jobs, Job{
				ID:          jobID(c, j.GUID),
				Company:     j.CompanyName + " (via Himalayas)",
				Title:       j.Title,
				Location:    loc,
				URL:         j.GUID,
				Posted:      unixDate(j.PubDate),
				Description: j.Description,
			})
		}
		if resp.NextCursor == "" || len(resp.Jobs) == 0 {
			break
		}
		cursor = resp.NextCursor
	}
	return jobs, nil
}

// ---------- Jobicy ----------

// Jobicy's public API. Query is the region ("apac"); results include
// roles open "Anywhere".
func fetchJobicy(ctx context.Context, c Company) ([]Job, error) {
	var resp struct {
		Jobs []struct {
			ID          json.Number `json:"id"`
			Title       string      `json:"jobTitle"`
			CompanyName string      `json:"companyName"`
			Geo         string      `json:"jobGeo"`
			URL         string      `json:"url"`
			PubDate     string      `json:"pubDate"`
			Description string      `json:"jobDescription"`
		} `json:"jobs"`
	}
	q := url.Values{}
	q.Set("count", "100")
	q.Set("geo", c.Query)
	q.Set("industry", "engineering")
	if err := getJSON(ctx, "https://jobicy.com/api/v2/remote-jobs?"+q.Encode(), &resp); err != nil {
		return nil, err
	}
	jobs := make([]Job, 0, len(resp.Jobs))
	for _, j := range resp.Jobs {
		jobs = append(jobs, Job{
			ID:          jobID(c, j.ID.String()),
			Company:     j.CompanyName + " (via Jobicy)",
			Title:       j.Title,
			Location:    j.Geo,
			URL:         j.URL,
			Posted:      shortDate(j.PubDate),
			Description: j.Description,
		})
	}
	return jobs, nil
}

// ---------- Working Nomads ----------

func fetchWorkingNomads(ctx context.Context, c Company) ([]Job, error) {
	var resp []struct {
		Title       string `json:"title"`
		CompanyName string `json:"company_name"`
		Location    string `json:"location"`
		URL         string `json:"url"`
		PubDate     string `json:"pub_date"`
		Description string `json:"description"`
	}
	if err := getJSON(ctx, "https://www.workingnomads.com/api/exposed_jobs/", &resp); err != nil {
		return nil, err
	}
	jobs := make([]Job, 0, len(resp))
	for i, j := range resp {
		id := j.URL
		if id == "" {
			id = strconv.Itoa(i)
		}
		jobs = append(jobs, Job{
			ID:          jobID(c, id),
			Company:     j.CompanyName + " (via Working Nomads)",
			Title:       j.Title,
			Location:    j.Location,
			URL:         j.URL,
			Posted:      shortDate(j.PubDate),
			Description: j.Description,
		})
	}
	return jobs, nil
}
