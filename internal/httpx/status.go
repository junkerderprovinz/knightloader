package httpx

// StatusError is a server's answer other than the one asked for, such as
// "404 Not Found". It reads as the status alone, so a message built around it
// keeps its words, and a caller can still tell a refusal from a broken
// connection.
type StatusError struct{ Status string }

func (e *StatusError) Error() string { return e.Status }
