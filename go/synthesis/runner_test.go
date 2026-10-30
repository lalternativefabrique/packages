package synthesis

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/lalternativefabrique/cortex/skill"
)

type fakeRunner struct {
	runs []Run
	err  error
}

func (f *fakeRunner) Run(_ context.Context, run Run) (Answer, error) {
	f.runs = append(f.runs, run)
	if f.err != nil {
		return Answer{}, f.err
	}
	if len(run.Skill.Output) == 0 {
		return Answer{Text: "claim [s1]"}, nil
	}
	return Answer{Data: json.RawMessage(sheetJSON)}, nil
}

func TestARunnerIsGivenEachSkillWithItsInputAndCall(t *testing.T) {
	r := &fakeRunner{}
	call := skill.Call{Subject: sharedUser, NoMemory: true}
	e := NewWithRunner(r, call)

	got, err := e.Synthesize(context.Background(), podcast, spacedSegments(2), "fr")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.runs) != 1 || r.runs[0].Skill.ID != "synthesize_source" || r.runs[0].Call.Subject != sharedUser || !r.runs[0].Call.NoMemory {
		t.Fatalf("runs = %+v", r.runs)
	}
	var in SynthesizeSourceInput
	if err := json.Unmarshal(r.runs[0].Input, &in); err != nil || in.Language != "fr" || len(in.Chunks) != 2 {
		t.Fatalf("input = %s", r.runs[0].Input)
	}
	if got.Title != "T" || got.Concepts[0].Citations[0].URL != "https://p.fm/e?id=1&t=70s" {
		t.Fatalf("got %+v", got)
	}
}

func TestARunnerAnswersATextSkillInText(t *testing.T) {
	r := &fakeRunner{}
	e := NewWithRunner(r, skill.Call{})
	e.budget = 100

	if _, err := e.Synthesize(context.Background(), podcast, spacedSegments(3), "en"); err != nil {
		t.Fatal(err)
	}
	var skills []string
	for _, run := range r.runs {
		skills = append(skills, run.Skill.ID)
	}
	if strings.Join(skills, ",") != "digest_source_part,digest_source_part,digest_source_part,merge_source_digests" {
		t.Fatalf("skills = %v", skills)
	}
	var merge MergeSourceDigestsInput
	_ = json.Unmarshal(r.runs[3].Input, &merge)
	if len(merge.Digests) != 3 || merge.Digests[0] != "claim [s1]" {
		t.Fatalf("the digests must reach the merge as the runner answered them: %+v", merge.Digests)
	}
}

func TestARunnerFailureIsReported(t *testing.T) {
	r := &fakeRunner{err: errors.New("model down")}
	_, err := NewWithRunner(r, skill.Call{}).Synthesize(context.Background(), podcast, spacedSegments(1), "fr")
	if err == nil || !strings.Contains(err.Error(), "model down") {
		t.Fatalf("err = %v", err)
	}
	var perm *PermanentError
	if errors.As(err, &perm) {
		t.Fatal("an outage must stay retryable")
	}
}
