package captcha

// Solvecha speaks Anti-Captcha's createTask protocol the way CapSolver does:
// the task id is a string, and ImageToTextTask may answer in createTask itself
// with status "ready". It reads pictures and text only, so there is no token
// task and no click task: a widget or click captcha is refused, and the next
// solver in the order gets it. getBalance is the prepaid balance only, which
// is used to check a key and never to hold back a solve.
//
//	https://solvecha.com/docs
//	https://solvecha.com/openapi.json

var solvecha = antiCaptchaAPI{
	name:      "solvecha",
	label:     "Solvecha",
	base:      "https://api.solvecha.com",
	tokenTask: func(string, tokenFields) (string, tokenFields, bool) { return "", tokenFields{}, false },
}

// NewSolvechaSolver builds a solver for one Solvecha API key, the one
// internal/accounts stores under catalogue id "solvecha".
func NewSolvechaSolver(apiKey string) *AntiCaptchaSolver {
	return newAntiCaptchaSolver(&solvecha, apiKey)
}
