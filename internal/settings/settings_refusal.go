package settings

// FieldError is a refusal about one place in the settings document, so the
// form can show it there rather than in a message about the whole save. Field
// is a top-level key such as "categories", or a dotted path below one, such as
// "reconnect.checkUrl" or "categories.2" for the third row of a list.
type FieldError struct {
	Field string
	Err   error
}

func (e *FieldError) Error() string { return e.Err.Error() }

func (e *FieldError) Unwrap() error { return e.Err }
