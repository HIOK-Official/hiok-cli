package cli

import (
	"context"
	"flag"
	"fmt"
)

// The remaining kinds: storage, the relational engines, messaging, and the jobs
// that read a stream.
//
// Split from topics_platform.go only because one file of every service is hard
// to read, not because these are different in kind.

// ── storage ─────────────────────────────────────────────────────────────────

func storageTopic() Topic {
	var name, id, prefix string
	return Topic{
		Name:    "storage",
		Summary: "Storage accounts and what is in them",
		Commands: []Command{
			{
				Name: "list", Summary: "List storage accounts",
				Run: func(ctx context.Context, app *App, args []string) error {
					return get(app, ctx, "/api/StorageAccount", "name", "status", "storageTier", "usedBytes", "primaryRegion")
				},
			},
			{
				Name: "create", Summary: "Create a storage account",
				Flags: func(fs *flag.FlagSet) { fs.StringVar(&name, "name", "", "account name") },
				Run: func(ctx context.Context, app *App, args []string) error {
					if name == "" && len(args) > 0 {
						name = args[0]
					}
					if name == "" {
						return fmt.Errorf("--name is required")
					}
					if _, err := post(app, ctx, "POST", "/api/StorageAccount", map[string]any{"name": name}); err != nil {
						return err
					}
					app.Print.Message("Storage account %q created.", name)
					return nil
				},
			},
			{
				Name: "ls", Summary: "List what is in an account",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&id, "id", "", "storage account name or id")
					fs.StringVar(&prefix, "path", "", "folder to list")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if id == "" && len(args) > 0 {
						id = args[0]
					}
					if id == "" {
						return fmt.Errorf("--id is required")
					}
					id, err := idOf(app, ctx, "/api/StorageAccount", "storage account", id)
					if err != nil {
						return err
					}
					path := "/api/StorageAccount/" + id + "/items"
					if prefix != "" {
						path += "?path=" + prefix
					}
					return get(app, ctx, path, "name", "mimeType", "sizeDisplay", "updatedAt")
				},
			},
			deleteByIDCommand("delete", "Delete a storage account", "/api/StorageAccount"),
		},
	}
}

// ── the relational engines ──────────────────────────────────────────────────

// relationalTopic is MySQL and SQL Server, which differ in their SQL rather than
// in what a customer does with them — the API has one controller for both, so
// this has one topic builder for both.
func relationalTopic(topicName, summary, apiPath string) Topic {
	var name, region, sku, database, sql, id string
	var storageGb int

	return Topic{
		Name:    topicName,
		Summary: summary,
		Commands: []Command{
			{
				Name: "list", Summary: "List servers",
				Run: func(ctx context.Context, app *App, args []string) error {
					return get(app, ctx, apiPath, "serverName", "status", "sku", "region")
				},
			},
			{
				Name: "create", Summary: "Create a server",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&name, "name", "", "server name")
					fs.StringVar(&region, "region", "", "region")
					fs.StringVar(&sku, "sku", "Burstable_B1ms", "size")
					fs.StringVar(&database, "database", "", "a database to create with it")
					fs.IntVar(&storageGb, "storage", 32, "storage in GiB")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if name == "" {
						return fmt.Errorf("--name is required")
					}
					body := map[string]any{
						"serverName": name, "region": app.Region(region),
						"sku": sku, "storageGb": storageGb,
					}
					if database != "" {
						body["databaseName"] = database
					}
					resp, err := post(app, ctx, "POST", apiPath, body)
					if err != nil {
						return err
					}
					// The generated password is shown once, on create, and never again.
					if data, ok := resp["data"].(map[string]any); ok {
						if password, ok := data["adminPassword"].(string); ok && password != "" {
							app.Print.Message("Server %q created.\nAdministrator password: %s\nThis is the only time it is shown.", name, password)
							return nil
						}
					}
					app.Print.Message("Server %q created.", name)
					return nil
				},
			},
			{
				Name: "connection", Summary: "How to connect to a server",
				Flags: func(fs *flag.FlagSet) { fs.StringVar(&id, "id", "", "server id") },
				Run: func(ctx context.Context, app *App, args []string) error {
					if id == "" && len(args) > 0 {
						id = args[0]
					}
					if id == "" {
						return fmt.Errorf("--id is required")
					}
					if err := app.RequireToken(); err != nil {
						return err
					}
					var resp map[string]any
					if err := app.Client.Do(ctx, "GET", apiPath+"/"+id+"/connection", nil, &resp, true); err != nil {
						return err
					}
					data, _ := resp["data"].(map[string]any)
					return app.Print.Record(data)
				},
			},
			{
				Name: "query", Summary: "Run a statement",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&id, "id", "", "server id")
					fs.StringVar(&sql, "sql", "", "the statement to run")
					fs.StringVar(&database, "database", "", "which database to run it against")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if id == "" || sql == "" {
						return fmt.Errorf("--id and --sql are required")
					}
					body := map[string]any{"sql": sql}
					if database != "" {
						body["database"] = database
					}
					if err := app.RequireToken(); err != nil {
						return err
					}
					var resp struct {
						Data struct {
							Rows []map[string]any `json:"rows"`
						} `json:"data"`
					}
					if err := app.Client.Do(ctx, "POST", apiPath+"/"+id+"/query", body, &resp, true); err != nil {
						return err
					}
					return app.Print.Table(resp.Data.Rows)
				},
			},
			deleteByIDCommand("delete", "Delete a server", apiPath),
		},
	}
}

// ── messaging ───────────────────────────────────────────────────────────────

func serviceBusTopic() Topic {
	var name, region, sku, namespaceID string
	return Topic{
		Name:    "servicebus",
		Summary: "Messaging namespaces, queues and topics",
		Commands: []Command{
			{
				Name: "list", Summary: "List namespaces",
				Run: func(ctx context.Context, app *App, args []string) error {
					return get(app, ctx, "/api/ServiceBus/namespaces", "name", "sku", "status", "primaryRegion")
				},
			},
			{
				Name: "create", Summary: "Create a namespace",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&name, "name", "", "namespace name")
					fs.StringVar(&region, "region", "", "region")
					fs.StringVar(&sku, "sku", "standard", "basic, standard or premium")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if name == "" {
						return fmt.Errorf("--name is required")
					}
					body := map[string]any{"name": name, "sku": sku, "primaryRegion": app.Region(region)}
					if _, err := post(app, ctx, "POST", "/api/ServiceBus/namespaces", body); err != nil {
						return err
					}
					app.Print.Message("Namespace %q created.", name)
					return nil
				},
			},
			{
				Name: "queues", Summary: "List the queues in a namespace",
				Flags: func(fs *flag.FlagSet) { fs.StringVar(&namespaceID, "namespace", "", "namespace name or id") },
				Run: func(ctx context.Context, app *App, args []string) error {
					if namespaceID == "" && len(args) > 0 {
						namespaceID = args[0]
					}
					if namespaceID == "" {
						return fmt.Errorf("--namespace is required")
					}
					namespaceID, err := idOf(app, ctx, "/api/ServiceBus/namespaces", "namespace", namespaceID)
					if err != nil {
						return err
					}
					return get(app, ctx,
						"/api/ServiceBus/namespaces/"+namespaceID+"/queues",
						"name", "activeMessageCount", "deadLetterMessageCount", "status")
				},
			},
			{
				Name: "create-queue", Summary: "Create a queue",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&namespaceID, "namespace", "", "namespace name or id")
					fs.StringVar(&name, "name", "", "queue name")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if namespaceID == "" || name == "" {
						return fmt.Errorf("--namespace and --name are required")
					}
					namespaceID, err := idOf(app, ctx, "/api/ServiceBus/namespaces", "namespace", namespaceID)
					if err != nil {
						return err
					}
					path := "/api/ServiceBus/namespaces/" + namespaceID + "/queues"
					if _, err := post(app, ctx, "POST", path, map[string]any{"name": name}); err != nil {
						return err
					}
					app.Print.Message("Queue %q created.", name)
					return nil
				},
			},
			{
				Name: "topics", Summary: "List the topics in a namespace",
				Flags: func(fs *flag.FlagSet) { fs.StringVar(&namespaceID, "namespace", "", "namespace name or id") },
				Run: func(ctx context.Context, app *App, args []string) error {
					if namespaceID == "" && len(args) > 0 {
						namespaceID = args[0]
					}
					if namespaceID == "" {
						return fmt.Errorf("--namespace is required")
					}
					namespaceID, err := idOf(app, ctx, "/api/ServiceBus/namespaces", "namespace", namespaceID)
					if err != nil {
						return err
					}
					return get(app, ctx,
						"/api/ServiceBus/namespaces/"+namespaceID+"/topics",
						"name", "subscriptionCount", "status")
				},
			},
			deleteByIDCommand("delete", "Delete a namespace", "/api/ServiceBus/namespaces"),
		},
	}
}

// ── stream analytics ────────────────────────────────────────────────────────

func streamJobTopic() Topic {
	var name, region, engine, id, query string
	return Topic{
		Name:    "streamjob",
		Summary: "Stream analytics jobs",
		Commands: []Command{
			{
				Name: "list", Summary: "List jobs",
				Run: func(ctx context.Context, app *App, args []string) error {
					return get(app, ctx, "/api/StreamAnalytics", "jobName", "status", "engine", "region")
				},
			},
			{
				Name: "create", Summary: "Create a job",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&name, "name", "", "job name")
					fs.StringVar(&region, "region", "", "region")
					fs.StringVar(&engine, "engine", "risingwave", "risingwave, flink or spark")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if name == "" {
						return fmt.Errorf("--name is required")
					}
					body := map[string]any{"jobName": name, "region": app.Region(region), "engine": engine}
					if _, err := post(app, ctx, "POST", "/api/StreamAnalytics", body); err != nil {
						return err
					}
					app.Print.Message("Job %q created.", name)
					return nil
				},
			},
			{
				Name: "bindings", Summary: "What a job can read from and write to",
				Run: func(ctx context.Context, app *App, args []string) error {
					return get(app, ctx, "/api/StreamAnalytics/bindings", "kind", "name", "topic", "region")
				},
			},
			jobLifecycle("start", "started", "Start a job", "start", &id),
			jobLifecycle("stop", "stopped", "Stop a job", "stop", &id),
			{
				Name: "query", Summary: "Set a job's query",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&id, "id", "", "job id")
					fs.StringVar(&query, "sql", "", "the query")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if id == "" || query == "" {
						return fmt.Errorf("--id and --sql are required")
					}
					path := "/api/StreamAnalytics/" + id + "/query"
					if _, err := post(app, ctx, "POST", path, map[string]any{"query": query}); err != nil {
						return err
					}
					app.Print.Message("Query saved.")
					return nil
				},
			},
			{
				Name: "status", Summary: "What a job is doing",
				Flags: func(fs *flag.FlagSet) { fs.StringVar(&id, "id", "", "job id") },
				Run: func(ctx context.Context, app *App, args []string) error {
					if id == "" && len(args) > 0 {
						id = args[0]
					}
					if id == "" {
						return fmt.Errorf("--id is required")
					}
					if err := app.RequireToken(); err != nil {
						return err
					}
					var resp map[string]any
					if err := app.Client.Do(ctx, "GET", "/api/StreamAnalytics/"+id+"/status", nil, &resp, true); err != nil {
						return err
					}
					data, _ := resp["data"].(map[string]any)
					return app.Print.Record(data)
				},
			},
			deleteByIDCommand("delete", "Delete a job", "/api/StreamAnalytics"),
		},
	}
}

// done is given rather than derived, for the reason lifecycle explains.
func jobLifecycle(verb, done, summary, action string, id *string) Command {
	return Command{
		Name: verb, Summary: summary,
		Flags: func(fs *flag.FlagSet) { fs.StringVar(id, "id", "", "job id") },
		Run: func(ctx context.Context, app *App, args []string) error {
			if *id == "" && len(args) > 0 {
				*id = args[0]
			}
			if *id == "" {
				return fmt.Errorf("--id is required")
			}
			if _, err := post(app, ctx, "POST", "/api/StreamAnalytics/"+*id+"/"+action, nil); err != nil {
				return err
			}
			app.Print.Message("Job %s.", done)
			return nil
		},
	}
}
