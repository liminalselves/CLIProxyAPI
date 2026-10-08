package usage

import "context"

type attributionKey struct{}

func WithAttribution(ctx context.Context, value Attribution) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, attributionKey{}, value)
}

func AttributionFromContext(ctx context.Context) Attribution {
	if ctx == nil {
		return Attribution{}
	}
	value, _ := ctx.Value(attributionKey{}).(Attribution)
	return value
}
