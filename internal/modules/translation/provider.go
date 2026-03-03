package translation

import "context"

// Translator produces an English translation summary for the given text input.
type Translator interface {
	Translate(ctx context.Context, input string) (string, error)
}
