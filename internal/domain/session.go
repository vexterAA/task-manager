package domain

import "time"

const (
	SessionStateIdle             = "IDLE"
	SessionStateCreateText       = "CREATE_TEXT"
	SessionStateCreateTitle      = "CREATE_TITLE"
	SessionStateCreateDeadline   = "CREATE_DEADLINE"
	SessionStateCreateRemind     = "CREATE_REMIND"
	SessionStateCreateConfirm    = "CREATE_CONFIRM"
	SessionStateCreateEdit       = "CREATE_EDIT"
	SessionStatePickTask         = "PICK_TASK"
	SessionStateSettingsMenu     = "SETTINGS_MENU"
	SessionStateSettingsTZ       = "SETTINGS_TZ"
	SessionStateSettingsRemind   = "SETTINGS_REMIND"
	SessionStateTaskEditMenu     = "TASK_EDIT_MENU"
	SessionStateTaskEditDeadline = "TASK_EDIT_DEADLINE"
	SessionStateTaskEditRemind   = "TASK_EDIT_REMIND"
)

type UserSession struct {
	UserID    int64
	State     string
	DraftJSON string
	UpdatedAt time.Time
}
