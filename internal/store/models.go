package store

import "encoding/json"

// User is an account on this box.
type User struct {
	ID           int64  `json:"id"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	PasswordHash string `json:"-"`
	Role         string `json:"role"` // admin | member
	CreatedAt    string `json:"created_at"`
}

// APIToken is a personal access token (hash stored, plaintext shown once).
type APIToken struct {
	ID         int64   `json:"id"`
	UserID     int64   `json:"user_id"`
	Name       string  `json:"name"`
	Prefix     string  `json:"prefix"`
	LastUsedAt *string `json:"last_used_at"`
	CreatedAt  string  `json:"created_at"`
}

// Trigger kinds.
const (
	TriggerEvent    = "event"
	TriggerSchedule = "schedule"
	TriggerWebhook  = "webhook"
	TriggerManual   = "manual"
)

// Condition compares one field with a value: "price" "lt" "100".
type Condition struct {
	Field string `json:"field"`
	Op    string `json:"op"` // equals | not_equals | contains | not_contains | starts_with | ends_with | gt | gte | lt | lte | exists | missing | matches
	Value string `json:"value"`
}

// Trigger is what starts a flow.
type Trigger struct {
	// event: the topic ("wishlist.price_dropped"; "wishlist.*" = anything from the app).
	Topic string `json:"topic,omitempty"`
	// event and webhook: only when every (or any) condition holds.
	Conditions []Condition `json:"conditions,omitempty"`
	Match      string      `json:"match,omitempty"` // all | any

	// schedule
	Repeat     string `json:"repeat,omitempty"`       // minutes | hourly | daily | weekly | monthly | once
	Every      int    `json:"every,omitempty"`        // minutes: every N minutes (5–720); hourly: every N hours (1–24)
	Time       string `json:"time,omitempty"`         // HH:MM for daily, weekly, monthly
	Days       string `json:"days,omitempty"`         // weekly: ISO weekdays "12345"
	DayOfMonth int    `json:"day_of_month,omitempty"` // monthly: 1–31 (past the end = the last day)
	At         string `json:"at,omitempty"`           // once: "2026-11-02T09:00" local time
}

// Step is one thing a flow does.
type Step struct {
	ID      string            `json:"id"`   // stable within the flow ("s1"), for {{steps.s1.…}}
	Kind    string            `json:"kind"` // see the engine's step kinds
	Name    string            `json:"name,omitempty"`
	Config  map[string]string `json:"config"`
	OnlyIf  []Condition       `json:"only_if,omitempty"`
	Match   string            `json:"match,omitempty"`    // all | any for OnlyIf
	OnError string            `json:"on_error,omitempty"` // stop (default) | continue
}

// Flow is "when this happens, do these things".
type Flow struct {
	ID          int64   `json:"id"`
	UserID      int64   `json:"-"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Enabled     bool    `json:"enabled"`
	TriggerKind string  `json:"trigger_kind"`
	Trigger     Trigger `json:"trigger"`
	Steps       []Step  `json:"steps"`
	HookToken   string  `json:"-"`
	NextRunAt   string  `json:"next_run_at"`
	LastRunAt   string  `json:"last_run_at"`
	LastStatus  string  `json:"last_status"`
	RunCount    int     `json:"run_count"`
	FailCount   int     `json:"fail_count"`
	Recipe      string  `json:"recipe"`
	Source      string  `json:"source"`
	ExternalID  string  `json:"external_id,omitempty"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

// Run statuses.
const (
	RunQueued    = "queued"
	RunRunning   = "running"
	RunWaiting   = "waiting"
	RunOK        = "ok"
	RunFailed    = "failed"
	RunStopped   = "stopped" // a condition step ended it early, on purpose
	RunCancelled = "cancelled"
)

// LogEntry is what one step did in a run.
type LogEntry struct {
	Step    int    `json:"step"`
	StepID  string `json:"step_id"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Status  string `json:"status"` // ok | failed | skipped | waiting | retry | stopped
	Message string `json:"message"`
	At      string `json:"at"`
	MS      int64  `json:"ms"`
}

// Run is one run of a flow.
type Run struct {
	ID          int64                      `json:"id"`
	UserID      int64                      `json:"-"`
	FlowID      int64                      `json:"flow_id"`
	FlowName    string                     `json:"flow_name"`
	TriggerKind string                     `json:"trigger_kind"`
	TriggerRef  string                     `json:"trigger_ref"`
	Summary     string                     `json:"summary"`
	Input       map[string]any             `json:"input"`
	Status      string                     `json:"status"`
	Step        int                        `json:"step"`
	Attempt     int                        `json:"attempt"`
	Outputs     map[string]json.RawMessage `json:"outputs"`
	Log         []LogEntry                 `json:"log"`
	Error       string                     `json:"error"`
	ResumeAt    string                     `json:"resume_at"`
	StartedAt   string                     `json:"started_at"`
	FinishedAt  string                     `json:"finished_at"`
	CreatedAt   string                     `json:"created_at"`
}

// Event is one event from the Pocket Core bus that belongs to a person.
type Event struct {
	ID         int64          `json:"id"`
	UserID     int64          `json:"-"`
	CoreID     int64          `json:"core_id"`
	Topic      string         `json:"topic"`
	Source     string         `json:"source"`
	Subject    string         `json:"subject"`
	Payload    map[string]any `json:"payload"`
	Matched    int            `json:"matched"`
	ReceivedAt string         `json:"received_at"`
}

// Conn is how a person reaches one sibling Pocket app.
type Conn struct {
	Mode      string `json:"mode,omitempty"` // core | direct | off ("" = automatic)
	URL       string `json:"url,omitempty"`
	Token     string `json:"token,omitempty"`
	PublicURL string `json:"public_url,omitempty"`
}

// Settings: box-wide keys (boxKeys) plus each person's own.
type Settings struct {
	Title         string `json:"title"`
	SetupComplete bool   `json:"setup_complete"`
	// HTTPPrivate lets members' "Call a web address" steps reach addresses on
	// the local network (admins always can). Box-wide, admin only.
	HTTPPrivate bool `json:"http_private"`

	DateFormat string `json:"date_format"`
	TimeFormat string `json:"time_format"`
	WeekStart  int    `json:"week_start"` // 1 = Monday, 7 = Sunday

	// Notifications: where "Send me a notification" steps and failure alerts go.
	NotifyURL      string `json:"notify_url"`
	NotifyFormat   string `json:"notify_format"` // apprise | ntfy | json
	NotifyFailures bool   `json:"notify_failures"`
	NotifyRecover  bool   `json:"notify_recover"` // when a flow works again after failing
	KeepRunsDays   int    `json:"keep_runs_days"`

	// Connections to sibling Pocket apps, by slug.
	Connections map[string]Conn `json:"connections"`

	// AI for "Ask AI" steps: Pocket AI through Core, or an OpenAI-compatible
	// API with the person's own key.
	AIProvider    string `json:"ai_provider"` // auto | pocket | openai
	AIPocketModel string `json:"ai_pocket_model"`
	OpenAIKey     string `json:"openai_api_key"`
	OpenAIModel   string `json:"openai_model"`
	OpenAIBaseURL string `json:"openai_base_url"`
}

func jsonText(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
