package exporter

import (
	"encoding/json"
	"fmt"
	"path"
	"strconv"
	"strings"
	"github.com/google/go-github/v56/github"
	"context"
	"golang.org/x/oauth2"
	log "github.com/sirupsen/logrus"
)

type GitHubManager struct {
	Client *github.Client
	Owner  string
	Repo   string
}

func (gm *GitHubManager) GetPRs(ctx context.Context) ([]*github.PullRequest, error) {
	var allPRs []*github.PullRequest
	opts := &github.PullRequestListOptions{State: "all"}

	for {
		prs, resp, err := gm.Client.PullRequests.List(ctx, gm.Owner, gm.Repo, opts)
		if err != nil {
			return nil, err
		}

		allPRs = append(allPRs, prs...)
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allPRs, nil
}

type GithubClientImpl struct {
	client *github.Client
}

type GithubClient interface {

}

func NewGitHubClient(token string) GithubClient {
	tokenSource := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tokenClient := oauth2.NewClient(context.Background(), tokenSource)
	return &GithubClientImpl{client: github.NewClient(tokenClient)}
}

func NewGitHubManager(ctx context.Context, owner string, repo string, token string) *GitHubManager {
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	return &GitHubManager{
		Client: client,
		Owner:  owner,
		Repo:   repo,
	}
}

// gatherData - Collects the data from the API and stores into struct
func (e *Exporter) gatherData() ([]*Datum,[]*github.PullRequest, error) {

	data := []*Datum{}
	prs := []*github.PullRequest{}
	responses, err := asyncHTTPGets(e.TargetURLs(), e.APIToken())

	if err != nil {
		return data, prs, err
	}

	for _, response := range responses {
		// Github can at times present an array, or an object for the same data set.
		// This code checks handles this variation.
		if isArray(response.body) {
			ds := []*Datum{}
			json.Unmarshal(response.body, &ds)
			data = append(data, ds...)
		} else {
			d := new(Datum)
			// Get releases
			if strings.Contains(response.url, "/repos/") {
				getReleases(e, response.url, &d.Releases)
			}
			// Get PRs
			if strings.Contains(response.url, "/repos/") {
				prs = getPRs(e, response.url)
			}
			json.Unmarshal(response.body, &d)
			data = append(data, d)
		}
		log.Infof("API data fetched for repository: %s", response.url)
	}

	//return data, rates, err
	return data, prs, nil

}

// getRates obtains the rate limit data for requests against the github API.
// Especially useful when operating without oauth and the subsequent lower cap.
func (e *Exporter) getRates() (*RateLimits, error) {
	u := *e.APIURL()
	u.Path = path.Join(u.Path, "rate_limit")

	resp, err := getHTTPResponse(u.String(), e.APIToken())
	if err != nil {
		return &RateLimits{}, err
	}
	defer resp.Body.Close()

	// Triggers if rate-limiting isn't enabled on private Github Enterprise installations
	if resp.StatusCode == 404 {
		return &RateLimits{}, fmt.Errorf("Rate Limiting not enabled in GitHub API")
	}

	limit, err := strconv.ParseFloat(resp.Header.Get("X-RateLimit-Limit"), 64)

	if err != nil {
		return &RateLimits{}, err
	}

	rem, err := strconv.ParseFloat(resp.Header.Get("X-RateLimit-Remaining"), 64)

	if err != nil {
		return &RateLimits{}, err
	}

	reset, err := strconv.ParseFloat(resp.Header.Get("X-RateLimit-Reset"), 64)

	if err != nil {
		return &RateLimits{}, err
	}

	return &RateLimits{
		Limit:     limit,
		Remaining: rem,
		Reset:     reset,
	}, err

}

func getReleases(e *Exporter, url string, data *[]Release) {
	i := strings.Index(url, "?")
	baseURL := url[:i]
	releasesURL := baseURL + "/releases"
	releasesResponse, err := asyncHTTPGets([]string{releasesURL}, e.APIToken())

	if err != nil {
		log.Errorf("Unable to obtain releases from API, Error: %s", err)
	}

	json.Unmarshal(releasesResponse[0].body, &data)
}

func getPRs(e *Exporter, url string) ([]*github.PullRequest) {
	ctx := context.Background()
	gm := NewGitHubManager(ctx, "sky-uk", "gtvd-azman2-aws", e.APIToken())
	prs, err := gm.GetPRs(ctx)

	if err != nil {
		log.Errorf("Unable to obtain pull requests from API, Error: %s", err)
	}
	return prs
}

// isArray simply looks for key details that determine if the JSON response is an array or not.
func isArray(body []byte) bool {

	isArray := false

	for _, c := range body {
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			continue
		}
		isArray = c == '['
		break
	}

	return isArray

}
