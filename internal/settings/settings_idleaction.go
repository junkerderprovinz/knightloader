package settings

// settings_idleaction.go: what happens once the wait queue has nothing
// enabled left to run, start or finish, and how long the cancellable
// countdown runs before it does - see internal/idleaction for the state
// machine this configures and internal/app/app_idle.go for what each action
// actually does and for what "idle" means in terms of the task list.

func sanitizeIdleAction(n Settings) Settings {
	n.IdleAction = n.IdleAction.Sanitize()
	return n
}
