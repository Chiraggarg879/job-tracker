package main

// Fetchers for career-site platforms that serve HTML or RSS rather than a
// clean JSON API: Avature, Radancy (TalentBrew) and SAP SuccessFactors.

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

func init() {
	fetchers["avature"] = fetchAvature
	fetchers["radancy"] = fetchRadancy
	fetchers["sfjobs"] = fetchSFJobs
	fetchers["sfrss"] = fetchSFRSS
}

func cleanText(s string) string {
	return strings.TrimSpace(spaceRe.ReplaceAllString(html.UnescapeString(tagRe.ReplaceAllString(s, " ")), " "))
}

// ---------- Avature ----------

// Job links are ".../JobDetail/<slug>/<id>" or just ".../JobDetail/<id>".
var avatureLinkRe = regexp.MustCompile(`(?s)<a[^>]+href="([^"]*/JobDetail/(?:[^"/?]+/)?(\d+)[^"]*)"[^>]*>(.*?)</a>`)

// Avature career portals (Synopsys, Siemens, EA, Bloomberg, Tesco). Host is
// the portal base ("synopsys.avature.net/careers"). The search is free text,
// so Location (and Query, if set) are searched for, and every hit is taken
// to be in Location.
func fetchAvature(ctx context.Context, c Company) ([]Job, error) {
	const pageSize = 50
	search := strings.TrimSpace(c.Query + " " + c.Location)
	seen := map[string]bool{}
	var jobs []Job
	// Some portals ignore jobRecordsPerPage (Synopsys shows 12), so the
	// offset advances by the number of jobs each page actually had.
	offset := 0
	for range pagesOr(c, 25) {
		q := url.Values{}
		q.Set("search", search)
		// Portals use either the job* or the folder* paging parameters.
		q.Set("jobRecordsPerPage", strconv.Itoa(pageSize))
		q.Set("jobOffset", strconv.Itoa(offset))
		q.Set("folderRecordsPerPage", strconv.Itoa(pageSize))
		q.Set("folderOffset", strconv.Itoa(offset))
		page, err := getPage(ctx, "https://"+c.Host+"/SearchJobs/?"+q.Encode())
		if err != nil {
			return nil, err
		}
		onPage := map[string]bool{}
		fresh := 0
		for _, m := range avatureLinkRe.FindAllStringSubmatch(page, -1) {
			id, title := m[2], cleanText(m[3])
			if title == "" {
				continue
			}
			onPage[id] = true // a job can have several links on a page
			if seen[id] {
				continue
			}
			seen[id] = true
			fresh++
			link := html.UnescapeString(m[1])
			if i := strings.Index(link, "?"); i > 0 {
				link = link[:i]
			}
			jobs = append(jobs, Job{
				ID:       jobID(c, id),
				Company:  c.Name,
				Title:    title,
				Location: c.Location,
				URL:      link,
			})
		}
		// A page of only already-seen jobs means the portal ignored the
		// offset or we ran past the end.
		if fresh == 0 {
			break
		}
		offset += len(onPage)
	}
	return jobs, nil
}

// ---------- Radancy (TalentBrew) ----------

var (
	radancyJobRe   = regexp.MustCompile(`(?s)<a href="(/[^"]*job/[^"]+/(\d+))"[^>]*>(.*?)</a>`)
	radancyTitleRe = regexp.MustCompile(`(?s)<h2[^>]*>(.*?)</h2>`)
	radancyLocRe   = regexp.MustCompile(`(?s)<span class="job-location[^"]*">(.*?)</span>`)
)

// Radancy career sites (Intuit, Optum, Moody's). Host is the site, Slug the
// country facet id (1269750 = India), Site an optional path prefix ("/en").
// The search API returns rendered HTML inside JSON.
func fetchRadancy(ctx context.Context, c Company) ([]Job, error) {
	const pageSize = 50
	var jobs []Job
	for page := 1; page <= pagesOr(c, 4); page++ {
		q := url.Values{}
		q.Set("ActiveFacetID", c.Slug)
		q.Set("CurrentPage", strconv.Itoa(page))
		q.Set("RecordsPerPage", strconv.Itoa(pageSize))
		q.Set("Keywords", c.Query)
		q.Set("Location", "")
		q.Set("Distance", "50")
		q.Set("RadiusUnitType", "0")
		q.Set("ShowRadius", "False")
		q.Set("IsPagination", "True")
		q.Set("FacetType", "0")
		q.Set("FacetFilters[0].ID", c.Slug)
		q.Set("FacetFilters[0].FacetType", "2")
		q.Set("FacetFilters[0].Count", "0")
		q.Set("FacetFilters[0].Display", c.Location)
		q.Set("FacetFilters[0].IsApplied", "true")
		q.Set("FacetFilters[0].FieldName", "")
		q.Set("SearchResultsModuleName", "Search Results")
		q.Set("SearchFiltersModuleName", "Search Filters")
		q.Set("SortCriteria", "0")
		q.Set("SortDirection", "0")
		q.Set("SearchType", "5")
		var resp struct {
			Results string `json:"results"`
		}
		if err := getJSONXHR(ctx, "https://"+c.Host+c.Site+"/search-jobs/results?"+q.Encode(), &resp); err != nil {
			return nil, err
		}
		found := 0
		for _, m := range radancyJobRe.FindAllStringSubmatch(resp.Results, -1) {
			t := radancyTitleRe.FindStringSubmatch(m[3])
			if t == nil {
				continue
			}
			found++
			// The search is already narrowed to the country facet.
			loc := c.Location
			if l := radancyLocRe.FindStringSubmatch(m[3]); l != nil {
				loc = cleanText(l[1]) + "; " + c.Location
			}
			jobs = append(jobs, Job{
				ID:       jobID(c, m[2]),
				Company:  c.Name,
				Title:    cleanText(t[1]),
				Location: loc,
				URL:      "https://" + c.Host + m[1],
			})
		}
		if found < pageSize {
			break
		}
	}
	return jobs, nil
}

// getPage fetches an HTML page or RSS feed with a browser-like Accept
// header; some sites answer the JSON-first header of doRequest with a
// different page or HTTP 406.
func getPage(ctx context.Context, rawURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", &httpError{rawURL, resp.StatusCode}
	}
	var b strings.Builder
	_, err = io.Copy(&b, resp.Body)
	return b.String(), err
}

// getJSONXHR is getJSON for endpoints that only answer AJAX requests.
func getJSONXHR(ctx context.Context, rawURL string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &httpError{rawURL, resp.StatusCode}
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

type httpError struct {
	url  string
	code int
}

func (e *httpError) Error() string { return "GET " + e.url + ": HTTP " + strconv.Itoa(e.code) }

// ---------- SAP SuccessFactors ----------

// SuccessFactors career sites with the JSON search API (Standard
// Chartered). Host is the site, Site the locale ("en_GB").
func fetchSFJobs(ctx context.Context, c Company) ([]Job, error) {
	var jobs []Job
	for page := range pagesOr(c, 5) {
		body := map[string]any{
			"keywords":   c.Query,
			"locale":     c.Site,
			"location":   c.Location,
			"pageNumber": page,
			"sortBy":     "recent",
		}
		data, err := doRequest(ctx, http.MethodPost, "https://"+c.Host+"/services/recruiting/v1/jobs", body)
		if err != nil {
			return nil, err
		}
		var resp struct {
			TotalJobs       int `json:"totalJobs"`
			JobSearchResult []struct {
				Response struct {
					ID       string   `json:"id"`
					Title    string   `json:"unifiedStandardTitle"`
					URLTitle string   `json:"urlTitle"`
					LocShort []string `json:"jobLocationShort"`
					Country  []string `json:"jobLocationCountry"`
					Start    string   `json:"unifiedStandardStart"`
				} `json:"response"`
			} `json:"jobSearchResult"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			return nil, err
		}
		for _, r := range resp.JobSearchResult {
			j := r.Response
			jobs = append(jobs, Job{
				ID:       jobID(c, j.ID),
				Company:  c.Name,
				Title:    j.Title,
				Location: strings.Join(append(j.LocShort, j.Country...), "; "),
				URL:      "https://" + c.Host + "/job/" + j.URLTitle + "/" + j.ID + "-" + c.Site,
				Posted:   j.Start,
			})
		}
		if len(resp.JobSearchResult) == 0 || (page+1)*10 >= resp.TotalJobs {
			break
		}
	}
	return jobs, nil
}

var (
	rssItemRe  = regexp.MustCompile(`(?s)<item>(.*?)</item>`)
	rssFieldRe = func(tag string) *regexp.Regexp {
		return regexp.MustCompile(`(?s)<` + tag + `>(?:<!\[CDATA\[)?(.*?)(?:\]\]>)?</` + tag + `>`)
	}
	rssTitleRe = rssFieldRe("title")
	rssLinkRe  = rssFieldRe("link")
	rssDescRe  = rssFieldRe("description")
	// "Software Engineer (Hyderabad, TG, IN, 500032)": location in the last parentheses.
	sfTitleLocRe = regexp.MustCompile(`^(.*)\s+\(([^()]*)\)\s*$`)
	sfJobIDRe    = regexp.MustCompile(`/(\d+)/?(?:\?|$)`)
)

// SuccessFactors sites whose JSON API is closed but whose RSS feed works
// (HSBC, Nomura). Host is the site, Site the locale. The feed returns the
// 20 most recent matches, enough between twice-daily checks.
func fetchSFRSS(ctx context.Context, c Company) ([]Job, error) {
	q := "locale=" + url.QueryEscape(c.Site) + "&keywords=" +
		url.PathEscape("("+c.Query+") AND locationSearch:("+c.Location+")")
	data, err := getPage(ctx, "https://"+c.Host+"/services/rss/job/?"+q)
	if err != nil {
		return nil, err
	}
	var jobs []Job
	for _, item := range rssItemRe.FindAllStringSubmatch(data, -1) {
		field := func(re *regexp.Regexp) string {
			if m := re.FindStringSubmatch(item[1]); m != nil {
				return strings.TrimSpace(m[1])
			}
			return ""
		}
		title, link := html.UnescapeString(field(rssTitleRe)), html.UnescapeString(field(rssLinkRe))
		if i := strings.Index(link, "?"); i > 0 {
			link = link[:i] // drop RSS tracking parameters
		}
		loc := c.Location
		if m := sfTitleLocRe.FindStringSubmatch(title); m != nil {
			title, loc = m[1], m[2]+", "+c.Location
		}
		id := link
		if m := sfJobIDRe.FindStringSubmatch(link); m != nil {
			id = m[1]
		}
		jobs = append(jobs, Job{
			ID:          jobID(c, id),
			Company:     c.Name,
			Title:       title,
			Location:    loc,
			URL:         link,
			Description: html.UnescapeString(field(rssDescRe)),
		})
	}
	return jobs, nil
}

// ---------- Single-company feeds ----------

func init() {
	fetchers["jobvitexml"] = fetchJobviteXML
	fetchers["deshaw"] = fetchDEShaw
	fetchers["teradata"] = fetchTeradata
	fetchers["mercedes"] = fetchMercedes
}

var (
	xmlJobRe = regexp.MustCompile(`(?s)<job>(.*?)</job>`)
	xmlField = func(item, tag string) string {
		if m := rssFieldRe(tag).FindStringSubmatch(item); m != nil {
			return strings.TrimSpace(html.UnescapeString(m[1]))
		}
		return ""
	}
)

// Jobvite XML job feed (Nutanix). Host is the full feed URL.
func fetchJobviteXML(ctx context.Context, c Company) ([]Job, error) {
	data, err := getPage(ctx, c.Host)
	if err != nil {
		return nil, err
	}
	var jobs []Job
	for _, m := range xmlJobRe.FindAllStringSubmatch(data, -1) {
		item := m[1]
		jobs = append(jobs, Job{
			ID:          jobID(c, xmlField(item, "apijobid")),
			Company:     c.Name,
			Title:       xmlField(item, "title"),
			Location:    xmlField(item, "city") + ", " + xmlField(item, "state") + ", " + xmlField(item, "country"),
			URL:         xmlField(item, "url"),
			Posted:      shortDate(xmlField(item, "date")),
			Description: xmlField(item, "description"),
		})
	}
	return jobs, nil
}

var nextDataRe = regexp.MustCompile(`(?s)<script id="__NEXT_DATA__" type="application/json">(.*?)</script>`)

// D. E. Shaw India's careers page embeds its listings as Next.js data.
// Every listing there is in India.
func fetchDEShaw(ctx context.Context, c Company) ([]Job, error) {
	page, err := getPage(ctx, "https://www.deshawindia.com/careers")
	if err != nil {
		return nil, err
	}
	m := nextDataRe.FindStringSubmatch(page)
	if m == nil {
		return nil, fmt.Errorf("deshaw: no __NEXT_DATA__ on careers page")
	}
	var d struct {
		Props struct {
			PageProps struct {
				RegularJobs []struct {
					Data struct {
						ID          json.Number `json:"id"`
						DisplayName string      `json:"displayName"`
						JobURL      string      `json:"jobUrl"`
					} `json:"data"`
				} `json:"regularJobs"`
			} `json:"pageProps"`
		} `json:"props"`
	}
	if err := json.Unmarshal([]byte(m[1]), &d); err != nil {
		return nil, err
	}
	var jobs []Job
	for _, j := range d.Props.PageProps.RegularJobs {
		jobs = append(jobs, Job{
			ID:       jobID(c, j.Data.ID.String()),
			Company:  c.Name,
			Title:    j.Data.DisplayName,
			Location: "India",
			URL:      "https://www.deshawindia.com/careers/" + j.Data.JobURL,
		})
	}
	return jobs, nil
}

// Teradata's careers GraphQL API; at most 100 per page.
func fetchTeradata(ctx context.Context, c Company) ([]Job, error) {
	const pageSize = 100
	const query = `query($query:String,$first:Int,$start:Int){ searchJobPostings(query:$query, first:$first, start:$start){ results { totalCount nodes { number title postedOn places { nodes { name } } } } } }`
	var jobs []Job
	for page := range pagesOr(c, 3) {
		data, err := doRequest(ctx, http.MethodPost, "https://careers.teradata.com/graphql", map[string]any{
			"query":     query,
			"variables": map[string]any{"query": c.Query, "first": pageSize, "start": page * pageSize},
		})
		if err != nil {
			return nil, err
		}
		var resp struct {
			Data struct {
				Search struct {
					Results struct {
						TotalCount int `json:"totalCount"`
						Nodes      []struct {
							Number   string `json:"number"`
							Title    string `json:"title"`
							PostedOn string `json:"postedOn"`
							Places   struct {
								Nodes []struct {
									Name string `json:"name"`
								} `json:"nodes"`
							} `json:"places"`
						} `json:"nodes"`
					} `json:"results"`
				} `json:"searchJobPostings"`
			} `json:"data"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			return nil, err
		}
		r := resp.Data.Search.Results
		for _, j := range r.Nodes {
			var locs []string
			for _, p := range j.Places.Nodes {
				locs = append(locs, p.Name)
			}
			jobs = append(jobs, Job{
				ID:       jobID(c, j.Number),
				Company:  c.Name,
				Title:    j.Title,
				Location: strings.Join(locs, "; "),
				URL:      "https://careers.teradata.com/jobs/" + j.Number,
				Posted:   shortDate(j.PostedOn),
			})
		}
		if len(r.Nodes) < pageSize || (page+1)*pageSize >= r.TotalCount {
			break
		}
	}
	return jobs, nil
}

// Mercedes-Benz group job search API. Slug is the numeric country code the
// API uses ("390" = India).
func fetchMercedes(ctx context.Context, c Company) ([]Job, error) {
	const pageSize = 100
	var jobs []Job
	for page := range pagesOr(c, 5) {
		params, _ := json.Marshal(map[string]any{
			"LanguageCode": "EN",
			"SearchParameters": map[string]any{
				"FirstItem": page*pageSize + 1,
				"CountItem": pageSize,
				"MatchedObjectDescriptor": []string{"PositionTitle", "PositionURI", "PositionLocation.CityName",
					"PositionLocation.CountryName", "PublicationStartDate"},
			},
			"SearchCriteria": []any{map[string]any{"CriterionName": "PositionLocation.Country", "CriterionValue": []string{c.Slug}}},
		})
		var resp struct {
			SearchResult struct {
				Count int `json:"SearchResultCountAll"`
				Items []struct {
					ID   string `json:"MatchedObjectId"`
					Desc struct {
						Title    string `json:"PositionTitle"`
						URI      string `json:"PositionURI"`
						Start    string `json:"PublicationStartDate"`
						Location []struct {
							City    string `json:"CityName"`
							Country string `json:"CountryName"`
						} `json:"PositionLocation"`
					} `json:"MatchedObjectDescriptor"`
				} `json:"SearchResultItems"`
			} `json:"SearchResult"`
		}
		if err := getJSON(ctx, "https://jobs.api.mercedes-benz.com/search/?data="+url.QueryEscape(string(params)), &resp); err != nil {
			return nil, err
		}
		sr := resp.SearchResult
		for _, j := range sr.Items {
			var locs []string
			for _, l := range j.Desc.Location {
				locs = append(locs, l.City+", "+l.Country)
			}
			jobs = append(jobs, Job{
				ID:       jobID(c, j.ID),
				Company:  c.Name,
				Title:    j.Desc.Title,
				Location: strings.Join(locs, "; ") + "; India",
				URL:      j.Desc.URI,
				Posted:   shortDate(j.Desc.Start),
			})
		}
		if len(sr.Items) < pageSize || (page+1)*pageSize >= sr.Count {
			break
		}
	}
	return jobs, nil
}
