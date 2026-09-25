package captcha

// Token tasks: a widget challenge is not a picture a worker reads but a site
// key the provider's own browsers solve on the hoster's page. The provider
// answers with the token that page would have posted, and the token goes back
// to the Source exactly as one from the widget page in the browser does.
//
// 2Captcha and Anti-Captcha name these task types and their fields the same
// way, so one builder serves both, and each provider keeps its own list of the
// types it takes. Every type is the proxyless one, solved from the provider's
// own network, since this app has no proxy to hand over.
//
//	https://anti-captcha.com/apidoc/task-types/RecaptchaV2TaskProxyless
//	https://anti-captcha.com/apidoc/task-types/RecaptchaV2EnterpriseTaskProxyless
//	https://anti-captcha.com/apidoc/task-types/RecaptchaV3TaskProxyless
//	https://anti-captcha.com/apidoc/task-types/TurnstileTaskProxyless

import "strings"

const (
	taskRecaptchaV2           = "RecaptchaV2TaskProxyless"
	taskRecaptchaV2Enterprise = "RecaptchaV2EnterpriseTaskProxyless"
	taskRecaptchaV3           = "RecaptchaV3TaskProxyless"
	taskHCaptcha              = "HCaptchaTaskProxyless"
	taskTurnstile             = "TurnstileTaskProxyless"
)

// recaptchaV3MinScore is the worker score a v3 task asks for. Both providers
// require one and the payload does not say what the hoster checks against, so
// it is 0.3, the lowest Anti-Captcha accepts and what JD's own Anti-Captcha
// solver sends.
const recaptchaV3MinScore = 0.3

// tokenFields are the createTask fields a token task adds to an image task's.
type tokenFields struct {
	WebsiteURL   string  `json:"websiteURL,omitempty"`
	WebsiteKey   string  `json:"websiteKey,omitempty"`
	IsInvisible  bool    `json:"isInvisible,omitempty"`
	IsEnterprise bool    `json:"isEnterprise,omitempty"`
	MinScore     float64 `json:"minScore,omitempty"`
	PageAction   string  `json:"pageAction,omitempty"`
}

// tokenTask names the task type for a KindWidget challenge and fills its
// fields. The type is empty for a payload that names no vendor this package
// knows, which neither provider's list contains. A reCAPTCHA v3 without an
// action the hoster would take a token for is refused before it is paid for,
// by the rule the widget page applies.
func tokenTask(c Challenge) (string, tokenFields, error) {
	p, ok := c.Payload.(*WidgetPayload)
	if !ok || p == nil {
		return "", tokenFields{}, nil
	}
	f := tokenFields{WebsiteURL: p.SiteURL, WebsiteKey: p.SiteKey}
	// JD's contextUrl is only the scheme and host of the page, which both
	// providers accept when the full address is missing.
	if f.WebsiteURL == "" {
		f.WebsiteURL = p.ContextURL
	}
	switch p.Vendor {
	case VendorTurnstile:
		return taskTurnstile, f, nil
	case VendorHCaptcha:
		return taskHCaptcha, f, nil
	case VendorRecaptcha:
		if p.V3Action != "" {
			action, ok := RecaptchaAction(p.V3Action)
			if !ok {
				return "", f, unsupported(taskRecaptchaV3 + " without a usable action")
			}
			f.PageAction = action
			f.IsEnterprise = p.Enterprise
			f.MinScore = recaptchaV3MinScore
			return taskRecaptchaV3, f, nil
		}
		f.IsInvisible = strings.EqualFold(p.Type, "invisible")
		if p.Enterprise {
			return taskRecaptchaV2Enterprise, f, nil
		}
		return taskRecaptchaV2, f, nil
	default:
		return "", f, nil
	}
}
