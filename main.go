package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/repository"
)

const colorBoldRed = "\033[1;31m"
const colorYellow = "\033[0;33m"
const termReset = "\033[0m"

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

func main() {
	repo, ref, useJSON, err := parseArgs()
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

	var events []Event
	err = client.Get(fmt.Sprintf("repos/%s/%s/events", repo.Owner, repo.Name), &events)
	if err != nil {
		panic(err)
	}

	if len(events) == 0 {
		panic("no events")
	}

	var event Event
	for _, candidateEvent := range events {
		if candidateEvent.Type == "PushEvent" && candidateEvent.Payload.Head == commit.SHA {
			event = candidateEvent
			break
		}
	}

	if event == (Event{}) {
		panic("couldn't find matching event")
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

	if useJSON {
		metadataJSON, err := json.MarshalIndent(metadata, "", "    ")
		if err != nil {
			panic(err)
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
