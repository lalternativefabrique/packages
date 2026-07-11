package pgx

import "github.com/lalternative/packages/go/eda/pkg/consumer"

// Compile-time proof that *Store implements the consumer's IdempotencyStore.
var _ consumer.IdempotencyStore = (*Store)(nil)
