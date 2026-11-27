package cmd

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
)

// repoFlagUsage is the help of every --repo that names one Konvu repository.
const repoFlagUsage = "Repository: owner/name, a repository URL, its Konvu id, or a name only one repository has"

// repoRefHint is the suggestion for a --repo that names no repository.
const repoRefHint = "Pass the repository as owner/name, such as acme/api, a repository URL, or its Konvu id."

// shortRepositoryPattern is a repository's name without its owner, like api.
var shortRepositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// matchRepository resolves ref to the one Konvu repository it names: its Konvu id, its URL (like
// github:acme/api or https://github.com/acme/api), its owner/name in any case, or a name like api
// that only one repository has. A ref naming several, or none, is refused with the candidates.
func matchRepository(ref string, lookup func() (map[string][]konvuRepository, error)) (konvuRepository, error) {
	ref = strings.TrimSpace(ref)
	id := hexID(ref)
	name, _ := konvuRepositoryName(ref)
	if name == "" {
		name = strings.Trim(ref, "/")
	}
	if id == "" && !validRepositoryRef(name) {
		return konvuRepository{}, bookDataUsage(fmt.Sprintf("--repo %q is not a repository", clean(ref)), repoRefHint)
	}
	byName, err := lookup()
	if err != nil {
		var authErr *api.AuthenticationError
		if errors.As(err, &authErr) {
			return konvuRepository{}, clierrors.NewAuthError(clean(authErr.Message))
		}
		return konvuRepository{}, clierrors.NewAPIError(fmt.Sprintf("could not look up repository %s (%s)", clean(ref), lookupReason(err)))
	}
	if id != "" {
		for _, repos := range byName {
			for _, repo := range repos {
				if hexID(repo.id) == id {
					return repo, nil
				}
			}
		}
		return konvuRepository{}, repositoryNotFound("id "+strings.ToLower(ref), nil)
	}
	key := strings.ToLower(name)
	matches := byName[key]
	// A --repo that names where the repository lives matches only there.
	where := name
	if host := repositoryHost(ref); host != "" {
		matches = slices.DeleteFunc(slices.Clone(matches), func(match konvuRepository) bool { return match.host != host })
		where = host + "/" + name
	}
	// A bare name, like api, matches any owner's repository of that name.
	if len(matches) == 0 && !strings.Contains(key, "/") {
		for owned, repos := range byName {
			if path.Base(owned) == key {
				matches = append(matches, repos...)
			}
		}
		sortKonvuRepositories(matches)
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return konvuRepository{}, repositoryNotFound(clean(where), similarRepositories(key, byName))
	}
	found := make([]string, len(matches))
	for i, match := range matches {
		found[i] = match.label + " (id " + match.id + ")"
	}
	return konvuRepository{}, &clierrors.CLIError{
		Code:       "AMBIGUOUS_REPOSITORY",
		Message:    fmt.Sprintf("%d Konvu repositories are %s: %s", len(matches), clean(name), strings.Join(found, ", ")),
		Suggestion: "Pass the one you mean as owner/name, its URL or its id.",
		ExitCode:   clierrors.ExitUsageError,
	}
}

func validRepositoryRef(name string) bool {
	if len(name) > maxRepositoryChars || slices.ContainsFunc(strings.Split(name, "/"), isDotSegment) {
		return false
	}
	return repositoryNamePattern.MatchString(name) || shortRepositoryPattern.MatchString(name)
}

func repositoryNotFound(what string, similar []string) error {
	suggestion := "Run 'konvu coverage list' to see your repositories."
	if len(similar) > 0 {
		suggestion = "Did you mean " + strings.Join(similar, ", ") + "? Pass it as owner/name, its URL or its id."
	}
	return &clierrors.CLIError{
		Code:       "REPOSITORY_NOT_FOUND",
		Message:    "none of your Konvu repositories is " + what,
		Suggestion: suggestion,
		ExitCode:   clierrors.ExitNotFound,
	}
}

// similarRepositories lists up to five repositories whose name contains the one asked for, or
// whose name it contains, such as acme/juice-shop for juice or juice-shop-v2.
func similarRepositories(key string, byName map[string][]konvuRepository) []string {
	asked := path.Base(key)
	var similar []string
	for owned, repos := range byName {
		name := path.Base(owned)
		if strings.Contains(name, asked) || (len(name) >= 3 && strings.Contains(asked, name)) {
			for _, repo := range repos {
				similar = append(similar, repo.label)
			}
		}
	}
	slices.Sort(similar)
	if len(similar) > 5 {
		similar = similar[:5]
	}
	return similar
}

// findingRepository resolves a finding filter's --repo to the Konvu repository it names, so the
// filter is by its id and a --repo naming no repository is an error, never an empty list.
func findingRepository(client *api.Client, ref string) (konvuRepository, error) {
	return matchRepository(ref, func() (map[string][]konvuRepository, error) {
		return konvuRepositoriesByName(client, repositoryLookupTimeout)
	})
}

// findingRepositoryIDs resolves each --repo of a repeatable filter to its Konvu id, once each.
func findingRepositoryIDs(client *api.Client, refs []string) ([]string, error) {
	var byName map[string][]konvuRepository
	lookup := func() (map[string][]konvuRepository, error) {
		if byName != nil {
			return byName, nil
		}
		var err error
		byName, err = konvuRepositoriesByName(client, repositoryLookupTimeout)
		return byName, err
	}
	var ids []string
	for _, ref := range refs {
		repo, err := matchRepository(ref, lookup)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(ids, repo.id) {
			ids = append(ids, repo.id)
		}
	}
	return ids, nil
}
