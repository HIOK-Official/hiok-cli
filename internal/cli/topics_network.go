package cli

import (
	"context"
	"flag"
	"fmt"
	"os"

	hiok "github.com/HIOK-Official/hiok-sdk/go"
)

// ── VPN ─────────────────────────────────────────────────────────────────────

func vpnTopic() Topic {
	var name, region, vnetID, vnetName, pool, protocol, out, email string
	var gatewayType, port int
	return Topic{
		Name:    "vpn",
		Summary: "VPN gateways and client profiles",
		Commands: []Command{
			{
				Name: "list", Summary: "List gateways",
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					gateways, err := app.Client.VPNGateways(ctx)
					if err != nil {
						return err
					}
					return app.Print.Table(gateways, "vpnGatewayName", "id", "status", "regionId", "protocol", "publicIPAddress")
				},
			},
			{
				Name: "create", Summary: "Create a gateway",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&name, "name", "", "gateway name")
					fs.StringVar(&region, "region", "", "region")
					fs.IntVar(&gatewayType, "type", 1, "0 for site-to-site (WireGuard), 1 for point-to-site (OpenVPN)")
					fs.StringVar(&vnetID, "vnet-id", "", "virtual network id")
					fs.StringVar(&vnetName, "vnet", "", "virtual network name")
					fs.StringVar(&pool, "address-pool", "", "range handed to clients, e.g. 10.71.0.0/24")
					fs.StringVar(&protocol, "protocol", "", "openvpn or wireguard")
					fs.IntVar(&port, "port", 0, "listening port")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if name == "" {
						return fmt.Errorf("--name is required")
					}
					result, err := app.Client.CreateVPNGateway(ctx, hiok.CreateVPNGatewayRequest{
						Name: name, Region: app.Region(region), GatewayType: gatewayType,
						VNetID: vnetID, VNetName: vnetName, AddressPool: pool,
						Protocol: protocol, Port: port, SplitTunneling: true,
					})
					if err != nil {
						return err
					}
					return app.Print.Record(result, "vpnGatewayName", "id", "status", "publicIPAddress", "protocol")
				},
			},
			{
				Name: "delete", Summary: "Delete a gateway",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&name, "name", "", "gateway name (the processor addresses it by name)")
					fs.StringVar(&region, "region", "", "region")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if len(args) == 0 || name == "" {
						return fmt.Errorf("usage: hiok vpn delete <gateway-id> --name <gateway-name>")
					}
					if err := app.Client.DeleteVPNGateway(ctx, args[0], name, app.Region(region)); err != nil {
						return err
					}
					app.Print.Message("Gateway deleted.")
					return nil
				},
			},
			{
				Name: "client-create", Summary: "Issue a point-to-site client certificate",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&email, "email", "", "who the certificate is for")
					fs.StringVar(&region, "region", "", "region")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if len(args) < 2 {
						return fmt.Errorf("usage: hiok vpn client-create <gateway-id> <client-name>")
					}
					result, err := app.Client.CreateVPNClient(ctx, args[0], args[1], email, app.Region(region))
					if err != nil {
						return err
					}
					app.Print.Message("Client issued. Fetch the profile with `hiok vpn client-config`.")
					return app.Print.Record(result, "name", "id", "status")
				},
			},
			{
				Name: "client-list", Summary: "List a gateway's clients",
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if len(args) == 0 {
						return fmt.Errorf("usage: hiok vpn client-list <gateway-id>")
					}
					clients, err := app.Client.VPNClients(ctx, args[0])
					if err != nil {
						return err
					}
					return app.Print.Table(clients, "name", "id", "status", "certExpiry")
				},
			},
			{
				Name: "client-config", Summary: "Download a client profile",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&out, "out", "", "write to this file instead of standard output")
					fs.StringVar(&region, "region", "", "region")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if len(args) == 0 {
						return fmt.Errorf("usage: hiok vpn client-config <client-id> [--out file.ovpn]")
					}
					profile, err := app.Client.VPNClientConfig(ctx, args[0], app.Region(region))
					if err != nil {
						return err
					}
					if out == "" {
						fmt.Fprint(os.Stdout, string(profile))
						return nil
					}
					// The profile embeds a private key, so it is written owner-only.
					if err := os.WriteFile(out, profile, 0o600); err != nil {
						return err
					}
					app.Print.Message("Profile written to %s.", out)
					return nil
				},
			},
			{
				Name: "client-revoke", Summary: "Revoke a client certificate",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&region, "region", "", "region")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if len(args) == 0 {
						return fmt.Errorf("usage: hiok vpn client-revoke <client-id>")
					}
					if err := app.Client.RevokeVPNClient(ctx, args[0], app.Region(region)); err != nil {
						return err
					}
					app.Print.Message("Client revoked. Its name stays taken so the identity cannot be reissued.")
					return nil
				},
			},
		},
	}
}

// ── IoT ─────────────────────────────────────────────────────────────────────

func iotTopic() Topic {
	var payload, properties string
	var assumeYes bool
	return Topic{
		Name:    "iot",
		Summary: "IoT hubs and devices",
		Commands: []Command{
			{
				Name: "hubs", Summary: "List IoT hubs",
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					hubs, err := app.Client.IoTHubs(ctx)
					if err != nil {
						return err
					}
					return app.Print.Table(hubs, "name", "id", "region", "tier")
				},
			},
			{
				Name: "devices", Summary: "List devices in a hub",
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if len(args) == 0 {
						return fmt.Errorf("usage: hiok iot devices <hub-id>")
					}
					devices, err := app.Client.IoTDevices(ctx, args[0])
					if err != nil {
						return err
					}
					return app.Print.Table(devices, "deviceId", "name", "status", "connectionState")
				},
			},
			{
				Name: "device-create", Summary: "Register a device",
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if len(args) < 2 {
						return fmt.Errorf("usage: hiok iot device-create <hub-id> <device-id>")
					}
					result, err := app.Client.CreateIoTDevice(ctx, args[0], args[1])
					if err != nil {
						return err
					}
					return app.Print.Record(result, "deviceId", "status", "connectionString")
				},
			},
			{
				Name: "device-delete", Summary: "Delete a device",
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if len(args) < 2 {
						return fmt.Errorf("usage: hiok iot device-delete <hub-id> <device-id>")
					}
					if err := app.Client.DeleteIoTDevice(ctx, args[0], args[1]); err != nil {
						return err
					}
					app.Print.Message("Device deleted.")
					return nil
				},
			},
			{
				Name: "connection-string", Summary: "Show a device's connection string",
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if len(args) < 2 {
						return fmt.Errorf("usage: hiok iot connection-string <hub-id> <device-id>")
					}
					result, err := app.Client.IoTDeviceConnectionString(ctx, args[0], args[1])
					if err != nil {
						return err
					}
					return app.Print.Record(result, "hostName", "deviceId", "connectionString")
				},
			},
			{
				Name: "rotate-key", Summary: "Regenerate a device's key",
				Flags: func(fs *flag.FlagSet) {
					fs.BoolVar(&assumeYes, "yes", false, "skip the confirmation prompt")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if len(args) < 2 {
						return fmt.Errorf("usage: hiok iot rotate-key <hub-id> <device-id>")
					}
					if !confirm("Rotate the key? The previous one stops working immediately.", assumeYes) {
						app.Print.Message("Key unchanged.")
						return nil
					}
					result, err := app.Client.RegenerateIoTDeviceKey(ctx, args[0], args[1])
					if err != nil {
						return err
					}
					return app.Print.Record(result)
				},
			},
			{
				Name: "twin", Summary: "Show a device twin",
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if len(args) < 2 {
						return fmt.Errorf("usage: hiok iot twin <hub-id> <device-id>")
					}
					twin, err := app.Client.IoTDeviceTwin(ctx, args[0], args[1])
					if err != nil {
						return err
					}
					return app.Print.Record(twin)
				},
			},
			{
				Name: "send", Summary: "Send device-to-cloud telemetry",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&payload, "payload", "", "message body")
					fs.StringVar(&properties, "properties", "", "message properties as JSON")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if len(args) < 2 || payload == "" {
						return fmt.Errorf(`usage: hiok iot send <hub-id> <device-id> --payload '{"temp":21}'`)
					}
					result, err := app.Client.SendIoTTelemetry(ctx, args[0], args[1], payload, properties)
					if err != nil {
						return err
					}
					return app.Print.Record(result)
				},
			},
			{
				Name: "c2d", Summary: "Queue a cloud-to-device message",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&payload, "payload", "", "message body")
					fs.StringVar(&properties, "properties", "", "message properties as JSON")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if len(args) < 2 || payload == "" {
						return fmt.Errorf(`usage: hiok iot c2d <hub-id> <device-id> --payload '{"cmd":"reboot"}'`)
					}
					result, err := app.Client.SendIoTCloudToDevice(ctx, args[0], args[1], payload, properties)
					if err != nil {
						return err
					}
					return app.Print.Record(result)
				},
			},
			{
				Name: "messages", Summary: "Show a device's message history",
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if len(args) < 2 {
						return fmt.Errorf("usage: hiok iot messages <hub-id> <device-id>")
					}
					messages, err := app.Client.IoTMessages(ctx, args[0], args[1])
					if err != nil {
						return err
					}
					return app.Print.Table(messages, "direction", "payload", "enqueuedAt", "deliveredAt")
				},
			},
			{
				Name: "monitor", Summary: "Show hub counters",
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if len(args) == 0 {
						return fmt.Errorf("usage: hiok iot monitor <hub-id>")
					}
					stats, err := app.Client.IoTHubMonitoring(ctx, args[0])
					if err != nil {
						return err
					}
					return app.Print.Record(stats)
				},
			},
		},
	}
}

// ── DPS ─────────────────────────────────────────────────────────────────────

func dpsTopic() Topic {
	var name, enrollmentType, registrationID, hubID, prefix string
	return Topic{
		Name:    "dps",
		Summary: "Device provisioning enrollments",
		Commands: []Command{
			{
				Name: "list", Summary: "List enrollments",
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					enrollments, err := app.Client.DpsEnrollments(ctx)
					if err != nil {
						return err
					}
					return app.Print.Table(enrollments, "name", "id", "enrollmentType", "registrationId", "provisioningStatus")
				},
			},
			{
				Name: "create", Summary: "Create an enrollment",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&name, "name", "", "enrollment name")
					fs.StringVar(&enrollmentType, "type", "individual", "individual or group")
					fs.StringVar(&registrationID, "registration-id", "", "the id an individual device presents")
					fs.StringVar(&hubID, "hub", "", "hub devices are provisioned into")
					fs.StringVar(&prefix, "prefix", "", "prefix for provisioned device names")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if name == "" || hubID == "" {
						return fmt.Errorf("--name and --hub are required")
					}
					result, err := app.Client.CreateDpsEnrollment(ctx, hiok.CreateDpsEnrollmentRequest{
						Name: name, EnrollmentType: enrollmentType, RegistrationID: registrationID,
						TargetHubID: hubID, DeviceIDPrefix: prefix,
					})
					if err != nil {
						return err
					}
					app.Print.Message("Keep the primary key safe — it is what devices present to register.")
					return app.Print.Record(result, "name", "id", "enrollmentType", "primaryKey", "provisioningStatus")
				},
			},
			{
				Name: "delete", Summary: "Delete an enrollment",
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if len(args) == 0 {
						return fmt.Errorf("usage: hiok dps delete <enrollment-id>")
					}
					if err := app.Client.DeleteDpsEnrollment(ctx, args[0]); err != nil {
						return err
					}
					app.Print.Message("Enrollment deleted.")
					return nil
				},
			},
			{
				Name: "register", Summary: "Register a device against an enrollment",
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if len(args) < 2 {
						return fmt.Errorf("usage: hiok dps register <registration-id> <attestation-key>")
					}
					result, err := app.Client.RegisterDevice(ctx, args[0], args[1])
					if err != nil {
						return err
					}
					return app.Print.Record(result, "status", "assignedHub", "deviceId", "connectionString")
				},
			},
			{
				Name: "registrations", Summary: "Show devices registered through an enrollment",
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if len(args) == 0 {
						return fmt.Errorf("usage: hiok dps registrations <enrollment-id>")
					}
					registrations, err := app.Client.DpsRegistrations(ctx, args[0])
					if err != nil {
						return err
					}
					return app.Print.Table(registrations, "registrationId", "deviceId", "assignedHub", "status")
				},
			},
		},
	}
}

// ── public IP ───────────────────────────────────────────────────────────────

func publicIPTopic() Topic {
	var region, target, targetKind, hostname string
	var family int
	var reverseDNS bool
	return Topic{
		Name:    "publicip",
		Summary: "Public IP pools and allocations",
		Commands: []Command{
			{
				Name: "pools", Summary: "Show address pools",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&region, "region", "", "region")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					pools, err := app.Client.IPPools(ctx, region)
					if err != nil {
						return err
					}
					return app.Print.Table(pools)
				},
			},
			{
				Name: "list", Summary: "Show allocations",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&region, "region", "", "region")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					allocations, err := app.Client.IPAllocations(ctx, region, false)
					if err != nil {
						return err
					}
					return app.Print.Table(allocations, "address", "id", "targetName", "targetKind", "hostname", "configured")
				},
			},
			{
				Name: "allocate", Summary: "Take an address and configure a host to carry it",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&region, "region", "", "region")
					fs.IntVar(&family, "family", 6, "4 or 6")
					fs.StringVar(&target, "target", "", "container name, or <guid>#<name> for a VM")
					fs.StringVar(&targetKind, "target-kind", "container", "container or vm")
					fs.StringVar(&hostname, "hostname", "", "publish DNS under this name")
					fs.BoolVar(&reverseDNS, "reverse-dns", true, "publish reverse DNS as well")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					result, err := app.Client.AllocateIP(ctx, hiok.AllocateIPRequest{
						Region: app.Region(region), Family: family,
						TargetContainer: target, TargetKind: targetKind,
						Hostname: hostname, SetReverseDNS: &reverseDNS,
					})
					if err != nil {
						return err
					}
					return app.Print.Record(result, "address", "id", "prefixLength", "gateway", "configured", "statusMessage", "ptrStatus")
				},
			},
			{
				Name: "release", Summary: "Give an address back",
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					if len(args) == 0 {
						return fmt.Errorf("usage: hiok publicip release <allocation-id>")
					}
					if err := app.Client.ReleaseIP(ctx, args[0], ""); err != nil {
						return err
					}
					app.Print.Message("Address released.")
					return nil
				},
			},
			{
				Name: "reconcile", Summary: "Re-apply live allocations on their hosts",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&region, "region", "", "region")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					results, err := app.Client.ReconcileIPs(ctx, region)
					if err != nil {
						return err
					}
					return app.Print.Table(results)
				},
			},
		},
	}
}
