package syncengine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mortenzwarenstein/eetmeter-sync/internal/eetmeter"
	"github.com/mortenzwarenstein/eetmeter-sync/internal/fingerprint"
	"github.com/mortenzwarenstein/eetmeter-sync/internal/store"
	"github.com/mortenzwarenstein/eetmeter-sync/internal/uid"
)

// ErrAlreadyRunning is returned when a sync is requested while one is in progress.
var ErrAlreadyRunning = errors.New("syncengine: a sync is already running")

// Client is the subset of the Eetmeter API the engine uses. *eetmeter.Client
// implements it; tests supply a fake.
type Client interface {
	Login(ctx context.Context) error
	ListRecipes(ctx context.Context) ([]eetmeter.Recipe, error)
	CreateRecipe(ctx context.Context, r eetmeter.Recipe) (eetmeter.Recipe, error)
	UpdateRecipe(ctx context.Context, id string, r eetmeter.Recipe) (eetmeter.Recipe, error)
}

// ClientFactory builds a Client for one account.
type ClientFactory func(AccountConfig) Client

// AccountConfig is one account's identity and credentials. Either Token+DeviceID
// or Password must be set (see package config). Email is a label only — it is
// never sent to the API in token mode.
type AccountConfig struct {
	Label    string
	Email    string
	Password string
	Token    string
	DeviceID string
}

// Engine runs the reconciliation. One instance is shared by the HTTP trigger and
// the scheduler; its mutex guarantees at most one run at a time.
type Engine struct {
	store      *store.Store
	newClient  ClientFactory
	a, b       AccountConfig
	log        *slog.Logger
	runTimeout time.Duration

	mu        sync.Mutex
	running   bool
	runID     string
	startedAt time.Time
}

// New creates an Engine. runTimeout <= 0 defaults to 10 minutes. Whether a run is
// a dry run (logs in and computes what it would do but writes nothing — not to
// Mijn Eetmeter and not to the recipe_link table) is decided per run by the
// dryRun argument to Trigger / RunSync.
func New(st *store.Store, a, b AccountConfig, factory ClientFactory, log *slog.Logger, runTimeout time.Duration) *Engine {
	if log == nil {
		log = slog.Default()
	}
	if runTimeout <= 0 {
		runTimeout = 10 * time.Minute
	}
	return &Engine{store: st, newClient: factory, a: a, b: b, log: log, runTimeout: runTimeout}
}

// Status reports whether a run is in progress and, if so, its id and start time.
func (e *Engine) Status() (running bool, runID string, startedAt time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.running, e.runID, e.startedAt
}

func (e *Engine) begin() (runID string, startedAt time.Time, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.running {
		return e.runID, e.startedAt, ErrAlreadyRunning
	}
	e.running = true
	e.runID = uid.New()
	e.startedAt = time.Now().UTC()
	return e.runID, e.startedAt, nil
}

func (e *Engine) end() {
	e.mu.Lock()
	e.running = false
	e.mu.Unlock()
}

// Trigger starts a run in the background and returns its id immediately, or
// ErrAlreadyRunning (with the in-progress run's id and start time). When dryRun
// is true the run writes nothing.
func (e *Engine) Trigger(trigger string, dryRun bool) (runID string, startedAt time.Time, err error) {
	runID, startedAt, err = e.begin()
	if err != nil {
		return runID, startedAt, err
	}
	go func() {
		defer e.end()
		ctx, cancel := context.WithTimeout(context.Background(), e.runTimeout)
		defer cancel()
		if _, err := e.execute(ctx, trigger, runID, startedAt, dryRun); err != nil {
			e.log.Error("sync run failed", "runId", runID, "trigger", trigger, "err", err)
		}
	}()
	return runID, startedAt, nil
}

// RunSync executes a run to completion on the caller's goroutine. Used by tests
// and anywhere a blocking run is wanted. When dryRun is true the run writes
// nothing.
func (e *Engine) RunSync(ctx context.Context, trigger string, dryRun bool) (*Summary, error) {
	runID, startedAt, err := e.begin()
	if err != nil {
		return nil, err
	}
	defer e.end()
	return e.execute(ctx, trigger, runID, startedAt, dryRun)
}

func (e *Engine) execute(ctx context.Context, trigger, runID string, startedAt time.Time, dryRun bool) (*Summary, error) {
	sum := newSummary(runID, trigger, e.a.Label, e.b.Label, startedAt)
	sum.DryRun = dryRun

	if err := e.store.StartRun(ctx, runID, trigger, startedAt); err != nil {
		return nil, err
	}

	fail := func(err error) (*Summary, error) {
		at := time.Now().UTC()
		sum.finish("failed", e.scrub(err.Error()), at)
		if ferr := e.store.FinishRun(ctx, runID, "failed", sum.Error, mustJSON(sum), at); ferr != nil {
			e.log.Error("could not persist failed run", "runId", runID, "err", ferr)
		}
		return sum, err
	}

	clientA := e.newClient(e.a)
	clientB := e.newClient(e.b)

	if err := clientA.Login(ctx); err != nil {
		return fail(fmt.Errorf("account a (%s): %w", e.a.Label, loginErr(err)))
	}
	if err := clientB.Login(ctx); err != nil {
		return fail(fmt.Errorf("account b (%s): %w", e.b.Label, loginErr(err)))
	}

	aRecipes, err := clientA.ListRecipes(ctx)
	if err != nil {
		return fail(fmt.Errorf("account a (%s): list recipes: %w", e.a.Label, err))
	}
	bRecipes, err := clientB.ListRecipes(ctx)
	if err != nil {
		return fail(fmt.Errorf("account b (%s): list recipes: %w", e.b.Label, err))
	}

	aByName, aDup := indexByName(aRecipes)
	bByName, bDup := indexByName(bRecipes)

	links, err := e.store.ListLinks(ctx)
	if err != nil {
		return fail(err)
	}
	linkByName := make(map[string]*store.Link, len(links))
	for i := range links {
		linkByName[links[i].Name] = &links[i]
	}

	resolutions, err := e.store.ListResolutions(ctx)
	if err != nil {
		return fail(err)
	}

	for _, name := range unionNames(aByName, bByName, linkByName) {
		link := linkByName[name]
		pending := ""
		if link != nil {
			pending = resolutions[link.ID]
		}
		in := Input{
			Link:       link,
			A:          aByName[name],
			B:          bByName[name],
			Pending:    pending,
			AmbiguousA: aDup[name],
			AmbiguousB: bDup[name],
		}
		if err := e.apply(ctx, sum, name, in, Decide(in), clientA, clientB, dryRun); err != nil {
			return fail(fmt.Errorf("recipe %q: %w", name, err))
		}
		if err := e.store.SaveRunSummary(ctx, runID, mustJSON(sum)); err != nil {
			e.log.Warn("could not save progress", "runId", runID, "err", err)
		}
	}

	at := time.Now().UTC()
	sum.finish("succeeded", "", at)
	if err := e.store.FinishRun(ctx, runID, "succeeded", "", mustJSON(sum), at); err != nil {
		return sum, err
	}
	e.log.Info("sync run finished",
		"runId", runID, "trigger", trigger, "dryRun", dryRun,
		"createdA", sum.Counts.CreatedInA, "createdB", sum.Counts.CreatedInB,
		"updatedA", sum.Counts.UpdatedInA, "updatedB", sum.Counts.UpdatedInB,
		"conflicts", sum.Counts.FlaggedConflicts, "retired", sum.Counts.LinksRetired,
		"skipped", sum.Counts.Skipped)
	return sum, nil
}

// apply performs the store and API effects of one Decision and records a summary
// item. In dry-run mode it records the item it would have produced and returns
// without touching Mijn Eetmeter or the store.
func (e *Engine) apply(ctx context.Context, sum *Summary, name string, in Input, d Decision, ca, cb Client, dryRun bool) error {
	if dryRun {
		if action, detail := plannedAction(d); action != "" {
			sum.add(name, action, detail)
		}
		return nil
	}

	now := time.Now().UTC()

	switch d.Kind {
	case Noop:
		sum.add(name, ActionNoop, "")

	case Skip:
		sum.add(name, ActionSkipped, d.Reason)

	case CreateInA:
		created, err := ca.CreateRecipe(ctx, *in.B)
		if err != nil {
			return err
		}
		l := activeLink(linkFor(name, in.Link), now)
		l.ARecipeID, l.BRecipeID = created.ID, in.B.ID
		l.ABaselineHash = fingerprint.Of(created).Bytes()
		l.BBaselineHash = fingerprint.Of(*in.B).Bytes()
		if _, err := e.store.UpsertLink(ctx, l); err != nil {
			return err
		}
		sum.add(name, ActionCreatedInA, "")

	case CreateInB:
		created, err := cb.CreateRecipe(ctx, *in.A)
		if err != nil {
			return err
		}
		l := activeLink(linkFor(name, in.Link), now)
		l.ARecipeID, l.BRecipeID = in.A.ID, created.ID
		l.ABaselineHash = fingerprint.Of(*in.A).Bytes()
		l.BBaselineHash = fingerprint.Of(created).Bytes()
		if _, err := e.store.UpsertLink(ctx, l); err != nil {
			return err
		}
		sum.add(name, ActionCreatedInB, "")

	case UpdateInA:
		updated, err := ca.UpdateRecipe(ctx, in.Link.ARecipeID, *in.B)
		if err != nil {
			return err
		}
		l := activeLink(*in.Link, now)
		l.ABaselineHash = fingerprint.Of(updated).Bytes()
		l.BBaselineHash = fingerprint.Of(*in.B).Bytes()
		if _, err := e.store.UpsertLink(ctx, l); err != nil {
			return err
		}
		sum.add(name, ActionUpdatedInA, "")

	case UpdateInB:
		updated, err := cb.UpdateRecipe(ctx, in.Link.BRecipeID, *in.A)
		if err != nil {
			return err
		}
		l := activeLink(*in.Link, now)
		l.ABaselineHash = fingerprint.Of(*in.A).Bytes()
		l.BBaselineHash = fingerprint.Of(updated).Bytes()
		if _, err := e.store.UpsertLink(ctx, l); err != nil {
			return err
		}
		sum.add(name, ActionUpdatedInB, "")

	case LinkOnly:
		l := activeLink(linkFor(name, in.Link), now)
		l.ARecipeID, l.BRecipeID = in.A.ID, in.B.ID
		l.ABaselineHash = fingerprint.Of(*in.A).Bytes()
		l.BBaselineHash = fingerprint.Of(*in.B).Bytes()
		if _, err := e.store.UpsertLink(ctx, l); err != nil {
			return err
		}
		sum.add(name, ActionLinked, "")

	case FlagConflict, KeepConflict:
		l := linkFor(name, in.Link)
		l.ARecipeID, l.BRecipeID = recipeID(in.A), recipeID(in.B)
		l.Status = store.StatusConflict
		if l.ConflictDetectedAt == nil {
			l.ConflictDetectedAt = &now
		}
		l.Note = d.Reason
		l.AConflictSnapshot = snapshot(in.A)
		l.BConflictSnapshot = snapshot(in.B)
		if _, err := e.store.UpsertLink(ctx, l); err != nil {
			return err
		}
		sum.add(name, ActionFlaggedConflict, d.Reason)

	case ApplyResolution:
		l := *in.Link
		switch d.Winner {
		case "a":
			updated, err := cb.UpdateRecipe(ctx, l.BRecipeID, *in.A)
			if err != nil {
				return err
			}
			l.ABaselineHash = fingerprint.Of(*in.A).Bytes()
			l.BBaselineHash = fingerprint.Of(updated).Bytes()
		case "b":
			updated, err := ca.UpdateRecipe(ctx, l.ARecipeID, *in.B)
			if err != nil {
				return err
			}
			l.ABaselineHash = fingerprint.Of(updated).Bytes()
			l.BBaselineHash = fingerprint.Of(*in.B).Bytes()
		default:
			return fmt.Errorf("invalid resolution winner %q", d.Winner)
		}
		l.LastSyncedAt = &now
		if err := e.store.ApplyResolution(ctx, l); err != nil {
			return err
		}
		sum.add(name, ActionResolutionApplied, "winner="+d.Winner)

	case Retire:
		if err := e.store.SetLinkStatus(ctx, in.Link.ID, store.StatusRetired, d.Reason); err != nil {
			return err
		}
		sum.add(name, ActionLinkRetired, d.Reason)

	default:
		return fmt.Errorf("unhandled decision kind %d", d.Kind)
	}
	return nil
}

// plannedAction maps a Decision to the summary action/detail it would produce,
// for dry-run previews.
func plannedAction(d Decision) (action, detail string) {
	switch d.Kind {
	case CreateInA:
		return ActionCreatedInA, ""
	case CreateInB:
		return ActionCreatedInB, ""
	case UpdateInA:
		return ActionUpdatedInA, ""
	case UpdateInB:
		return ActionUpdatedInB, ""
	case LinkOnly:
		return ActionLinked, ""
	case FlagConflict, KeepConflict:
		return ActionFlaggedConflict, d.Reason
	case ApplyResolution:
		return ActionResolutionApplied, "winner=" + d.Winner
	case Retire:
		return ActionLinkRetired, d.Reason
	case Skip:
		return ActionSkipped, d.Reason
	default:
		return ActionNoop, ""
	}
}

func (e *Engine) scrub(msg string) string {
	for _, secret := range []string{e.a.Password, e.b.Password} {
		if secret != "" {
			msg = strings.ReplaceAll(msg, secret, "***")
		}
	}
	return msg
}

func linkFor(name string, l *store.Link) store.Link {
	if l != nil {
		return *l
	}
	return store.Link{Name: name}
}

// activeLink resets the conflict-related fields of a link to the "in agreement"
// state as of now, leaving IDs and baselines for the caller to set.
func activeLink(l store.Link, now time.Time) store.Link {
	l.Status = store.StatusActive
	l.LastSyncedAt = &now
	l.ConflictDetectedAt = nil
	l.Note = ""
	l.AConflictSnapshot = nil
	l.BConflictSnapshot = nil
	return l
}

func snapshot(r *eetmeter.Recipe) []byte {
	if r == nil {
		return nil
	}
	return mustJSON(r)
}

func recipeID(r *eetmeter.Recipe) string {
	if r == nil {
		return ""
	}
	return r.ID
}

func loginErr(err error) error {
	if errors.Is(err, eetmeter.ErrAuth) {
		return errors.New("authentication failed")
	}
	return err
}

func indexByName(rs []eetmeter.Recipe) (byName map[string]*eetmeter.Recipe, dup map[string]bool) {
	byName = make(map[string]*eetmeter.Recipe, len(rs))
	dup = map[string]bool{}
	for i := range rs {
		n := rs[i].Name
		if _, ok := byName[n]; ok {
			dup[n] = true
			continue
		}
		byName[n] = &rs[i]
	}
	return byName, dup
}

func unionNames(a, b map[string]*eetmeter.Recipe, links map[string]*store.Link) []string {
	set := make(map[string]struct{}, len(a)+len(b)+len(links))
	for n := range a {
		set[n] = struct{}{}
	}
	for n := range b {
		set[n] = struct{}{}
	}
	for n := range links {
		set[n] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic("syncengine: marshal summary: " + err.Error())
	}
	return b
}
