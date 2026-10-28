package server

import "github.com/abjelosevic88/pocket-homelab-flows/internal/store"

// recipe is a ready-made flow to start from. Making one creates the flow
// switched off, so the person looks it over (and picks their list,
// account…) before it runs.
type recipe struct {
	Key         string     `json:"key"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Icon        string     `json:"icon"`
	Group       string     `json:"group"`
	Apps        []string   `json:"apps"` // the Pocket apps it needs
	Flow        store.Flow `json:"flow"`
}

func notifyStep(id, title, body, link, prio string) store.Step {
	return store.Step{ID: id, Kind: "notify", Config: map[string]string{"title": title, "body": body, "url": link, "priority": prio}}
}

func eventFlow(name, topic string, conds []store.Condition, steps ...store.Step) store.Flow {
	return store.Flow{Name: name, TriggerKind: store.TriggerEvent, Trigger: store.Trigger{Topic: topic, Conditions: conds}, Steps: steps}
}

var recipes = []recipe{
	{Key: "wishlist-price-drop", Name: "Tell me when a wish gets cheaper", Icon: "gift", Group: "Shopping", Apps: []string{"wishlist"},
		Description: "A push the moment Pocket Wishlist sees a lower price, with a link to the shop.",
		Flow: eventFlow("Wish got cheaper", "wishlist.price_dropped", nil,
			notifyStep("s1", "Cheaper: {{wish}}", "Now {{price | cents}} {{currency}} at {{store}} (was {{was | cents}}).", "{{url}}", "high"))},
	{Key: "wishlist-back-in-stock", Name: "Tell me when a wish is back in stock", Icon: "gift", Group: "Shopping", Apps: []string{"wishlist"},
		Description: "A push when something you want can be bought again.",
		Flow: eventFlow("Wish back in stock", "wishlist.back_in_stock", nil,
			notifyStep("s1", "Back in stock: {{wish}}", "{{price | cents}} {{currency}} at {{store}}.", "{{url}}", "high"))},
	{Key: "document-review-task", Name: "A task to check every new document", Icon: "file", Group: "Paperwork", Apps: []string{"documents", "tasks"},
		Description: "When Pocket Documents has read a new document, add “Check …” to your tasks for tomorrow.",
		Flow: eventFlow("Check new documents", "documents.processed", nil,
			store.Step{ID: "s1", Kind: "tasks.add", Config: map[string]string{"text": "Check document: {{title}} tomorrow", "list_id": ""}})},
	{Key: "invoice-paid-income", Name: "Book paid invoices as income", Icon: "receipt", Group: "Money", Apps: []string{"invoicing", "money"},
		Description: "When an invoice is marked paid in Pocket Invoicing, record the income in Pocket Money. Pick the account once.",
		Flow: eventFlow("Paid invoices → income", "invoicing.invoice_paid", nil,
			store.Step{ID: "s1", Kind: "money.add", Config: map[string]string{"kind": "income", "account_id": "", "amount": "{{total}}", "description": "Invoice {{number}} paid", "payee": "{{client}}", "date": "{{today}}"}},
			notifyStep("s2", "Invoice {{number}} paid", "{{client}} paid {{total}}.", "", "default"))},
	{Key: "finished-book-note", Name: "Note every book I finish", Icon: "book", Group: "Reading", Apps: []string{"bookshelf", "notes"},
		Description: "A line in today's note in Pocket Notes when you finish a book in Pocket Bookshelf.",
		Flow: eventFlow("Finished books in my notes", "bookshelf.finished", nil,
			store.Step{ID: "s1", Kind: "notes.capture", Config: map[string]string{"text": "Finished reading **{{title}}** by {{authors}}"}})},
	{Key: "task-reminder-push", Name: "Push my task reminders", Icon: "check-square", Group: "Everyday", Apps: []string{"tasks"},
		Description: "Pocket Tasks reminders as a push to your phone, with the due time.",
		Flow: eventFlow("Task reminders", "tasks.reminder", nil,
			notifyStep("s1", "⏰ {{title}}", "Due {{due | default: today}}", "", "high"))},
	{Key: "calendar-invite-push", Name: "Tell me about calendar invitations", Icon: "calendar", Group: "Everyday", Apps: []string{"calendar"},
		Description: "A push when someone invites you to an event in Pocket Calendar.",
		Flow: eventFlow("New invitations", "calendar.invited", nil,
			notifyStep("s1", "Invitation: {{title}}", "From {{organizer}}, starts {{start}}.", "", "default"))},
	{Key: "request-ready", Name: "Tell me when my movie or show is ready", Icon: "tv", Group: "Media", Apps: []string{"requests"},
		Description: "A push when something you requested in Pocket Requests can be watched.",
		Flow: eventFlow("Requests ready to watch", "requests.available", nil,
			notifyStep("s1", "Ready to watch: {{title}}", "{{title}} ({{year}}) is in the library.", "", "default"))},
	{Key: "send-downloaded", Name: "Tell me when someone downloads my send", Icon: "folder", Group: "Files", Apps: []string{"files"},
		Description: "A push when a Pocket Files send link is downloaded.",
		Flow: eventFlow("Send downloaded", "files.send_downloaded", nil,
			notifyStep("s1", "Downloaded: {{label}}", "{{downloads}} of {{max_downloads | default: unlimited}} downloads.", "", "default"))},
	{Key: "server-alert", Name: "Urgent push for server alerts", Icon: "activity", Group: "Homelab", Apps: []string{"monitoring"},
		Description: "Critical Pocket Monitoring alerts as an urgent push; the rest stay quiet.",
		Flow: eventFlow("Critical server alerts", "monitoring.alert", []store.Condition{{Field: "severity", Op: "equals", Value: "critical"}},
			notifyStep("s1", "🔥 {{target}}: {{metric}}", "{{message}}", "", "urgent"))},
	{Key: "goal-reached-note", Name: "Celebrate reached health goals", Icon: "heart", Group: "Health", Apps: []string{"health", "notes"},
		Description: "A line in today's note when you reach a goal in Pocket Health.",
		Flow: eventFlow("Health goals in my notes", "health.goal_reached", nil,
			store.Step{ID: "s1", Kind: "notes.capture", Config: map[string]string{"text": "Reached my {{metric}} goal: {{value}} (target {{target}})"}})},
	{Key: "weekly-plan", Name: "Plan the week on Sunday evening", Icon: "calendar", Group: "Everyday", Apps: []string{"tasks"},
		Description: "Every Sunday at 18:00 a task “Plan the week” for the same evening.",
		Flow: store.Flow{Name: "Plan the week", TriggerKind: store.TriggerSchedule, Trigger: store.Trigger{Repeat: "weekly", Days: "7", Time: "18:00"},
			Steps: []store.Step{{ID: "s1", Kind: "tasks.add", Config: map[string]string{"text": "Plan the week today 19:00", "list_id": ""}}}}},
	{Key: "morning-ai-quote", Name: "A thought for the morning", Icon: "sparkles", Group: "Everyday", Apps: []string{"ai"},
		Description: "Every weekday at 07:30, Pocket AI writes one short, practical thought for the day and sends it to your phone.",
		Flow: store.Flow{Name: "Morning thought", TriggerKind: store.TriggerSchedule, Trigger: store.Trigger{Repeat: "weekly", Days: "12345", Time: "07:30"},
			Steps: []store.Step{
				{ID: "s1", Kind: "ai", Config: map[string]string{"prompt": "Write one short, practical thought for a {{weekday}} morning, under 30 words. No quotes from famous people.", "json": "false"}},
				notifyStep("s2", "Good morning", "{{steps.s1.text}}", "", "low")}}},
	{Key: "webhook-task", Name: "Add a task from anywhere", Icon: "link", Group: "Shortcuts", Apps: []string{"tasks"},
		Description: "A private web address that adds whatever you send it as a task: from iOS Shortcuts, a script, Home Assistant or a form. Send {\"text\": \"Buy milk tomorrow\"}.",
		Flow: store.Flow{Name: "Task from a webhook", TriggerKind: store.TriggerWebhook,
			Steps: []store.Step{{ID: "s1", Kind: "tasks.add", Config: map[string]string{"text": "{{text}}", "list_id": ""}}}}},
	{Key: "webhook-forward", Name: "Forward a webhook to my phone", Icon: "globe", Group: "Shortcuts", Apps: []string{},
		Description: "A private web address; whatever calls it (a camera, a printer, a service) becomes a push with its message.",
		Flow: store.Flow{Name: "Webhook → push", TriggerKind: store.TriggerWebhook,
			Steps: []store.Step{notifyStep("s1", "{{title | default: Webhook}}", "{{message}}{{text}}", "", "default")}}},
	{Key: "daily-check-url", Name: "Check a website every hour", Icon: "globe", Group: "Homelab", Apps: []string{},
		Description: "Calls a web address every hour and pushes you only when it does not answer.",
		Flow: store.Flow{Name: "Is my site up?", TriggerKind: store.TriggerSchedule, Trigger: store.Trigger{Repeat: "hourly", Every: 1, Time: "00:05"},
			Steps: []store.Step{
				{ID: "s1", Kind: "http", OnError: "continue", Config: map[string]string{"method": "GET", "url": "https://example.com", "headers": "", "body": ""}},
				{ID: "s2", Kind: "filter", OnlyIf: []store.Condition{{Field: "steps.s1.status", Op: "missing"}}, Config: map[string]string{}},
				notifyStep("s3", "Site down", "https://example.com did not answer at {{time}}.", "", "high")}}},
}

func recipeByKey(k string) (recipe, bool) {
	for _, r := range recipes {
		if r.Key == k {
			return r, true
		}
	}
	return recipe{}, false
}
