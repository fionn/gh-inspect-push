# Inspect Push

GitHub CLI extension to fetch commit metadata from push events that's only available on the server side, not via Git.
This can include the pusher identity, push time, the GitHub identity of the author and committer and if GitHub considers the commit verified, amongst other things.

## Installation

### Normal

```shell
gh extension install fionn/gh-inspect-push
```

### Development

```shell
go build
gh extension install .
```

## Usage

```
gh inspect-push [--json] [--repo owner/name] [ref]
```

In a Git repository, invoking `gh inspect-push` with no arguments will inspect the latest commit on the default branch of the remote. To inspect the push of a specific commit or ref, pass it as a positional argument.

If not in a Git repository, or you want to inspect a different repository, pass `--repo owner/name` to inspect the push commit of repository `owner/name`.

The output mimics that of `git log` or `git show`, but for machine-readable output pass `--json` which will print the data in JSON format.

### Example

```console
% gh inspect-push --repo fionn/gh-inspect-push 47c9f40
commit 47c9f40f6e971eb7898232e55e5a753e90f8cf6c (refs/heads/master)
Author:     Fionn Fitzmaurice <fionn@github.com> (@fionn)
AuthorDate: 2026-05-24 11:53:05 +0000 UTC
Commit:     Fionn Fitzmaurice <fionn@github.com> (@fionn)
CommitDate: 2026-05-24 11:53:05 +0000 UTC
Pusher:     fionn (1897918)
PusherDate: 2026-05-24 11:55:30 +0000 UTC
Verified:   false (unsigned)

    Add documentation
```

## Caveats

### Push Events Only

We only look at events of type `PushEvent` as these are the only type of event that can be used to join on the commit hash. If a commit is pushed to a new branch, this creates a `CreateEvent`, which does not contain the commit hash. So if invoked with a commit that created a branch and wasn't pushed elsewhere, this will result in a failed lookup.

### Push Events Too Old

If querying a repository that has many other event types (e.g. `IssuesEvent`, `PullRequestEvent`, etc.), it might be that the `PushEvents` are too old. We get ~30 events returned to us so if there are 30 more recent events then we will not see it. This is because `gh-gh`'s `client.Get` doesn't support pagination. We could implement this ourselves by making the raw request, but have not done so. Even without this constraint, it might be that the most recent push was a long time ago and the API no longer returns this event.
