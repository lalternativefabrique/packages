package synthesis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/lalternativefabrique/cortex/client"
	"github.com/lalternativefabrique/cortex/host"
	"github.com/lalternativefabrique/cortex/skill"
)

const (
	contextWindow           = 131072
	maxOutputTokens         = 4096
	systemPromptTokenBudget = 2000
	maxReduceDepth          = 4
)

var ErrNoSegments = errors.New("synthesis: no segments")

// Runner runs one of Skills for the engine. Input is the skill's input as
// JSON. A skill declaring an output schema answers Data, checked against that
// schema; a text skill answers Text.
type Runner interface {
	Run(ctx context.Context, run Run) (Answer, error)
}

type Run struct {
	Skill host.Skill
	Input json.RawMessage
	Call  skill.Call
}

type Answer struct {
	Text string
	Data json.RawMessage
}

// AgentRunner runs the skills through a cortex agent over A2A whose catalog
// declares Skills.
type AgentRunner struct {
	Agent client.Agent
}

func (r AgentRunner) Run(ctx context.Context, run Run) (Answer, error) {
	req := client.Request{
		Text:            string(run.Input),
		Context:         run.Call.Context,
		Subject:         run.Call.Subject,
		ConversationID:  run.Call.ConversationID,
		MCPHeaders:      run.Call.MCPHeaders,
		Skill:           run.Skill.ID,
		Model:           run.Call.Model,
		ReasoningEffort: run.Call.ReasoningEffort,
		NoMemory:        run.Call.NoMemory,
	}
	if len(run.Skill.Output) == 0 {
		text, _, err := client.Say(ctx, r.Agent, req)
		return Answer{Text: text}, err
	}
	// The agent answers a declared skill in the skill's own output schema;
	// a raw message asks for no other.
	data, _, err := client.Ask[json.RawMessage](ctx, r.Agent, req)
	return Answer{Data: data}, err
}

// Engine writes a source's sheet through a Runner of Skills. A cached sheet
// belongs to no one, so Call should name no person and use no memory.
type Engine struct {
	runner Runner
	call   skill.Call
	budget int
}

// New is NewWithRunner through the cortex agent at agent.
func New(agent client.Agent, call skill.Call) *Engine {
	return NewWithRunner(AgentRunner{Agent: agent}, call)
}

func NewWithRunner(runner Runner, call skill.Call) *Engine {
	return &Engine{runner: runner, call: call, budget: contextWindow - maxOutputTokens - systemPromptTokenBudget}
}

// Synthesize groups the segments, writes the sheet in one run or, for a source
// too long for one, digests its parts and merges them, then resolves its
// citations against the source.
func (e *Engine) Synthesize(ctx context.Context, source Source, segments []Segment, language string) (Content, error) {
	if len(segments) == 0 {
		return Content{}, ErrNoSegments
	}
	chunks := groupSegments(segments)

	var sheet SynthesisSheet
	var err error
	if estimateTokens(chunks) <= e.budget {
		sheet, err = run(ctx, e, SynthesizeSource, newSynthesizeSourceInput(chunks, source, language))
	} else {
		sheet, err = e.synthesizeLong(ctx, chunks, source, language)
	}
	if err != nil {
		return Content{}, err
	}
	if err := sheet.Validate(); err != nil {
		return Content{}, err
	}
	return sheet.content(chunks, source), nil
}

func (e *Engine) synthesizeLong(ctx context.Context, chunks []chunk, source Source, language string) (SynthesisSheet, error) {
	batches := partitionChunks(chunks, e.budget)
	lang := sheetLanguage(language)
	timecoded := isTimecoded(source)
	digests := make([]string, 0, len(batches))
	offset := 0
	for i, batch := range batches {
		digest, err := run(ctx, e, DigestSourcePart, DigestSourcePartInput{
			Language: lang,
			Chunks:   sourceChunks(batch, timecoded, offset),
		})
		if err != nil {
			return SynthesisSheet{}, fmt.Errorf("digest part %d/%d: %w", i+1, len(batches), err)
		}
		digests = append(digests, digest)
		offset += len(batch)
	}

	merge := MergeSourceDigestsInput{
		Language: lang,
		Targets:  newSheetTargets(computeSynthesisTargets(countWords(chunks))),
		Source:   SheetSource{Type: source.Type, Title: source.Title},
	}
	return e.mergeDigests(ctx, merge, digests, 0)
}

// mergeDigests folds digests too large for one call pairwise into partial
// sheets, then merges those, at most maxReduceDepth levels deep.
func (e *Engine) mergeDigests(ctx context.Context, merge MergeSourceDigestsInput, digests []string, depth int) (SynthesisSheet, error) {
	if len(digests) == 1 || estimateTextTokens(strings.Join(digests, "\n\n")) <= e.budget || depth >= maxReduceDepth {
		merge.Digests = digests
		return run(ctx, e, MergeSourceDigests, merge)
	}

	var folded []string
	group := make([]string, 0, 2)
	flush := func() error {
		if len(group) == 0 {
			return nil
		}
		merge.Digests = group
		sheet, err := run(ctx, e, MergeSourceDigests, merge)
		if err != nil {
			return err
		}
		partial, err := json.Marshal(sheet)
		if err != nil {
			return err
		}
		folded = append(folded, string(partial))
		group = make([]string, 0, 2)
		return nil
	}
	for _, d := range digests {
		group = append(group, d)
		if len(group) > 1 && estimateTextTokens(strings.Join(group, "\n\n")) >= e.budget {
			last := group[len(group)-1]
			group = group[:len(group)-1]
			if err := flush(); err != nil {
				return SynthesisSheet{}, err
			}
			group = append(group, last)
		}
	}
	if err := flush(); err != nil {
		return SynthesisSheet{}, err
	}
	return e.mergeDigests(ctx, merge, folded, depth+1)
}

func run[In, Out any](ctx context.Context, e *Engine, s skill.Skill[In, Out], in In) (Out, error) {
	var out Out
	declared := s.Declaration()
	input, err := json.Marshal(in)
	if err != nil {
		return out, fmt.Errorf("skill %q input: %w", declared.ID, err)
	}
	answer, err := e.runner.Run(ctx, Run{Skill: declared, Input: input, Call: e.call})
	if err != nil {
		return out, classify(err)
	}
	if text, ok := any(&out).(*string); ok {
		*text = answer.Text
		return out, nil
	}
	if err := json.Unmarshal(answer.Data, &out); err != nil {
		return out, fmt.Errorf("skill %q answer: %w", declared.ID, err)
	}
	return out, nil
}

// classify marks an input the agent refused against its schema as permanent:
// sending the same input again would be refused the same way.
func classify(err error) error {
	if strings.Contains(err.Error(), "does not match its schema") {
		return NewPermanentError("cortex rejected the input", err)
	}
	return fmt.Errorf("cortex: %w", err)
}
