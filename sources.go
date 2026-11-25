package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Job struct {
	ID       string // globally unique: "<source>:<company>:<job id>"
	Company  string
	Title    string
	Location string
	URL      string
	Posted   string

	// RawID is the source's own job reference, used to fetch details later.
	RawID string
	// Description holds the requirements text. Sources whose listing omits
	// it get it lazily from a describer, only for jobs that pass the filters.
	Description string
	// Years is the minimum experience the posting asks for, when YearsKnown.
	Years      int
	YearsKnown bool
}

type fetchFunc func(ctx context.Context, c Company) ([]Job, error)

// describeFunc fetches the description of a single job.
type describeFunc func(ctx context.Context, c Company, j Job) (string, error)

var describers = map[string]describeFunc{
	"greenhouse":      describeGreenhouse,
	"workday":         describeWorkday,
	"microsoft":       describeMicrosoft,
	"google":          describeGoogle,
	"smartrecruiters": describeSmartRecruiters,
	"oracle":          describeOracle,
}

var fetchers = map[string]fetchFunc{
	"greenhouse":      fetchGreenhouse,
	"lever":           fetchLever,
	"ashby":           fetchAshby,
	"workday":         fetchWorkday,
	"amazon":          fetchAmazon,
	"microsoft":       fetchMicrosoft,
	"google":          fetchGoogle,
	"atlassian":       fetchAtlassian,
	"smartrecruiters": fetchSmartRecruiters,
	"oracle":          fetchOracle,
}

// Generous because some boards are large and slow: Paytm's Lever feed is
// ~1.4 MB and has taken up to 90s to download.
var httpClient = &http.Client{Timeout: 120 * time.Second}

const userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36"

// errRetryable marks rate-limit and server errors worth retrying.
type errRetryable struct{ err error }

func (e errRetryable) Error() string { return e.err.Error() }

// doRequest performs the request, retrying rate limits (429) and server
// errors with backoff: 5s, 15s, 45s.
func doRequest(ctx context.Context, method, rawURL string, body any) ([]byte, error) {
	var payload []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		payload = b
	}
	backoff := 5 * time.Second
	for attempt := 0; ; attempt++ {
		data, err := doRequestOnce(ctx, method, rawURL, payload)
		var re errRetryable
		if err == nil || attempt == 3 || !errors.As(err, &re) {
			return data, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 3
	}
}

func doRequestOnce(ctx context.Context, method, rawURL string, payload []byte) ([]byte, error) {
	var rdr io.Reader
	if payload != nil {
		rdr = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json, text/html;q=0.9")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("%s %s: HTTP %d", method, rawURL, resp.StatusCode)
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			return nil, errRetryable{err}
		}
		return nil, err
	}
	return data, nil
}

func getJSON(ctx context.Context, rawURL string, out any) error {
	data, err := doRequest(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func jobID(c Company, id string) string {
	return c.Source + ":" + strings.ToLower(c.Name) + ":" + id
}

func pagesOr(c Company, def int) int {
	if c.Pages > 0 {
		return c.Pages
	}
	return def
}

// ---------- Greenhouse ----------

func fetchGreenhouse(ctx context.Context, c Company) ([]Job, error) {
	var resp struct {
		Jobs []struct {
			ID             int64  `json:"id"`
			Title          string `json:"title"`
			AbsoluteURL    string `json:"absolute_url"`
			FirstPublished string `json:"first_published"`
			UpdatedAt      string `json:"updated_at"`
			Location       struct {
				Name string `json:"name"`
			} `json:"location"`
		} `json:"jobs"`
	}
	if err := getJSON(ctx, "https://boards-api.greenhouse.io/v1/boards/"+url.PathEscape(c.Slug)+"/jobs", &resp); err != nil {
		return nil, err
	}
	jobs := make([]Job, 0, len(resp.Jobs))
	for _, j := range resp.Jobs {
		posted := j.FirstPublished
		if posted == "" {
			posted = j.UpdatedAt
		}
		jobs = append(jobs, Job{
			ID:       jobID(c, strconv.FormatInt(j.ID, 10)),
			Company:  c.Name,
			Title:    j.Title,
			Location: j.Location.Name,
			URL:      j.AbsoluteURL,
			Posted:   shortDate(posted),
			RawID:    strconv.FormatInt(j.ID, 10),
		})
	}
	return jobs, nil
}

func describeGreenhouse(ctx context.Context, c Company, j Job) (string, error) {
	var resp struct {
		Content string `json:"content"`
	}
	err := getJSON(ctx, "https://boards-api.greenhouse.io/v1/boards/"+url.PathEscape(c.Slug)+"/jobs/"+j.RawID, &resp)
	return resp.Content, err
}

// ---------- Lever ----------

func fetchLever(ctx context.Context, c Company) ([]Job, error) {
	var resp []struct {
		ID               string `json:"id"`
		Text             string `json:"text"`
		HostedURL        string `json:"hostedUrl"`
		CreatedAt        int64  `json:"createdAt"`
		DescriptionPlain string `json:"descriptionPlain"`
		AdditionalPlain  string `json:"additionalPlain"`
		Lists            []struct {
			Text    string `json:"text"`
			Content string `json:"content"`
		} `json:"lists"`
		Categories struct {
			Location     string   `json:"location"`
			AllLocations []string `json:"allLocations"`
		} `json:"categories"`
	}
	if err := getJSON(ctx, "https://api.lever.co/v0/postings/"+url.PathEscape(c.Slug)+"?mode=json", &resp); err != nil {
		return nil, err
	}
	jobs := make([]Job, 0, len(resp))
	for _, j := range resp {
		locs := j.Categories.AllLocations
		if len(locs) == 0 && j.Categories.Location != "" {
			locs = []string{j.Categories.Location}
		}
		// Requirements usually live in the "lists" sections (e.g. "What you need").
		desc := []string{j.DescriptionPlain}
		for _, l := range j.Lists {
			desc = append(desc, l.Text+": "+l.Content)
		}
		desc = append(desc, j.AdditionalPlain)
		jobs = append(jobs, Job{
			ID:          jobID(c, j.ID),
			Company:     c.Name,
			Title:       j.Text,
			Location:    strings.Join(locs, "; "),
			URL:         j.HostedURL,
			Posted:      time.UnixMilli(j.CreatedAt).Format("2006-01-02"),
			Description: strings.Join(desc, "\n"),
		})
	}
	return jobs, nil
}

// ---------- Ashby ----------

func fetchAshby(ctx context.Context, c Company) ([]Job, error) {
	var resp struct {
		Jobs []struct {
			ID                 string `json:"id"`
			Title              string `json:"title"`
			Location           string `json:"location"`
			JobURL             string `json:"jobUrl"`
			PublishedAt        string `json:"publishedAt"`
			IsListed           bool   `json:"isListed"`
			DescriptionPlain   string `json:"descriptionPlain"`
			SecondaryLocations []struct {
				Location string `json:"location"`
			} `json:"secondaryLocations"`
		} `json:"jobs"`
	}
	if err := getJSON(ctx, "https://api.ashbyhq.com/posting-api/job-board/"+url.PathEscape(c.Slug), &resp); err != nil {
		return nil, err
	}
	jobs := make([]Job, 0, len(resp.Jobs))
	for _, j := range resp.Jobs {
		if !j.IsListed {
			continue
		}
		locs := []string{j.Location}
		for _, s := range j.SecondaryLocations {
			locs = append(locs, s.Location)
		}
		jobs = append(jobs, Job{
			ID:          jobID(c, j.ID),
			Company:     c.Name,
			Title:       j.Title,
			Location:    strings.Join(locs, "; "),
			URL:         j.JobURL,
			Posted:      shortDate(j.PublishedAt),
			Description: j.DescriptionPlain,
		})
	}
	return jobs, nil
}

// ---------- Workday ----------

// workdayReqIDRe matches requisition IDs like "JR2025205", "R-294544",
// "REF088312W" or "1205422": no spaces and at least four digits.
var workdayReqIDRe = regexp.MustCompile(`^[A-Za-z]*[-_]?[A-Za-z]*\d{4,}[-_A-Za-z0-9]*$`)

const indianCities = `Bengaluru|Bangalore|Hyderabad|Pune|Chennai|Mumbai|Navi Mumbai|Thane|Gurugram|Gurgaon|Noida|New Delhi|Delhi|` +
	`Kolkata|Ahmedabad|Coimbatore|Kochi|Trivandrum|Thiruvananthapuram|Jaipur|Chandigarh|Mohali|Indore|Vadodara|Mysore|Mysuru`

type workdayFacet struct {
	FacetParameter string         `json:"facetParameter"`
	Descriptor     string         `json:"descriptor"`
	ID             string         `json:"id"`
	Count          int            `json:"count"`
	Values         []workdayFacet `json:"values"`
}

// workdayLocationFacet finds the tenant's facet values for a location such
// as "India". Tenants name the facet differently ("locationCountry",
// "Location_Country", "locations" with one value per city), so it walks all
// facets and prefers a country-level one. It returns nil when none matches.
func workdayLocationFacet(ctx context.Context, api, location string) (map[string]any, error) {
	data, err := doRequest(ctx, http.MethodPost, api, map[string]any{
		"appliedFacets": map[string]any{}, "limit": 1, "offset": 0, "searchText": "",
	})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Facets []workdayFacet `json:"facets"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	// Whole word, so "India" doesn't match "Indiana" or "Indianapolis". Some
	// tenants list only cities ("Pune", "Office - Noida"), so for India the
	// main tech cities count too.
	pattern := regexp.QuoteMeta(location)
	if strings.EqualFold(location, "India") {
		pattern += "|" + indianCities
	}
	re := regexp.MustCompile(`(?i)\b(?:` + pattern + `)\b`)
	ids := map[string][]string{}
	counts := map[string]int{}
	var walk func(fs []workdayFacet, param string)
	walk = func(fs []workdayFacet, param string) {
		for _, f := range fs {
			p := param
			if f.FacetParameter != "" {
				p = f.FacetParameter
			}
			if len(f.Values) > 0 {
				walk(f.Values, p)
			} else if f.ID != "" && re.MatchString(f.Descriptor) {
				ids[p] = append(ids[p], f.ID)
				counts[p] += f.Count
			}
		}
	}
	walk(resp.Facets, "")
	best := ""
	for p := range ids {
		isCountry := strings.Contains(strings.ToLower(p), "country")
		bestIsCountry := strings.Contains(strings.ToLower(best), "country")
		if best == "" || (isCountry && !bestIsCountry) || (isCountry == bestIsCountry && counts[p] > counts[best]) {
			best = p
		}
	}
	if best == "" {
		return nil, nil
	}
	return map[string]any{best: ids[best]}, nil
}

// With Location set, results are narrowed by the tenant's location facet
// and every page is read (up to Pages), so a new posting can't hide below a
// relevance cutoff. Without it, Workday has no date sort, so we take the top
// N pages of a search; since we track seen IDs, any new posting that lands
// in those pages is caught.
func fetchWorkday(ctx context.Context, c Company) ([]Job, error) {
	base := fmt.Sprintf("https://%s.%s.myworkdayjobs.com", c.Slug, c.Host)
	api := fmt.Sprintf("%s/wday/cxs/%s/%s/jobs", base, c.Slug, c.Site)
	const pageSize = 20

	facets := map[string]any{}
	pages := pagesOr(c, 3)
	query := c.Query
	if c.Location != "" {
		f, err := workdayLocationFacet(ctx, api, c.Location)
		if err != nil {
			return nil, err
		}
		if f != nil {
			facets = f
			pages = pagesOr(c, 40)
		} else {
			// No location facet: fall back to searching for the location.
			query = strings.TrimSpace(query + " " + c.Location)
		}
	}

	var jobs []Job
	total := 0
	for page := range pages {
		body := map[string]any{
			"appliedFacets": facets,
			"limit":         pageSize,
			"offset":        page * pageSize,
			"searchText":    query,
		}
		data, err := doRequest(ctx, http.MethodPost, api, body)
		if err != nil {
			return nil, err
		}
		var resp struct {
			Total       int `json:"total"`
			JobPostings []struct {
				Title         string   `json:"title"`
				ExternalPath  string   `json:"externalPath"`
				LocationsText string   `json:"locationsText"`
				PostedOn      string   `json:"postedOn"`
				BulletFields  []string `json:"bulletFields"`
			} `json:"jobPostings"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			return nil, err
		}
		// Workday reports the total only on the first page; later pages say 0.
		if page == 0 {
			total = resp.Total
		}
		for _, j := range resp.JobPostings {
			// bulletFields[0] is usually the requisition ID ("JR123"), but some
			// tenants put shared text there (a location or date), which would
			// merge different jobs into one; fall back to the unique path then.
			id := j.ExternalPath
			if len(j.BulletFields) > 0 && workdayReqIDRe.MatchString(j.BulletFields[0]) {
				id = j.BulletFields[0]
			}
			// externalPath looks like "/job/India-Pune/Title_JR123"; the second
			// segment is the primary location, which matters when locationsText
			// just says "3 Locations".
			loc := j.LocationsText
			if parts := strings.Split(j.ExternalPath, "/"); len(parts) > 2 {
				loc += "; " + strings.ReplaceAll(parts[2], "-", " ")
			}
			// The location facet guarantees one of the job's locations is
			// c.Location, even when the primary one shown is elsewhere
			// ("Singapore; 2 Locations").
			if len(facets) > 0 {
				loc += "; " + c.Location
			}
			jobs = append(jobs, Job{
				ID:       jobID(c, id),
				Company:  c.Name,
				Title:    j.Title,
				Location: loc,
				URL:      base + "/en-US/" + c.Site + j.ExternalPath,
				Posted:   j.PostedOn,
				RawID:    j.ExternalPath,
			})
		}
		if (page+1)*pageSize >= total {
			break
		}
	}
	if len(c.Location) > 0 && len(facets) > 0 && len(jobs) < total {
		log.Printf("[%s] read only %d of %d jobs; raise \"pages\" or add a \"query\"", c.Name, len(jobs), total)
	}
	return jobs, nil
}

func describeWorkday(ctx context.Context, c Company, j Job) (string, error) {
	var resp struct {
		JobPostingInfo struct {
			JobDescription string `json:"jobDescription"`
		} `json:"jobPostingInfo"`
	}
	api := fmt.Sprintf("https://%s.%s.myworkdayjobs.com/wday/cxs/%s/%s%s", c.Slug, c.Host, c.Slug, c.Site, j.RawID)
	err := getJSON(ctx, api, &resp)
	return resp.JobPostingInfo.JobDescription, err
}

// ---------- Oracle Recruiting Cloud ----------

// Oracle (and other companies on Oracle Recruiting Cloud) expose a public
// REST API. Host is the pod ("eeho.fa.us2", "jpmc.fa") or a company's own
// domain ("enterpriseplatform.dell.com"); Slug the site number
// ("CX_45001"). Results are sorted newest first, so the top pages catch
// every new posting.

// oracleHost expands a pod name to its oraclecloud.com host; a full
// hostname (a company's custom domain) is used as is.
func oracleHost(c Company) string {
	if strings.HasSuffix(c.Host, ".com") {
		return c.Host
	}
	return c.Host + ".oraclecloud.com"
}

func fetchOracle(ctx context.Context, c Company) ([]Job, error) {
	const pageSize = 25
	api := fmt.Sprintf("https://%s/hcmRestApi/resources/latest/recruitingCEJobRequisitions", oracleHost(c))
	var jobs []Job
	for page := range pagesOr(c, 4) {
		finder := fmt.Sprintf("findReqs;siteNumber=%s,limit=%d,offset=%d,keyword=%q,location=%s,sortBy=POSTING_DATES_DESC",
			c.Slug, pageSize, page*pageSize, c.Query, c.Location)
		var resp struct {
			Items []struct {
				TotalJobsCount  int `json:"TotalJobsCount"`
				RequisitionList []struct {
					ID              string `json:"Id"`
					Title           string `json:"Title"`
					PrimaryLocation string `json:"PrimaryLocation"`
					PostedDate      string `json:"PostedDate"`
				} `json:"requisitionList"`
			} `json:"items"`
		}
		q := "onlyData=true&expand=requisitionList.secondaryLocations&finder=" + url.PathEscape(finder)
		if err := getJSON(ctx, api+"?"+q, &resp); err != nil {
			return nil, err
		}
		if len(resp.Items) == 0 {
			break
		}
		it := resp.Items[0]
		for _, j := range it.RequisitionList {
			jobs = append(jobs, Job{
				ID:       jobID(c, j.ID),
				Company:  c.Name,
				Title:    j.Title,
				Location: j.PrimaryLocation,
				URL:      fmt.Sprintf("https://%s/hcmUI/CandidateExperience/en/sites/%s/job/%s", oracleHost(c), c.Slug, j.ID),
				Posted:   j.PostedDate,
				RawID:    j.ID,
			})
		}
		if len(it.RequisitionList) < pageSize || (page+1)*pageSize >= it.TotalJobsCount {
			break
		}
	}
	return jobs, nil
}

func describeOracle(ctx context.Context, c Company, j Job) (string, error) {
	var resp struct {
		Items []struct {
			Description    string `json:"ExternalDescriptionStr"`
			Qualifications string `json:"ExternalQualificationsStr"`
		} `json:"items"`
	}
	finder := fmt.Sprintf("ById;Id=%q,siteNumber=%s", j.RawID, c.Slug)
	api := fmt.Sprintf("https://%s/hcmRestApi/resources/latest/recruitingCEJobRequisitionDetails?expand=all&onlyData=true&finder=%s",
		oracleHost(c), url.PathEscape(finder))
	if err := getJSON(ctx, api, &resp); err != nil || len(resp.Items) == 0 {
		return "", err
	}
	return resp.Items[0].Description + "\n" + resp.Items[0].Qualifications, nil
}

// ---------- Amazon ----------

func fetchAmazon(ctx context.Context, c Company) ([]Job, error) {
	const pageSize = 100
	var jobs []Job
	for page := range pagesOr(c, 1) {
		q := url.Values{}
		q.Set("base_query", c.Query)
		q.Set("normalized_country_code[]", c.Location)
		q.Set("result_limit", strconv.Itoa(pageSize))
		q.Set("offset", strconv.Itoa(page*pageSize))
		q.Set("sort", "recent")
		var resp struct {
			Hits int `json:"hits"`
			Jobs []struct {
				IDIcims             string `json:"id_icims"`
				Title               string `json:"title"`
				NormalizedLocation  string `json:"normalized_location"`
				JobPath             string `json:"job_path"`
				PostedDate          string `json:"posted_date"`
				BasicQualifications string `json:"basic_qualifications"`
			} `json:"jobs"`
		}
		if err := getJSON(ctx, "https://www.amazon.jobs/en/search.json?"+q.Encode(), &resp); err != nil {
			return nil, err
		}
		for _, j := range resp.Jobs {
			jobs = append(jobs, Job{
				ID:       jobID(c, j.IDIcims),
				Company:  c.Name,
				Title:    j.Title,
				Location: j.NormalizedLocation,
				URL:      "https://www.amazon.jobs" + j.JobPath,
				Posted:   j.PostedDate,
				// Only the minimum bar; preferred_qualifications often asks for more.
				Description: j.BasicQualifications,
			})
		}
		if len(resp.Jobs) < pageSize || (page+1)*pageSize >= resp.Hits {
			break
		}
	}
	return jobs, nil
}

// ---------- Microsoft ----------

func fetchMicrosoft(ctx context.Context, c Company) ([]Job, error) {
	var jobs []Job
	for page := range pagesOr(c, 3) {
		// Microsoft rate-limits (HTTP 429) bursts of page requests.
		if page > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Second):
			}
		}
		q := url.Values{}
		q.Set("domain", "microsoft.com")
		q.Set("query", c.Query)
		q.Set("location", c.Location)
		q.Set("start", strconv.Itoa(page*10))
		q.Set("sort_by", "timestamp")
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
		if err := getJSON(ctx, "https://apply.careers.microsoft.com/api/pcsx/search?"+q.Encode(), &resp); err != nil {
			return nil, err
		}
		if len(resp.Data.Positions) == 0 {
			break
		}
		for _, j := range resp.Data.Positions {
			jobs = append(jobs, Job{
				ID:       jobID(c, j.ID.String()),
				Company:  c.Name,
				Title:    j.Name,
				Location: strings.Join(j.Locations, "; "),
				URL:      "https://apply.careers.microsoft.com" + j.PositionURL,
				Posted:   time.Unix(j.PostedTs, 0).Format("2006-01-02"),
				RawID:    j.ID.String(),
			})
		}
	}
	return jobs, nil
}

func describeMicrosoft(ctx context.Context, c Company, j Job) (string, error) {
	var resp struct {
		Data struct {
			JobDescription string `json:"jobDescription"`
		} `json:"data"`
	}
	q := url.Values{}
	q.Set("position_id", j.RawID)
	q.Set("domain", "microsoft.com")
	q.Set("hl", "en")
	err := getJSON(ctx, "https://apply.careers.microsoft.com/api/pcsx/position_details?"+q.Encode(), &resp)
	return resp.Data.JobDescription, err
}

// ---------- Google ----------

// Google has no public JSON API, so this parses the server-rendered results
// page. Each card renders as: <h3 class="QJPWVe">title</h3>, location
// spans, then a "jobs/results/<id>-<slug>" link.
var (
	googleTitleRe = regexp.MustCompile(`<h3 class="QJPWVe">([^<]*)</h3>`)
	googleLocRe   = regexp.MustCompile(`<span class="r0wTof ">([^<]*)</span>`)
	googleLinkRe  = regexp.MustCompile(`jobs/results/(\d+)-[a-z0-9-]+`)
)

func fetchGoogle(ctx context.Context, c Company) ([]Job, error) {
	var jobs []Job
	for page := 1; page <= pagesOr(c, 2); page++ {
		q := url.Values{}
		q.Set("q", c.Query)
		q.Set("location", c.Location)
		q.Set("sort_by", "date")
		if page > 1 {
			q.Set("page", strconv.Itoa(page))
		}
		data, err := doRequest(ctx, http.MethodGet, "https://www.google.com/about/careers/applications/jobs/results/?"+q.Encode(), nil)
		if err != nil {
			return nil, err
		}
		pageHTML := string(data)
		idx := googleTitleRe.FindAllStringSubmatchIndex(pageHTML, -1)
		for i, m := range idx {
			end := len(pageHTML)
			if i+1 < len(idx) {
				end = idx[i+1][0]
			}
			card := pageHTML[m[1]:end]
			link := googleLinkRe.FindStringSubmatch(card)
			if link == nil {
				continue
			}
			var locs []string
			for _, l := range googleLocRe.FindAllStringSubmatch(card, -1) {
				loc := html.UnescapeString(l[1])
				if !containsStr(locs, loc) {
					locs = append(locs, loc)
				}
			}
			jobs = append(jobs, Job{
				ID:       jobID(c, link[1]),
				Company:  c.Name,
				Title:    html.UnescapeString(pageHTML[m[2]:m[3]]),
				Location: strings.Join(locs, "; "),
				URL:      "https://www.google.com/about/careers/applications/" + link[0],
			})
		}
		if len(idx) == 0 {
			if page == 1 {
				return nil, fmt.Errorf("google: no job cards found (page layout may have changed)")
			}
			break
		}
	}
	return jobs, nil
}

// describeGoogle returns only the "Minimum qualifications" block of the job
// page, so preferred-qualification years are never counted.
func describeGoogle(ctx context.Context, c Company, j Job) (string, error) {
	data, err := doRequest(ctx, http.MethodGet, j.URL, nil)
	if err != nil {
		return "", err
	}
	page := string(data)
	start := strings.Index(page, "Minimum qualifications")
	if start < 0 {
		return "", nil
	}
	section := page[start:]
	if end := strings.Index(section, "Preferred qualifications"); end > 0 {
		section = section[:end]
	} else if len(section) > 4000 {
		section = section[:4000]
	}
	return section, nil
}

// ---------- Atlassian ----------

func fetchAtlassian(ctx context.Context, c Company) ([]Job, error) {
	var resp []struct {
		ID             int64    `json:"id"`
		Title          string   `json:"title"`
		Locations      []string `json:"locations"`
		Qualifications string   `json:"qualifications"`
		PortalJobPost  struct {
			PortalURL   string `json:"portalUrl"`
			UpdatedDate string `json:"updatedDate"`
		} `json:"portalJobPost"`
	}
	if err := getJSON(ctx, "https://www.atlassian.com/endpoint/careers/listings", &resp); err != nil {
		return nil, err
	}
	jobs := make([]Job, 0, len(resp))
	for _, j := range resp {
		jobs = append(jobs, Job{
			ID:          jobID(c, strconv.FormatInt(j.ID, 10)),
			Company:     c.Name,
			Title:       j.Title,
			Location:    strings.Join(j.Locations, "; "),
			URL:         j.PortalJobPost.PortalURL,
			Posted:      shortDate(j.PortalJobPost.UpdatedDate),
			Description: j.Qualifications,
		})
	}
	return jobs, nil
}

// ---------- SmartRecruiters ----------

// The listing omits the description, so it is fetched per job by the describer.
func fetchSmartRecruiters(ctx context.Context, c Company) ([]Job, error) {
	const pageSize = 100
	var jobs []Job
	for page := range pagesOr(c, 3) {
		q := url.Values{}
		q.Set("limit", strconv.Itoa(pageSize))
		q.Set("offset", strconv.Itoa(page*pageSize))
		if c.Query != "" {
			q.Set("q", c.Query)
		}
		if c.Location != "" {
			q.Set("country", c.Location)
		}
		var resp struct {
			TotalFound int `json:"totalFound"`
			Content    []struct {
				ID           string `json:"id"`
				Name         string `json:"name"`
				ReleasedDate string `json:"releasedDate"`
				Location     struct {
					FullLocation string `json:"fullLocation"`
				} `json:"location"`
			} `json:"content"`
		}
		if err := getJSON(ctx, "https://api.smartrecruiters.com/v1/companies/"+url.PathEscape(c.Slug)+"/postings?"+q.Encode(), &resp); err != nil {
			return nil, err
		}
		for _, j := range resp.Content {
			jobs = append(jobs, Job{
				ID:       jobID(c, j.ID),
				Company:  c.Name,
				Title:    j.Name,
				Location: j.Location.FullLocation,
				URL:      "https://jobs.smartrecruiters.com/" + url.PathEscape(c.Slug) + "/" + j.ID,
				Posted:   shortDate(j.ReleasedDate),
				RawID:    j.ID,
			})
		}
		if (page+1)*pageSize >= resp.TotalFound {
			break
		}
	}
	return jobs, nil
}

func describeSmartRecruiters(ctx context.Context, c Company, j Job) (string, error) {
	var resp struct {
		JobAd struct {
			Sections struct {
				Qualifications struct {
					Text string `json:"text"`
				} `json:"qualifications"`
			} `json:"sections"`
		} `json:"jobAd"`
	}
	err := getJSON(ctx, "https://api.smartrecruiters.com/v1/companies/"+url.PathEscape(c.Slug)+"/postings/"+j.RawID, &resp)
	return resp.JobAd.Sections.Qualifications.Text, err
}

// ---------- helpers ----------

func shortDate(s string) string {
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
