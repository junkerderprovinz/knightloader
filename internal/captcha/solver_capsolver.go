package captcha

// CapSolver speaks Anti-Captcha's createTask protocol with two twists the
// client in solver_anticaptcha.go handles: the task id is a string, and
// ImageToTextTask answers in createTask itself with status "ready". Its task
// list has neither hCaptcha nor a general coordinates task, and
// ImageToTextTask documents no comment.
//
//	https://docs.capsolver.com/en/guide/api-createtask/
//	https://docs.capsolver.com/en/guide/api-gettaskresult/
//	https://docs.capsolver.com/en/guide/api-getbalance/
//	https://docs.capsolver.com/en/guide/api-error/
//	https://docs.capsolver.com/en/guide/recognition/ImageToTextTask/
//	https://docs.capsolver.com/en/guide/captcha/ReCaptchaV2/
//	https://docs.capsolver.com/en/guide/captcha/ReCaptchaV3/
//	https://docs.capsolver.com/en/guide/captcha/cloudflare_turnstile/

var capSolver = antiCaptchaAPI{
	name:      "capsolver",
	label:     "CapSolver",
	base:      "https://api.capsolver.com",
	tokenTask: capSolverTokenTask,
}

// capSolverTokenTask names CapSolver's proxyless task for each token type.
// Its reCAPTCHA v3 tasks take no minScore, and Enterprise is a task of its
// own rather than a field.
func capSolverTokenTask(typ string, f tokenFields) (string, tokenFields, bool) {
	switch typ {
	case taskRecaptchaV2:
		return "ReCaptchaV2TaskProxyLess", f, true
	case taskRecaptchaV2Enterprise:
		return "ReCaptchaV2EnterpriseTaskProxyLess", f, true
	case taskRecaptchaV3:
		name := "ReCaptchaV3TaskProxyLess"
		if f.IsEnterprise {
			name = "ReCaptchaV3EnterpriseTaskProxyLess"
		}
		f.IsEnterprise, f.MinScore = false, 0
		return name, f, true
	case taskTurnstile:
		return "AntiTurnstileTaskProxyLess", f, true
	}
	return "", f, false
}

// NewCapSolverSolver builds a solver for one CapSolver API key, the one
// internal/accounts stores under catalogue id "capsolver".
func NewCapSolverSolver(apiKey string) *AntiCaptchaSolver {
	return newAntiCaptchaSolver(&capSolver, apiKey)
}
