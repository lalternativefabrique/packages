package composition_test

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cucumber/godog"
	"github.com/google/uuid"

	"github.com/lalternative/packages/go/composition"
)

// world is one scenario's state. StoreFactory is what makes the same feature
// file run twice: against the in-memory store by default, and against a real
// JetStream server under -tags=integration.
type world struct {
	newStore func() composition.Store

	repo *composition.Repository
	id   composition.ID
	c    *composition.Composition
	err  error
	at   time.Time
}

func (w *world) reset(context.Context, *godog.Scenario) (context.Context, error) {
	w.repo = composition.NewRepository(w.newStore())
	w.id = uuid.NewString()
	w.c = nil
	w.err = nil
	w.at = time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	return context.Background(), nil
}

// tick keeps every event at a distinct instant, so a history reads in the
// order it happened rather than in whatever order equal timestamps sort. The
// step is a second by default, which keeps a scenario's writing inside one
// session unless it says otherwise.
func (w *world) tick() time.Time {
	w.at = w.at.Add(time.Second)
	return w.at
}

func (w *world) theAuthorRestores(text string) error {
	if err := w.c.RewriteSource(composition.Content{Body: text}, composition.OriginRestored, w.tick()); err != nil {
		return err
	}
	return w.repo.Save(context.Background(), w.c)
}

func (w *world) theAuthorRewrote(origin composition.Origin, text string) error {
	if err := w.c.RewriteSource(composition.Content{Body: text}, origin, w.tick()); err != nil {
		return err
	}
	return w.repo.Save(context.Background(), w.c)
}

func (w *world) theAuthorPausedFor(minutes int) error {
	w.at = w.at.Add(time.Duration(minutes) * time.Minute)
	return nil
}

func (w *world) anAuthorWriting() error {
	c, err := w.repo.Load(context.Background(), w.id)
	if err != nil {
		return err
	}
	w.c = c
	return nil
}

func (w *world) theAuthorWrote(text string) error {
	if err := w.c.UpdateSource(composition.Content{Body: text}, w.tick()); err != nil {
		return err
	}
	return w.repo.Save(context.Background(), w.c)
}

func (w *world) theAuthorSavesTheSameTextAgain() error {
	return w.theAuthorWrote(w.c.Source().Body)
}

func (w *world) theAuthorWroteWithRichContent(text, rich string) error {
	if err := w.c.UpdateSource(composition.Content{
		Body: text,
		Rich: json.RawMessage(rich),
	}, w.tick()); err != nil {
		return err
	}
	return w.repo.Save(context.Background(), w.c)
}

func (w *world) theAuthorSavesTheSameTextWithRichContent(rich string) error {
	return w.theAuthorWroteWithRichContent(w.c.Source().Body, rich)
}

// adaptedFor is the whole generation round trip: the request that turns a tab
// into "generating", then the result landing.
func (w *world) adaptedFor(kind, text string) error {
	now := w.tick()
	if err := w.c.RequestVariant(kind, now); err != nil {
		w.err = err
		return nil
	}
	if err := w.c.GenerateVariant(kind, composition.Content{Body: text}, now); err != nil {
		w.err = err
		return nil
	}
	return w.repo.Save(context.Background(), w.c)
}

func (w *world) theAuthorsTextIsStill(want string) error {
	if got := w.c.Source().Body; got != want {
		return fmt.Errorf("the author's text was overwritten: want %q, got %q", want, got)
	}
	return nil
}

func (w *world) theVariantReads(kind, want string) error {
	v, ok := w.c.Variant(kind)
	if !ok {
		return fmt.Errorf("no %s variant", kind)
	}
	if v.Content.Body != want {
		return fmt.Errorf("the %s variant reads %q, want %q", kind, v.Content.Body, want)
	}
	return nil
}

func (w *world) theCompositionHasSourceVersions(n int) error {
	versions, err := w.sourceVersions()
	if err != nil {
		return err
	}
	if len(versions) != n {
		return fmt.Errorf("want %d source versions, got %d", n, len(versions))
	}
	return nil
}

func (w *world) sourceVersionReads(nth int, want string) error {
	versions, err := w.sourceVersions()
	if err != nil {
		return err
	}
	if nth < 1 || nth > len(versions) {
		return fmt.Errorf("no source version %d among %d", nth, len(versions))
	}
	if got := versions[nth-1].Content.Body; got != want {
		return fmt.Errorf("source version %d reads %q, want %q", nth, got, want)
	}
	return nil
}

func (w *world) replayedAtFirstSourceVersion() error {
	versions, err := w.sourceVersions()
	if err != nil {
		return err
	}
	if len(versions) == 0 {
		return fmt.Errorf("no source version to replay")
	}
	history, err := w.repo.History(context.Background(), w.id)
	if err != nil {
		return err
	}
	c, err := composition.Replay(w.id, history, versions[0].AggregateVersion)
	if err != nil {
		return err
	}
	w.c = c
	return nil
}

func (w *world) theReplayedTextIs(want string) error {
	return w.theAuthorsTextIsStill(want)
}

func (w *world) theVariantHasVersions(kind string, n int) error {
	history, err := w.repo.History(context.Background(), w.id)
	if err != nil {
		return err
	}
	versions := composition.VariantVersions(history, kind)
	if len(versions) != n {
		return fmt.Errorf("want %d %s versions, got %d", n, kind, len(versions))
	}
	return nil
}

func (w *world) theVariantIsStale(kind string) error {
	v, ok := w.c.Variant(kind)
	if !ok {
		return fmt.Errorf("no %s variant", kind)
	}
	if !v.Stale(w.c.SourceVersion()) {
		return fmt.Errorf("the %s variant should be stale after the source changed", kind)
	}
	return nil
}

func (w *world) adaptingFailsWith(kind, reason string) error {
	now := w.tick()
	if err := w.c.RequestVariant(kind, now); err != nil {
		return err
	}
	if err := w.c.FailVariant(kind, reason, now); err != nil {
		return err
	}
	return w.repo.Save(context.Background(), w.c)
}

func (w *world) anAdaptationIsRequestedFor(kind string) error {
	if err := w.c.RequestVariant(kind, w.tick()); err != nil {
		return err
	}
	return w.repo.Save(context.Background(), w.c)
}

func (w *world) theAuthorCorrectsTheVariant(kind, text string) error {
	if err := w.c.EditVariant(kind, composition.Content{Body: text}, w.tick()); err != nil {
		return err
	}
	return w.repo.Save(context.Background(), w.c)
}

func (w *world) theVariantHasStatus(kind string, want composition.Status) error {
	v, ok := w.c.Variant(kind)
	if !ok {
		return fmt.Errorf("no %s variant", kind)
	}
	if v.Status != want {
		return fmt.Errorf("the %s variant is %q, want %q", kind, v.Status, want)
	}
	return nil
}

func (w *world) theVariantReasonIs(kind, want string) error {
	v, ok := w.c.Variant(kind)
	if !ok {
		return fmt.Errorf("no %s variant", kind)
	}
	if v.Reason != want {
		return fmt.Errorf("the %s variant reason is %q, want %q", kind, v.Reason, want)
	}
	return nil
}

func (w *world) theLastVersionIsAnAuthorEdit(kind string) error {
	history, err := w.repo.History(context.Background(), w.id)
	if err != nil {
		return err
	}
	versions := composition.VariantVersions(history, kind)
	if len(versions) == 0 {
		return fmt.Errorf("no %s history", kind)
	}
	if got := versions[len(versions)-1].EventKind; got != composition.KindVariantEdited {
		return fmt.Errorf("the last %s version is %q, want an author edit", kind, got)
	}
	return nil
}

func (w *world) theAdaptationIsRefused() error {
	if w.err == nil {
		return fmt.Errorf("the adaptation should have been refused")
	}
	return nil
}

// loadedAgain drops every bit of in-memory state, so what the assertions read
// next can only have come out of the event stream.
func (w *world) loadedAgain() error {
	c, err := w.repo.Load(context.Background(), w.id)
	if err != nil {
		return err
	}
	w.c = c
	return nil
}

func (w *world) sourceVersions() ([]composition.Version, error) {
	history, err := w.repo.History(context.Background(), w.id)
	if err != nil {
		return nil, err
	}
	return composition.SourceVersions(history), nil
}

// registerSteps binds the feature to a world backed by newStore.
func registerSteps(sc *godog.ScenarioContext, newStore func() composition.Store) {
	w := &world{newStore: newStore}
	sc.Before(w.reset)

	sc.Given(`^an author writing a composition$`, w.anAuthorWriting)
	sc.Step(`^the author wrote "([^"]*)"$`, w.theAuthorWrote)
	sc.Step(`^the author wrote "([^"]*)" with rich content '(.*)'$`, w.theAuthorWroteWithRichContent)
	sc.Step(`^the author saves the same text again$`, w.theAuthorSavesTheSameTextAgain)
	sc.Step(`^the author saves the same text with rich content '(.*)'$`, w.theAuthorSavesTheSameTextWithRichContent)
	sc.Step(`^the author paused for (\d+) minutes$`, w.theAuthorPausedFor)
	sc.Step(`^the author restores "([^"]*)"$`, w.theAuthorRestores)
	sc.Step(`^the author revised a passage into "([^"]*)"$`, func(text string) error {
		return w.theAuthorRewrote(composition.OriginRevised, text)
	})
	sc.Step(`^the author dictated "([^"]*)"$`, func(text string) error {
		return w.theAuthorRewrote(composition.OriginDictated, text)
	})
	sc.Step(`^the author illustrated "([^"]*)"$`, func(text string) error {
		return w.theAuthorRewrote(composition.OriginIllustrated, text)
	})
	sc.Step(`^the picture settled into "([^"]*)"$`, func(text string) error {
		return w.theAuthorRewrote(composition.OriginSettled, text)
	})
	sc.Step(`^the composition is adapted for "([^"]*)" as "([^"]*)"$`, w.adaptedFor)
	sc.Step(`^the composition is replayed at its first source version$`, w.replayedAtFirstSourceVersion)
	sc.Step(`^the composition is loaded again from the event store$`, w.loadedAgain)

	sc.Then(`^the author's text is still "([^"]*)"$`, w.theAuthorsTextIsStill)
	sc.Then(`^the "([^"]*)" variant reads "([^"]*)"$`, w.theVariantReads)
	sc.Then(`^the composition has (\d+) source versions$`, w.theCompositionHasSourceVersions)
	sc.Then(`^source version (\d+) reads "([^"]*)"$`, w.sourceVersionReads)
	sc.Then(`^the replayed text is "([^"]*)"$`, w.theReplayedTextIs)
	sc.Then(`^the "([^"]*)" variant has (\d+) versions$`, w.theVariantHasVersions)
	sc.Then(`^the "([^"]*)" variant is stale$`, w.theVariantIsStale)
	sc.Then(`^the adaptation is refused$`, w.theAdaptationIsRefused)

	sc.Step(`^adapting for "([^"]*)" fails with "([^"]*)"$`, w.adaptingFailsWith)
	sc.Step(`^an adaptation is requested for "([^"]*)"$`, w.anAdaptationIsRequestedFor)
	sc.Step(`^the author corrects the "([^"]*)" variant to "([^"]*)"$`, w.theAuthorCorrectsTheVariant)
	sc.Then(`^the "([^"]*)" variant is failed$`, func(kind string) error {
		return w.theVariantHasStatus(kind, composition.StatusFailed)
	})
	sc.Then(`^the "([^"]*)" variant is generating$`, func(kind string) error {
		return w.theVariantHasStatus(kind, composition.StatusGenerating)
	})
	sc.Then(`^the "([^"]*)" variant reason is "([^"]*)"$`, w.theVariantReasonIs)
	sc.Then(`^the last "([^"]*)" version is an author edit$`, w.theLastVersionIsAnAuthorEdit)
}
