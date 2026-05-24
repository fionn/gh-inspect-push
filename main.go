package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/jq"
	"github.com/cli/go-gh/v2/pkg/repository"
)

type CommitAuthor struct {
	Name  string
	Email string
	Date  time.Time
}

type Actor struct {
	ID    int
	Login string
}

type Commit struct {
	SHA    string
	Commit struct {
		Author       CommitAuthor
		Committer    CommitAuthor
		Message      string
		Verification struct {
			Verified bool
			Reason   string
		}
	}
	Author    Actor
	Committer Actor
	Parents   []struct {
		SHA string
	}
}

type Event struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Actor Actor  `json:"actor"`
	Repo  struct {
		ID   int
		Name string
	} `json:"repo"`
	Payload struct {
		Ref    string
		Head   string
		Before string
	} `json:"payload"`
	Public    bool      `json:"public"`
	CreatedAt time.Time `json:"created_at"`
}

func parseArgs() (repository.Repository, string, error) {
	repoOwnerAndName := flag.String("repo", "", "Optional repository in owner/name format")
	flag.Parse()

	ref := "HEAD"
	arguments := flag.Args()
	if len(arguments) > 1 {
		return repository.Repository{}, "", fmt.Errorf("too many arguments, expected at most one")
	}
	if len(arguments) == 1 {
		ref = arguments[0]
	}

	if *repoOwnerAndName != "" {
		repo, err := repository.Parse(*repoOwnerAndName)
		if err != nil {
			return repository.Repository{}, "", fmt.Errorf("failed to parse repository \"%s\": %w", *repoOwnerAndName, err)
		}
		return repo, ref, nil
	}

	repo, err := repository.Current()
	if err != nil {
		return repository.Repository{}, "", fmt.Errorf("not a Git repository or couldn't find remote: %w", err)
	}
	return repo, ref, nil
}

func main() {
	repo, ref, err := parseArgs()
	if err != nil {
		panic(err)
	}

	client, err := api.DefaultRESTClient()
	if err != nil {
		panic(err)
	}

	var commit Commit
	err = client.Get(fmt.Sprintf("repos/%s/%s/commits/%s", repo.Owner, repo.Name, ref), &commit)
	if err != nil {
		panic(err)
	}

	resp, err := client.Request(http.MethodGet, fmt.Sprintf("repos/%s/%s/events", repo.Owner, repo.Name), nil)
	if err != nil {
		panic(err)
	}
	defer func() {
		err = resp.Body.Close()
		if err != nil {
			panic(err)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		panic(resp.StatusCode)
	}

	var out bytes.Buffer
	err = jq.Evaluate(resp.Body, &out, fmt.Sprintf(".[] | select(.type == \"PushEvent\" and (.payload.head == \"%s\"))", commit.SHA))
	if err != nil {
		panic(err)
	}

	var event Event
	err = json.NewDecoder(&out).Decode(&event)
	if err != nil {
		panic(err)
	}

	fmt.Printf("commit %s (%s)\n", commit.SHA, event.Payload.Ref)
	fmt.Printf("Author:     %s <%s> (@%s)\n", commit.Commit.Author.Name, commit.Commit.Author.Email, commit.Author.Login)
	fmt.Printf("AuthorDate: %s\n", commit.Commit.Author.Date)
	fmt.Printf("Commit:     %s <%s> (@%s)\n", commit.Commit.Committer.Name, commit.Commit.Committer.Email, commit.Committer.Login)
	fmt.Printf("CommitDate: %s\n", commit.Commit.Committer.Date)
	fmt.Printf("Pusher:     %s (%d)\n", event.Actor.Login, event.Actor.ID)
	fmt.Printf("PusherDate: %s\n", event.CreatedAt)
	fmt.Printf("Verified:   %t (%s)\n", commit.Commit.Verification.Verified, commit.Commit.Verification.Reason)

	fmt.Printf("\n\t%s\n", strings.ReplaceAll(commit.Commit.Message, "\n", "\n\t"))
}
