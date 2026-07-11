package ddd

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- a tiny test domain --------------------------------------------------

type counterID = string

type counterCreated struct {
	Owner string `json:"owner"`
}

func (counterCreated) EventKind() string { return "counter.created" }

type counterIncremented struct {
	By int `json:"by"`
}

func (counterIncremented) EventKind() string { return "counter.incremented" }

type counter struct {
	BaseAggregateRoot[counterID]
	owner string
	count int
}

func newCounter(id counterID, clock Clock) *counter {
	c := &counter{}
	c.Init(id, "Counter", clock)
	return c
}

func (c *counter) Apply(env EventEnvelope[counterID]) error {
	switch p := env.Payload.(type) {
	case counterCreated:
		c.owner = p.Owner
	case counterIncremented:
		c.count += p.By
	default:
		return fmt.Errorf("%w: %T", ErrUnknownEvent, env.Payload)
	}
	return nil
}

func (c *counter) Create(owner string) error {
	return Raise[counterID, *counter](c, &c.BaseAggregateRoot, counterCreated{Owner: owner}, c.Apply)
}

func (c *counter) Increment(by int) error {
	return Raise[counterID, *counter](c, &c.BaseAggregateRoot, counterIncremented{By: by}, c.Apply)
}

// -------------------------------------------------------------------------

func TestRaiseAppendsAndIncrementsVersion(t *testing.T) {
	clock := FixedClock{T: time.Unix(1700000000, 0).UTC()}
	c := newCounter("c-1", clock)

	require.NoError(t, c.Create("alice"))
	require.NoError(t, c.Increment(3))
	require.NoError(t, c.Increment(2))

	assert.Equal(t, "alice", c.owner)
	assert.Equal(t, 5, c.count)
	assert.Equal(t, 3, c.Version())

	uncommitted := c.Uncommitted()
	require.Len(t, uncommitted, 3)
	assert.Equal(t, 1, uncommitted[0].AggregateVersion)
	assert.Equal(t, 2, uncommitted[1].AggregateVersion)
	assert.Equal(t, 3, uncommitted[2].AggregateVersion)
	assert.Equal(t, "Counter", uncommitted[0].AggregateType)
	assert.Equal(t, "counter.created", uncommitted[0].EventType)
	assert.Equal(t, clock.T, uncommitted[0].OccurredAt)
}

func TestMarkCommittedClearsUncommitted(t *testing.T) {
	c := newCounter("c-2", nil)
	require.NoError(t, c.Create("bob"))
	require.NotEmpty(t, c.Uncommitted())

	c.MarkCommitted()
	assert.Empty(t, c.Uncommitted())
	// Version persists across MarkCommitted.
	assert.Equal(t, 1, c.Version())
}

func TestLoadFromHistory(t *testing.T) {
	src := newCounter("c-3", nil)
	require.NoError(t, src.Create("carol"))
	require.NoError(t, src.Increment(10))
	require.NoError(t, src.Increment(5))

	history := src.Uncommitted()

	dst := newCounter("c-3", nil)
	require.NoError(t, LoadFromHistory[counterID, *counter](dst, &dst.BaseAggregateRoot, history))

	assert.Equal(t, "carol", dst.owner)
	assert.Equal(t, 15, dst.count)
	assert.Equal(t, 3, dst.Version())
	assert.Empty(t, dst.Uncommitted(), "replay must not produce uncommitted events")
}

func TestEnvelopeOptions(t *testing.T) {
	env := NewEnvelope[string](
		SystemClock{},
		"Counter",
		"c-4",
		1,
		counterCreated{Owner: "dave"},
		WithCorrelation[string]("corr-1", "cause-1"),
		WithTenant[string]("acme"),
		WithMetadata[string](map[string]string{"source": "test"}),
	)

	assert.Equal(t, "corr-1", env.CorrelationID)
	assert.Equal(t, "cause-1", env.CausationID)
	assert.Equal(t, "acme", env.TenantID)
	assert.Equal(t, "test", env.Metadata["source"])
	assert.NotEmpty(t, env.EventID)
}
