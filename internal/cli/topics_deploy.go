package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The commands a pipeline reaches for: build and push an image, roll a container
// app or a container onto it, run a job and wait for it, run a script on a VM,
// ship files to storage, pull secrets into the pipeline, get a kubeconfig.
//
// Every CI integration (the GitHub Actions, the Azure DevOps tasks, the GitLab
// templates, the Docker image for everything else) is a thin wrapper over these,
// so a deployment behaves the same whichever CI runs it.

// withDeployCommands adds the pipeline commands to the topics they belong to.
func withDeployCommands(ts []Topic) []Topic {
	extra := map[string][]Command{
		"registry":   registryDeployCommands(),
		"app":        appDeployCommands(),
		"docker":     dockerDeployCommands(),
		"vm":         vmDeployCommands(),
		"storage":    storageDeployCommands(),
		"keyvault":   keyVaultDeployCommands(),
		"kubernetes": kubernetesDeployCommands(),
	}
	for i := range ts {
		ts[i].Commands = append(ts[i].Commands, extra[ts[i].Name]...)
	}
	return append(ts, jobTopic(), idTopic(), apiKeyTopic())
}

// ── helpers ─────────────────────────────────────────────────────────────────

// listItems reads a list endpoint whichever shape it answers in: a bare array,
// {data:[…]}, or {data:{items:[…]}}.
func listItems(app *App, ctx context.Context, path string) ([]map[string]any, error) {
	if err := app.RequireToken(); err != nil {
		return nil, err
	}
	var raw json.RawMessage
	if err := app.Client.Do(ctx, "GET", path, nil, &raw, true); err != nil {
		return nil, err
	}
	var arr []map[string]any
	if json.Unmarshal(raw, &arr) == nil {
		return arr, nil
	}
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &env) == nil && len(env.Data) > 0 {
		if json.Unmarshal(env.Data, &arr) == nil {
			return arr, nil
		}
		var paged struct {
			Items []map[string]any `json:"items"`
		}
		if json.Unmarshal(env.Data, &paged) == nil {
			return paged.Items, nil
		}
	}
	return nil, fmt.Errorf("unexpected answer from %s", path)
}

// resolve finds one item by id or by any of the given name fields.
func resolve(app *App, ctx context.Context, path, what, ref string, nameKeys ...string) (map[string]any, error) {
	items, err := listItems(app, ctx, path)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, it := range items {
		if strings.EqualFold(fmt.Sprint(it["id"]), ref) {
			return it, nil
		}
		for _, k := range nameKeys {
			if v, ok := it[k]; ok && v != nil {
				names = append(names, fmt.Sprint(v))
				if strings.EqualFold(fmt.Sprint(v), ref) {
					return it, nil
				}
			}
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil, fmt.Errorf("no %s named %q (this account has none)", what, ref)
	}
	return nil, fmt.Errorf("no %s named %q (have: %s)", what, ref, strings.Join(names, ", "))
}

func str(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil && fmt.Sprint(v) != "" {
			return fmt.Sprint(v)
		}
	}
	return ""
}

func dataOf(resp map[string]any) map[string]any {
	if d, ok := resp["data"].(map[string]any); ok {
		return d
	}
	return resp
}

// run executes a local tool, streaming its output, with optional stdin.
func run(stdin string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(redact(args), " "), err)
	}
	return nil
}

func redact(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		if i > 0 && (args[i-1] == "-p" || args[i-1] == "--password") {
			a = "***"
		}
		out[i] = a
	}
	return out
}

// setOutput hands a value to the CI system running us, when there is one:
// GitHub Actions (GITHUB_OUTPUT), Azure DevOps (logging command), GitLab (dotenv).
func setOutput(name, value string, secret bool) {
	if f := os.Getenv("GITHUB_OUTPUT"); f != "" {
		if secret {
			fmt.Printf("::add-mask::%s\n", value)
		}
		appendLine(f, fmt.Sprintf("%s=%s", name, value))
	}
	if os.Getenv("TF_BUILD") != "" {
		fmt.Printf("##vso[task.setvariable variable=%s;isOutput=true%s]%s\n", name, map[bool]string{true: ";issecret=true", false: ""}[secret], value)
	}
	if f := os.Getenv("HIOK_OUTPUT_ENV"); f != "" {
		appendLine(f, fmt.Sprintf("%s=%s", strings.ToUpper(name), value))
	}
}

func appendLine(file, line string) {
	f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintln(f, line)
}

// ── registry ────────────────────────────────────────────────────────────────

func registryCredentials(app *App, ctx context.Context, ref string) (server, user, pass string, err error) {
	reg, err := resolve(app, ctx, "/api/container-registry", "registry", ref, "registryName", "name")
	if err != nil {
		return
	}
	resp, err := post(app, ctx, "GET", "/api/container-registry/"+str(reg, "id")+"/credentials", nil)
	if err != nil {
		return
	}
	d := dataOf(resp)
	return str(d, "loginServer"), str(d, "username"), str(d, "password"), nil
}

func registryDeployCommands() []Command {
	var build, dockerfile, repo, tag, platform string
	var buildArgs multiFlag
	var show bool
	return []Command{
		{
			Name: "login", Summary: "docker login to a registry (for later pushes and pulls)",
			Run: func(ctx context.Context, app *App, args []string) error {
				if len(args) != 1 {
					return fmt.Errorf("usage: hiok registry login <registry>")
				}
				server, user, pass, err := registryCredentials(app, ctx, args[0])
				if err != nil {
					return err
				}
				if err := run(pass, "docker", "login", server, "-u", user, "--password-stdin"); err != nil {
					return err
				}
				setOutput("login_server", server, false)
				app.Print.Message("Logged in to %s", server)
				return nil
			},
		},
		{
			Name: "credentials", Summary: "Login server, user and password for a registry",
			Flags: func(fs *flag.FlagSet) {
				fs.BoolVar(&show, "show-password", false, "print the password instead of masking it")
			},
			Run: func(ctx context.Context, app *App, args []string) error {
				if len(args) != 1 {
					return fmt.Errorf("usage: hiok registry credentials <registry>")
				}
				server, user, pass, err := registryCredentials(app, ctx, args[0])
				if err != nil {
					return err
				}
				setOutput("login_server", server, false)
				setOutput("username", user, false)
				setOutput("password", pass, true)
				if !show {
					pass = "******** (use --show-password)"
				}
				return app.Print.Record(map[string]any{"loginServer": server, "username": user, "password": pass}, "loginServer", "username", "password")
			},
		},
		{
			Name: "push", Summary: "Build (optional), tag and push an image to a registry",
			Flags: func(fs *flag.FlagSet) {
				fs.StringVar(&build, "build", "", "build this context directory first (docker build)")
				fs.StringVar(&dockerfile, "file", "", "Dockerfile, when not <context>/Dockerfile")
				fs.StringVar(&repo, "repository", "", "repository in the registry (default: the local image's name)")
				fs.StringVar(&tag, "tag", "", "tag (default: the local tag, else the commit SHA, else latest)")
				fs.StringVar(&platform, "platform", "", "target platform for the build, e.g. linux/amd64")
				fs.Var(&buildArgs, "build-arg", "KEY=VALUE for the build (repeatable)")
			},
			Run: func(ctx context.Context, app *App, args []string) error {
				if len(args) < 1 || len(args) > 2 {
					return fmt.Errorf("usage: hiok registry push <registry> [local-image[:tag]] [--build DIR]")
				}
				server, user, pass, err := registryCredentials(app, ctx, args[0])
				if err != nil {
					return err
				}
				local := ""
				if len(args) == 2 {
					local = args[1]
				}
				name, localTag := local, ""
				if i := strings.LastIndex(local, ":"); i > strings.LastIndex(local, "/") {
					name, localTag = local[:i], local[i+1:]
				}
				if repo == "" {
					repo = path.Base(name)
				}
				if repo == "" || repo == "." {
					return fmt.Errorf("say which repository with --repository")
				}
				if tag == "" {
					tag = firstNonEmpty(localTag, os.Getenv("GITHUB_SHA"), os.Getenv("BUILD_SOURCEVERSION"), os.Getenv("CI_COMMIT_SHA"), "latest")
					if len(tag) == 40 {
						tag = tag[:12]
					}
				}
				target := fmt.Sprintf("%s/%s:%s", server, repo, tag)
				if err := run(pass, "docker", "login", server, "-u", user, "--password-stdin"); err != nil {
					return err
				}
				if build != "" {
					bargs := []string{"build", "-t", target}
					if dockerfile != "" {
						bargs = append(bargs, "-f", dockerfile)
					}
					if platform != "" {
						bargs = append(bargs, "--platform", platform)
					}
					for _, a := range buildArgs {
						bargs = append(bargs, "--build-arg", a)
					}
					if err := run("", "docker", append(bargs, build)...); err != nil {
						return err
					}
				} else {
					if local == "" {
						return fmt.Errorf("give a local image to push, or --build DIR to build one")
					}
					if err := run("", "docker", "tag", local, target); err != nil {
						return err
					}
				}
				if err := run("", "docker", "push", target); err != nil {
					return err
				}
				setOutput("image", target, false)
				app.Print.Message("Pushed %s", target)
				fmt.Println(target)
				return nil
			},
		},
	}
}

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// ── container apps ──────────────────────────────────────────────────────────

func appDeployCommands() []Command {
	var image string
	var port int
	var cpu, ram float64
	var envs multiFlag
	var wait time.Duration
	return []Command{{
		Name: "deploy", Summary: "Roll a container app onto a new image (a new revision, traffic moved when healthy)",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&image, "image", "", "image to run (required)")
			fs.IntVar(&port, "port", 0, "target port (default: unchanged)")
			fs.Float64Var(&cpu, "cpu", 0, "vCPU (default: unchanged)")
			fs.Float64Var(&ram, "memory", 0, "memory in GiB (default: unchanged)")
			fs.Var(&envs, "env", "KEY=VALUE environment variable (repeatable)")
			fs.DurationVar(&wait, "wait", 5*time.Minute, "how long to wait for the revision to become active (0: don't wait)")
		},
		Run: func(ctx context.Context, app *App, args []string) error {
			if len(args) != 1 || image == "" {
				return fmt.Errorf("usage: hiok app deploy <app> --image IMAGE [--env K=V] [--port N]")
			}
			apps, err := app.Client.ContainerApps(ctx)
			if err != nil {
				return err
			}
			var target map[string]any
			for _, a := range apps {
				if strings.EqualFold(str(a, "id"), args[0]) || strings.EqualFold(str(a, "name"), args[0]) {
					target = a
				}
			}
			if target == nil {
				return fmt.Errorf("no container app named %q", args[0])
			}
			id := str(target, "id")
			body := map[string]any{"image": image, "activate": true}
			if port > 0 {
				body["targetPort"] = port
			}
			if cpu > 0 {
				body["vCpu"] = cpu
			}
			if ram > 0 {
				body["ramGb"] = ram
			}
			if len(envs) > 0 {
				body["environment"] = splitKeyValues(envs)
			}
			resp, err := post(app, ctx, "POST", "/api/ContainerApp/"+id+"/revisions", body)
			if err != nil {
				return err
			}
			revision := str(dataOf(resp), "revisionName", "name")
			app.Print.Message("Revision %s created for %s", firstNonEmpty(revision, "(new)"), str(target, "name"))
			setOutput("revision", revision, false)
			if wait == 0 {
				return nil
			}
			deadline := time.Now().Add(wait)
			for time.Now().Before(deadline) {
				revs, err := listItems(app, ctx, "/api/ContainerApp/"+id+"/revisions")
				if err == nil {
					for _, r := range revs {
						if revision != "" && str(r, "revisionName", "name") != revision {
							continue
						}
						status := strings.ToLower(str(r, "status", "state", "provisioningState"))
						active := strings.EqualFold(str(r, "active"), "true") || status == "active" || status == "running"
						if active && (revision != "" || str(r, "image") == image) {
							app.Print.Message("Revision %s is active.", str(r, "revisionName", "name"))
							setOutput("url", str(target, "url", "fqdn", "hostname"), false)
							return nil
						}
						if status == "failed" || status == "unhealthy" {
							return fmt.Errorf("revision %s failed: %s", str(r, "revisionName", "name"), str(r, "error", "statusMessage"))
						}
					}
				}
				time.Sleep(5 * time.Second)
			}
			return fmt.Errorf("revision did not become active within %s", wait)
		},
	}}
}

// ── containers ──────────────────────────────────────────────────────────────

func dockerDeployCommands() []Command {
	var image, region, ports, env string
	var cpus float64
	var memoryMb int
	return []Command{{
		Name: "deploy", Summary: "Create or replace a container with a new image, keeping its settings",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&image, "image", "", "image to run (required)")
			fs.StringVar(&region, "region", "", "region (new containers)")
			fs.StringVar(&ports, "ports", "", "HOST:CONTAINER[,…] (default: the existing container's)")
			fs.StringVar(&env, "env", "", "KEY=VALUE[,…] (default: the existing container's)")
			fs.Float64Var(&cpus, "cpus", 0, "CPU limit")
			fs.IntVar(&memoryMb, "memory", 0, "memory limit in MiB")
		},
		Run: func(ctx context.Context, app *App, args []string) error {
			if len(args) != 1 || image == "" {
				return fmt.Errorf("usage: hiok docker deploy <name> --image IMAGE [--ports 8080:80] [--env K=V,…]")
			}
			name := args[0]
			if err := app.RequireToken(); err != nil {
				return err
			}
			// Keep what the running container has unless told otherwise.
			var inspect map[string]any
			exists := app.Client.Do(ctx, "GET", "/api/Containers/"+url.PathEscape(name)+"/inspect", nil, &inspect, true) == nil
			envMap := map[string]string{}
			var portList []map[string]any
			if exists {
				d := dataOf(inspect)
				// Only what was deployed, not what the old image baked in (its own
				// PATH, versions…): deploy records the variables it set in a label.
				if labels, ok := d["labels"].(map[string]any); ok {
					_ = json.Unmarshal([]byte(str(labels, "hiok.deploy.env")), &envMap)
				}
				if ns, ok := d["networkSettings"].(map[string]any); ok {
					if pm, ok := ns["Ports"].(map[string]any); ok {
						for cport, host := range pm {
							if h := fmt.Sprint(host); host != nil && h != "" && h != "<nil>" {
								portList = append(portList, map[string]any{"hostPort": atoi(h), "containerPort": atoi(strings.Split(cport, "/")[0]), "protocol": "tcp"})
							}
						}
					}
				}
			}
			if env != "" {
				envMap = splitKeyValues(splitList(env))
			}
			if ports != "" {
				portList = parsePorts(ports)
			}
			var envList []string
			for k, v := range envMap {
				envList = append(envList, k+"="+v)
			}
			sort.Strings(envList)
			imageName, imageTag := image, "latest"
			if i := strings.LastIndex(image, ":"); i > strings.LastIndex(image, "/") {
				imageName, imageTag = image[:i], image[i+1:]
			}
			if exists {
				app.Print.Message("Replacing %s…", name)
				if _, err := post(app, ctx, "POST", "/api/Containers/deletecontainer", map[string]any{"containerName": name, "force": true}); err != nil {
					return err
				}
			}
			body := map[string]any{
				"containerName": name, "name": name, "image": image, "imageName": imageName, "imageTag": imageTag,
				"env": envList, "ports": portList, "restartPolicy": "unless-stopped", "region": app.Region(region),
				"labels": map[string]string{"hiok.deploy.env": string(mustJSON(envMap))},
			}
			if cpus > 0 {
				body["cpus"] = cpus
			}
			if memoryMb > 0 {
				body["memoryBytes"] = int64(memoryMb) * 1024 * 1024
			}
			resp, err := post(app, ctx, "POST", "/api/Containers/createcontainer", body)
			if err != nil {
				return err
			}
			app.Print.Message("%s is running %s. %s", name, image, str(resp, "message"))
			return nil
		},
	}}
}

// ── container jobs ──────────────────────────────────────────────────────────

func jobTopic() Topic {
	var wait time.Duration
	var noLogs bool
	return Topic{
		Name:    "job",
		Summary: "Container Jobs: run on demand or on a schedule",
		Commands: []Command{
			{
				Name: "list", Summary: "List jobs",
				Run: func(ctx context.Context, app *App, args []string) error {
					return get(app, ctx, "/api/container-jobs", "name", "status", "schedule", "lastRunStatus", "nextRunAt")
				},
			},
			{
				Name: "run", Summary: "Start a job now; with --wait, follow it and fail when it fails",
				Flags: func(fs *flag.FlagSet) {
					fs.DurationVar(&wait, "wait", 30*time.Minute, "wait this long for the run to finish (0: don't wait)")
					fs.BoolVar(&noLogs, "no-logs", false, "don't print the run's output")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if len(args) != 1 {
						return fmt.Errorf("usage: hiok job run <job> [--wait 30m]")
					}
					job, err := resolve(app, ctx, "/api/container-jobs", "Container Job", args[0], "name")
					if err != nil {
						return err
					}
					id := str(job, "id")
					resp, err := post(app, ctx, "POST", "/api/container-jobs/"+id+"/run", map[string]any{})
					if err != nil {
						return err
					}
					runID := str(dataOf(resp), "id")
					app.Print.Message("Started %s (run %s)", str(job, "name"), runID)
					setOutput("run_id", runID, false)
					if wait == 0 || runID == "" {
						return nil
					}
					deadline := time.Now().Add(wait)
					for time.Now().Before(deadline) {
						time.Sleep(4 * time.Second)
						r, err := post(app, ctx, "GET", "/api/container-jobs/"+id+"/runs/"+runID, nil)
						if err != nil {
							continue
						}
						d := dataOf(r)
						switch status := str(d, "status"); status {
						case "succeeded", "failed", "timed_out", "cancelled":
							if !noLogs && str(d, "logs") != "" {
								fmt.Fprintln(os.Stderr, "── output ──")
								fmt.Fprintln(os.Stderr, str(d, "logs"))
							}
							setOutput("status", status, false)
							setOutput("exit_code", str(d, "exitCode"), false)
							if status != "succeeded" {
								return fmt.Errorf("run %s %s (exit %s) %s", runID, status, firstNonEmpty(str(d, "exitCode"), "?"), str(d, "error"))
							}
							app.Print.Message("Run %s succeeded in %sms.", runID, str(d, "durationMs"))
							return nil
						}
					}
					return fmt.Errorf("run %s did not finish within %s", runID, wait)
				},
			},
			{
				Name: "runs", Summary: "Recent runs of a job",
				Run: func(ctx context.Context, app *App, args []string) error {
					if len(args) != 1 {
						return fmt.Errorf("usage: hiok job runs <job>")
					}
					job, err := resolve(app, ctx, "/api/container-jobs", "Container Job", args[0], "name")
					if err != nil {
						return err
					}
					return get(app, ctx, "/api/container-jobs/"+str(job, "id")+"/runs", "id", "trigger", "status", "exitCode", "startedAt", "durationMs")
				},
			},
		},
	}
}

// ── virtual machines ────────────────────────────────────────────────────────

func vmDeployCommands() []Command {
	var command, script, user string
	var ipv6 bool
	power := func(action string) Command {
		return Command{
			Name: action, Summary: strings.ToUpper(action[:1]) + action[1:] + " a virtual machine",
			Run: func(ctx context.Context, app *App, args []string) error {
				if len(args) != 1 {
					return fmt.Errorf("usage: hiok vm %s <name>", action)
				}
				steps := map[string][]string{"start": {"start-vm"}, "stop": {"stop-vm"}, "restart": {"stop-vm", "start-vm"}}[action]
				for _, s := range steps {
					if _, err := post(app, ctx, "POST", "/api/VirtualMachine/"+s, map[string]any{"vmName": args[0]}); err != nil {
						return err
					}
				}
				app.Print.Message("%s: %s done.", args[0], action)
				return nil
			},
		}
	}
	return []Command{
		power("start"), power("stop"), power("restart"),
		{
			Name: "ssh", Summary: "SSH to a VM with its platform key — interactive, a --command, or a --script file",
			Flags: func(fs *flag.FlagSet) {
				fs.StringVar(&command, "command", "", "run this and exit with its status")
				fs.StringVar(&script, "script", "", "run this local script file on the VM (bash)")
				fs.StringVar(&user, "user", "", "guest account (default: the one the VM was built with)")
				fs.BoolVar(&ipv6, "ipv6", false, "connect over the VM's own IPv6 address instead of the IPv4 relay")
			},
			Run: func(ctx context.Context, app *App, args []string) error {
				if len(args) != 1 {
					return fmt.Errorf("usage: hiok vm ssh <name> [--command CMD | --script FILE]")
				}
				resp, err := post(app, ctx, "GET", "/api/VirtualMachine/"+url.PathEscape(args[0])+"/connect", nil)
				if err != nil {
					return err
				}
				info := dataOf(resp)
				if !strings.EqualFold(str(info, "hasPrivateKey"), "true") {
					return fmt.Errorf("%s has no platform-held key (built with a password or your own key); use your own SSH client", args[0])
				}
				key, err := app.Client.DoRaw(ctx, "GET", "/api/VirtualMachine/"+url.PathEscape(args[0])+"/ssh-key")
				if err != nil {
					return err
				}
				dir, err := os.MkdirTemp("", "hiok-ssh-")
				if err != nil {
					return err
				}
				defer os.RemoveAll(dir)
				keyFile := filepath.Join(dir, "id")
				if err := os.WriteFile(keyFile, key, 0o600); err != nil {
					return err
				}
				host, port := str(info, "preferredAddress"), "22"
				// CI runners rarely have IPv6; the IPv4 relay (host:port) works from anywhere.
				if ep := str(info, "iPv4SshEndpoint", "ipv4SshEndpoint", "IPv4SshEndpoint"); ep != "" && !ipv6 {
					if h, p, ok := strings.Cut(ep, ":"); ok {
						host, port = h, p
					}
				}
				if host == "" {
					return fmt.Errorf("%s has no address to connect to", args[0])
				}
				sshArgs := []string{"-i", keyFile, "-p", port, "-o", "StrictHostKeyChecking=accept-new", "-o", "UserKnownHostsFile=" + filepath.Join(dir, "known_hosts"),
					"-o", "ServerAliveInterval=30", firstNonEmpty(user, str(info, "username"), "hiok") + "@" + host}
				var stdin *os.File
				switch {
				case script != "":
					f, err := os.Open(script)
					if err != nil {
						return err
					}
					defer f.Close()
					stdin = f
					sshArgs = append(sshArgs, "bash -s")
				case command != "":
					sshArgs = append(sshArgs, command)
				}
				cmd := exec.Command("ssh", sshArgs...)
				cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
				if stdin != nil {
					cmd.Stdin = stdin
				} else {
					cmd.Stdin = os.Stdin
				}
				return cmd.Run()
			},
		},
	}
}

// ── storage ─────────────────────────────────────────────────────────────────

func storageAccountID(app *App, ctx context.Context, ref string) (string, error) {
	acct, err := resolve(app, ctx, "/api/StorageAccount", "storage account", ref, "name", "displayName")
	if err != nil {
		return "", err
	}
	return str(acct, "id"), nil
}

func storageDeployCommands() []Command {
	var prefix, kind string
	return []Command{
		{
			Name: "upload", Summary: "Upload a file or a whole folder to a container (any size)",
			Flags: func(fs *flag.FlagSet) {
				fs.StringVar(&prefix, "prefix", "", "put everything under this path in the container")
				fs.StringVar(&kind, "create", "", "create the container if missing: blob, fileshare")
			},
			Run: func(ctx context.Context, app *App, args []string) error {
				if len(args) != 3 {
					return fmt.Errorf("usage: hiok storage upload <account> <container> <file-or-folder> [--prefix p/]")
				}
				id, err := storageAccountID(app, ctx, args[0])
				if err != nil {
					return err
				}
				s := app.Client.Storage()
				if kind != "" {
					if err := s.EnsureContainer(ctx, id, args[1], kind); err != nil {
						return err
					}
				}
				root := args[2]
				st, err := os.Stat(root)
				if err != nil {
					return err
				}
				count := 0
				upload := func(local, key string) error {
					key = strings.TrimPrefix(path.Join(prefix, filepath.ToSlash(key)), "/")
					if _, err := s.UploadFile(ctx, id, args[1], key, local); err != nil {
						return fmt.Errorf("%s: %w", local, err)
					}
					count++
					fmt.Fprintf(os.Stderr, "  %s\n", key)
					return nil
				}
				if !st.IsDir() {
					if err := upload(root, filepath.Base(root)); err != nil {
						return err
					}
				} else if err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
					if err != nil || d.IsDir() {
						return err
					}
					rel, _ := filepath.Rel(root, p)
					return upload(p, rel)
				}); err != nil {
					return err
				}
				app.Print.Message("Uploaded %d file(s) to %s/%s.", count, args[0], args[1])
				return nil
			},
		},
		{
			Name: "download", Summary: "Download an object, or everything under a prefix, to a folder",
			Run: func(ctx context.Context, app *App, args []string) error {
				if len(args) != 4 {
					return fmt.Errorf("usage: hiok storage download <account> <container> <key-or-prefix> <local-folder>")
				}
				id, err := storageAccountID(app, ctx, args[0])
				if err != nil {
					return err
				}
				s := app.Client.Storage()
				objects, err := s.List(ctx, id, args[1], args[2])
				if err != nil {
					return err
				}
				n := 0
				for _, o := range objects {
					key := o.Key
					if key == "" || strings.HasSuffix(key, "/") {
						continue
					}
					dest := filepath.Join(args[3], filepath.FromSlash(key))
					if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
						return err
					}
					if _, err := s.DownloadFile(ctx, id, args[1], key, dest); err != nil {
						return fmt.Errorf("%s: %w", key, err)
					}
					n++
					fmt.Fprintf(os.Stderr, "  %s\n", dest)
				}
				app.Print.Message("Downloaded %d file(s).", n)
				return nil
			},
		},
	}
}

// ── key vault ───────────────────────────────────────────────────────────────

func keyVaultDeployCommands() []Command {
	var format, names string
	return []Command{{
		Name: "export", Summary: "Put a vault's secrets into the pipeline as (masked) variables, or print them as env/json",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&format, "format", "auto", "auto (the CI you run in), github, azure-devops, dotenv, env, json")
			fs.StringVar(&names, "names", "", "only these secrets (comma-separated); default all")
		},
		Run: func(ctx context.Context, app *App, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("usage: hiok keyvault export <vault> [--names a,b] [--format …]")
			}
			if err := app.RequireToken(); err != nil {
				return err
			}
			vaults, err := app.Client.KeyVaults(ctx)
			if err != nil {
				return err
			}
			var vaultID string
			for _, v := range vaults {
				if strings.EqualFold(str(v, "id"), args[0]) || strings.EqualFold(str(v, "name", "vaultName"), args[0]) {
					vaultID = str(v, "id")
				}
			}
			if vaultID == "" {
				return fmt.Errorf("no key vault named %q", args[0])
			}
			items, err := app.Client.KeyVaultItems(ctx, vaultID)
			if err != nil {
				return err
			}
			want := map[string]bool{}
			for _, n := range splitList(names) {
				want[strings.ToLower(n)] = true
			}
			values := map[string]string{}
			for _, it := range items {
				name := str(it, "name")
				if name == "" || (len(want) > 0 && !want[strings.ToLower(name)]) {
					continue
				}
				if k := strings.ToLower(str(it, "kind", "type", "itemType")); k != "" && k != "secret" {
					continue
				}
				v, err := app.Client.GetSecret(ctx, vaultID, name, "")
				if err != nil {
					return fmt.Errorf("%s: %w", name, err)
				}
				values[name] = str(dataOf(v), "value")
			}
			envName := func(n string) string {
				return strings.ToUpper(strings.NewReplacer("-", "_", ".", "_", " ", "_").Replace(n))
			}
			if format == "auto" {
				switch {
				case os.Getenv("GITHUB_ENV") != "":
					format = "github"
				case os.Getenv("TF_BUILD") != "":
					format = "azure-devops"
				default:
					format = "dotenv"
				}
			}
			keys := make([]string, 0, len(values))
			for k := range values {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			switch format {
			case "github":
				for _, k := range keys {
					fmt.Printf("::add-mask::%s\n", values[k])
					if strings.Contains(values[k], "\n") {
						appendLine(os.Getenv("GITHUB_ENV"), fmt.Sprintf("%s<<HIOK_EOF\n%s\nHIOK_EOF", envName(k), values[k]))
					} else {
						appendLine(os.Getenv("GITHUB_ENV"), envName(k)+"="+values[k])
					}
				}
				app.Print.Message("Exported %d secret(s) as masked environment variables.", len(keys))
			case "azure-devops":
				for _, k := range keys {
					fmt.Printf("##vso[task.setvariable variable=%s;issecret=true]%s\n", envName(k), strings.ReplaceAll(values[k], "\n", "%0A"))
				}
				app.Print.Message("Exported %d secret(s) as secret pipeline variables.", len(keys))
			case "json":
				out, _ := json.MarshalIndent(values, "", "  ")
				fmt.Println(string(out))
			case "env":
				for _, k := range keys {
					fmt.Printf("export %s=%q\n", envName(k), values[k])
				}
			default:
				var b bytes.Buffer
				for _, k := range keys {
					fmt.Fprintf(&b, "%s=%q\n", envName(k), values[k])
				}
				fmt.Print(b.String())
			}
			return nil
		},
	}}
}

// ── kubernetes ──────────────────────────────────────────────────────────────

func kubernetesDeployCommands() []Command {
	var out string
	return []Command{
		{
			Name: "kubeconfig", Summary: "Write a cluster's kubeconfig (for kubectl, helm, …)",
			Flags: func(fs *flag.FlagSet) {
				fs.StringVar(&out, "out", "", "write here (default: stdout); sets KUBECONFIG for later CI steps")
			},
			Run: func(ctx context.Context, app *App, args []string) error {
				if len(args) != 1 {
					return fmt.Errorf("usage: hiok kubernetes kubeconfig <cluster> [--out FILE]")
				}
				cl, err := resolve(app, ctx, "/api/kubernetes/clusters", "cluster", args[0], "name")
				if err != nil {
					return err
				}
				raw, err := app.Client.DoRaw(ctx, "GET", "/api/kubernetes/clusters/"+str(cl, "id")+"/kubeconfig")
				if err != nil {
					return err
				}
				// The endpoint answers either the YAML itself or {data:{kubeconfig:"…"}}.
				var env map[string]any
				if json.Unmarshal(raw, &env) == nil {
					if s := str(dataOf(env), "kubeconfig", "content", "yaml"); s != "" {
						raw = []byte(s)
					}
				}
				if out == "" {
					_, err := os.Stdout.Write(raw)
					return err
				}
				if err := os.WriteFile(out, raw, 0o600); err != nil {
					return err
				}
				abs, _ := filepath.Abs(out)
				if f := os.Getenv("GITHUB_ENV"); f != "" {
					appendLine(f, "KUBECONFIG="+abs)
				}
				if os.Getenv("TF_BUILD") != "" {
					fmt.Printf("##vso[task.setvariable variable=KUBECONFIG]%s\n", abs)
				}
				app.Print.Message("Wrote %s", abs)
				return nil
			},
		},
		{
			Name: "kubectl", Summary: "Run a kubectl command on the cluster from HIOK (no local kubectl needed)",
			Run: func(ctx context.Context, app *App, args []string) error {
				if len(args) < 2 {
					return fmt.Errorf("usage: hiok kubernetes kubectl <cluster> \"get pods -A\"")
				}
				cl, err := resolve(app, ctx, "/api/kubernetes/clusters", "cluster", args[0], "name")
				if err != nil {
					return err
				}
				resp, err := post(app, ctx, "POST", "/api/kubernetes/clusters/"+str(cl, "id")+"/kubectl", map[string]any{"command": strings.Join(args[1:], " ")})
				if err != nil {
					return err
				}
				d := dataOf(resp)
				fmt.Print(firstNonEmpty(str(d, "output", "stdout"), str(resp, "message")))
				if e := str(d, "stderr", "error"); e != "" {
					fmt.Fprintln(os.Stderr, e)
				}
				if code := str(d, "exitCode"); code != "" && code != "0" {
					return fmt.Errorf("kubectl exited %s", code)
				}
				return nil
			},
		},
	}
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
