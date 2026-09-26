package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	hiok "github.com/HIOK-Official/hiok-sdk/go"
)

func topics() []Topic {
	return withDeployCommands([]Topic{
		authTopic(),
		apiTopic(),
		vmTopic(),
		vnetTopic(),
		containerAppTopic(),
		keyVaultTopic(),
		certificateTopic(),
		mongoTopic(),
		yugabyteTopic(),
		postgresTopic(),
		analyticsTopic(),
		vpnTopic(),
		iotTopic(),
		dpsTopic(),
		publicIPTopic(),
		dockerTopic(),
		swarmTopic(),
		kubernetesTopic(),
		registryTopic(),
		cacheTopic(),
		streamingTopic(),
		costTopic(),
		storageTopic(),
		relationalTopic("mysql", "MySQL servers", "/api/MySqlDatabase"),
		relationalTopic("sqlserver", "SQL Server instances", "/api/SqlServerDatabase"),
		serviceBusTopic(),
		streamJobTopic(),
	})
}

// ── auth ────────────────────────────────────────────────────────────────────

func authTopic() Topic {
	var email, password, endpoint, region, clientID, clientSecret string
	return Topic{
		Name:    "login",
		Summary: "Sign in and store a token",
		Commands: []Command{
			{
				Name:    "run",
				Summary: "Sign in with an email and password",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&email, "email", "", "account email")
					fs.StringVar(&password, "password", "", "account password (or set HIOK_PASSWORD)")
					fs.StringVar(&endpoint, "endpoint", "", "API endpoint, e.g. https://hiokcloud.com")
					fs.StringVar(&region, "region", "", "default region for commands that need one")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if endpoint != "" {
						app.Config.Endpoint = endpoint
						app.Client = hiok.New(endpoint, "")
					}
					if password == "" {
						password = os.Getenv("HIOK_PASSWORD")
					}
					if email == "" || password == "" {
						return fmt.Errorf("--email and --password are required (password may come from HIOK_PASSWORD)")
					}

					if err := app.Client.Login(ctx, email, password); err != nil {
						return err
					}

					app.Config.Email = email
					app.Config.Token = app.Client.Token
					if region != "" {
						app.Config.Region = region
					}
					if err := SaveConfig(app.Config); err != nil {
						return fmt.Errorf("signed in, but the token could not be saved: %w", err)
					}
					app.Print.Message("Signed in as %s. Token saved to %s.", email, ConfigPath())
					return nil
				},
			},
			{
				Name:    "sp",
				Summary: "Sign in as a service principal (client ID and secret) — for CI/CD",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&clientID, "client-id", "", "service principal client ID (or HIOK_CLIENT_ID)")
					fs.StringVar(&clientSecret, "client-secret", "", "client secret (or HIOK_CLIENT_SECRET)")
					fs.StringVar(&endpoint, "endpoint", "", "API endpoint, e.g. https://hiokcloud.com")
					fs.StringVar(&region, "region", "", "default region for commands that need one")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if endpoint != "" {
						app.Config.Endpoint = endpoint
						app.Client = hiok.New(endpoint, "")
					}
					if clientID == "" {
						clientID = os.Getenv("HIOK_CLIENT_ID")
					}
					if clientSecret == "" {
						clientSecret = os.Getenv("HIOK_CLIENT_SECRET")
					}
					if clientID == "" || clientSecret == "" {
						return fmt.Errorf("--client-id and --client-secret are required (or HIOK_CLIENT_ID / HIOK_CLIENT_SECRET)")
					}
					if err := app.Client.LoginServicePrincipal(ctx, clientID, clientSecret); err != nil {
						return err
					}
					// The token lasts an hour. Pipelines should prefer the environment
					// variables, which renew it; the stored token suits a short job.
					app.Config.Email = "service principal " + clientID
					app.Config.Token = app.Client.Token
					if region != "" {
						app.Config.Region = region
					}
					if err := SaveConfig(app.Config); err != nil {
						return fmt.Errorf("signed in, but the token could not be saved: %w", err)
					}
					app.Print.Message("Signed in as service principal %s (token valid for one hour). Saved to %s.", clientID, ConfigPath())
					return nil
				},
			},
			{
				Name:    "status",
				Summary: "Show the endpoint and account currently configured",
				Run: func(ctx context.Context, app *App, args []string) error {
					return app.Print.Record(map[string]any{
						"endpoint":   app.Config.Endpoint,
						"email":      app.Config.Email,
						"region":     app.Region(""),
						"signedIn":   app.Config.Token != "",
						"configFile": ConfigPath(),
					}, "endpoint", "email", "region", "signedIn", "configFile")
				},
			},
			{
				Name:    "logout",
				Summary: "Forget the stored token",
				Run: func(ctx context.Context, app *App, args []string) error {
					app.Config.Token = ""
					if err := SaveConfig(app.Config); err != nil {
						return err
					}
					app.Print.Message("Token removed from %s.", ConfigPath())
					return nil
				},
			},
		},
	}
}

// ── virtual machines ────────────────────────────────────────────────────────

func vmTopic() Topic {
	var name, region, image string
	var vcpus int
	var ramGb float64
	return Topic{
		Name:    "vm",
		Summary: "Virtual machines",
		Commands: []Command{
			{
				Name: "list", Summary: "List virtual machines",
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					vms, err := app.Client.VirtualMachines(ctx)
					if err != nil {
						return err
					}
					return app.Print.Table(vms, "name", "state", "vcpus", "memory", "ipAddress")
				},
			},
			{
				Name: "create", Summary: "Create a virtual machine",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&name, "name", "", "machine name")
					fs.StringVar(&region, "region", "", "region")
					fs.StringVar(&image, "image", "ubuntu-24.04", "base image")
					fs.IntVar(&vcpus, "vcpus", 1, "virtual CPUs")
					fs.Float64Var(&ramGb, "ram", 1, "memory in GB")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if name == "" {
						return fmt.Errorf("--name is required")
					}
					if err := app.Client.CreateVirtualMachine(ctx, name, app.Region(region), image, vcpus, ramGb); err != nil {
						return err
					}
					app.Print.Message("Machine %q requested in %s.", name, app.Region(region))
					return nil
				},
			},
		},
	}
}

// ── virtual networks ────────────────────────────────────────────────────────

func vnetTopic() Topic {
	var name, addressSpace, region, forwardMode string
	return Topic{
		Name:    "vnet",
		Summary: "Virtual networks",
		Commands: []Command{
			{
				Name: "list", Summary: "List virtual networks",
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					nets, err := app.Client.VirtualNetworks(ctx)
					if err != nil {
						return err
					}
					return app.Print.Table(nets, "name", "id", "region", "status")
				},
			},
			{
				Name: "create", Summary: "Create a virtual network",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&name, "name", "", "network name")
					fs.StringVar(&addressSpace, "address-space", "10.20.0.0/16", "CIDR for the network")
					fs.StringVar(&region, "region", "", "region")
					fs.StringVar(&forwardMode, "forward-mode", "nat", "nat or route")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if name == "" {
						return fmt.Errorf("--name is required")
					}
					result, err := app.Client.CreateVirtualNetwork(ctx, hiok.CreateVirtualNetworkRequest{
						Name: name, AddressSpace: addressSpace,
						Region: app.Region(region), ForwardMode: forwardMode,
					})
					if err != nil {
						return err
					}
					return app.Print.Record(result)
				},
			},
		},
	}
}

// ── container apps ──────────────────────────────────────────────────────────

func containerAppTopic() Topic {
	var name, image, region, environmentID string
	var targetPort int
	var vcpu, ramGb float64
	var external bool
	var envVars stringSlice
	return Topic{
		Name:    "app",
		Summary: "Container apps and their environments",
		Commands: []Command{
			{
				Name: "list", Summary: "List container apps",
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					apps, err := app.Client.ContainerApps(ctx)
					if err != nil {
						return err
					}
					return app.Print.Table(apps, "name", "image", "status", "hostPort", "externalIngress", "defaultHostname")
				},
			},
			{
				Name: "create", Summary: "Create a container app",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&name, "name", "", "app name")
					fs.StringVar(&image, "image", "", "container image")
					fs.StringVar(&region, "region", "", "region")
					fs.StringVar(&environmentID, "environment", "", "environment id to join")
					fs.IntVar(&targetPort, "port", 80, "port the app listens on")
					fs.Float64Var(&vcpu, "vcpu", 0.5, "virtual CPUs")
					fs.Float64Var(&ramGb, "ram", 1, "memory in GB")
					fs.BoolVar(&external, "external", true, "publish a host port; false keeps it inside its environment")
					fs.Var(&envVars, "env", "environment variable KEY=VALUE (repeatable)")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if name == "" || image == "" {
						return fmt.Errorf("--name and --image are required")
					}
					result, err := app.Client.CreateContainerApp(ctx, hiok.CreateContainerAppRequest{
						Name: name, Image: image, Region: app.Region(region),
						EnvironmentID: environmentID, TargetPort: targetPort,
						VCpu: vcpu, RamGb: ramGb, ExternalIngress: external,
						Environment: splitKeyValues(envVars),
					})
					if err != nil {
						return err
					}
					return app.Print.Record(result)
				},
			},
			{
				Name: "delete", Summary: "Delete a container app by id",
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if len(args) == 0 {
						return fmt.Errorf("usage: hiok app delete <app-id>")
					}
					if err := app.Client.DeleteContainerApp(ctx, args[0]); err != nil {
						return err
					}
					app.Print.Message("App deleted.")
					return nil
				},
			},
			{
				Name: "env-list", Summary: "List container app environments",
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					envs, err := app.Client.ContainerAppEnvironments(ctx)
					if err != nil {
						return err
					}
					return app.Print.Table(envs, "name", "id", "region", "status", "networkName")
				},
			},
			{
				Name: "env-create", Summary: "Create a container app environment",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&name, "name", "", "environment name")
					fs.StringVar(&region, "region", "", "region")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if name == "" {
						return fmt.Errorf("--name is required")
					}
					result, err := app.Client.CreateContainerAppEnvironment(ctx, hiok.CreateContainerAppEnvironmentRequest{
						Name: name, Region: app.Region(region),
					})
					if err != nil {
						return err
					}
					return app.Print.Record(result)
				},
			},
		},
	}
}

// stringSlice collects a repeatable flag.
type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ",") }
func (s *stringSlice) Set(v string) error {
	*s = append(*s, v)
	return nil
}
