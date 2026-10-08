package multiuser

import "context"

type credentialContextKey struct{}

type CredentialContext struct {
	Actor   User
	Channel Channel
}

func WithCredentialContext(ctx context.Context, value CredentialContext) context.Context {
	return context.WithValue(ctx, credentialContextKey{}, value)
}

func CredentialContextFrom(ctx context.Context) (CredentialContext, bool) {
	if ctx == nil {
		return CredentialContext{}, false
	}
	value, ok := ctx.Value(credentialContextKey{}).(CredentialContext)
	return value, ok
}
