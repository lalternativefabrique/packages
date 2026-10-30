package synthesis

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/lalternativefabrique/cortex/client"
	"github.com/lalternativefabrique/cortex/skill"
)

type turn struct {
	skill   string
	endUser string
	input   string
	recall  any
}

const sheetJSON = `{"title":"T","summary":"S","whyItMatters":"W","impacts":"","tags":["x"],
	"concepts":[{"text":"c","chunkRefs":["s2"]}],"development":[{"axis":"a","content":"d","chunkRefs":["s1"]}]}`

const sharedUser = "synthiz:shared"

func fakeAgent(t *testing.T) (*Engine, *[]turn) {
	t.Helper()
	var mu sync.Mutex
	var turns []turn
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Params struct {
				Message struct {
					Metadata map[string]any `json:"metadata"`
					Parts    []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"message"`
			} `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		name, _ := body.Params.Message.Metadata["skill"].(string)
		mu.Lock()
		turns = append(turns, turn{skill: name, endUser: r.Header.Get("X-End-User-Id"), input: body.Params.Message.Parts[0].Text, recall: body.Params.Message.Metadata["recall"]})
		mu.Unlock()

		part := map[string]any{"kind": "data", "data": json.RawMessage(sheetJSON)}
		if name == "digest_source_part" {
			part = map[string]any{"kind": "text", "text": "claim [s1]"}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{
			"kind": "task", "status": map[string]any{"state": "completed"},
			"artifacts": []any{map[string]any{"name": "answer", "parts": []any{part}}},
		}})
	}))
	t.Cleanup(srv.Close)
	agent := client.Agent{URL: srv.URL, Token: "key", Header: http.Header{"X-End-User-Id": {sharedUser}}}
	return New(agent, skill.Call{Subject: sharedUser, NoMemory: true}), &turns
}

func spacedSegments(n int) []Segment {
	out := make([]Segment, n)
	for i := range out {
		start := float64(i * 70)
		out[i] = Segment{Text: strings.Repeat("mot ", 50), Start: start, End: start + 60}
	}
	return out
}

var podcast = Source{ID: "src", Type: "podcast", URL: "https://p.fm/e?id=1", Title: "Épisode"}

func TestAShortSourceIsOneSharedTurn(t *testing.T) {
	e, turns := fakeAgent(t)

	got, err := e.Synthesize(context.Background(), podcast, spacedSegments(2), "fr")
	if err != nil {
		t.Fatal(err)
	}
	if len(*turns) != 1 || (*turns)[0].skill != "synthesize_source" || (*turns)[0].endUser != sharedUser || (*turns)[0].recall != false {
		t.Fatalf("turns = %+v", *turns)
	}
	var in SynthesizeSourceInput
	if err := json.Unmarshal([]byte((*turns)[0].input), &in); err != nil || in.Language != "fr" || len(in.Chunks) != 2 {
		t.Fatalf("input = %s", (*turns)[0].input)
	}
	if got.Concepts[0].Citations[0].URL != "https://p.fm/e?id=1&t=70s" || got.Concepts[0].Citations[0].SourceID != "src" {
		t.Fatalf("citations = %+v", got.Concepts[0].Citations)
	}
}

func TestALongSourceIsDigestedInPartsThenMerged(t *testing.T) {
	e, turns := fakeAgent(t)
	e.budget = 100

	if _, err := e.Synthesize(context.Background(), podcast, spacedSegments(3), "en"); err != nil {
		t.Fatal(err)
	}
	var skills []string
	for _, tr := range *turns {
		skills = append(skills, tr.skill)
	}
	if strings.Join(skills, ",") != "digest_source_part,digest_source_part,digest_source_part,merge_source_digests" {
		t.Fatalf("skills = %v", skills)
	}
	var second DigestSourcePartInput
	_ = json.Unmarshal([]byte((*turns)[1].input), &second)
	if second.Chunks[0].Ref != "s2" {
		t.Fatalf("the second part must keep the source's refs: %+v", second.Chunks)
	}
}

func TestAPageWithoutTimecodesIsOneChunk(t *testing.T) {
	e, turns := fakeAgent(t)
	page := Source{ID: "p", Type: "webpage", URL: "https://example.org/a", Title: "A"}

	if _, err := e.Synthesize(context.Background(), page, []Segment{{Text: "un"}, {Text: "deux"}}, "fr"); err != nil {
		t.Fatal(err)
	}
	var in SynthesizeSourceInput
	_ = json.Unmarshal([]byte((*turns)[0].input), &in)
	if len(in.Chunks) != 1 || in.Chunks[0].Text != "un deux" || in.Chunks[0].StartSeconds != nil {
		t.Fatalf("chunks = %+v", in.Chunks)
	}
}

func TestNoSegmentsIsRefused(t *testing.T) {
	e, _ := fakeAgent(t)
	if _, err := e.Synthesize(context.Background(), podcast, nil, "fr"); !errors.Is(err, ErrNoSegments) {
		t.Fatalf("err = %v", err)
	}
}

func TestASchemaRefusalIsPermanent(t *testing.T) {
	var perm *PermanentError
	if !errors.As(classify(errors.New(`cortex: input does not match its schema`)), &perm) {
		t.Fatal("a schema refusal must not be retried")
	}
	if errors.As(classify(errors.New("cortex answered 502")), &perm) {
		t.Fatal("a transient failure must be retried")
	}
}
