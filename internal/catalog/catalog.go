// Package catalog describes what Flows knows about: the Pocket apps whose
// events can start a flow, and the steps a flow can take (built in, or in a
// sibling app through a Pocket Core link). The web UI builds its pickers from it.
package catalog

// Option is one choice of a select field.
type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Field is one setting of a step.
type Field struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Type        string   `json:"type"` // text | textarea | number | select | picker | bool | keyvalue
	Help        string   `json:"help,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
	Default     string   `json:"default,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Options     []Option `json:"options,omitempty"`
	Picker      string   `json:"picker,omitempty"` // "<app>.<list>", see Pickers
	NoTemplate  bool     `json:"no_template,omitempty"`
}

// StepKind is a kind of step.
type StepKind struct {
	Kind        string   `json:"kind"`
	App         string   `json:"app,omitempty"` // "" = built in
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Icon        string   `json:"icon"`
	Fields      []Field  `json:"fields"`
	Scopes      []string `json:"scopes,omitempty"` // the link scopes it needs in App
	Output      []string `json:"output,omitempty"` // what later steps can use: {{steps.<id>.<name>}}
}

// Topic is one event an app publishes.
type Topic struct {
	Topic string `json:"topic"`
	Name  string `json:"name"`
}

// App is a sibling Pocket app.
type App struct {
	Slug   string  `json:"slug"`
	Name   string  `json:"name"`
	Icon   string  `json:"icon"`
	Events []Topic `json:"events"`
}

// Picker is a list Flows reads from a sibling app for a select ("your task lists").
type Picker struct {
	App   string
	Scope string
	Path  string
	Value string // the field used as the value ("id", "key")
}

// Pickers by name.
var Pickers = map[string]Picker{
	"tasks.lists":        {"tasks", "tasks:read", "/api/v1/lists", "id"},
	"calendar.calendars": {"calendar", "calendar:read", "/api/v1/calendars", "id"},
	"money.accounts":     {"money", "accounts:read", "/api/v1/accounts", "id"},
	"money.categories":   {"money", "accounts:read", "/api/v1/categories", "id"},
	"habits.habits":      {"habits", "habits:read", "/api/v1/habits", "id"},
	"inventory.items":    {"inventory", "items:read", "/api/v1/items", "id"},
	"projects.teams":     {"projects", "projects:read", "/api/v1/teams", "key"},
	"notes.folders":      {"notes", "notes:read", "/api/v1/folders", "path"},
}

// Apps are the Pocket apps whose events Flows can listen to (through an
// events:read link each person switches on in Pocket Core).
var Apps = []App{
	{"tasks", "Pocket Tasks", "check-square", []Topic{
		{"tasks.created", "A task was added"}, {"tasks.completed", "A task was done"}, {"tasks.updated", "A task changed"},
		{"tasks.deleted", "A task was deleted"}, {"tasks.reminder", "A task reminder went off"}}},
	{"calendar", "Pocket Calendar", "calendar", []Topic{
		{"calendar.created", "An event was added"}, {"calendar.updated", "An event changed"}, {"calendar.deleted", "An event was deleted"},
		{"calendar.invited", "You were invited to an event"}, {"calendar.rsvp", "Someone answered an invitation"}, {"calendar.reminder", "An event reminder went off"}}},
	{"notes", "Pocket Notes", "file-text", []Topic{
		{"notes.created", "A note was created"}, {"notes.updated", "A note changed"}, {"notes.deleted", "A note was deleted"}}},
	{"money", "Pocket Money", "wallet", []Topic{
		{"money.transaction_created", "A transaction was added"}, {"money.transaction_deleted", "A transaction was deleted"},
		{"money.subscription_paid", "A subscription was paid"}}},
	{"wishlist", "Pocket Wishlist", "gift", []Topic{
		{"wishlist.price_dropped", "A price dropped"}, {"wishlist.back_in_stock", "Something is back in stock"}, {"wishlist.better_deal", "A better deal was found"},
		{"wishlist.wish_added", "A wish was added"}, {"wishlist.reserved", "Someone reserved a wish"}, {"wishlist.bought", "A wish was bought"}}},
	{"documents", "Pocket Documents", "file", []Topic{
		{"documents.added", "A document was added"}, {"documents.processed", "A document was read (OCR done)"}, {"documents.updated", "A document changed"},
		{"documents.deleted", "A document was deleted"}, {"documents.failed", "A document could not be read"}, {"documents.shared", "A document was shared"}}},
	{"files", "Pocket Files", "folder", []Topic{
		{"files.uploaded", "A file was uploaded"}, {"files.deleted", "A file was deleted"}, {"files.shared", "A file was shared"},
		{"files.send_created", "A send link was made"}, {"files.send_downloaded", "Someone downloaded a send"}, {"files.request_received", "Someone uploaded to your file request"}}},
	{"invoicing", "Pocket Invoicing", "receipt", []Topic{
		{"invoicing.invoice_created", "An invoice was created"}, {"invoicing.invoice_sent", "An invoice was sent"}, {"invoicing.invoice_paid", "An invoice was paid"}}},
	{"habits", "Pocket Habits", "repeat", []Topic{
		{"habits.marked", "A habit was marked"}, {"habits.unmarked", "A habit mark was cleared"}, {"habits.checkin", "A daily check-in was written"}}},
	{"health", "Pocket Health", "heart", []Topic{
		{"health.goal_reached", "A health goal was reached"}, {"health.workout_recorded", "A workout was recorded"}, {"health.sleep_recorded", "Sleep was recorded"},
		{"health.weight_recorded", "Weight was recorded"}, {"health.medication_taken", "A medication was taken"}, {"health.symptom_logged", "A symptom was logged"},
		{"health.imported", "Health data was imported"}}},
	{"bookshelf", "Pocket Bookshelf", "book", []Topic{
		{"bookshelf.started", "You started a book"}, {"bookshelf.finished", "You finished a book"}, {"bookshelf.abandoned", "You put a book down"}, {"bookshelf.rated", "You rated a book"}}},
	{"library", "Pocket Library", "library", []Topic{
		{"library.started", "You started reading"}, {"library.finished", "You finished an ebook"}, {"library.highlighted", "You highlighted a passage"}, {"library.sent", "A book was sent to a reader"}}},
	{"audiobooks", "Pocket Audiobooks", "headphones", []Topic{{"audiobooks.finished", "You finished an audiobook"}}},
	{"podcasts", "Pocket Podcasts", "mic", []Topic{
		{"podcasts.episode_added", "A new episode arrived"}, {"podcasts.finished", "You finished an episode"}, {"podcasts.played", "You played an episode"},
		{"podcasts.downloaded", "An episode was downloaded"}, {"podcasts.subscribed", "You subscribed to a podcast"}, {"podcasts.unsubscribed", "You unsubscribed"}}},
	{"music", "Pocket Music", "music", []Topic{
		{"music.album_arrived", "A requested album arrived"}, {"music.album_requested", "An album was requested"}, {"music.loved", "You loved a song"},
		{"music.playlist_created", "A playlist was made"}, {"music.mix_made", "A mix was made"}}},
	{"watch", "Pocket Watch", "tv", []Topic{
		{"watch.watched", "You watched something"}, {"watch.watchlist_added", "Something went on your watchlist"}, {"watch.requested", "You requested a title"}, {"watch.request_available", "A requested title is ready"}}},
	{"requests", "Pocket Requests", "inbox", []Topic{
		{"requests.created", "A request was made"}, {"requests.approved", "A request was approved"}, {"requests.declined", "A request was declined"},
		{"requests.available", "A request is ready to watch"}, {"requests.problem_reported", "A problem was reported"}, {"requests.attention", "A request needs attention"}}},
	{"kitchen", "Pocket Kitchen", "chef", []Topic{{"kitchen.cooked", "You cooked a recipe"}, {"kitchen.checkout", "A shopping list was checked out"}}},
	{"inventory", "Pocket Inventory", "box", []Topic{
		{"inventory.item_created", "An item was added"}, {"inventory.adjusted", "An item's quantity changed"}, {"inventory.item_archived", "An item was archived"}}},
	{"garage", "Pocket Garage", "car", []Topic{
		{"garage.fuel_logged", "Fuel was logged"}, {"garage.service_logged", "A service was logged"}, {"garage.odometer_updated", "The odometer was updated"}}},
	{"travel", "Pocket Travel", "plane", []Topic{
		{"travel.trip_created", "A trip was planned"}, {"travel.trip_updated", "A trip changed"}, {"travel.expense_linked", "An expense was added to a trip"}}},
	{"split", "Pocket Split", "split", []Topic{
		{"split.expense_created", "A shared expense was added"}, {"split.expense_deleted", "A shared expense was deleted"}, {"split.settled", "Someone settled up"}}},
	{"contacts", "Pocket Contacts", "users", []Topic{
		{"contacts.birthday", "It is someone's birthday"}, {"contacts.created", "A contact was added"}, {"contacts.updated", "A contact changed"}, {"contacts.deleted", "A contact was deleted"}}},
	{"day", "Pocket Day", "sun", []Topic{
		{"day.entry_written", "A journal entry was written"}, {"day.moment_created", "A moment was saved"}, {"day.story_created", "A story was made"}}},
	{"clips", "Pocket Clips", "clipboard", []Topic{
		{"clips.clip_created", "Something was copied"}, {"clips.snippet_created", "A snippet was saved"}, {"clips.quicklink_created", "A quicklink was saved"}}},
	{"office", "Pocket Office", "file-text", []Topic{
		{"office.created", "A document was created"}, {"office.saved", "A document was saved"}, {"office.opened", "A document was opened"}, {"office.sent_for_review", "A document was sent for review"}}},
	{"review", "Pocket Review", "message", []Topic{
		{"review.review_started", "A review started"}, {"review.comment_added", "A comment was added"}, {"review.review_decided", "A review was decided"}}},
	{"projects", "Pocket Projects", "kanban", []Topic{
		{"projects.issue_created", "An issue was created"}, {"projects.issue_completed", "An issue was done"}, {"projects.issue_updated", "An issue changed"},
		{"projects.comment_added", "A comment was added"}, {"projects.project_created", "A project was created"}, {"projects.project_update_posted", "A project update was posted"},
		{"projects.cycle_started", "A cycle started"}, {"projects.cycle_completed", "A cycle ended"}}},
	{"still", "Pocket Still", "lotus", []Topic{
		{"still.practice_completed", "You finished a practice"}, {"still.lesson_completed", "You finished a lesson"}, {"still.milestone_reached", "You reached a milestone"}, {"still.program_completed", "You finished a course"}}},
	{"movement", "Pocket Movement", "map-pin", []Topic{
		{"movement.visit_detected", "A visit was detected"}, {"movement.country_entered", "You entered another country"},
		{"movement.tracking_stopped", "Location tracking stopped"}, {"movement.tracking_resumed", "Location tracking resumed"}}},
	{"mail", "Pocket Mail", "mail", []Topic{{"mail.received", "An email arrived"}, {"mail.sent", "An email was sent"}}},
	{"subtitles", "Pocket Subtitles", "captions", []Topic{{"subtitles.synced", "Subtitles were synced"}, {"subtitles.failed", "Subtitles could not be synced"}, {"subtitles.nudged", "Subtitles were shifted"}}},
	{"monitoring", "Pocket Monitoring", "activity", []Topic{
		{"monitoring.alert", "A server alert went off"}, {"monitoring.resolved", "A server alert cleared"}, {"monitoring.server_added", "A server was added"}, {"monitoring.server_removed", "A server was removed"}}},
	{"backups", "Pocket Backups", "database", []Topic{{"backups.run.finished", "A backup finished"}, {"backups.run.failed", "A backup failed"}}},
	{"ai", "Pocket AI", "sparkles", []Topic{{"ai.turn_finished", "Pocket AI finished a reply"}, {"ai.needs_input", "Pocket AI needs your answer"}, {"ai.schedule_attention", "A scheduled AI run needs attention"}}},
}

// AppBySlug finds an app.
func AppBySlug(slug string) (App, bool) {
	for _, a := range Apps {
		if a.Slug == slug {
			return a, true
		}
	}
	return App{}, false
}

// TopicName is the plain words for a topic ("" when unknown).
func TopicName(topic string) string {
	for _, a := range Apps {
		for _, t := range a.Events {
			if t.Topic == topic {
				return t.Name
			}
		}
	}
	return ""
}

var priorities = []Option{{"default", "Normal"}, {"low", "Low"}, {"high", "High"}, {"urgent", "Urgent"}}

// Steps are every kind of step a flow can take.
var Steps = []StepKind{
	{Kind: "notify", Name: "Send me a notification", Icon: "bell",
		Description: "A push to your phone through the notification address in Settings (Apprise, ntfy or a webhook).",
		Fields: []Field{
			{Key: "title", Label: "Title", Type: "text", Placeholder: "Price drop: {{wish}}", Required: true},
			{Key: "body", Label: "Message", Type: "textarea", Placeholder: "Now {{price | cents}} {{currency}}"},
			{Key: "url", Label: "Link (optional)", Type: "text", Placeholder: "{{url}}", Help: "Opens when you tap the notification (ntfy)."},
			{Key: "priority", Label: "Priority", Type: "select", Options: priorities, Default: "default", NoTemplate: true},
		}},
	{Kind: "http", Name: "Call a web address", Icon: "globe",
		Description: "Send a request to any web address or webhook: Home Assistant, a script, another service. The answer can be used in later steps.",
		Fields: []Field{
			{Key: "method", Label: "Method", Type: "select", Options: []Option{{"POST", "POST"}, {"GET", "GET"}, {"PUT", "PUT"}, {"PATCH", "PATCH"}, {"DELETE", "DELETE"}}, Default: "POST", NoTemplate: true},
			{Key: "url", Label: "Address", Type: "text", Placeholder: "https://example.com/hook", Required: true},
			{Key: "headers", Label: "Headers", Type: "keyvalue", Help: "One per line: Name: value"},
			{Key: "body", Label: "Body", Type: "textarea", Placeholder: `{"title": "{{title}}"}`, Help: "Sent as JSON when it looks like JSON, else as text. Leave empty to send the whole trigger as JSON."},
		},
		Output: []string{"status", "body", "json"}},
	{Kind: "ai", Name: "Ask AI", Icon: "sparkles",
		Description: "Have Pocket AI (or your own OpenAI key) write, summarise or sort something. Later steps use its answer as {{steps.<step>.text}}.",
		Fields: []Field{
			{Key: "prompt", Label: "What to ask", Type: "textarea", Placeholder: "Summarise this in one friendly sentence: {{title}}", Required: true},
			{Key: "json", Label: "Answer as JSON", Type: "bool", Help: "Ask for a JSON object; its keys become fields of the answer.", NoTemplate: true},
		},
		Output: []string{"text", "json"}},
	{Kind: "wait", Name: "Wait", Icon: "clock",
		Description: "Pause before the next step: minutes, hours or days. The run continues after a restart.",
		Fields: []Field{
			{Key: "amount", Label: "How long", Type: "number", Default: "10", Required: true},
			{Key: "unit", Label: "Unit", Type: "select", Options: []Option{{"minutes", "minutes"}, {"hours", "hours"}, {"days", "days"}}, Default: "minutes", NoTemplate: true},
		}},
	{Kind: "filter", Name: "Only continue if…", Icon: "filter",
		Description: "Stop the run here, quietly, unless the conditions hold. Use it after an AI or web step to decide on its answer.",
		Fields:      []Field{}},

	{Kind: "tasks.add", App: "tasks", Name: "Add a task", Icon: "check-square", Scopes: []string{"tasks:write", "tasks:read"},
		Description: "A task in Pocket Tasks. Write it the way you would type it there: “Pay {{client}} invoice tomorrow 9am #work !high”.",
		Fields: []Field{
			{Key: "text", Label: "Task", Type: "text", Placeholder: "Review {{title}} tomorrow", Required: true},
			{Key: "list_id", Label: "List", Type: "picker", Picker: "tasks.lists", Help: "Empty = your default list.", NoTemplate: true},
		},
		Output: []string{"id", "title"}},
	{Kind: "notes.capture", App: "notes", Name: "Add a line to today's note", Icon: "file-text", Scopes: []string{"notes:write"},
		Description: "A time-stamped line in today's daily note in Pocket Notes.",
		Fields: []Field{
			{Key: "text", Label: "Text", Type: "textarea", Placeholder: "Finished {{title}} by {{authors}}", Required: true},
		}},
	{Kind: "notes.create", App: "notes", Name: "Create a note", Icon: "file-plus", Scopes: []string{"notes:write", "notes:read"},
		Description: "A new note in Pocket Notes.",
		Fields: []Field{
			{Key: "title", Label: "Title", Type: "text", Placeholder: "{{title}}", Required: true},
			{Key: "content", Label: "Content", Type: "textarea", Placeholder: "Markdown is fine."},
			{Key: "category", Label: "Folder", Type: "picker", Picker: "notes.folders", Help: "Empty = no folder.", NoTemplate: true},
		},
		Output: []string{"id"}},
	{Kind: "calendar.add", App: "calendar", Name: "Add a calendar event", Icon: "calendar", Scopes: []string{"calendar:write", "calendar:read"},
		Description: "An event in Pocket Calendar, written the way you would type it there: “Dentist Friday 10am for 1h”.",
		Fields: []Field{
			{Key: "text", Label: "Event", Type: "text", Placeholder: "Car service {{date}} 9am", Required: true},
			{Key: "calendar_id", Label: "Calendar", Type: "picker", Picker: "calendar.calendars", Help: "Empty = your default calendar.", NoTemplate: true},
		},
		Output: []string{"id", "title"}},
	{Kind: "money.add", App: "money", Name: "Record a transaction", Icon: "wallet", Scopes: []string{"transactions:write", "accounts:read"},
		Description: "An expense or income in Pocket Money. Bookkeeping only: no money moves.",
		Fields: []Field{
			{Key: "kind", Label: "Type", Type: "select", Options: []Option{{"expense", "Expense"}, {"income", "Income"}}, Default: "expense", NoTemplate: true},
			{Key: "account_id", Label: "Account", Type: "picker", Picker: "money.accounts", Required: true, NoTemplate: true},
			{Key: "amount", Label: "Amount", Type: "text", Placeholder: "{{total}}", Required: true, Help: "A number like 12.50 (in the account's currency)."},
			{Key: "description", Label: "Description", Type: "text", Placeholder: "Invoice {{number}} paid", Required: true},
			{Key: "payee", Label: "Payee", Type: "text", Placeholder: "{{client}}"},
			{Key: "category_id", Label: "Category", Type: "picker", Picker: "money.categories", NoTemplate: true},
			{Key: "date", Label: "Date", Type: "text", Placeholder: "{{today}}", Help: "YYYY-MM-DD; empty = today."},
		},
		Output: []string{"id"}},
	{Kind: "habits.mark", App: "habits", Name: "Tick a habit", Icon: "repeat", Scopes: []string{"entries:write", "habits:read"},
		Description: "Mark a habit done (or missed) in Pocket Habits.",
		Fields: []Field{
			{Key: "habit_id", Label: "Habit", Type: "picker", Picker: "habits.habits", Required: true, NoTemplate: true},
			{Key: "status", Label: "Mark as", Type: "select", Options: []Option{{"1", "Done"}, {"-1", "Missed"}}, Default: "1", NoTemplate: true},
			{Key: "date", Label: "Day", Type: "text", Placeholder: "{{today}}", Help: "YYYY-MM-DD; empty = today."},
		}},
	{Kind: "inventory.adjust", App: "inventory", Name: "Change a stock count", Icon: "box", Scopes: []string{"items:adjust", "items:read"},
		Description: "Add to or take from an item's quantity in Pocket Inventory.",
		Fields: []Field{
			{Key: "item_id", Label: "Item", Type: "picker", Picker: "inventory.items", Required: true, NoTemplate: true},
			{Key: "delta", Label: "Change by", Type: "text", Default: "-1", Required: true, Help: "Negative uses some up."},
			{Key: "note", Label: "Note", Type: "text", Placeholder: "Used by a flow"},
		}},
	{Kind: "projects.issue", App: "projects", Name: "Create an issue", Icon: "kanban", Scopes: []string{"projects:write", "projects:read"},
		Description: "An issue in Pocket Projects.",
		Fields: []Field{
			{Key: "team", Label: "Team", Type: "picker", Picker: "projects.teams", Required: true, NoTemplate: true},
			{Key: "title", Label: "Title", Type: "text", Placeholder: "{{title}}", Required: true},
			{Key: "description", Label: "Description", Type: "textarea"},
			{Key: "priority", Label: "Priority", Type: "select", Options: []Option{{"0", "None"}, {"1", "Urgent"}, {"2", "High"}, {"3", "Medium"}, {"4", "Low"}}, Default: "0", NoTemplate: true},
		},
		Output: []string{"id", "identifier"}},
}

// StepByKind finds a step kind.
func StepByKind(kind string) (StepKind, bool) {
	for _, k := range Steps {
		if k.Kind == kind {
			return k, true
		}
	}
	return StepKind{}, false
}

// ActionApps are the apps steps act in, with every scope their steps and pickers need.
func ActionApps() map[string][]string {
	out := map[string][]string{}
	for _, k := range Steps {
		if k.App == "" {
			continue
		}
		for _, sc := range k.Scopes {
			found := false
			for _, have := range out[k.App] {
				if have == sc {
					found = true
				}
			}
			if !found {
				out[k.App] = append(out[k.App], sc)
			}
		}
	}
	return out
}
