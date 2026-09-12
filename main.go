package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/repository"
)

const colorBoldRed = "\033[1;31m"
const colorYellow = "\033[0;33m"
const termReset = "\033[0m"

const perPage = 100

var linkRE = regexp.MustCompile(`<([^>]+)>;\s*rel="([^"]+)"`)

func findNextPage(response *http.Response) (string, bool) {
	for _, m := range linkRE.FindAllStringSubmatch(response.Header.Get("Link"), -1) {
		if len(m) > 2 && m[2] == "next" {
			return m[1], true
		}
	}
	return "", false
}

type Client struct {
	api.RESTClient
}

type CommitAuthor struct {
	Name  string    `json:"name"`
	Email string    `json:"email"`
	Date  time.Time `json:"date"`
}

type Actor struct {
	ID    int    `json:"id"`
	Login string `json:"login"`
}

type AuthorActor struct {
	CommitAuthor
	Actor
}

type Pusher struct {
	Actor
	Date time.Time `json:"date"`
}

type Verification struct {
	Verified   bool       `json:"verified"`
	Reason     string     `json:"reason"`
	VerifiedAt *time.Time `json:"verified_at"`
}

type Commit struct {
	SHA    string
	Commit struct {
		Author       CommitAuthor
		Committer    CommitAuthor
		Message      string
		Verification Verification
	}
	Author    Actor
	Committer Actor
	Parents   []struct {
		SHA string
	}
}

type Event struct {
	ID    string
	Type  string
	Actor Actor
	Repo  struct {
		ID   int
		Name string
	}
	Payload struct {
		Ref    string
		Head   string
		Before string
	}
	Public    bool
	CreatedAt time.Time `json:"created_at"`
}

type Activity struct {
	Ref       string
	Before    string
	After     string
	Timestamp time.Time
	Actor     Actor
}

type CommitMetadata struct {
	SHA          string       `json:"sha"`
	Ref          string       `json:"ref"`
	Parents      []string     `json:"parents"`
	Author       AuthorActor  `json:"author"`
	Committer    AuthorActor  `json:"committer"`
	Pusher       Pusher       `json:"pusher"`
	Message      string       `json:"message"`
	Verification Verification `json:"verification"`
}

func parseArgs() (repository.Repository, string, bool, error) {
	repoOwnerAndName := flag.String("repo", "", "Optional repository in owner/name format")
	useJSON := flag.Bool("json", false, "Print output in JSON format")
	flag.Parse()

	ref := "HEAD"
	arguments := flag.Args()
	if len(arguments) > 1 {
		return repository.Repository{}, "", false, fmt.Errorf("too many arguments, expected at most one")
	}
	if len(arguments) == 1 {
		ref = arguments[0]
	}

	if *repoOwnerAndName != "" {
		repo, err := repository.Parse(*repoOwnerAndName)
		if err != nil {
			return repository.Repository{}, "", false, fmt.Errorf("failed to parse repository \"%s\": %w", *repoOwnerAndName, err)
		}
		return repo, ref, *useJSON, nil
	}

	repo, err := repository.Current()
	if err != nil {
		return repository.Repository{}, "", false, fmt.Errorf("not a Git repository or couldn't find remote: %w", err)
	}
	return repo, ref, *useJSON, nil
}

func (c Client) commitMetadataFromEvents(repo repository.Repository, commit Commit) (*CommitMetadata, error) {
	event := Event{}
	for page := 1; event == (Event{}); page++ {
		var events []Event
		path := fmt.Sprintf("repos/%s/%s/events?per_page=%d&page=%d", repo.Owner, repo.Name, perPage, page)
		err := c.Get(path, &events)
		if err != nil {
			var httpErr *api.HTTPError
			if page > 1 && errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusUnprocessableEntity {
				break
			}
			return nil, fmt.Errorf("failed to get events for %s/%s: %w", repo.Owner, repo.Name, err)
		}

		if len(events) == 0 {
			if page == 1 {
				return nil, fmt.Errorf("repository %s/%s returned no events", repo.Owner, repo.Name)
			}
			break
		}

		for _, candidateEvent := range events {
			if candidateEvent.Type == "PushEvent" && candidateEvent.Payload.Head == commit.SHA {
				event = candidateEvent
				break
			}
		}

		if len(events) < perPage {
			break
		}
	}

	if event == (Event{}) {
		return nil, fmt.Errorf("failed to find event for commit %s", commit.SHA)
	}

	var parents []string
	for _, parent := range commit.Parents {
		parents = append(parents, parent.SHA)
	}

	metadata := CommitMetadata{
		SHA:     commit.SHA,
		Ref:     event.Payload.Ref,
		Parents: parents,
		Author: AuthorActor{
			commit.Commit.Author,
			commit.Author,
		},
		Committer: AuthorActor{
			commit.Commit.Committer,
			commit.Committer,
		},
		Pusher: Pusher{
			event.Actor,
			event.CreatedAt,
		},
		Message:      commit.Commit.Message,
		Verification: commit.Commit.Verification,
	}

	return &metadata, nil
}

func (c Client) commitMetadataFromActivity(repo repository.Repository, commit Commit) (*CommitMetadata, error) {
	requestPath := fmt.Sprintf("repos/%s/%s/activity?per_page=%d", repo.Owner, repo.Name, perPage)

	activity := Activity{}
	firstPage := true
	for {
		response, err := c.Request(http.MethodGet, requestPath, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to get activity for %s/%s: %w", repo.Owner, repo.Name, err)
		}

		var activities []Activity
		decoder := json.NewDecoder(response.Body)
		err = decoder.Decode(&activities)
		if closeErr := response.Body.Close(); closeErr != nil {
			return nil, closeErr
		}
		if err != nil {
			return nil, fmt.Errorf("failed to decode activity for %s/%s: %w", repo.Owner, repo.Name, err)
		}

		if firstPage && len(activities) == 0 {
			return nil, fmt.Errorf("repository %s/%s returned no activities", repo.Owner, repo.Name)
		}
		firstPage = false

		for _, candidateActivity := range activities {
			if candidateActivity.After == commit.SHA {
				activity = candidateActivity
				break
			}
		}
		if activity != (Activity{}) {
			break
		}

		var hasNextPage bool
		if requestPath, hasNextPage = findNextPage(response); !hasNextPage {
			break
		}
	}

	if activity == (Activity{}) {
		return nil, fmt.Errorf("failed to find activity for commit %s", commit.SHA)
	}

	var parents []string
	for _, parent := range commit.Parents {
		parents = append(parents, parent.SHA)
	}

	metadata := CommitMetadata{
		SHA:     commit.SHA,
		Ref:     activity.Ref,
		Parents: parents,
		Author: AuthorActor{
			commit.Commit.Author,
			commit.Author,
		},
		Committer: AuthorActor{
			commit.Commit.Committer,
			commit.Committer,
		},
		Pusher: Pusher{
			activity.Actor,
			activity.Timestamp,
		},
		Message:      commit.Commit.Message,
		Verification: commit.Commit.Verification,
	}

	return &metadata, nil
}

func main() {
	repo, ref, useJSON, err := parseArgs()
	if err != nil {
		log.Fatalf("Failed to parse arguments: %s", err)
	}

	_client, err := api.DefaultRESTClient()
	if err != nil {
		log.Fatalf("Failed to create REST client: %s", err)
	}
	client := Client{*_client}

	var commit Commit
	err = client.Get(fmt.Sprintf("repos/%s/%s/commits/%s", repo.Owner, repo.Name, ref), &commit)
	if err != nil {
		log.Fatalf("Failed to get commit %s from %s/%s: %s", ref, repo.Owner, repo.Name, err)
	}

	metadata, err := client.commitMetadataFromEvents(repo, commit)
	if err != nil {
		log.Printf("Failed to get commit metadata from event: %s", err)
		metadata, err = client.commitMetadataFromActivity(repo, commit)
		if err != nil {
			log.Fatalf("Failed to get commit metadata from activity: %s", err)
		}
	}

	if useJSON {
		metadataJSON, err := json.MarshalIndent(metadata, "", "    ")
		if err != nil {
			log.Fatalf("Failed to serialise data: %s", err)
		}
		fmt.Println(string(metadataJSON))
	} else {
		fmt.Printf("%scommit %s (%s)%s\n", colorYellow, metadata.SHA, colorBoldRed+metadata.Ref+colorYellow, termReset)

		fmt.Printf("Author:     %s <%s> (@%s)\n", metadata.Author.Name, metadata.Author.Email, metadata.Author.Login)
		fmt.Printf("AuthorDate: %s\n", metadata.Author.Date)

		fmt.Printf("Commit:     %s <%s> (@%s)\n", metadata.Committer.Name, metadata.Committer.Email, metadata.Committer.Login)
		fmt.Printf("CommitDate: %s\n", metadata.Committer.Date)

		fmt.Printf("Pusher:     %s (%d)\n", metadata.Pusher.Login, metadata.Pusher.ID)
		fmt.Printf("PusherDate: %s\n", metadata.Pusher.Date)

		fmt.Printf("Verified:   %t (%s)\n", metadata.Verification.Verified, metadata.Verification.Reason)

		fmt.Printf("\n\t%s\n", strings.ReplaceAll(metadata.Message, "\n", "\n\t"))
	}
}
