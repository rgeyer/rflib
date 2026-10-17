// Package store defines how rflib keeps its data, independent of the
// database: a Store holds libraries, and a Library holds one library's
// channels, radios, and saved searches. Package sqlite implements
// it; package storetest is the contract every implementation must pass.
//
// Stores check what a database can check: that records are well formed,
// names are unique within a library, and references stay inside their
// library. Rules that need more than one record, such as what a saved
// search may refer to, belong to package app.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/ryan/rflib/internal/domain"
)

var (
	// ErrNotFound is returned (wrapped) when a lookup matches nothing in
	// the library.
	ErrNotFound = errors.New("not found")
	// ErrConflict is returned (wrapped) when a name is already taken in the
	// library.
	ErrConflict = errors.New("already exists")
)

type changeKey struct{}

type change struct{ reason, author string }

// WithChange attaches why a change is made, and by whom, to ctx; stores
// record it in shared channels' revisions.
func WithChange(ctx context.Context, reason, author string) context.Context {
	return context.WithValue(ctx, changeKey{}, change{reason, author})
}

// ChangeOf returns what WithChange attached to ctx. Without an author
// there, it's the one WithAuthor attached.
func ChangeOf(ctx context.Context) (reason, author string) {
	c, _ := ctx.Value(changeKey{}).(change)
	if c.author == "" {
		c.author, _ = ctx.Value(authorKey{}).(string)
	}
	return c.reason, c.author
}

type authorKey struct{}

// WithAuthor attaches who is making every change under ctx, such as the
// signed-in user of a request.
func WithAuthor(ctx context.Context, author string) context.Context {
	return context.WithValue(ctx, authorKey{}, author)
}

// Store is a database of libraries.
type Store interface {
	// Migrate creates or upgrades the schema.
	Migrate(ctx context.Context) error
	// Ready reports an error unless the schema is current.
	Ready(ctx context.Context) error
	Close() error

	CreateLibrary(ctx context.Context, lib domain.Library) (domain.Library, error)
	// GetLibrary looks a library up by ID or name; "" is the default
	// library.
	GetLibrary(ctx context.Context, idOrName string) (domain.Library, error)
	ListLibraries(ctx context.Context) ([]domain.Library, error)
	// UpdateLibrary saves a library's name and description. Setting
	// Default makes it the default library in place of the old one;
	// clearing it on the default library is refused, since there is always
	// one default.
	UpdateLibrary(ctx context.Context, lib domain.Library) (domain.Library, error)
	// DeleteLibrary deletes a library and everything in it. The default
	// library can't be deleted.
	DeleteLibrary(ctx context.Context, id string) error

	// Library returns the library with the given ID. It doesn't check
	// that the library exists; look it up with GetLibrary first.
	Library(id string) Library

	Accounts
}

// Accounts are the users of a server with accounts, their sign-in
// methods, sessions, and invitations (see domain.User).
type Accounts interface {
	// CreateUser stores a user, with passwordHash as their local identity,
	// in one transaction with their private library: a new one named after
	// them, or, with opts.Claim, an existing library no one owns (the first
	// admin takes the default library). With opts.Invite, it spends that
	// invite (by its code's hash), and fails with ErrNotFound unless the
	// invite is unused and unexpired.
	CreateUser(ctx context.Context, u domain.User, passwordHash string, opts NewUser) (domain.User, error)
	// GetUser looks a user up by ID or name, ignoring case.
	GetUser(ctx context.Context, idOrName string) (domain.User, error)
	ListUsers(ctx context.Context) ([]domain.User, error)
	// UpdateUser saves a user's display name, email, role, and whether
	// they're disabled.
	UpdateUser(ctx context.Context, u domain.User) (domain.User, error)
	// DeleteUser deletes a user, their sessions, and their library with
	// everything in it.
	DeleteUser(ctx context.Context, id string) error
	// PasswordHash returns a user's local password hash; SetPasswordHash
	// replaces it.
	PasswordHash(ctx context.Context, userID string) (string, error)
	SetPasswordHash(ctx context.Context, userID, hash string) error

	CreateSession(ctx context.Context, s domain.Session) error
	// Session returns an unexpired session by its token's hash.
	Session(ctx context.Context, tokenHash string) (domain.Session, error)
	// ExtendSession moves a session's expiry.
	ExtendSession(ctx context.Context, tokenHash string, expires time.Time) error
	DeleteSession(ctx context.Context, tokenHash string) error
	// DeleteSessions signs a user out everywhere, but for keep (a token
	// hash, or "").
	DeleteSessions(ctx context.Context, userID, keep string) error

	CreateInvite(ctx context.Context, inv domain.Invite) error
	ListInvites(ctx context.Context) ([]domain.Invite, error)
}

// NewUser is how CreateUser makes a user's library and checks their
// invitation.
type NewUser struct {
	Claim  string
	Invite string
}

// Library is one library's data. Every lookup is confined to the library:
// a record in another library is not found.
type Library interface {
	ID() string
	// InTx runs fn in one transaction, committing when it returns nil. Only
	// the Library passed to fn may be used inside it.
	InTx(ctx context.Context, fn func(Library) error) error

	// CreateChannel stores a channel. A supplied ID is kept, so restored
	// backups keep their identity, unless another record already has it;
	// then a new one is assigned.
	CreateChannel(ctx context.Context, c domain.Channel) (domain.Channel, error)
	GetChannel(ctx context.Context, id string) (domain.Channel, error)
	// ListChannels returns every channel in domain.CompareChannels order.
	// Links come with their shared channel's fields filled in (see
	// domain.Linked).
	ListChannels(ctx context.Context) ([]domain.Channel, error)
	// UpdateChannel saves a channel. A link saves only what's its own;
	// setting Link turns a channel into a link, and clearing it turns a
	// link into a private channel with the fields given.
	UpdateChannel(ctx context.Context, c domain.Channel) (domain.Channel, error)
	// DeleteChannel deletes a channel and takes it off every radio. A
	// shared channel that is linked can't be deleted (ErrConflict).
	DeleteChannel(ctx context.Context, id string) error
	// ChannelRevisions returns a shared channel's revisions, oldest
	// first; for a link, its shared channel's. Private channels have none.
	ChannelRevisions(ctx context.Context, id string) ([]domain.Revision, error)
	// SharedViews returns the library's views of shared channels, by ID:
	// each shared channel, under its own ID, with the library's annotation
	// (its tags, notes, and short names) laid over it. GetChannel returns
	// a view too, for a shared channel's ID, and UpdateChannel and
	// DeleteChannel save or delete the annotation. Radios hold shared
	// channels by their IDs.
	SharedViews(ctx context.Context, ids []string) (map[string]domain.Channel, error)
	// SaveAnnotation saves the library's annotation of c.Link.SharedID from
	// c's tags, notes, and short names; clearing them all removes it. It
	// returns the view.
	SaveAnnotation(ctx context.Context, c domain.Channel) (domain.Channel, error)
	// Follow makes the library follow a shared channel, seen at its current
	// revision, unless it already does; MarkSeen records a revision seen.
	// Annotating a shared channel or putting it on a radio follows it.
	Follow(ctx context.Context, sharedID string) error
	MarkSeen(ctx context.Context, sharedID string, revision int) error
	// ReplaceOnRadios puts newID in oldID's place on the library's radios,
	// keeping each entry's overrides; a radio holding both keeps newID's.
	ReplaceOnRadios(ctx context.Context, oldID, newID string) error
	// NotesRevision is the revision of the library's own notes (tags,
	// notes, and short names) on a shared channel: 0 for none ever, and a
	// new one each time they change, kept even when they're removed.
	NotesRevision(ctx context.Context, sharedID string) (int, error)
	// SeedChannels returns the library's channels that came from built-in
	// lists (see domain.SeedChannel); SetSeedChannel records or replaces
	// one. A record goes when its channel is deleted.
	SeedChannels(ctx context.Context) ([]domain.SeedChannel, error)
	SetSeedChannel(ctx context.Context, sc domain.SeedChannel) error
	// ChangedSince returns the library's channels with a revision recorded
	// after since, and the time of the latest one (since, when there are
	// none), to ask from next time. Only shared channels have revisions.
	ChangedSince(ctx context.Context, since time.Time) ([]domain.Channel, time.Time, error)

	// CreateRadio creates a radio, with an empty default section, "Other".
	CreateRadio(ctx context.Context, r domain.Radio) (domain.Radio, error)
	// GetRadio looks a radio up by ID or name.
	GetRadio(ctx context.Context, idOrName string) (domain.Radio, error)
	ListRadios(ctx context.Context) ([]domain.Radio, error)
	UpdateRadio(ctx context.Context, r domain.Radio) (domain.Radio, error)
	// DeleteRadio deletes a radio, and its draft; its channels stay in
	// the library.
	DeleteRadio(ctx context.Context, id string) error
	// RadioDraft returns a radio's draft, or ErrNotFound.
	RadioDraft(ctx context.Context, radioID string) (domain.Radio, error)
	// StartRadioDraft makes a radio's draft: a radio with its name and
	// settings, and a copy of its sections, sources, and channels as they
	// are (with new section and source IDs). GetRadio finds a draft only by
	// its ID, and ListRadios leaves drafts out; UpdateRadio changes a
	// radio's draft's settings too. ErrConflict when it has one already.
	StartRadioDraft(ctx context.Context, radioID string) (domain.Radio, error)
	// SaveRadioDraft makes a radio's list its draft's, and removes the
	// draft, in one transaction.
	SaveRadioDraft(ctx context.Context, radioID string) error
	// DiscardRadioDraft removes a radio's draft.
	DiscardRadioDraft(ctx context.Context, radioID string) error

	// RadioSnapshot is a radio as it is now (see domain.RadioSnapshot).
	RadioSnapshot(ctx context.Context, radioID string) (domain.RadioSnapshot, error)
	// SaveRadioRevision saves a radio revision, numbered after the last,
	// with the reason and author in ctx (see WithChange).
	SaveRadioRevision(ctx context.Context, radioID string, snap domain.RadioSnapshot, exported bool) (domain.RadioRevision, error)
	// RadioRevisions returns a radio's revisions, oldest first.
	RadioRevisions(ctx context.Context, radioID string) ([]domain.RadioRevision, error)
	// MarkRadioRevisionExported marks a revision as one an export saw.
	MarkRadioRevisionExported(ctx context.Context, radioID string, revision int) error
	// ChannelAt returns a channel as it was at a revision, from its
	// history; NotesAt, the library's notes on a shared channel at one of
	// their revisions (none at 0).
	ChannelAt(ctx context.Context, id string, revision int) (domain.Channel, error)
	NotesAt(ctx context.Context, sharedID string, revision int) (tags []string, notes string, shortNames []string, err error)
	// ReplaceRadioList replaces a radio's sections, sources, and channels
	// with a snapshot's (its settings stay); each section and source
	// remembers the one it restores, as a draft's do (see SaveRadioDraft).
	ReplaceRadioList(ctx context.Context, radioID string, snap domain.RadioSnapshot) error

	// RadioSections returns a radio's sections in order.
	RadioSections(ctx context.Context, radioID string) ([]domain.RadioSection, error)
	// CreateRadioSection adds a section after the others, or before the
	// default section when that is last. A new default section takes over
	// from the old one.
	CreateRadioSection(ctx context.Context, sec domain.RadioSection) (domain.RadioSection, error)
	// UpdateRadioSection changes a section's name, start, size, order, and
	// whether it's the default; another section must be made the default
	// for this one to stop being it.
	UpdateRadioSection(ctx context.Context, sec domain.RadioSection) error
	// MoveRadioSection moves a section to index to (from 0).
	MoveRadioSection(ctx context.Context, radioID, sectionID string, to int) error
	// DeleteRadioSection deletes a section; its channels go to the end of
	// another (intoID, or the default section for ""), in order, and its
	// sources feed that one: merging it. The default section can only be
	// merged into another, which then becomes the default.
	DeleteRadioSection(ctx context.Context, radioID, sectionID, intoID string) error

	// RadioEntries returns a radio's channels in list order: sections in
	// order, each section's channels in the order placed, or sorted as the
	// section sorts them. Each entry carries its section and the radio's
	// overrides.
	RadioEntries(ctx context.Context, radioID string) ([]domain.RadioEntry, error)
	// AddRadioChannels appends channels to the end of a section (the
	// default section for "") in the order given, skipping any already on
	// the radio, and returns how many were added.
	AddRadioChannels(ctx context.Context, radioID, sectionID string, channelIDs []string) (int, error)
	// AddSourceChannels adds channels for a radio source to the end of the
	// source's section, skipping any already on the radio.
	AddSourceChannels(ctx context.Context, src domain.RadioSource, channelIDs []string) (int, error)
	// RemoveRadioChannels takes channels off a radio and returns how many
	// were removed.
	RemoveRadioChannels(ctx context.Context, radioID string, channelIDs []string) (int, error)
	// MoveRadioChannel moves a channel to index to (from 0) within a
	// section (its own for ""); an index past the end moves it last. It
	// stays its source's.
	MoveRadioChannel(ctx context.Context, radioID, channelID, sectionID string, to int) error
	// ReorderRadioChannels puts a radio's channels in the order given,
	// which must list each of them once; each stays in its section, in the
	// order given there.
	ReorderRadioChannels(ctx context.Context, radioID string, channelIDs []string) error
	// ReleaseRadioChannels makes channels a source added count as added by
	// hand, so no sync takes them off; it returns how many changed.
	ReleaseRadioChannels(ctx context.Context, radioID string, channelIDs []string) (int, error)
	// SetRadioEntry stores a radio's overrides for one of its channels: the
	// entry's Name, Power, Skip, and PlanNumber.
	SetRadioEntry(ctx context.Context, radioID string, e domain.RadioEntry) error
	// RadioChannelIDs returns the channels on a radio, or on any radio when
	// radioID is empty.
	RadioChannelIDs(ctx context.Context, radioID string) (map[string]bool, error)

	// RadioSources returns a radio's sources in order.
	RadioSources(ctx context.Context, radioID string) ([]domain.RadioSource, error)
	// AllRadioSources returns every radio's sources.
	AllRadioSources(ctx context.Context) ([]domain.RadioSource, error)
	// AddRadioSource appends a source to its radio's sources. Without a
	// section, it gets a new one of its own, named after it.
	AddRadioSource(ctx context.Context, src domain.RadioSource) (domain.RadioSource, error)
	// UpdateRadioSource changes a source's query, sort, plan numbering, and
	// section; channels already on the radio stay where they are.
	UpdateRadioSource(ctx context.Context, src domain.RadioSource) error
	// MoveRadioSource moves a source to index to (from 0). Channels already
	// on the radio stay where they are.
	MoveRadioSource(ctx context.Context, src domain.RadioSource, to int) error
	// RemoveRadioSource deletes a source. Its channels stay on the radio as
	// if added by hand, unless withChannels; it returns how many channels
	// were taken off. Its section stays.
	RemoveRadioSource(ctx context.Context, src domain.RadioSource, withChannels bool) (int, error)

	// Organizations, systems, and plans. Each change to one records a
	// revision (see RecordRevisions) with the reason in ctx. They refer only
	// to the library's own channels and organizations, and not to links;
	// package app keeps them to the shared library.

	CreateOrg(ctx context.Context, o domain.Organization) (domain.Organization, error)
	// GetOrg looks an organization up by ID or name, ignoring case.
	GetOrg(ctx context.Context, idOrName string) (domain.Organization, error)
	ListOrgs(ctx context.Context) ([]domain.Organization, error)
	UpdateOrg(ctx context.Context, o domain.Organization) (domain.Organization, error)
	// DeleteOrg deletes an organization attached to nothing
	// (ErrConflict otherwise).
	DeleteOrg(ctx context.Context, id string) error

	// CreateSystem stores a system with its networks, organizations, and
	// members.
	CreateSystem(ctx context.Context, s domain.System) (domain.System, error)
	// GetSystem looks a system up by ID or name, ignoring case. Members
	// come in their channels' name order.
	GetSystem(ctx context.Context, idOrName string) (domain.System, error)
	ListSystems(ctx context.Context) ([]domain.System, error)
	// UpdateSystem saves a system, replacing its lists.
	UpdateSystem(ctx context.Context, s domain.System) (domain.System, error)
	DeleteSystem(ctx context.Context, id string) error
	// SystemChannelIDs returns the channels in a system.
	SystemChannelIDs(ctx context.Context, systemID string) (map[string]bool, error)

	// CreatePlan stores a plan with its entries.
	CreatePlan(ctx context.Context, p domain.Plan) (domain.Plan, error)
	// GetPlan looks a plan up by ID or name, ignoring case.
	GetPlan(ctx context.Context, idOrName string) (domain.Plan, error)
	ListPlans(ctx context.Context) ([]domain.Plan, error)
	// UpdatePlan saves a plan, replacing its entries. Plans are archived,
	// never deleted.
	UpdatePlan(ctx context.Context, p domain.Plan) (domain.Plan, error)
	// PlanByID returns a plan by ID from any library: radio sources use
	// the shared library's plans.
	PlanByID(ctx context.Context, id string) (domain.Plan, error)

	// RecordRevisions returns an organization's, system's, or plan's
	// revisions, oldest first; kind is "organization", "system", or
	// "plan".
	RecordRevisions(ctx context.Context, kind, id string) ([]domain.RecordRevision, error)

	CreateSearch(ctx context.Context, s domain.SavedSearch) (domain.SavedSearch, error)
	// GetSearch looks a saved search up by ID or name; a leading @ is
	// ignored.
	GetSearch(ctx context.Context, idOrName string) (domain.SavedSearch, error)
	ListSearches(ctx context.Context) ([]domain.SavedSearch, error)
	UpdateSearch(ctx context.Context, s domain.SavedSearch) (domain.SavedSearch, error)
	DeleteSearch(ctx context.Context, id string) error
}
