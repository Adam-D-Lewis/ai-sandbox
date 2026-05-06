// psb — launch a Docker (colima) sandbox per project. Image bundles `pi`
// and `claude`; pick whichever you need from inside the shell.
//
// Per-project overrides via ~/.config/ai-sandbox/config.json:
//
//	{
//	  "default": {
//	    "mounts": ["{{HOME}}/.pi/agent/skills", "{{HOME}}/dev/dotfiles", "{{CWD}}"],
//	    "extra_mounts": ["~/dev/shared"]
//	  },
//	  "projects": {
//	    "/Users/me/dev/some-project":     { "extra_mounts": ["~/dev/shared-lib"] },
//	    "/Users/me/dev/big-monorepo":     { "memory": "16g" }
//	  }
//	}
//
// "mounts" replaces the built-in default list.  Use {{HOME}}, {{SHARED_DIR}},
// {{CWD}} as placeholders, or ~/ and $VAR for shell-style expansion.
package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/aktech/ai-sandbox/internal/dx"
	"github.com/aktech/ai-sandbox/internal/mountresolver"
)

//go:embed Dockerfile
var embeddedDockerfile []byte

// ---------- config types ----------

type projectCfg struct {
	Memory      string   `json:"memory,omitempty"`
	CPUs        any      `json:"cpus,omitempty"` // number or string in JSON
	Image       string   `json:"image,omitempty"`
	Mounts      []string `json:"mounts,omitempty"`       // declarative mount list (replaces defaults)
	ExtraMounts []string `json:"extra_mounts,omitempty"` // appended after mounts
}

type configFile struct {
	Default  projectCfg            `json:"default"`
	Projects map[string]projectCfg `json:"projects"`
}

type effectiveCfg struct {
	Image       string
	Memory      string
	CPUs        string
	SharedDir   string
	Mounts      []string // declarative mounts from config; empty = use defaults
	ExtraMounts []string // appended after mounts
}

// ---------- env defaults ----------

func envDefault(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func cfgPath() string {
	return envDefault("PSB_CONFIG_FILE", filepath.Join(os.Getenv("HOME"), ".config", "ai-sandbox", "config.json"))
}

// ---------- log helpers ----------

type logger struct {
	prefix string
	color  bool
}

func newLogger(prefix string) *logger {
	return &logger{prefix: prefix, color: isTerminal(1)}
}

func (l *logger) tag(c, msg string) string {
	if !l.color {
		return fmt.Sprintf("[%s] %s", l.prefix, msg)
	}
	return fmt.Sprintf("\033[%sm[%s]\033[0m %s", c, l.prefix, msg)
}

func (l *logger) Log(msg string)  { fmt.Println(l.tag("0;36", msg)) }
func (l *logger) Step(msg string) { fmt.Println(l.tag("0;36", "→ "+msg)) }
func (l *logger) OK(msg string)   { fmt.Println(l.tag("0;32", msg)) }
func (l *logger) Warn(msg string) { fmt.Fprintln(os.Stderr, l.tag("0;33", msg)) }
func (l *logger) Die(msg string, code int) {
	fmt.Fprintln(os.Stderr, l.tag("0;31", msg))
	os.Exit(code)
}

func isTerminal(fd uintptr) bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// ---------- helpers ----------

func sanitizeName(s string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9_-]+`)
	cleaned := re.ReplaceAllString(s, "-")
	return strings.Trim(cleaned, "-")
}

func containerName(prefix string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%s", prefix, sanitizeName(filepath.Base(cwd))), nil
}

// ---------- config load ----------

func loadConfig(path, project string, base effectiveCfg) effectiveCfg {
	cfg := base
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg // missing file is fine
	}
	var raw configFile
	if err := json.Unmarshal(data, &raw); err != nil {
		fmt.Fprintf(os.Stderr, "warn: %s parse failed: %v\n", path, err)
		return cfg
	}
	apply := func(p projectCfg) {
		if p.Memory != "" {
			cfg.Memory = p.Memory
		}
		if p.Image != "" {
			cfg.Image = p.Image
		}
		if p.CPUs != nil {
			cfg.CPUs = fmt.Sprintf("%v", p.CPUs)
		}
		cfg.Mounts = append(cfg.Mounts, p.Mounts...)
		cfg.ExtraMounts = append(cfg.ExtraMounts, p.ExtraMounts...)
	}
	apply(raw.Default)
	if pc, ok := raw.Projects[project]; ok {
		apply(pc)
	}
	return cfg
}

// ---------- create ----------

func createContainer(log *logger, docker dx.Executor, name string, cfg effectiveCfg, home, cwd string) error {
	log.Step(fmt.Sprintf("creating container %s (image=%s, mem=%s, cpus=%s)", name, cfg.Image, cfg.Memory, cfg.CPUs))
	if err := os.MkdirAll(cfg.SharedDir, 0o755); err != nil {
		return err
	}
	mounts := mountresolver.Resolve(cfg.Mounts, cfg.ExtraMounts,
		mountresolver.Env{Home: home, CWD: cwd, SharedDir: cfg.SharedDir}, log)
	return dx.Create(docker, dx.ContainerSpec{
		Name:    name,
		Image:   cfg.Image,
		Memory:  cfg.Memory,
		CPUs:    cfg.CPUs,
		Workdir: cwd,
		Labels:  map[string]string{"psb.cwd": cwd},
		Env: map[string]string{
			"HOME":              home,
			"SB_SHARED":         cfg.SharedDir,
			"HOMELAB_URL":       os.Getenv("HOMELAB_URL"),
			"ANTHROPIC_API_KEY": os.Getenv("ANTHROPIC_API_KEY"),
		},
		Mounts: mounts,
	})
}

// ---------- commands ----------

func cmdUp(log *logger, docker dx.Executor, name string, cfg effectiveCfg, home, cwd string) error {
	log.Log(fmt.Sprintf("project: %s  container: %s", filepath.Base(cwd), name))

	if !dx.ImageExists(docker, cfg.Image) {
		log.Die(fmt.Sprintf("image %s not found — run `psb build`", cfg.Image), 1)
	}

	if dx.ContainerExists(docker, name) {
		if !dx.ContainerRunning(docker, name) {
			log.Step("starting existing container")
			if err := dx.Start(docker, name); err != nil {
				return err
			}
		} else {
			log.Log("container already running")
		}
	} else {
		if err := createContainer(log, docker, name, cfg, home, cwd); err != nil {
			return err
		}
		log.OK("container created")
	}

	log.OK("entering shell — run `pi` (or `claude`) inside")
	return dx.Shell(docker, name)
}

func cmdStop(log *logger, docker dx.Executor, name string) error {
	if !dx.ContainerExists(docker, name) {
		log.Die(fmt.Sprintf("container %s does not exist", name), 1)
	}
	log.Step("stopping " + name)
	if err := dx.Stop(docker, name); err != nil {
		return err
	}
	log.OK("stopped")
	return nil
}

func cmdRM(log *logger, docker dx.Executor, name string) error {
	if !dx.ContainerExists(docker, name) {
		log.Die(fmt.Sprintf("container %s does not exist", name), 1)
	}
	log.Step("removing " + name + " (force)")
	if err := dx.Remove(docker, name); err != nil {
		return err
	}
	log.OK("removed")
	return nil
}

func cmdStatus(log *logger, docker dx.Executor, name string) error {
	if !dx.ContainerExists(docker, name) {
		log.Warn("container " + name + " does not exist")
		return nil
	}
	return dx.PrintStatusTable(docker, name)
}

func cmdLS(docker dx.Executor) error {
	names, err := dx.ListNames(docker, "psb-")
	if err != nil {
		return err
	}
	if len(names) == 0 {
		fmt.Println("no psb-* containers")
		return nil
	}
	infos, err := dx.Inspect(docker, names...)
	if err != nil {
		return err
	}
	rows := [][5]string{{"name", "uptime", "cpus", "mem", "cwd"}}
	for _, i := range infos {
		rows = append(rows, [5]string{i.Name, dx.CompactUptime(i.Status, i.StartedAt),
			dx.CompactCPUs(i.NanoCpus), dx.CompactMem(i.Memory), i.CWDLabel})
	}
	w := [5]int{}
	for _, r := range rows {
		for i, c := range r {
			if n := len(c); n > w[i] {
				w[i] = n
			}
		}
	}
	pad := func(s string, n int) string { return s + strings.Repeat(" ", n-len(s)) }
	for i, r := range rows {
		c0 := pad(r[0], w[0])
		c1 := pad(r[1], w[1])
		c2 := pad(r[2], w[2])
		c3 := pad(r[3], w[3])
		c4 := r[4]
		if i == 0 {
			fmt.Printf("\033[1;36m%s\033[0m  \033[1;36m%s\033[0m  \033[1;36m%s\033[0m  \033[1;36m%s\033[0m  \033[1;36m%s\033[0m\n",
				c0, c1, c2, c3, c4)
		} else {
			st := fmt.Sprintf("\033[2m%s\033[0m", c1)
			if last := r[1][len(r[1])-1]; last == 's' || last == 'm' || last == 'h' || last == 'd' {
				st = fmt.Sprintf("\033[32m%s\033[0m", c1)
			}
			fmt.Printf("\033[35m%s\033[0m  %s  \033[33m%s\033[0m  \033[33m%s\033[0m  \033[34m%s\033[0m\n",
				c0, st, c2, c3, c4)
		}
	}
	return nil
}

func cmdBuild(log *logger) error {
	image := envDefault("PSB_IMAGE_NAME", "ai-sandbox-pi:latest")
	piVersion := envDefault("PI_VERSION", "latest")
	uid := fmt.Sprintf("%d", os.Getuid())
	gid := fmt.Sprintf("%d", os.Getgid())
	home := os.Getenv("HOME")
	if home == "" {
		return fmt.Errorf("HOME not set")
	}

	tmp, err := os.MkdirTemp("", "psb-build-*")
	if err != nil {
		return fmt.Errorf("temp dir: %w", err)
	}
	defer os.RemoveAll(tmp)

	if err := os.WriteFile(filepath.Join(tmp, "Dockerfile"), embeddedDockerfile, 0o644); err != nil {
		return fmt.Errorf("write Dockerfile: %w", err)
	}

	log.Step(fmt.Sprintf("building %s (pi=%s, home=%s, uid=%s, gid=%s)", image, piVersion, home, uid, gid))
	build := exec.Command("docker", "build",
		"--build-arg", "PI_VERSION="+piVersion,
		"--build-arg", "AGENT_UID="+uid,
		"--build-arg", "AGENT_GID="+gid,
		"--build-arg", "AGENT_HOME="+home,
		"-t", image,
		tmp,
	)
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		return err
	}

	log.OK("built " + image)
	list := exec.Command("docker", "images",
		"--format", "table {{.Repository}}:{{.Tag}}\t{{.Size}}\t{{.CreatedSince}}",
		image,
	)
	list.Stdout = os.Stdout
	list.Stderr = os.Stderr
	return list.Run()
}

// ---------- main ----------

func usage() {
	fmt.Println(`psb — launch a Docker sandbox per project. Image bundles pi + claude.

Usage:
  psb              create or attach + shell into psb-<project>
  psb stop         stop the project's container
  psb rm [n...]    destroy current container, or named ones
  psb status       show container status
  psb ls           list all psb-* containers
  psb build        (re)build the image

Config file (JSON):
  ` + filepath.Join(os.Getenv("HOME"), ".config/ai-sandbox/config.json") + `

  Keys:
    mounts         declarative mount list (one entry per -v src:src)
    extra_mounts   appended after mounts
    memory / cpus  resource limits
    image          custom image tag

  Template vars in mounts: {{HOME}}, {{SHARED_DIR}}, {{CWD}}
  Also supports ~/ and $ENV_VAR expansion.

Env vars (override config defaults):
  PSB_IMAGE_NAME   image tag (default: ai-sandbox-pi:latest)
  PSB_MEMORY       memory limit (default: 4g)
  PSB_CPUS         cpu limit (default: 2)
  PSB_SHARED_DIR   host↔container exchange dir (default: ~/sb-shared)
  PSB_CONFIG_FILE  config file path (default: ~/.config/ai-sandbox/config.json)
  HOMELAB_URL      passed through to container
  ANTHROPIC_API_KEY passed through to container (claude API auth)`)
}

func main() {
	log := newLogger("psb")
	docker := dx.Cmd{}

	if _, err := exec.LookPath("docker"); err != nil {
		log.Die("docker not found — install or `colima start`", 1)
	}

	home := os.Getenv("HOME")
	if home == "" {
		log.Die("HOME not set", 1)
	}
	cwd, err := os.Getwd()
	if err != nil {
		log.Die("cannot read cwd: "+err.Error(), 1)
	}

	base := effectiveCfg{
		Image:     envDefault("PSB_IMAGE_NAME", "ai-sandbox-pi:latest"),
		Memory:    envDefault("PSB_MEMORY", "4g"),
		CPUs:      envDefault("PSB_CPUS", "2"),
		SharedDir: envDefault("PSB_SHARED_DIR", filepath.Join(home, "sb-shared")),
	}
	cfg := loadConfig(cfgPath(), cwd, base)
	if len(cfg.Mounts) == 0 {
		cfg.Mounts = []string{"{{CWD}}"}
	}

	name, err := containerName("psb")
	if err != nil {
		log.Die("container name: "+err.Error(), 1)
	}

	sub := "up"
	if len(os.Args) > 1 {
		sub = os.Args[1]
	}

	switch sub {
	case "", "up":
		if err := cmdUp(log, docker, name, cfg, home, cwd); err != nil {
			log.Die(err.Error(), 1)
		}
	case "stop":
		if err := cmdStop(log, docker, name); err != nil {
			log.Die(err.Error(), 1)
		}
	case "rm", "remove":
		// `psb rm`            → current project's container
		// `psb rm name [...]` → explicit list of psb-* containers
		targets := []string{name}
		if len(os.Args) > 2 {
			targets = os.Args[2:]
		}
		for _, t := range targets {
			if err := cmdRM(log, docker, t); err != nil {
				log.Die(err.Error(), 1)
			}
		}
	case "status":
		if err := cmdStatus(log, docker, name); err != nil {
			log.Die(err.Error(), 1)
		}
	case "ls", "list":
		if err := cmdLS(docker); err != nil {
			log.Die(err.Error(), 1)
		}
	case "build":
		if err := cmdBuild(log); err != nil {
			log.Die(err.Error(), 1)
		}
	case "-h", "--help", "help":
		usage()
	default:
		log.Die("unknown command: "+sub+" (use: up | stop | rm | status | ls | build)", 2)
	}
}
