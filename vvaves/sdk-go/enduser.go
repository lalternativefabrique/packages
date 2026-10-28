package sdk

import "context"

// EndUserHeader names the calling application's own user a request is made
// for, the header lalter takes for the same purpose.
const EndUserHeader = "X-End-User-Id"

type endUserKey struct{}

// ForEndUser makes every call on ctx, and every URL signed with it, bill the
// reading or transcription to user, the calling application's own id for the
// person it acts for. vvaves passes it to Lungor as the cost's end user.
func ForEndUser(ctx context.Context, user string) context.Context {
	return context.WithValue(ctx, endUserKey{}, user)
}

func endUserFrom(ctx context.Context) string {
	u, _ := ctx.Value(endUserKey{}).(string)
	return u
}
