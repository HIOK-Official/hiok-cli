package cli

import (
	"context"
	"flag"
	"fmt"
	"strings"
)

// HIOK ID from the command line: who can work in the account and how — people,
// groups, service principals, app registrations, tenants, sign-ins — and the
// person's own API keys.

func idTopic() Topic {
	var role, description, cidrs, redirects, audience, kind string
	var days int
	var enabled string
	return Topic{
		Name:    "id",
		Summary: "HIOK ID: people, groups, service principals, apps, tenants, sign-ins",
		Commands: []Command{
			{
				Name: "users", Summary: "People in this account's directory",
				Run: func(ctx context.Context, app *App, args []string) error {
					return get(app, ctx, "/api/hiok-id/users", "email", "name", "role", "effectiveRole", "status", "lastSignInAt")
				},
			},
			{
				Name: "invite", Summary: "Invite someone by email with a role (owner, contributor, reader)",
				Flags: func(fs *flag.FlagSet) { fs.StringVar(&role, "role", "reader", "owner, contributor or reader") },
				Run: func(ctx context.Context, app *App, args []string) error {
					if len(args) != 1 {
						return fmt.Errorf("usage: hiok id invite <email> [--role reader]")
					}
					resp, err := post(app, ctx, "POST", "/api/hiok-id/users", map[string]any{"email": args[0], "role": role})
					if err != nil {
						return err
					}
					app.Print.Message("%s", str(resp, "message"))
					return nil
				},
			},
			{
				Name: "remove", Summary: "Remove someone's access",
				Run: func(ctx context.Context, app *App, args []string) error {
					if len(args) != 1 {
						return fmt.Errorf("usage: hiok id remove <email>")
					}
					u, err := resolve(app, ctx, "/api/hiok-id/users", "person", args[0], "email")
					if err != nil {
						return err
					}
					resp, err := post(app, ctx, "DELETE", "/api/hiok-id/users/"+str(u, "id"), nil)
					if err != nil {
						return err
					}
					app.Print.Message("%s", str(resp, "message"))
					return nil
				},
			},
			{
				Name: "groups", Summary: "Groups and their role",
				Run: func(ctx context.Context, app *App, args []string) error {
					return get(app, ctx, "/api/hiok-id/groups", "name", "role", "description")
				},
			},
			{
				Name: "group-create", Summary: "Create a group (--role gives every member that role)",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&role, "role", "", "owner, contributor, reader, or empty")
					fs.StringVar(&description, "description", "", "description")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if len(args) != 1 {
						return fmt.Errorf("usage: hiok id group-create <name> [--role contributor]")
					}
					_, err := post(app, ctx, "POST", "/api/hiok-id/groups", map[string]any{"name": args[0], "role": role, "description": description})
					if err == nil {
						app.Print.Message("Group %q created.", args[0])
					}
					return err
				},
			},
			{
				Name: "group-add", Summary: "Add a person (email) or service principal (client id) to a group",
				Flags: func(fs *flag.FlagSet) { fs.StringVar(&kind, "kind", "", "user or serviceprincipal (default: guessed)") },
				Run: func(ctx context.Context, app *App, args []string) error {
					if len(args) != 2 {
						return fmt.Errorf("usage: hiok id group-add <group> <email-or-client-id>")
					}
					g, err := resolve(app, ctx, "/api/hiok-id/groups", "group", args[0], "name")
					if err != nil {
						return err
					}
					k := kind
					if k == "" {
						k = "serviceprincipal"
						if strings.Contains(args[1], "@") {
							k = "user"
						}
					}
					_, err = post(app, ctx, "POST", "/api/hiok-id/groups/"+str(g, "id")+"/members", map[string]any{"kind": k, "ref": args[1]})
					if err == nil {
						app.Print.Message("Added.")
					}
					return err
				},
			},
			{
				Name: "group-delete", Summary: "Delete a group",
				Run: func(ctx context.Context, app *App, args []string) error {
					if len(args) != 1 {
						return fmt.Errorf("usage: hiok id group-delete <group>")
					}
					g, err := resolve(app, ctx, "/api/hiok-id/groups", "group", args[0], "name")
					if err != nil {
						return err
					}
					_, err = post(app, ctx, "DELETE", "/api/hiok-id/groups/"+str(g, "id"), nil)
					if err == nil {
						app.Print.Message("Deleted.")
					}
					return err
				},
			},
			{
				Name: "sp", Summary: "Service principals",
				Run: func(ctx context.Context, app *App, args []string) error {
					return get(app, ctx, "/api/hiok-id/service-principals", "name", "clientId", "role", "effectiveRole", "enabled", "lastSignInAt")
				},
			},
			{
				Name: "sp-create", Summary: "Create a service principal; prints its client ID and secret once",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&role, "role", "contributor", "owner, contributor or reader")
					fs.StringVar(&cidrs, "allowed-ips", "", "comma-separated CIDRs it may sign in from")
					fs.StringVar(&description, "description", "", "description")
					fs.IntVar(&days, "secret-days", 180, "secret lifetime in days (1–730)")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if len(args) != 1 {
						return fmt.Errorf("usage: hiok id sp-create <name> [--role contributor] [--allowed-ips 203.0.113.0/24]")
					}
					resp, err := post(app, ctx, "POST", "/api/hiok-id/service-principals", map[string]any{
						"name": args[0], "role": role, "description": description, "allowedCidrs": splitList(cidrs), "secretExpiresInDays": days,
					})
					if err != nil {
						return err
					}
					d := dataOf(resp)
					setOutput("client_id", str(d, "clientId"), false)
					setOutput("client_secret", str(d, "clientSecret"), true)
					return app.Print.Record(map[string]any{"clientId": str(d, "clientId"), "clientSecret": str(d, "clientSecret"), "expires": str(d, "secretExpiresAt")},
						"clientId", "clientSecret", "expires")
				},
			},
			{
				Name: "sp-update", Summary: "Change a service principal's role, IP limits or on/off",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&role, "role", "", "owner, contributor or reader")
					fs.StringVar(&cidrs, "allowed-ips", "", "comma-separated CIDRs; \"none\" to allow anywhere")
					fs.StringVar(&enabled, "enabled", "", "true or false")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if len(args) != 1 {
						return fmt.Errorf("usage: hiok id sp-update <name> [--role reader] [--enabled false]")
					}
					p, err := resolve(app, ctx, "/api/hiok-id/service-principals", "service principal", args[0], "name", "clientId")
					if err != nil {
						return err
					}
					body := map[string]any{}
					if role != "" {
						body["role"] = role
					}
					if cidrs == "none" {
						body["allowedCidrs"] = []string{}
					} else if cidrs != "" {
						body["allowedCidrs"] = splitList(cidrs)
					}
					if enabled != "" {
						body["enabled"] = enabled == "true"
					}
					_, err = post(app, ctx, "PUT", "/api/hiok-id/service-principals/"+str(p, "id"), body)
					if err == nil {
						app.Print.Message("Saved.")
					}
					return err
				},
			},
			{
				Name: "sp-secret", Summary: "Add a secret to a service principal (rotation); prints it once",
				Flags: func(fs *flag.FlagSet) {
					fs.IntVar(&days, "days", 180, "lifetime in days (1–730)")
					fs.StringVar(&description, "description", "", "what uses it")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if len(args) != 1 {
						return fmt.Errorf("usage: hiok id sp-secret <name> [--days 90]")
					}
					p, err := resolve(app, ctx, "/api/hiok-id/service-principals", "service principal", args[0], "name", "clientId")
					if err != nil {
						return err
					}
					resp, err := post(app, ctx, "POST", "/api/hiok-id/service-principals/"+str(p, "id")+"/credentials", map[string]any{"expiresInDays": days, "description": description})
					if err != nil {
						return err
					}
					d := dataOf(resp)
					setOutput("client_secret", str(d, "clientSecret"), true)
					return app.Print.Record(map[string]any{"clientId": str(p, "clientId"), "clientSecret": str(d, "clientSecret"), "expires": str(d, "expiresAt")},
						"clientId", "clientSecret", "expires")
				},
			},
			{
				Name: "sp-delete", Summary: "Delete a service principal (its tokens stop working)",
				Run: func(ctx context.Context, app *App, args []string) error {
					if len(args) != 1 {
						return fmt.Errorf("usage: hiok id sp-delete <name>")
					}
					p, err := resolve(app, ctx, "/api/hiok-id/service-principals", "service principal", args[0], "name", "clientId")
					if err != nil {
						return err
					}
					_, err = post(app, ctx, "DELETE", "/api/hiok-id/service-principals/"+str(p, "id"), nil)
					if err == nil {
						app.Print.Message("Deleted.")
					}
					return err
				},
			},
			{
				Name: "apps", Summary: "App registrations (Sign in with HIOK)",
				Run: func(ctx context.Context, app *App, args []string) error {
					return get(app, ctx, "/api/hiok-id/apps", "name", "clientId", "signInAudience", "enabled")
				},
			},
			{
				Name: "app-create", Summary: "Register an app for Sign in with HIOK; prints its client ID and secret once",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&redirects, "redirect-uri", "", "comma-separated redirect URIs (https, or http://localhost)")
					fs.StringVar(&audience, "audience", "directory", "directory (this account's people) or any (every HIOK account)")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if len(args) != 1 || redirects == "" {
						return fmt.Errorf("usage: hiok id app-create <name> --redirect-uri https://app.example.com/callback")
					}
					resp, err := post(app, ctx, "POST", "/api/hiok-id/apps", map[string]any{"name": args[0], "redirectUris": splitList(redirects), "signInAudience": audience})
					if err != nil {
						return err
					}
					d := dataOf(resp)
					return app.Print.Record(map[string]any{"client_id": str(d, "clientId"), "client_secret": str(d, "clientSecret"),
						"issuer": strings.TrimRight(app.Config.Endpoint, "/") + "/api/hiok-id/oidc"}, "client_id", "client_secret", "issuer")
				},
			},
			{
				Name: "tenants", Summary: "Accounts you can work in: yours, tenants, and ones shared with you",
				Run: func(ctx context.Context, app *App, args []string) error {
					resp, err := post(app, ctx, "GET", "/api/hiok-id/directories", nil)
					if err != nil {
						return err
					}
					d := dataOf(resp)
					var rows []map[string]any
					if list, ok := d["memberships"].([]any); ok {
						for _, m := range list {
							if r, ok := m.(map[string]any); ok {
								rows = append(rows, r)
							}
						}
					}
					return app.Print.Table(rows, "name", "account", "role", "isTenant")
				},
			},
			{
				Name: "tenant-create", Summary: "Create a tenant: a separate account you own",
				Run: func(ctx context.Context, app *App, args []string) error {
					if len(args) != 1 {
						return fmt.Errorf("usage: hiok id tenant-create \"Acme Production\"")
					}
					resp, err := post(app, ctx, "POST", "/api/hiok-id/tenants", map[string]any{"name": args[0]})
					if err != nil {
						return err
					}
					app.Print.Message("%s (%s)", str(resp, "message"), str(dataOf(resp), "account"))
					return nil
				},
			},
			{
				Name: "sign-ins", Summary: "Who signed in to this account, from where, allowed or refused",
				Run: func(ctx context.Context, app *App, args []string) error {
					return get(app, ctx, "/api/hiok-id/sign-ins?take=100", "at", "actorKind", "actorName", "actor", "ip", "outcome", "detail")
				},
			},
		},
	}
}

func apiKeyTopic() Topic {
	var role string
	var days int
	return Topic{
		Name:    "apikey",
		Summary: "Your personal API keys (use one as HIOK_TOKEN)",
		Commands: []Command{
			{
				Name: "list", Summary: "List your API keys",
				Run: func(ctx context.Context, app *App, args []string) error {
					return get(app, ctx, "/api/account/api-keys", "name", "hint", "role", "expiresAt", "lastUsedAt")
				},
			},
			{
				Name: "create", Summary: "Create an API key; prints it once",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&role, "role", "contributor", "owner, contributor or reader")
					fs.IntVar(&days, "days", 90, "lifetime in days (0: no expiry)")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if len(args) != 1 {
						return fmt.Errorf("usage: hiok apikey create <name> [--role reader] [--days 30]")
					}
					body := map[string]any{"name": args[0], "role": role}
					if days > 0 {
						body["expiresInDays"] = days
					}
					resp, err := post(app, ctx, "POST", "/api/account/api-keys", body)
					if err != nil {
						return err
					}
					d := dataOf(resp)
					setOutput("api_key", str(d, "key"), true)
					return app.Print.Record(map[string]any{"apiKey": str(d, "key"), "expires": str(d, "expiresAt")}, "apiKey", "expires")
				},
			},
			{
				Name: "revoke", Summary: "Revoke an API key by name",
				Run: func(ctx context.Context, app *App, args []string) error {
					if len(args) != 1 {
						return fmt.Errorf("usage: hiok apikey revoke <name>")
					}
					k, err := resolve(app, ctx, "/api/account/api-keys", "API key", args[0], "name")
					if err != nil {
						return err
					}
					_, err = post(app, ctx, "DELETE", "/api/account/api-keys/"+str(k, "id"), nil)
					if err == nil {
						app.Print.Message("Revoked.")
					}
					return err
				},
			},
		},
	}
}
