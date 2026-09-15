package authz

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/open-policy-agent/opa/rego"
)

type opaPDP struct {
	query rego.PreparedEvalQuery
}

// OPA is a PDP that evaluates the .rego files found under fsys, embedded in
// the process. query names the document the policies produce, for instance
// "data.lalter.cortex"; that document must hold `allow` (bool) and may hold
// `reason` (string) and `context` (object), which become the Decision.
func OPA(fsys fs.FS, query string) (PDP, error) {
	opts := []func(*rego.Rego){rego.Query(query)}
	found := 0
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || path.Ext(p) != ".rego" || strings.HasSuffix(p, "_test.rego") {
			return nil
		}
		src, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		found++
		opts = append(opts, rego.Module(p, string(src)))
		return nil
	})
	if err != nil {
		return nil, err
	}
	if found == 0 {
		return nil, errors.New("authz: no .rego file found")
	}
	pq, err := rego.New(opts...).PrepareForEval(context.Background())
	if err != nil {
		return nil, fmt.Errorf("authz: policies do not compile: %w", err)
	}
	return &opaPDP{query: pq}, nil
}

func (p *opaPDP) Evaluate(ctx context.Context, req Request) (Decision, error) {
	input, err := inputDocument(req)
	if err != nil {
		return Decision{}, err
	}
	rs, err := p.query.Eval(ctx, rego.EvalInput(input))
	if err != nil {
		return Decision{}, fmt.Errorf("authz: evaluation failed: %w", err)
	}
	if len(rs) == 0 || len(rs[0].Expressions) == 0 {
		return Decision{Allow: false, Reason: ReasonDeniedByPolicy}, nil
	}
	doc, ok := rs[0].Expressions[0].Value.(map[string]any)
	if !ok {
		return Decision{}, errors.New("authz: the query must produce an object")
	}
	d := Decision{Allow: false, Reason: ReasonDeniedByPolicy}
	if allow, ok := doc["allow"].(bool); ok && allow {
		d.Allow, d.Reason = true, ""
	}
	if reason, ok := doc["reason"].(string); ok && reason != "" {
		d.Reason = reason
	}
	if extra, ok := doc["context"].(map[string]any); ok {
		d.Context = extra
	}
	return d, nil
}
