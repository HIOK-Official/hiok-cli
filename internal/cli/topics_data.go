package cli

import (
	"context"
	"flag"
	"fmt"
	"os"

	hiok "github.com/HIOK-Official/hiok-sdk/go"
)

// ── key vault ───────────────────────────────────────────────────────────────

func keyVaultTopic() Topic {
	var name, region, regions, vnetID, value string
	var retention int
	var purgeProtection, generate, deleted, assumeYes bool
	var size int
	return Topic{
		Name:    "keyvault",
		Summary: "Key vaults and their secrets",
		Commands: []Command{
			{
				Name: "list", Summary: "List vaults",
				Flags: func(fs *flag.FlagSet) {
					fs.BoolVar(&deleted, "deleted", false, "list soft-deleted vaults instead")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					var vaults []map[string]any
					var err error
					if deleted {
						vaults, err = app.Client.DeletedKeyVaults(ctx)
					} else {
						vaults, err = app.Client.KeyVaults(ctx)
					}
					if err != nil {
						return err
					}
					if deleted {
						return app.Print.Table(vaults, "name", "id", "deletedTime", "daysUntilPurge", "purgeProtection")
					}
					return app.Print.Table(vaults, "name", "id", "status", "primaryRegion", "itemCount", "networkRestricted", "purgeProtection")
				},
			},
			{
				Name: "create", Summary: "Create a vault",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&name, "name", "", "vault name")
					fs.StringVar(&region, "region", "", "primary region")
					fs.StringVar(&regions, "regions", "", "comma-separated replica regions")
					fs.StringVar(&vnetID, "vnet", "", "restrict to this virtual network id")
					fs.IntVar(&retention, "retention-days", 90, "soft-delete retention, 7-365")
					fs.BoolVar(&purgeProtection, "purge-protection", false, "prevent early purge; cannot be turned off later")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if name == "" {
						return fmt.Errorf("--name is required")
					}
					result, err := app.Client.CreateKeyVault(ctx, hiok.CreateKeyVaultRequest{
						Name: name, PrimaryRegion: app.Region(region), Regions: splitList(regions),
						VnetID: vnetID, SoftDeleteRetentionDays: retention, PurgeProtection: purgeProtection,
					})
					if err != nil {
						return err
					}
					return app.Print.Record(result)
				},
			},
			{
				Name: "delete", Summary: "Soft-delete a vault (recoverable)",
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if len(args) == 0 {
						return fmt.Errorf("usage: hiok keyvault delete <vault-id>")
					}
					if err := app.Client.DeleteKeyVault(ctx, args[0]); err != nil {
						return err
					}
					app.Print.Message("Vault soft-deleted; it stays recoverable until its retention window ends.")
					return nil
				},
			},
			{
				Name: "recover", Summary: "Recover a soft-deleted vault",
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if len(args) == 0 {
						return fmt.Errorf("usage: hiok keyvault recover <vault-id>")
					}
					result, err := app.Client.RecoverKeyVault(ctx, args[0])
					if err != nil {
						return err
					}
					return app.Print.Record(result)
				},
			},
			{
				Name: "purge", Summary: "Permanently destroy a soft-deleted vault",
				Flags: func(fs *flag.FlagSet) {
					fs.BoolVar(&assumeYes, "yes", false, "skip the confirmation prompt")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if len(args) == 0 {
						return fmt.Errorf("usage: hiok keyvault purge <vault-id>")
					}
					// Irreversible, so it asks — unless output is being piped, where a
					// prompt nobody can answer would just hang.
					if !confirm(fmt.Sprintf("Purge vault %s and everything in it? This cannot be undone.", args[0]), assumeYes) {
						app.Print.Message("Nothing was purged.")
						return nil
					}
					if err := app.Client.PurgeKeyVault(ctx, args[0]); err != nil {
						return err
					}
					app.Print.Message("Vault purged.")
					return nil
				},
			},
			{
				Name: "items", Summary: "List items in a vault",
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if len(args) == 0 {
						return fmt.Errorf("usage: hiok keyvault items <vault-id>")
					}
					items, err := app.Client.KeyVaultItems(ctx, args[0])
					if err != nil {
						return err
					}
					return app.Print.Table(items, "name", "itemType", "enabled", "isUsable", "isPending", "expiresOn")
				},
			},
			{
				Name: "set", Summary: "Write a secret or key (a new version if the name exists)",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&value, "value", "", "the value to store")
					fs.BoolVar(&generate, "generate", false, "generate a random value instead")
					fs.IntVar(&size, "size", 0, "entropy in bytes, or key size in bits")
					fs.StringVar(&name, "type", "secret", "secret or key")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if len(args) < 2 {
						return fmt.Errorf("usage: hiok keyvault set <vault-id> <item-name> [--value X | --generate]")
					}
					if !generate && value == "" {
						return fmt.Errorf("pass --value, or --generate to have one made")
					}
					result, err := app.Client.SetSecret(ctx, args[0], hiok.SetSecretRequest{
						Name: args[1], ItemType: name, Value: value, Generate: generate, Size: size,
					})
					if err != nil {
						return err
					}
					return app.Print.Record(result)
				},
			},
			{
				Name: "get", Summary: "Read one item's value",
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if len(args) < 2 {
						return fmt.Errorf("usage: hiok keyvault get <vault-id> <item-name> [version]")
					}
					version := ""
					if len(args) > 2 {
						version = args[2]
					}
					item, err := app.Client.GetSecret(ctx, args[0], args[1], version)
					if err != nil {
						return err
					}
					if app.Print.JSON {
						return app.Print.Record(item)
					}
					// The bare value is what a script substitutes, so it is printed alone.
					if value, ok := item["value"].(string); ok && value != "" {
						fmt.Fprintln(os.Stdout, value)
						return nil
					}
					return fmt.Errorf("this item is disabled, expired or not yet valid, so it has no readable value")
				},
			},
			{
				Name: "versions", Summary: "List an item's versions",
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if len(args) < 2 {
						return fmt.Errorf("usage: hiok keyvault versions <vault-id> <item-name>")
					}
					versions, err := app.Client.SecretVersions(ctx, args[0], args[1])
					if err != nil {
						return err
					}
					return app.Print.Table(versions, "version", "isCurrent", "enabled", "createTime")
				},
			},
			{
				Name: "delete-item", Summary: "Soft-delete every version of an item",
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if len(args) < 2 {
						return fmt.Errorf("usage: hiok keyvault delete-item <vault-id> <item-name>")
					}
					if err := app.Client.DeleteSecret(ctx, args[0], args[1]); err != nil {
						return err
					}
					app.Print.Message("Item deleted; it stays recoverable until the vault is purged.")
					return nil
				},
			},
		},
	}
}

// ── certificates ────────────────────────────────────────────────────────────

func certificateTopic() Topic {
	var action, subject, sans, content, password, format, out string
	var keySize, validityDays int
	var includeKey bool
	return Topic{
		Name:    "cert",
		Summary: "Certificates inside a key vault",
		Commands: []Command{
			{
				Name: "create", Summary: "Self-sign, request signing, or import a certificate",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&action, "action", "self-signed", "self-signed, csr or import")
					fs.StringVar(&subject, "subject", "", "host name, or a full distinguished name")
					fs.StringVar(&sans, "sans", "", "comma-separated subject alternative names")
					fs.IntVar(&keySize, "key-size", 2048, "RSA key size in bits")
					fs.IntVar(&validityDays, "days", 365, "validity for a self-signed certificate")
					fs.StringVar(&content, "file", "", "PEM or .pfx file to import")
					fs.StringVar(&password, "password", "", "password protecting a .pfx being imported")
					fs.StringVar(&out, "out", "", "write the signing request to this file")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if len(args) < 2 {
						return fmt.Errorf("usage: hiok cert create <vault-id> <name> [flags]")
					}

					req := hiok.CreateCertificateRequest{
						Name: args[1], Action: action, Subject: subject,
						SubjectAlternativeNames: splitList(sans), KeySize: keySize,
					}
					if action == "self-signed" {
						req.ValidityDays = validityDays
					}
					if action == "import" {
						if content == "" {
							return fmt.Errorf("--file is required when importing")
						}
						raw, err := os.ReadFile(content)
						if err != nil {
							return err
						}
						// A .pfx is binary, so it travels base64-encoded; PEM goes as text.
						if isPEM(raw) {
							req.Content = string(raw)
						} else {
							req.Content = encodeBase64(raw)
						}
						req.Password = password
					}

					result, err := app.Client.CreateCertificate(ctx, args[0], req)
					if err != nil {
						return err
					}

					if result.Csr != "" && out != "" {
						if err := os.WriteFile(out, []byte(result.Csr), 0o600); err != nil {
							return fmt.Errorf("certificate created, but the request could not be written: %w", err)
						}
						app.Print.Message("Signing request written to %s. Have it signed, then run `hiok cert merge`.", out)
					}
					if app.Print.JSON {
						return app.Print.Record(map[string]any{
							"item": result.Item, "csr": result.Csr, "certificate": result.Certificate,
						})
					}
					if result.Certificate != nil {
						return app.Print.Record(result.Certificate,
							"subject", "issuer", "notBefore", "notAfter", "keyAlgorithm", "keySize", "selfSigned", "hasPrivateKey", "thumbprint")
					}
					app.Print.Message("%s", result.Message)
					return nil
				},
			},
			{
				Name: "merge", Summary: "Merge the certificate an authority returned",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&content, "file", "", "the signed certificate file")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if len(args) < 2 || content == "" {
						return fmt.Errorf("usage: hiok cert merge <vault-id> <name> --file signed.crt")
					}
					raw, err := os.ReadFile(content)
					if err != nil {
						return err
					}
					result, err := app.Client.MergeCertificate(ctx, args[0], args[1], string(raw))
					if err != nil {
						return err
					}
					app.Print.Message("%s", result.Message)
					if result.Certificate != nil {
						return app.Print.Record(result.Certificate, "subject", "issuer", "notBefore", "notAfter", "selfSigned")
					}
					return nil
				},
			},
			{
				Name: "export", Summary: "Export a certificate as PEM or PKCS#12",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&format, "format", "pem", "pem or pfx")
					fs.StringVar(&password, "password", "", "password for a .pfx export")
					fs.BoolVar(&includeKey, "include-key", false, "include the private key in a PEM export")
					fs.StringVar(&out, "out", "", "write to this file instead of standard output")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if len(args) < 2 {
						return fmt.Errorf("usage: hiok cert export <vault-id> <name> [flags]")
					}
					result, err := app.Client.ExportCertificate(ctx, args[0], args[1], hiok.ExportCertificateRequest{
						Format: format, Password: password, IncludePrivateKey: includeKey,
					})
					if err != nil {
						return err
					}
					if result.Message != "" {
						fmt.Fprintln(os.Stderr, "note:", result.Message)
					}

					payload := []byte(result.Content)
					if result.Format == "pfx" {
						// A PKCS#12 arrives base64-encoded and has to be written as bytes.
						decoded, err := decodeBase64(result.Content)
						if err != nil {
							return fmt.Errorf("the exported archive could not be decoded: %w", err)
						}
						payload = decoded
					}

					if out == "" {
						if result.Format == "pfx" {
							return fmt.Errorf("a .pfx is binary — pass --out to write it to a file")
						}
						fmt.Fprint(os.Stdout, string(payload))
						return nil
					}
					if err := os.WriteFile(out, payload, 0o600); err != nil {
						return err
					}
					app.Print.Message("Written to %s.", out)
					return nil
				},
			},
		},
	}
}
