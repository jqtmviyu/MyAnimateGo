package jobs

import "context"

type jobIDContextKey struct{}

// WithJobID carries a persisted task ID through its callback stack for stable side-effect keys.
func WithJobID(ctx context.Context, id string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, jobIDContextKey{}, id)
}

// JobID returns the persisted task ID inherited from WithJobID.
func JobID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(jobIDContextKey{}).(string)
	return id
}
