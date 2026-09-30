package captcha

// CapMonster Cloud speaks Anti-Captcha's createTask protocol, so it is an
// antiCaptchaAPI for the client in solver_anticaptcha.go. Its tasks solve
// through CapMonster's own proxies unless one is given, whatever the name:
// RecaptchaV2Task is the proxyless one here. ImageToTextTask documents no
// comment, and the only coordinate tasks are ComplexImageTask classes for
// particular sites, so a click captcha is refused.
//
//	https://docs.capmonster.cloud/docs/api/methods/create-task
//	https://docs.capmonster.cloud/docs/api/methods/get-task-result
//	https://docs.capmonster.cloud/docs/api/methods/get-balance
//	https://docs.capmonster.cloud/docs/api/api-errors
//	https://docs.capmonster.cloud/docs/captchas/ImageToText/image-to-text
//	https://docs.capmonster.cloud/docs/captchas/no-captcha-task
//	https://docs.capmonster.cloud/docs/captchas/recaptcha-v2-enterprise-task
//	https://docs.capmonster.cloud/docs/captchas/recaptcha-v3-task
//	https://docs.capmonster.cloud/docs/captchas/hcaptcha-task
//	https://docs.capmonster.cloud/docs/captchas/turnstile-task

var capMonster = antiCaptchaAPI{
	name:      "capmonster",
	label:     "CapMonster Cloud",
	base:      "https://api.capmonster.cloud",
	tokenTask: capMonsterTokenTask,
}

// capMonsterTokenTask names CapMonster's task for each token type. reCAPTCHA
// v3 keeps the shared name, with isEnterprise for the Enterprise variant as
// at Anti-Captcha.
func capMonsterTokenTask(typ string, f tokenFields) (string, tokenFields, bool) {
	switch typ {
	case taskRecaptchaV2:
		return "RecaptchaV2Task", f, true
	case taskRecaptchaV2Enterprise:
		// RecaptchaV2EnterpriseTask documents no isInvisible.
		f.IsInvisible = false
		return "RecaptchaV2EnterpriseTask", f, true
	case taskRecaptchaV3:
		return taskRecaptchaV3, f, true
	case taskHCaptcha:
		return "HCaptchaTask", f, true
	case taskTurnstile:
		return "TurnstileTask", f, true
	}
	return "", f, false
}

// NewCapMonsterSolver builds a solver for one CapMonster Cloud API key, the
// one internal/accounts stores under catalogue id "capmonster".
func NewCapMonsterSolver(apiKey string) *AntiCaptchaSolver {
	return newAntiCaptchaSolver(&capMonster, apiKey)
}
