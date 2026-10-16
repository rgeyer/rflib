package app

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/query"
)

// CheckQuery parses and compiles text, so a query is known to work before
// it's saved. saving, when set, is the saved search being saved: it stands
// in for the stored copy, so a search can't end up using itself.
func (s *Service) CheckQuery(ctx context.Context, text string, saving *domain.SavedSearch) (query.Node, error) {
	n, err := query.Parse(text)
	if err != nil {
		return nil, err
	}
	env := s.QueryEnv(ctx)
	compile := n
	if saving != nil {
		stored := env.Search
		env.Search = func(name string) (string, error) {
			if name == saving.Name {
				return saving.Query, nil
			}
			return stored(name)
		}
		// Compiling @name reports a cycle starting from this search.
		compile = query.Ref{Name: saving.Name}
	}
	if _, err := query.Compile(compile, env); err != nil {
		return nil, err
	}
	return n, nil
}

func (s *Service) GetSearch(ctx context.Context, idOrName string) (domain.SavedSearch, error) {
	return s.lib.GetSearch(ctx, idOrName)
}

// CreateSearch saves a search after checking its query works and doesn't
// use itself.
func (s *Service) CreateSearch(ctx context.Context, x domain.SavedSearch) (domain.SavedSearch, error) {
	if err := s.writable(); err != nil {
		return x, err
	}
	x.Name = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(x.Name), "@"))
	if err := domain.ValidateSavedSearch(x); err != nil {
		return x, err
	}
	if _, err := s.CheckQuery(ctx, x.Query, &x); err != nil {
		return x, err
	}
	return s.lib.CreateSearch(ctx, x)
}

// UpdateSearch saves changes to a search. It can't be renamed while
// something uses it, since that would break the users.
func (s *Service) UpdateSearch(ctx context.Context, x domain.SavedSearch) (domain.SavedSearch, error) {
	old, err := s.lib.GetSearch(ctx, x.ID)
	if err != nil {
		return x, err
	}
	x.Name = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(x.Name), "@"))
	if x.Name != old.Name {
		if err := s.unused(ctx, old.Name); err != nil {
			return x, err
		}
	}
	if err := domain.ValidateSavedSearch(x); err != nil {
		return x, err
	}
	if _, err := s.CheckQuery(ctx, x.Query, &x); err != nil {
		return x, err
	}
	return s.lib.UpdateSearch(ctx, x)
}

// DeleteSearch deletes a search nothing uses.
func (s *Service) DeleteSearch(ctx context.Context, idOrName string) error {
	x, err := s.lib.GetSearch(ctx, idOrName)
	if err != nil {
		return err
	}
	if err := s.unused(ctx, x.Name); err != nil {
		return err
	}
	return s.lib.DeleteSearch(ctx, x.ID)
}

func (s *Service) unused(ctx context.Context, name string) error {
	users, err := s.SearchUsers(ctx, name)
	if err != nil {
		return err
	}
	if len(users) > 0 {
		return domain.Invalidf("@%s is used by %s; change those first", name, strings.Join(users, ", "))
	}
	return nil
}

// SearchUsers lists what uses a saved search: other searches ("@name") and
// radio sources ("radio NAME source N").
func (s *Service) SearchUsers(ctx context.Context, name string) ([]string, error) {
	var users []string
	uses := func(text string) bool {
		n, err := query.Parse(text)
		return err == nil && slices.Contains(query.Refs(n), name)
	}
	xs, err := s.lib.ListSearches(ctx)
	if err != nil {
		return nil, err
	}
	for _, x := range xs {
		if x.Name != name && uses(x.Query) {
			users = append(users, "@"+x.Name)
		}
	}
	names, err := s.radioIDNames(ctx)
	if err != nil {
		return nil, err
	}
	sources, err := s.lib.AllRadioSources(ctx)
	if err != nil {
		return nil, err
	}
	for _, src := range sources {
		// A draft's sources count too, under its radio's name, once.
		if u := fmt.Sprintf("radio %s source %d", names[src.RadioID], src.Position+1); uses(src.Query) && !slices.Contains(users, u) {
			users = append(users, u)
		}
	}
	return users, nil
}

// SearchSummary is a saved search with how many channels it matches now.
// Err is set, and Matches is -1, when it no longer works, such as when a
// radio it names is gone.
type SearchSummary struct {
	domain.SavedSearch
	Matches int
	Err     error
}

// ListSearches returns every saved search with its match count.
func (s *Service) ListSearches(ctx context.Context) ([]SearchSummary, error) {
	xs, err := s.lib.ListSearches(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]SearchSummary, len(xs))
	for i, x := range xs {
		out[i] = s.summarize(ctx, x)
	}
	return out, nil
}

// SearchDetail is a saved search, its match count, and what uses it.
type SearchDetail struct {
	SearchSummary
	Users []string
}

func (s *Service) SearchDetail(ctx context.Context, idOrName string) (SearchDetail, error) {
	x, err := s.lib.GetSearch(ctx, idOrName)
	if err != nil {
		return SearchDetail{}, err
	}
	users, err := s.SearchUsers(ctx, x.Name)
	return SearchDetail{SearchSummary: s.summarize(ctx, x), Users: users}, err
}

func (s *Service) summarize(ctx context.Context, x domain.SavedSearch) SearchSummary {
	cs, err := s.Find(ctx, x.Query)
	if err != nil {
		return SearchSummary{SavedSearch: x, Matches: -1, Err: err}
	}
	return SearchSummary{SavedSearch: x, Matches: len(cs)}
}
