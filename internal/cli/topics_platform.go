package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// The rest of the platform, on the command line.
//
// The CLI covered fourteen topics of a catalogue with more than thirty, so
// anything else had to be done in a browser — including the things people most
// want a command for, like running a container or bringing a stack up on a
// region. These call the same endpoints the console does, through the SDK's
// generic request, so a topic here is a thin description of an API rather than a
// second implementation of it.

// get is a read that prints a table of whatever came back under "data".
func get(app *App, ctx context.Context, path string, columns ...string) error {
	// Lists come back as a bare array, {data:[…]} or {data:{items:[…]}}
	// depending on the controller; listItems reads all three.
	items, err := listItems(app, ctx, path)
	if err != nil {
		return err
	}
	return app.Print.Table(items, columns...)
}

// post sends a request and reports what it said, which for a create is usually
// all the caller wants.
func post(app *App, ctx context.Context, method, path string, body any) (map[string]any, error) {
	if err := app.RequireToken(); err != nil {
		return nil, err
	}
	var resp map[string]any
	if err := app.Client.Do(ctx, method, path, body, &resp, true); err != nil {
		return nil, err
	}
	return resp, nil
}

// ── docker ──────────────────────────────────────────────────────────────────

func dockerTopic() Topic {
	var name, image, region, ports, env, volume, compose, file, project string
	var cpus float64
	var memoryMb int
	var removeVolumes bool

	return Topic{
		Name:    "docker",
		Summary: "Containers, stacks, images and volumes on a region",
		Commands: []Command{
			{
				Name: "ps", Summary: "List containers",
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					var resp struct {
						Data []map[string]any `json:"data"`
					}
					body := map[string]any{"pageNumber": 1, "pageSize": 200}
					if err := app.Client.Do(ctx, "POST", "/api/Containers/listallcontainers", body, &resp, true); err != nil {
						return err
					}
					return app.Print.Table(resp.Data, "name", "image", "status", "dnsHostname")
				},
			},
			{
				Name: "run", Summary: "Run a container",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&name, "name", "", "container name")
					fs.StringVar(&image, "image", "", "image reference, e.g. nginx:alpine")
					fs.StringVar(&region, "region", "", "region")
					fs.StringVar(&ports, "publish", "", "published ports as host:container, comma separated")
					fs.StringVar(&env, "env", "", "environment as KEY=value, comma separated")
					fs.StringVar(&volume, "volume", "", "storage volume as account/path:/mount, comma separated")
					fs.Float64Var(&cpus, "cpus", 0, "CPU limit")
					fs.IntVar(&memoryMb, "memory", 0, "memory limit in MiB")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if name == "" || image == "" {
						return fmt.Errorf("--name and --image are required")
					}
					body := map[string]any{
						"name":          name,
						"image":         image,
						"restartPolicy": "unless-stopped",
					}
					if cpus > 0 {
						body["cpus"] = cpus
					}
					if memoryMb > 0 {
						body["memoryBytes"] = int64(memoryMb) * 1024 * 1024
					}
					if published := parsePorts(ports); len(published) > 0 {
						body["ports"] = published
					}
					if vars := splitList(env); len(vars) > 0 {
						body["env"] = vars
					}
					if mounts := parseVolumes(volume); len(mounts) > 0 {
						body["volumes"] = mounts
					}
					if _, err := post(app, ctx, "POST", "/api/Containers/createcontainer", body); err != nil {
						return err
					}
					app.Print.Message("Container %q is starting.", name)
					return nil
				},
			},
			{
				Name: "logs", Summary: "Read a container's output",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&name, "name", "", "container name")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if name == "" && len(args) > 0 {
						name = args[0]
					}
					if name == "" {
						return fmt.Errorf("--name is required")
					}
					if err := app.RequireToken(); err != nil {
						return err
					}
					var resp struct {
						Data struct {
							Lines []string `json:"lines"`
						} `json:"data"`
					}
					path := fmt.Sprintf("/api/Containers/%s/logs?tail=200", name)
					if err := app.Client.Do(ctx, "GET", path, nil, &resp, true); err != nil {
						return err
					}
					for _, line := range resp.Data.Lines {
						app.Print.Message("%s", line)
					}
					return nil
				},
			},
			lifecycle("start", "started", "Start a container", "/api/Containers/startcontainer", &name),
			lifecycle("stop", "stopped", "Stop a container", "/api/Containers/stopcontainer", &name),
			lifecycle("restart", "restarted", "Restart a container", "/api/Containers/restartcontainer", &name),
			{
				Name: "rm", Summary: "Delete a container",
				Flags: func(fs *flag.FlagSet) { fs.StringVar(&name, "name", "", "container name") },
				Run: func(ctx context.Context, app *App, args []string) error {
					if name == "" && len(args) > 0 {
						name = args[0]
					}
					if name == "" {
						return fmt.Errorf("--name is required")
					}
					body := map[string]any{"containerName": name, "force": true}
					if _, err := post(app, ctx, "POST", "/api/Containers/deletecontainer", body); err != nil {
						return err
					}
					app.Print.Message("Container %q deleted.", name)
					return nil
				},
			},
			{
				Name: "images", Summary: "Images on the region",
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					var resp struct {
						Data []map[string]any `json:"data"`
					}
					if err := app.Client.Do(ctx, "POST", "/api/DockerImages/listallimages", map[string]any{}, &resp, true); err != nil {
						return err
					}
					return app.Print.Table(resp.Data, "repoTags", "size")
				},
			},
			{
				Name: "search", Summary: "Search Docker Hub",
				Flags: func(fs *flag.FlagSet) { fs.StringVar(&image, "term", "", "what to search for") },
				Run: func(ctx context.Context, app *App, args []string) error {
					if image == "" && len(args) > 0 {
						image = args[0]
					}
					if image == "" {
						return fmt.Errorf("--term is required")
					}
					if err := app.RequireToken(); err != nil {
						return err
					}
					var resp struct {
						Data []map[string]any `json:"data"`
					}
					body := map[string]any{"term": image}
					if err := app.Client.Do(ctx, "POST", "/api/DockerImages/searchimage", body, &resp, true); err != nil {
						return err
					}
					return app.Print.Table(resp.Data, "name", "description", "starCount", "isOfficial")
				},
			},
			{
				Name: "volumes", Summary: "Volumes you own",
				Run: func(ctx context.Context, app *App, args []string) error {
					return get(app, ctx, "/api/Containers/volumes?region=canada", "displayName", "driver", "createdAt")
				},
			},
			{
				Name: "stack-up", Summary: "Bring a compose stack up",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&project, "name", "", "stack name")
					fs.StringVar(&file, "file", "", "path to a local compose file")
					fs.StringVar(&compose, "storage-path", "", `a compose file in storage, as "account/path.yml"`)
					fs.StringVar(&region, "region", "", "region")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if project == "" {
						return fmt.Errorf("--name is required")
					}
					body := map[string]any{"project": project, "region": app.Region(region)}
					switch {
					case file != "":
						content, err := readFile(file)
						if err != nil {
							return err
						}
						body["content"] = content
					case compose != "":
						body["storagePath"] = compose
					default:
						return fmt.Errorf("--file or --storage-path is required")
					}
					resp, err := post(app, ctx, "POST", "/api/Containers/stacks/up", body)
					if err != nil {
						return err
					}
					app.Print.Message("Stack %q is up.", stackName(resp, project))
					return nil
				},
			},
			{
				Name: "stack-down", Summary: "Remove a compose stack",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&project, "name", "", "stack name")
					fs.BoolVar(&removeVolumes, "remove-volumes", false, "also remove the stack's volumes")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if project == "" && len(args) > 0 {
						project = args[0]
					}
					if project == "" {
						return fmt.Errorf("--name is required")
					}
					body := map[string]any{"project": project, "removeVolumes": removeVolumes}
					if _, err := post(app, ctx, "POST", "/api/Containers/stacks/down", body); err != nil {
						return err
					}
					app.Print.Message("Stack %q removed.", project)
					return nil
				},
			},
		},
	}
}

// lifecycle is start/stop/restart, which differ only in their path and wording.
//
// done is given rather than derived: "stop" + "ed" is "stoped", and a tool that
// misspells its own confirmation looks like it got the rest wrong too.
func lifecycle(verb, done, summary, path string, name *string) Command {
	return Command{
		Name: verb, Summary: summary,
		Flags: func(fs *flag.FlagSet) { fs.StringVar(name, "name", "", "container name") },
		Run: func(ctx context.Context, app *App, args []string) error {
			if *name == "" && len(args) > 0 {
				*name = args[0]
			}
			if *name == "" {
				return fmt.Errorf("--name is required")
			}
			if _, err := post(app, ctx, "POST", path, map[string]any{"containerName": *name}); err != nil {
				return err
			}
			app.Print.Message("%s: %s.", *name, done)
			return nil
		},
	}
}

// ── swarm ───────────────────────────────────────────────────────────────────

func swarmTopic() Topic {
	var name, image, region string
	var replicas, publishedPort, targetPort int

	return Topic{
		Name:    "swarm",
		Summary: "Services the engine keeps running",
		Commands: []Command{
			{
				Name: "status", Summary: "Whether this region is a swarm",
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					var resp map[string]any
					if err := app.Client.Do(ctx, "GET", "/api/Containers/swarm", nil, &resp, true); err != nil {
						return err
					}
					if data, ok := resp["data"].(map[string]any); ok {
						return app.Print.Table([]map[string]any{data}, "active", "nodes", "services", "localState")
					}
					return nil
				},
			},
			{
				Name: "services", Summary: "List services",
				Run: func(ctx context.Context, app *App, args []string) error {
					return get(app, ctx, "/api/Containers/swarm/services", "name", "image", "replicas", "running")
				},
			},
			{
				Name: "nodes", Summary: "List the machines in the swarm",
				Run: func(ctx context.Context, app *App, args []string) error {
					return get(app, ctx, "/api/Containers/swarm/nodes", "hostname", "role", "state", "engineVersion")
				},
			},
			{
				Name: "create", Summary: "Declare a service",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&name, "name", "", "service name")
					fs.StringVar(&image, "image", "", "image reference")
					fs.IntVar(&replicas, "replicas", 1, "how many copies should run")
					fs.IntVar(&publishedPort, "publish", 0, "published port")
					fs.IntVar(&targetPort, "target-port", 80, "port inside the container")
					fs.StringVar(&region, "region", "", "region")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if name == "" || image == "" {
						return fmt.Errorf("--name and --image are required")
					}
					body := map[string]any{
						"name": name, "image": image, "replicas": replicas, "region": app.Region(region),
					}
					if publishedPort > 0 {
						body["ports"] = []map[string]any{{
							"hostPort": publishedPort, "containerPort": targetPort, "protocol": "tcp",
						}}
					}
					if _, err := post(app, ctx, "POST", "/api/Containers/swarm/services", body); err != nil {
						return err
					}
					app.Print.Message("Service %q declared with %d replica(s).", name, replicas)
					return nil
				},
			},
			{
				Name: "scale", Summary: "Change how many copies run",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&name, "name", "", "service name")
					fs.IntVar(&replicas, "replicas", 1, "how many copies should run")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if name == "" {
						return fmt.Errorf("--name is required")
					}
					path := fmt.Sprintf("/api/Containers/swarm/services/%s/scale?replicas=%d", name, replicas)
					if _, err := post(app, ctx, "POST", path, nil); err != nil {
						return err
					}
					app.Print.Message("%s scaled to %d.", name, replicas)
					return nil
				},
			},
			{
				Name: "rm", Summary: "Remove a service",
				Flags: func(fs *flag.FlagSet) { fs.StringVar(&name, "name", "", "service name") },
				Run: func(ctx context.Context, app *App, args []string) error {
					if name == "" && len(args) > 0 {
						name = args[0]
					}
					if name == "" {
						return fmt.Errorf("--name is required")
					}
					if _, err := post(app, ctx, "DELETE", "/api/Containers/swarm/services/"+name, nil); err != nil {
						return err
					}
					app.Print.Message("Service %q removed.", name)
					return nil
				},
			},
		},
	}
}

// ── the kinds the CLI could not reach ───────────────────────────────────────

func kubernetesTopic() Topic {
	var name, region, version string
	var nodes, cpus, memoryMb int
	return Topic{
		Name:    "kubernetes",
		Summary: "Managed Kubernetes clusters",
		Commands: []Command{
			{
				Name: "list", Summary: "List clusters",
				Run: func(ctx context.Context, app *App, args []string) error {
					return get(app, ctx, "/api/kubernetes/clusters", "name", "status", "nodeCount", "regionId")
				},
			},
			{
				Name: "create", Summary: "Create a cluster",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&name, "name", "", "cluster name")
					fs.StringVar(&region, "region", "", "region")
					fs.StringVar(&version, "version", "", "Kubernetes version")
					fs.IntVar(&nodes, "nodes", 1, "worker nodes")
					fs.IntVar(&cpus, "node-cpus", 2, "vCPU per node")
					fs.IntVar(&memoryMb, "node-memory", 2048, "memory per node in MiB")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if name == "" {
						return fmt.Errorf("--name is required")
					}
					body := map[string]any{
						"name": name, "region": app.Region(region), "nodeCount": nodes,
						"nodeCpus": cpus, "nodeMemoryMb": memoryMb,
					}
					if version != "" {
						body["version"] = version
					}
					if _, err := post(app, ctx, "POST", "/api/kubernetes/clusters", body); err != nil {
						return err
					}
					app.Print.Message("Cluster %q requested.", name)
					return nil
				},
			},
			deleteByIDCommand("delete", "Delete a cluster", "/api/kubernetes/clusters"),
		},
	}
}

func registryTopic() Topic {
	var name, region, sku string
	var storageGb int
	return Topic{
		Name:    "registry",
		Summary: "Private container registries",
		Commands: []Command{
			{
				Name: "list", Summary: "List registries",
				Run: func(ctx context.Context, app *App, args []string) error {
					return get(app, ctx, "/api/container-registry", "registryName", "sku", "loginServer", "status")
				},
			},
			{
				Name: "create", Summary: "Create a registry",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&name, "name", "", "registry name")
					fs.StringVar(&region, "region", "", "region")
					fs.StringVar(&sku, "sku", "Basic", "Basic, Standard or Premium")
					fs.IntVar(&storageGb, "storage", 10, "included storage in GiB")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if name == "" {
						return fmt.Errorf("--name is required")
					}
					body := map[string]any{
						"registryName": name, "region": app.Region(region),
						"sku": sku, "storageGb": storageGb,
					}
					if _, err := post(app, ctx, "POST", "/api/container-registry", body); err != nil {
						return err
					}
					app.Print.Message("Registry %q requested.", name)
					return nil
				},
			},
			deleteByIDCommand("delete", "Delete a registry", "/api/container-registry"),
		},
	}
}

func cacheTopic() Topic {
	var name, region, engine string
	var memoryMb int
	var cpus float64
	return Topic{
		Name:    "cache",
		Summary: "Managed in-memory caches",
		Commands: []Command{
			{
				Name: "list", Summary: "List caches",
				Run: func(ctx context.Context, app *App, args []string) error {
					return get(app, ctx, "/api/Cache", "name", "engine", "status", "regionId")
				},
			},
			{
				Name: "create", Summary: "Create a cache",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&name, "name", "", "cache name")
					fs.StringVar(&region, "region", "", "region")
					fs.StringVar(&engine, "engine", "hiok", "cache engine")
					fs.IntVar(&memoryMb, "memory", 512, "memory in MiB")
					fs.Float64Var(&cpus, "cpus", 1, "CPU limit")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if name == "" {
						return fmt.Errorf("--name is required")
					}
					body := map[string]any{
						"name": name, "regionId": app.Region(region), "engine": engine,
						"memoryMb": memoryMb, "cpus": cpus,
					}
					if _, err := post(app, ctx, "POST", "/api/Cache", body); err != nil {
						return err
					}
					app.Print.Message("Cache %q requested.", name)
					return nil
				},
			},
			deleteByIDCommand("delete", "Delete a cache", "/api/Cache"),
		},
	}
}

func streamingTopic() Topic {
	var name, region, endpointID, platform, url, key, mode string
	return Topic{
		Name:    "streaming",
		Summary: "Live streams and where they are repeated to",
		Commands: []Command{
			{
				Name: "list", Summary: "List streams",
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					var resp struct {
						Data []struct {
							Endpoint map[string]any `json:"endpoint"`
						} `json:"data"`
					}
					if err := app.Client.Do(ctx, "GET", "/api/streaming", nil, &resp, true); err != nil {
						return err
					}
					rows := make([]map[string]any, 0, len(resp.Data))
					for _, item := range resp.Data {
						rows = append(rows, item.Endpoint)
					}
					return app.Print.Table(rows, "name", "path", "kind", "region", "status")
				},
			},
			{
				Name: "create", Summary: "Create a stream",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&name, "name", "", "stream name")
					fs.StringVar(&region, "region", "", "region")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if name == "" {
						return fmt.Errorf("--name is required")
					}
					body := map[string]any{"name": name, "region": app.Region(region), "kind": "video"}
					if _, err := post(app, ctx, "POST", "/api/streaming", body); err != nil {
						return err
					}
					app.Print.Message("Stream %q created.", name)
					return nil
				},
			},
			{
				Name: "destinations", Summary: "Where a stream is repeated to",
				Flags: func(fs *flag.FlagSet) { fs.StringVar(&endpointID, "stream", "", "stream id") },
				Run: func(ctx context.Context, app *App, args []string) error {
					if endpointID == "" {
						return fmt.Errorf("--stream is required")
					}
					return get(app, ctx,
						fmt.Sprintf("/api/streaming/%s/destinations?refresh=true", endpointID),
						"name", "platform", "state", "mode")
				},
			},
			{
				Name: "add-destination", Summary: "Repeat a stream to a platform",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&endpointID, "stream", "", "stream id")
					fs.StringVar(&name, "name", "", "what to call this destination")
					fs.StringVar(&platform, "platform", "rtmp", "youtube, facebook, twitch, rtmp, srt…")
					fs.StringVar(&url, "url", "", "the platform's ingest URL")
					fs.StringVar(&key, "key", "", "stream key")
					fs.StringVar(&mode, "mode", "copy", "copy or transcode")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if endpointID == "" || url == "" {
						return fmt.Errorf("--stream and --url are required")
					}
					body := map[string]any{
						"name": name, "platform": platform, "targetUrl": url,
						"streamKey": key, "mode": mode, "enabled": true,
					}
					path := fmt.Sprintf("/api/streaming/%s/destinations", endpointID)
					if _, err := post(app, ctx, "POST", path, body); err != nil {
						return err
					}
					app.Print.Message("Destination added.")
					return nil
				},
			},
		},
	}
}

func costTopic() Topic {
	return Topic{
		Name:    "cost",
		Summary: "What this account is spending",
		Commands: []Command{
			{
				Name: "overview", Summary: "Spend so far and what is forecast",
				Run: func(ctx context.Context, app *App, args []string) error {
					if err := app.RequireToken(); err != nil {
						return err
					}
					var resp map[string]any
					if err := app.Client.Do(ctx, "GET", "/api/CostTracking/overview", nil, &resp, true); err != nil {
						return err
					}
					if data, ok := resp["data"].(map[string]any); ok {
						return app.Print.Table([]map[string]any{data},
							"period", "monthToDate", "nextSevenDays", "forecastMonth")
					}
					return nil
				},
			},
			{
				Name: "rates", Summary: "The published price list",
				Run: func(ctx context.Context, app *App, args []string) error {
					var resp map[string]any
					if err := app.Client.Do(ctx, "GET", "/api/Pricing/ratecard", nil, &resp, false); err != nil {
						return err
					}
					data, _ := resp["data"].(map[string]any)
					rates, _ := data["rates"].(map[string]any)
					rows := make([]map[string]any, 0, len(rates))
					for key, value := range rates {
						if rate, ok := value.(map[string]any); ok {
							rate["meter"] = key
							rows = append(rows, rate)
						}
					}
					return app.Print.Table(rows, "meter", "rateDollars", "unit", "displayName")
				},
			},
		},
	}
}

// deleteByIDCommand is the delete every id-addressed resource shares.
func deleteByIDCommand(verb, summary, path string) Command {
	var id string
	return Command{
		Name: verb, Summary: summary,
		Flags: func(fs *flag.FlagSet) { fs.StringVar(&id, "id", "", "resource id") },
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
			if err := app.Client.Do(ctx, "DELETE", path+"/"+id, nil, nil, true); err != nil {
				return err
			}
			app.Print.Message("Deleted %s.", id)
			return nil
		},
	}
}

// ── small parsers ───────────────────────────────────────────────────────────

// parsePorts reads "8080:80,9000:9000" as published ports.
func parsePorts(value string) []map[string]any {
	var out []map[string]any
	for _, pair := range splitList(value) {
		host, container, found := strings.Cut(pair, ":")
		if !found {
			// A single number is the container's port, published on a free one.
			container = host
			host = "0"
		}
		out = append(out, map[string]any{
			"hostPort":      atoi(host),
			"containerPort": atoi(container),
			"protocol":      "tcp",
		})
	}
	return out
}

// parseVolumes reads "account/path:/mount" as a storage volume, which is the only
// kind the API accepts from outside: a path on the host is not a customer's to name.
func parseVolumes(value string) []map[string]any {
	var out []map[string]any
	for _, entry := range splitList(value) {
		source, destination, found := strings.Cut(entry, ":")
		if !found {
			continue
		}
		out = append(out, map[string]any{
			"type":        "storage",
			"source":      source,
			"destination": destination,
		})
	}
	return out
}

func stackName(resp map[string]any, fallback string) string {
	if data, ok := resp["data"].(map[string]any); ok {
		if project, ok := data["project"].(string); ok && project != "" {
			return project
		}
	}
	return fallback
}

func atoi(value string) int {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0
	}
	return n
}

// readFile is how a compose file on the caller's machine reaches the region: the
// API takes the text, because the region has no access to their filesystem.
func readFile(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("%s could not be read: %w", path, err)
	}
	return string(raw), nil
}
