package cli

import (
	"context"
	"flag"
	"fmt"

	hiok "github.com/HIOK-Official/hiok-sdk/go"
)

// ── MongoDB ─────────────────────────────────────────────────────────────────

func mongoTopic() Topic {
	var name, regions, consistency, sku, database string
	var staleness, storageGb int
	var assumeYes bool
	return Topic{
		Name:    "mongo",
		Summary: "MongoDB clusters",
		Commands: []Command{
			{
				Name: "list", Summary: "List clusters",
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					clusters, err := app.Client.MongoClusters(ctx)
					if err != nil {
						return err
					}
					return app.Print.Table(clusters, "clusterName", "id", "status", "consistencyLabel", "isReplicaSet", "engineVersion")
				},
			},
			{
				Name: "create", Summary: "Create a cluster",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&name, "name", "", "cluster name")
					fs.StringVar(&regions, "regions", "", "comma-separated regions; several make it a replica set")
					fs.StringVar(&consistency, "consistency", "strong", "strong, session, bounded or eventual")
					fs.IntVar(&staleness, "max-staleness", 0, "staleness bound in seconds, for bounded reads")
					fs.StringVar(&sku, "sku", "small", "dev, small, medium or large")
					fs.IntVar(&storageGb, "storage", 20, "storage in GB")
					fs.StringVar(&database, "database", "", "default database")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if name == "" {
						return fmt.Errorf("--name is required")
					}
					list := splitList(regions)
					if len(list) == 0 {
						list = []string{app.Region("")}
					}
					result, err := app.Client.CreateMongoCluster(ctx, hiok.CreateMongoClusterRequest{
						ClusterName: name, Regions: list, Region: list[0],
						Consistency: consistency, MaxStalenessSeconds: staleness,
						Sku: sku, StorageGb: storageGb, DatabaseName: database,
					})
					if err != nil {
						return err
					}
					return app.Print.Record(result, "clusterName", "id", "status", "statusMessage",
						"consistencyLabel", "readConcern", "writeConcern", "readPreference", "isReplicaSet")
				},
			},
			{
				Name: "delete", Summary: "Delete a cluster",
				Flags: func(fs *flag.FlagSet) {
					fs.BoolVar(&assumeYes, "yes", false, "skip the confirmation prompt")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if len(args) == 0 {
						return fmt.Errorf("usage: hiok mongo delete <cluster-id>")
					}
					if !confirm(fmt.Sprintf("Delete cluster %s? Every document in it is destroyed.", args[0]), assumeYes) {
						app.Print.Message("Nothing was deleted.")
						return nil
					}
					if err := app.Client.DeleteMongoCluster(ctx, args[0]); err != nil {
						return err
					}
					app.Print.Message("Cluster deleted.")
					return nil
				},
			},
			{
				Name: "connection", Summary: "Show the connection string",
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if len(args) == 0 {
						return fmt.Errorf("usage: hiok mongo connection <cluster-id>")
					}
					conn, err := app.Client.MongoConnection(ctx, args[0])
					if err != nil {
						return err
					}
					return app.Print.Record(conn, "connectionString", "host", "port", "username", "replicaSetName", "consistencyOptions")
				},
			},
			{
				Name: "run", Summary: "Run a database command",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&database, "database", "", "database to run against")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if len(args) < 2 {
						return fmt.Errorf(`usage: hiok mongo run <cluster-id> '{"find":"orders","limit":10}'`)
					}
					result, err := app.Client.RunMongoCommand(ctx, args[0], database, args[1])
					if err != nil {
						return err
					}
					return app.Print.Record(result, "isSuccess", "elapsedMs", "result", "error")
				},
			},
			{
				Name: "status", Summary: "Show live replica set membership",
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if len(args) == 0 {
						return fmt.Errorf("usage: hiok mongo status <cluster-id>")
					}
					members, err := app.Client.MongoReplicaStatus(ctx, args[0])
					if err != nil {
						return err
					}
					return app.Print.Table(members, "name", "state", "regionId", "health")
				},
			},
			{
				Name: "consistency", Summary: "Change the consistency level",
				Flags: func(fs *flag.FlagSet) {
					fs.IntVar(&staleness, "max-staleness", 0, "staleness bound in seconds")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if len(args) < 2 {
						return fmt.Errorf("usage: hiok mongo consistency <cluster-id> <strong|session|bounded|eventual>")
					}
					result, err := app.Client.SetMongoConsistency(ctx, args[0], args[1], staleness)
					if err != nil {
						return err
					}
					app.Print.Message("Existing connections keep their current settings until they reconnect.")
					return app.Print.Record(result, "consistencyLabel", "readConcern", "writeConcern", "readPreference")
				},
			},
		},
	}
}

// ── YugabyteDB ──────────────────────────────────────────────────────────────

func yugabyteTopic() Topic {
	var name, regions, sku, database string
	var storageGb int
	var assumeYes bool
	return Topic{
		Name:    "yugabyte",
		Summary: "YugabyteDB clusters",
		Commands: []Command{
			{
				Name: "list", Summary: "List clusters",
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					clusters, err := app.Client.YugabyteClusters(ctx)
					if err != nil {
						return err
					}
					return app.Print.Table(clusters, "clusterName", "id", "status", "replicationFactor", "engineVersion", "ysqlPort")
				},
			},
			{
				Name: "create", Summary: "Create a cluster",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&name, "name", "", "cluster name")
					fs.StringVar(&regions, "regions", "", "comma-separated regions; one node each")
					fs.StringVar(&sku, "sku", "small", "dev, small, medium or large")
					fs.IntVar(&storageGb, "storage", 20, "storage in GB")
					fs.StringVar(&database, "database", "", "default database")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if name == "" {
						return fmt.Errorf("--name is required")
					}
					list := splitList(regions)
					if len(list) == 0 {
						list = []string{app.Region("")}
					}
					result, err := app.Client.CreateYugabyteCluster(ctx, hiok.CreateYugabyteClusterRequest{
						ClusterName: name, Regions: list, Region: list[0],
						Sku: sku, StorageGb: storageGb, DatabaseName: database,
					})
					if err != nil {
						return err
					}
					return app.Print.Record(result, "clusterName", "id", "status", "statusMessage", "replicationFactor", "host", "ysqlPort")
				},
			},
			{
				Name: "delete", Summary: "Delete a cluster",
				Flags: func(fs *flag.FlagSet) {
					fs.BoolVar(&assumeYes, "yes", false, "skip the confirmation prompt")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if len(args) == 0 {
						return fmt.Errorf("usage: hiok yugabyte delete <cluster-id>")
					}
					if !confirm(fmt.Sprintf("Delete cluster %s? Its data is destroyed.", args[0]), assumeYes) {
						app.Print.Message("Nothing was deleted.")
						return nil
					}
					if err := app.Client.DeleteYugabyteCluster(ctx, args[0]); err != nil {
						return err
					}
					app.Print.Message("Cluster deleted.")
					return nil
				},
			},
			{
				Name: "query", Summary: "Run SQL",
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if len(args) < 2 {
						return fmt.Errorf(`usage: hiok yugabyte query <cluster-id> "select 1"`)
					}
					result, err := app.Client.YugabyteQuery(ctx, args[0], args[1], 1000)
					if err != nil {
						return err
					}
					return app.Print.Record(result, "isSuccess", "rowCount", "affected", "elapsedMs", "error")
				},
			},
			{
				Name: "tables", Summary: "List tables",
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if len(args) == 0 {
						return fmt.Errorf("usage: hiok yugabyte tables <cluster-id>")
					}
					tables, err := app.Client.YugabyteTables(ctx, args[0])
					if err != nil {
						return err
					}
					return app.Print.Table(tables, "schema", "name", "type")
				},
			},
			{
				Name: "connection", Summary: "Show connection details",
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if len(args) == 0 {
						return fmt.Errorf("usage: hiok yugabyte connection <cluster-id>")
					}
					conn, err := app.Client.YugabyteConnection(ctx, args[0])
					if err != nil {
						return err
					}
					return app.Print.Record(conn)
				},
			},
		},
	}
}

// ── PostgreSQL ──────────────────────────────────────────────────────────────

func postgresTopic() Topic {
	var name, region, version, sku, database, adminUser string
	var storageGb int
	var assumeYes bool
	return Topic{
		Name:    "postgres",
		Summary: "PostgreSQL servers",
		Commands: []Command{
			{
				Name: "list", Summary: "List servers",
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					servers, err := app.Client.PostgresDatabases(ctx)
					if err != nil {
						return err
					}
					return app.Print.Table(servers, "serverName", "id", "status", "postgresVersion", "host", "port")
				},
			},
			{
				Name: "create", Summary: "Create a server",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&name, "name", "", "server name")
					fs.StringVar(&region, "region", "", "region")
					fs.StringVar(&version, "version", "16", "PostgreSQL major version")
					fs.StringVar(&sku, "sku", "Burstable_B1ms", "size")
					fs.IntVar(&storageGb, "storage", 32, "storage in GB")
					fs.StringVar(&database, "database", "", "initial database")
					fs.StringVar(&adminUser, "admin", "pgadmin", "administrator username")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if name == "" {
						return fmt.Errorf("--name is required")
					}
					result, err := app.Client.CreatePostgres(ctx, hiok.CreatePostgresRequest{
						ServerName: name, Region: app.Region(region), PostgresVersion: version,
						Sku: sku, StorageGb: storageGb, DatabaseName: database, AdminUsername: adminUser,
					})
					if err != nil {
						return err
					}
					return app.Print.Record(result, "serverName", "id", "status", "host", "port", "adminUsername", "databaseName")
				},
			},
			{
				Name: "delete", Summary: "Delete a server",
				Flags: func(fs *flag.FlagSet) {
					fs.BoolVar(&assumeYes, "yes", false, "skip the confirmation prompt")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if len(args) == 0 {
						return fmt.Errorf("usage: hiok postgres delete <database-id>")
					}
					if !confirm(fmt.Sprintf("Delete server %s? Its data is destroyed.", args[0]), assumeYes) {
						app.Print.Message("Nothing was deleted.")
						return nil
					}
					if err := app.Client.DeletePostgres(ctx, args[0]); err != nil {
						return err
					}
					app.Print.Message("Server deleted.")
					return nil
				},
			},
		},
	}
}

// ── ClickHouse ──────────────────────────────────────────────────────────────

func analyticsTopic() Topic {
	return Topic{
		Name:    "analytics",
		Summary: "ClickHouse clusters",
		Commands: []Command{
			{
				Name: "list", Summary: "List clusters",
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					clusters, err := app.Client.AnalyticsClusters(ctx)
					if err != nil {
						return err
					}
					return app.Print.Table(clusters, "clusterName", "id", "status", "region", "httpPort")
				},
			},
			{
				Name: "query", Summary: "Run SQL against a cluster",
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if len(args) < 2 {
						return fmt.Errorf(`usage: hiok analytics query <cluster-id> "select 1"`)
					}
					result, err := app.Client.AnalyticsQuery(ctx, args[0], args[1], 1000)
					if err != nil {
						return err
					}
					return app.Print.Record(result)
				},
			},
			{
				Name: "tables", Summary: "List tables",
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if len(args) == 0 {
						return fmt.Errorf("usage: hiok analytics tables <cluster-id>")
					}
					tables, err := app.Client.AnalyticsTables(ctx, args[0])
					if err != nil {
						return err
					}
					return app.Print.Table(tables)
				},
			},
		},
	}
}
