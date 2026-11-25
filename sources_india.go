package main

// Fetchers for applicant-tracking systems popular with Indian startups:
// Keka, Kula, MyNextHire, Recruiterbox, PyjamaHR, aInterviews, plus the
// plain JSON job exports some companies publish (MakeMyTrip, Juspay...).

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func init() {
	for k, v := range map[string]fetchFunc{
		"keka":         fetchKeka,
		"kula":         fetchKula,
		"mynexthire":   fetchMyNextHire,
		"recruiterbox": fetchRecruiterbox,
		"pyjamahr":     fetchPyjamaHR,
		"ainterviews":  fetchAInterviews,
		"jsonfeed":     fetchJSONFeed,
	} {
		fetchers[k] = v
	}
}

// expText turns a structured minimum-experience field into a sentence the
// experience parser understands; empty when the field is missing.
func expText(min string) string {
	f, err := strconv.ParseFloat(strings.TrimSpace(min), 64)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("\nMinimum %d years of experience.", int(f))
}

// ---------- Keka ----------

// Slug is the Keka tenant ("gokwik"), Site the career-portal id from the
// careers page.
func fetchKeka(ctx context.Context, c Company) ([]Job, error) {
	var resp []struct {
		ID           json.Number `json:"id"`
		Title        string      `json:"title"`
		Description  string      `json:"description"`
		Experience   string      `json:"experience"`
		PublishedOn  string      `json:"publishedOn"`
		JobLocations []struct {
			Name        string `json:"name"`
			City        string `json:"city"`
			CountryName string `json:"countryName"`
		} `json:"jobLocations"`
	}
	api := fmt.Sprintf("https://%s.keka.com/careers/api/embedjobs/default/active/%s", c.Slug, c.Site)
	if err := getJSON(ctx, api, &resp); err != nil {
		return nil, err
	}
	jobs := make([]Job, 0, len(resp))
	for _, j := range resp {
		var locs []string
		for _, l := range j.JobLocations {
			locs = append(locs, strings.Trim(l.Name+", "+l.City+", "+l.CountryName, ", "))
		}
		desc := j.Description
		if j.Experience != "" {
			desc += "\nExperience: " + j.Experience + " of experience."
		}
		jobs = append(jobs, Job{
			ID:          jobID(c, j.ID.String()),
			Company:     c.Name,
			Title:       j.Title,
			Location:    strings.Join(locs, "; "),
			URL:         fmt.Sprintf("https://%s.keka.com/careers/jobdetails/%s", c.Slug, j.ID),
			Posted:      shortDate(j.PublishedOn),
			Description: desc,
		})
	}
	return jobs, nil
}

// ---------- Kula ----------

func fetchKula(ctx context.Context, c Company) ([]Job, error) {
	var jobs []Job
	for page := 1; page <= pagesOr(c, 5); page++ {
		q := url.Values{}
		q.Set("accountName", c.Slug)
		q.Set("page", strconv.Itoa(page))
		q.Set("items", "99")
		q.Set("type", "ats_job_post.index")
		var resp struct {
			Data []struct {
				ID       json.Number `json:"id"`
				Title    string      `json:"title"`
				LaunchAt string      `json:"launch_at"`
				AtsJob   struct {
					Description string `json:"job_description"`
					Offices     []struct {
						Name    string `json:"name"`
						City    string `json:"city"`
						Country string `json:"country"`
					} `json:"offices"`
				} `json:"ats_job"`
			} `json:"data"`
			Meta struct {
				Pages int `json:"pages"`
			} `json:"meta"`
		}
		if err := getJSON(ctx, "https://careers.kula.ai/api/internal/ats_job_posts?"+q.Encode(), &resp); err != nil {
			return nil, err
		}
		for _, j := range resp.Data {
			var locs []string
			for _, o := range j.AtsJob.Offices {
				locs = append(locs, strings.Trim(o.City+", "+o.Country+", "+o.Name, ", "))
			}
			jobs = append(jobs, Job{
				ID:          jobID(c, j.ID.String()),
				Company:     c.Name,
				Title:       j.Title,
				Location:    strings.Join(locs, "; "),
				URL:         "https://careers.kula.ai/" + c.Slug + "/" + j.ID.String(),
				Posted:      shortDate(j.LaunchAt),
				Description: j.AtsJob.Description,
			})
		}
		if page >= resp.Meta.Pages {
			break
		}
	}
	return jobs, nil
}

// ---------- MyNextHire ----------

func fetchMyNextHire(ctx context.Context, c Company) ([]Job, error) {
	base := "https://" + c.Slug + ".mynexthire.com"
	data, err := doRequest(ctx, http.MethodPost, base+"/employer/careers/reqlist/get",
		map[string]any{"source": "careers", "code": "", "filterByBuId": -1})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Reqs []struct {
			ReqID           json.Number `json:"reqId"`
			Title           string      `json:"reqTitle"`
			Location        string      `json:"location"`
			LocationAddress string      `json:"locationAddress"`
			ExpMin          json.Number `json:"expMin"`
			JD              string      `json:"jdDisplay"`
			ApprovedOn      string      `json:"approvedOn"`
		} `json:"reqDetailsBOList"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	jobs := make([]Job, 0, len(resp.Reqs))
	for _, j := range resp.Reqs {
		jobs = append(jobs, Job{
			ID:          jobID(c, j.ReqID.String()),
			Company:     c.Name,
			Title:       j.Title,
			Location:    j.Location + "; " + j.LocationAddress,
			URL:         base + "/employer/jobs?src=careers",
			Posted:      shortDate(j.ApprovedOn),
			Description: j.JD + expText(j.ExpMin.String()),
		})
	}
	return jobs, nil
}

// ---------- Recruiterbox (Trakstar Hire) ----------

func fetchRecruiterbox(ctx context.Context, c Company) ([]Job, error) {
	var resp struct {
		Objects []struct {
			ID       string `json:"id"`
			Title    string `json:"title"`
			URL      string `json:"hosted_url"`
			Desc     string `json:"description"`
			Location struct {
				City    string `json:"city"`
				State   string `json:"state"`
				Country string `json:"country"`
			} `json:"location"`
		} `json:"objects"`
	}
	if err := getJSON(ctx, "https://jsapi.recruiterbox.com/v1/openings?limit=100&client_name="+url.QueryEscape(c.Slug), &resp); err != nil {
		return nil, err
	}
	jobs := make([]Job, 0, len(resp.Objects))
	for _, j := range resp.Objects {
		jobs = append(jobs, Job{
			ID:          jobID(c, j.ID),
			Company:     c.Name,
			Title:       j.Title,
			Location:    strings.Trim(j.Location.City+", "+j.Location.State+", "+j.Location.Country, ", "),
			URL:         j.URL,
			Description: j.Desc,
		})
	}
	return jobs, nil
}

// ---------- PyjamaHR ----------

func fetchPyjamaHR(ctx context.Context, c Company) ([]Job, error) {
	next := "https://api.pyjamahr.com/api/career/jobs/?company_uuid=" + url.QueryEscape(c.Slug)
	var jobs []Job
	for page := 0; next != "" && page < pagesOr(c, 20); page++ {
		var resp struct {
			Next    string `json:"next"`
			Results []struct {
				ID            json.Number `json:"id"`
				Title         string      `json:"title"`
				Location      string      `json:"location"`
				Country       string      `json:"country"`
				MinExperience json.Number `json:"min_experience"`
			} `json:"results"`
		}
		if err := getJSON(ctx, next, &resp); err != nil {
			return nil, err
		}
		for _, j := range resp.Results {
			jobs = append(jobs, Job{
				ID:          jobID(c, j.ID.String()),
				Company:     c.Name,
				Title:       j.Title,
				Location:    j.Location + "; " + j.Country,
				URL:         c.Host,
				Description: expText(j.MinExperience.String()),
			})
		}
		next = resp.Next
	}
	return jobs, nil
}

// ---------- aInterviews ----------

func fetchAInterviews(ctx context.Context, c Company) ([]Job, error) {
	var resp struct {
		Jobs []struct {
			ID          json.Number `json:"id"`
			Title       string      `json:"title"`
			Description string      `json:"description"`
			Location    string      `json:"location"`
			ApplyURL    string      `json:"apply_url"`
			PostedDate  string      `json:"posted_date"`
		} `json:"jobs"`
	}
	if err := getJSON(ctx, "https://ainterviews.com/api/job_board/"+url.PathEscape(c.Slug)+"/jobs/", &resp); err != nil {
		return nil, err
	}
	jobs := make([]Job, 0, len(resp.Jobs))
	for _, j := range resp.Jobs {
		jobs = append(jobs, Job{
			ID:          jobID(c, j.ID.String()),
			Company:     c.Name,
			Title:       j.Title,
			Location:    j.Location,
			URL:         "https://ainterviews.com" + j.ApplyURL,
			Posted:      shortDate(j.PostedDate),
			Description: j.Description,
		})
	}
	return jobs, nil
}

// ---------- Plain JSON job feeds ----------

// Some companies publish their job list as plain JSON (MakeMyTrip, Upstox
// and Cars24 export from Darwinbox; Juspay has its own). Host is the full
// feed URL, Site the page to link to. Jobs flagged as not for the careers
// page (post_on_careers_page = 0) or closed are skipped.
func fetchJSONFeed(ctx context.Context, c Company) ([]Job, error) {
	var resp struct {
		AllJobs []feedJob `json:"allJobs"`
		Data    []feedJob `json:"data"`
	}
	if err := getJSON(ctx, c.Host, &resp); err != nil {
		return nil, err
	}
	var jobs []Job
	for _, j := range append(resp.AllJobs, resp.Data...) {
		if j.PostOnCareers.String() == "0" || j.OpeningStatus == false || j.OpeningStatus == "False" {
			continue
		}
		locs := append(append([]string{j.JobLocation}, j.LocationCity...), j.Location...)
		jobs = append(jobs, Job{
			ID:          jobID(c, j.JobID),
			Company:     c.Name,
			Title:       j.JobTitle,
			Location:    strings.Join(locs, "; "),
			URL:         c.Site,
			Description: j.Description + expText(j.ExperienceYear.String()),
		})
	}
	return jobs, nil
}

type feedJob struct {
	JobID          string      `json:"job_id"`
	JobTitle       string      `json:"job_title"`
	JobLocation    string      `json:"job_location"`
	Location       []string    `json:"location"`
	LocationCity   []string    `json:"location_city"`
	PostOnCareers  json.Number `json:"post_on_careers_page"`
	OpeningStatus  any         `json:"opening_status"` // bool or "True"/"False"
	ExperienceYear json.Number `json:"experience_year"`
	Description    string      `json:"job_description_career"`
}
