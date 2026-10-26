package synthesis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/lalternative/packages/go/cortex/client"
	"github.com/lalternative/packages/go/cortex/skill"
)

const (
	contextWindow           = 131072
	maxOutputTokens         = 4096
	systemPromptTokenBudget = 2000
	maxReduceDepth          = 4
)

var ErrNoSegments = errors.New("synthesis: no segments")

// Engine writes a source's sheet through an agent that declares Skills. A
// cached sheet belongs to no one, so Call should name no person and use no
// memory.
type Engine struct {
	agent  client.Agent
	call   skill.Call
	budget int
}

func New(agent client.Agent, call skill.Call) *Engine {
	return &Engine{agent: agent, call: call, budget: contextWindow - maxOutputTokens - systemPromptTokenBudget}
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
	out, _, err := s.Run(ctx, e.agent, in, e.call)
	if err != nil {
		return out, classify(err)
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
