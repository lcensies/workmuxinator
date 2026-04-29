package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	untilAllTasksComplete = "ALL_TASKS_COMPLETE"
	untilApproved         = "APPROVED"
)

var version = "0.3.0"

type Workflow struct {
	Nodes []Node `yaml:"nodes"`
}

type Node struct {
	ID         string   `yaml:"id"`
	DependsOn  []string `yaml:"depends_on"`
	Prompt     string   `yaml:"prompt"`
	PromptFile string   `yaml:"prompt_file"`
	Bash       string   `yaml:"bash"`
	Loop       *Loop    `yaml:"loop"`
	OnFailure  string   `yaml:"on_failure"`
}

type Loop struct {
	Prompt        string `yaml:"prompt"`
	PromptFile    string `yaml:"prompt_file"`
	Until         string `yaml:"until"`
	FreshContext  bool   `yaml:"fresh_context"`
	Interactive   bool   `yaml:"interactive"`
	MaxIterations int    `yaml:"max_iterations"`
}

type AgentConfig struct {
	Command         string   `yaml:"command"`
	Args            []string `yaml:"args"`
	PromptFlag      string   `yaml:"prompt_flag"`
	FreshContextArg string   `yaml:"fresh_context_arg"`
}

type NodeStatus string

const (
	statusPending NodeStatus = "pending"
	statusSuccess NodeStatus = "success"
	statusFailed  NodeStatus = "failed"
	statusSkipped NodeStatus = "skipped"
)

func main() {
	if len(os.Args) < 2 {
		if err := launchLegacy(false, false); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	switch os.Args[1] {
	case "run":
		if shouldUseWorkflowRun(os.Args[2:]) {
			if err := runWorkflowCmd(os.Args[2:]); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			return
		}
		if err := runLegacyCmd(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "open":
		if err := launchLegacy(false, false); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "workflow":
		if len(os.Args) < 3 {
			printHelp()
			os.Exit(1)
		}
		switch os.Args[2] {
		case "run":
			if err := runWorkflowCmd(os.Args[3:]); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
		case "validate":
			if err := validateCmd(os.Args[3:]); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
		default:
			printHelp()
			os.Exit(1)
		}
	case "validate":
		if err := validateCmd(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "add":
		if err := addLegacyCmd(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "rm":
		if err := rmLegacyCmd(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "version", "-V", "--version":
		fmt.Println("workmuxinator", version)
	case "help", "-h", "--help":
		printHelp()
	default:
		printHelp()
		os.Exit(1)
	}
}

func shouldUseWorkflowRun(args []string) bool {
	for _, arg := range args {
		switch arg {
		case "-f", "--agent", "--prompt-flag", "--fresh-context-arg", "--dry-run":
			return true
		}
	}
	return false
}

func runWorkflowCmd(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	workflowPath := fs.String("f", ".workmuxinator/workflows/build-feature.yaml", "workflow yaml path")
	agentCmd := fs.String("agent", "", "agent command override (example: codex|opencode|cursor-agent)")
	promptFlag := fs.String("prompt-flag", "", "agent prompt flag override (example: --prompt)")
	freshContextArg := fs.String("fresh-context-arg", "", "fresh context arg override")
	dryRun := fs.Bool("dry-run", false, "print plan without executing commands")
	if err := fs.Parse(args); err != nil {
		return err
	}

	wf, wfDir, err := loadWorkflow(*workflowPath)
	if err != nil {
		return err
	}
	if err := validateWorkflow(wf); err != nil {
		return err
	}

	cfg, err := loadAgentConfig(".workmuxinator/agent.yaml")
	if err != nil {
		return err
	}
	if *agentCmd != "" {
		cfg.Command = *agentCmd
	}
	if *promptFlag != "" {
		cfg.PromptFlag = *promptFlag
	}
	if *freshContextArg != "" {
		cfg.FreshContextArg = *freshContextArg
	}
	if cfg.Command == "" {
		cfg.Command = "codex"
	}
	applyWorkflowAgentDefaults(&cfg)

	runner := &Runner{
		Workflow:    wf,
		WorkflowDir: wfDir,
		Agent:       cfg,
		DryRun:      *dryRun,
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
		Stdin:       os.Stdin,
	}
	return runner.Run()
}

func validateCmd(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	workflowPath := fs.String("f", ".workmuxinator/workflows/build-feature.yaml", "workflow yaml path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	wf, _, err := loadWorkflow(*workflowPath)
	if err != nil {
		return err
	}
	if err := validateWorkflow(wf); err != nil {
		return err
	}
	fmt.Println("workflow is valid")
	return nil
}

type Runner struct {
	Workflow    *Workflow
	WorkflowDir string
	Agent       AgentConfig
	DryRun      bool
	Stdout      io.Writer
	Stderr      io.Writer
	Stdin       io.Reader
}

func (r *Runner) Run() error {
	index := make(map[string]Node, len(r.Workflow.Nodes))
	order := make([]string, 0, len(r.Workflow.Nodes))
	fallbackTargets := make(map[string]struct{})
	for _, n := range r.Workflow.Nodes {
		index[n.ID] = n
		order = append(order, n.ID)
		if n.OnFailure != "" {
			fallbackTargets[n.OnFailure] = struct{}{}
		}
	}

	status := make(map[string]NodeStatus, len(order))
	for _, id := range order {
		status[id] = statusPending
	}

	fallbackQueue := make([]string, 0)
	for {
		if allDone(status, fallbackTargets) {
			fmt.Fprintln(r.Stdout, "workflow completed")
			return nil
		}

		progress := false

		if len(fallbackQueue) > 0 {
			id := fallbackQueue[0]
			fallbackQueue = fallbackQueue[1:]
			if status[id] != statusPending {
				continue
			}
			n := index[id]
			fmt.Fprintf(r.Stdout, "-> fallback node: %s\n", id)
			err := r.executeNode(n)
			progress = true
			if err != nil {
				status[id] = statusFailed
				return fmt.Errorf("fallback node %q failed: %w", id, err)
			}
			status[id] = statusSuccess
			continue
		}

		for _, id := range order {
			if status[id] != statusPending {
				continue
			}

			n := index[id]
			if !depsCompleted(n, status) {
				continue
			}
			if len(n.DependsOn) == 0 {
				if _, isFallbackOnly := fallbackTargets[n.ID]; isFallbackOnly {
					continue
				}
			}
			if hasFailedDependency(n, status) {
				status[id] = statusSkipped
				fmt.Fprintf(r.Stdout, "-> skip node %s (dependency failed)\n", id)
				progress = true
				continue
			}

			fmt.Fprintf(r.Stdout, "-> run node: %s\n", id)
			err := r.executeNode(n)
			progress = true
			if err != nil {
				status[id] = statusFailed
				fmt.Fprintf(r.Stderr, "node %s failed: %v\n", id, err)
				if n.OnFailure != "" {
					fmt.Fprintf(r.Stdout, "-> schedule fallback: %s\n", n.OnFailure)
					fallbackQueue = append(fallbackQueue, n.OnFailure)
					continue
				}
				return fmt.Errorf("node %q failed", id)
			}
			status[id] = statusSuccess
		}

		if !progress {
			if noActionablePending(status, fallbackTargets) {
				fmt.Fprintln(r.Stdout, "workflow completed")
				return nil
			}
			return errors.New("workflow stuck: unresolved dependencies or cyclic graph")
		}
	}
}

func (r *Runner) executeNode(n Node) error {
	if n.Bash != "" {
		return r.execBash(n.Bash)
	}
	if n.Loop != nil {
		return r.execLoop(*n.Loop)
	}
	if n.Prompt != "" || n.PromptFile != "" {
		prompt, err := r.resolvePrompt(n.Prompt, n.PromptFile)
		if err != nil {
			return err
		}
		_, err = r.execAgent(prompt, false)
		return err
	}
	return fmt.Errorf("node %q has no action", n.ID)
}

func (r *Runner) execLoop(loop Loop) error {
	if r.DryRun {
		prompt, err := r.resolvePrompt(loop.Prompt, loop.PromptFile)
		if err != nil {
			return err
		}
		fmt.Fprintln(r.Stdout, "   [dry-run] loop node")
		_, err = r.execAgent(prompt, loop.FreshContext)
		return err
	}

	maxIterations := loop.MaxIterations
	if maxIterations <= 0 {
		maxIterations = 8
	}

	for i := 1; i <= maxIterations; i++ {
		fmt.Fprintf(r.Stdout, "   loop iteration %d/%d\n", i, maxIterations)

		prompt, err := r.resolvePrompt(loop.Prompt, loop.PromptFile)
		if err != nil {
			return err
		}

		out, err := r.execAgent(prompt, loop.FreshContext)
		if err != nil {
			return err
		}

		if loop.Interactive {
			ok, askErr := askForApproval(r.Stdin, r.Stdout)
			if askErr != nil {
				return askErr
			}
			if ok {
				return nil
			}
			continue
		}

		if loop.Until == "" {
			return nil
		}

		if strings.Contains(strings.ToUpper(out), strings.ToUpper(loop.Until)) {
			return nil
		}
	}

	return fmt.Errorf("loop did not reach condition %q", loop.Until)
}

func (r *Runner) execBash(command string) error {
	if r.DryRun {
		fmt.Fprintf(r.Stdout, "   [dry-run] bash: %s\n", command)
		return nil
	}
	cmd := exec.Command("bash", "-lc", command)
	cmd.Stdout = r.Stdout
	cmd.Stderr = r.Stderr
	return cmd.Run()
}

func (r *Runner) execAgent(prompt string, freshContext bool) (string, error) {
	if r.DryRun {
		fmt.Fprintf(r.Stdout, "   [dry-run] agent: %s\n", oneLine(prompt))
		return "", nil
	}

	args := append([]string{}, r.Agent.Args...)
	if freshContext && r.Agent.FreshContextArg != "" {
		args = append(args, r.Agent.FreshContextArg)
	}

	if r.Agent.PromptFlag != "" {
		args = append(args, r.Agent.PromptFlag, prompt)
	} else {
		args = append(args, prompt)
	}

	cmd := exec.Command(r.Agent.Command, args...)
	var out strings.Builder
	cmd.Stdout = io.MultiWriter(r.Stdout, &out)
	cmd.Stderr = r.Stderr
	err := cmd.Run()
	return out.String(), err
}

func (r *Runner) resolvePrompt(inlinePrompt, promptFile string) (string, error) {
	switch {
	case inlinePrompt != "" && promptFile != "":
		return "", errors.New("use either prompt or prompt_file, not both")
	case promptFile != "":
		path := promptFile
		if !filepath.IsAbs(path) {
			path = filepath.Join(r.WorkflowDir, path)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read prompt_file %q: %w", promptFile, err)
		}
		return strings.TrimSpace(string(b)), nil
	case inlinePrompt != "":
		return inlinePrompt, nil
	default:
		return "", errors.New("missing prompt")
	}
}

func depsCompleted(n Node, status map[string]NodeStatus) bool {
	for _, dep := range n.DependsOn {
		if status[dep] == statusPending {
			return false
		}
	}
	return true
}

func hasFailedDependency(n Node, status map[string]NodeStatus) bool {
	for _, dep := range n.DependsOn {
		if status[dep] == statusFailed || status[dep] == statusSkipped {
			return true
		}
	}
	return false
}

func allDone(status map[string]NodeStatus, fallbackTargets map[string]struct{}) bool {
	for id, st := range status {
		if _, isFallbackOnly := fallbackTargets[id]; isFallbackOnly && st == statusPending {
			continue
		}
		if st == statusPending {
			return false
		}
	}
	return true
}

func noActionablePending(status map[string]NodeStatus, fallbackTargets map[string]struct{}) bool {
	for id, st := range status {
		if st != statusPending {
			continue
		}
		if _, isFallbackOnly := fallbackTargets[id]; isFallbackOnly {
			continue
		}
		return false
	}
	return true
}

func askForApproval(in io.Reader, out io.Writer) (bool, error) {
	reader := bufio.NewReader(in)
	fmt.Fprint(out, "   approval required (y/N): ")
	line, err := reader.ReadString('\n')
	if err != nil {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}

func loadWorkflow(path string) (*Workflow, string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, "", err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, "", err
	}
	var wf Workflow
	if err := yaml.Unmarshal(data, &wf); err != nil {
		return nil, "", err
	}
	return &wf, filepath.Dir(abs), nil
}

func validateWorkflow(wf *Workflow) error {
	if len(wf.Nodes) == 0 {
		return errors.New("workflow has no nodes")
	}

	ids := make(map[string]struct{}, len(wf.Nodes))
	for _, n := range wf.Nodes {
		if n.ID == "" {
			return errors.New("node missing id")
		}
		if _, exists := ids[n.ID]; exists {
			return fmt.Errorf("duplicate node id: %s", n.ID)
		}
		ids[n.ID] = struct{}{}
		if n.Bash == "" && n.Prompt == "" && n.PromptFile == "" && n.Loop == nil {
			return fmt.Errorf("node %q has no action", n.ID)
		}
		if n.Loop != nil && n.Bash != "" {
			return fmt.Errorf("node %q cannot have both bash and loop", n.ID)
		}
		if n.Loop != nil {
			if n.Loop.Prompt == "" && n.Loop.PromptFile == "" {
				return fmt.Errorf("loop node %q requires loop.prompt or loop.prompt_file", n.ID)
			}
			if n.Loop.Until != "" &&
				n.Loop.Until != untilAllTasksComplete &&
				n.Loop.Until != untilApproved {
				return fmt.Errorf("loop node %q has unsupported until condition %q", n.ID, n.Loop.Until)
			}
		}
	}

	for _, n := range wf.Nodes {
		for _, dep := range n.DependsOn {
			if _, exists := ids[dep]; !exists {
				return fmt.Errorf("node %q depends on unknown node %q", n.ID, dep)
			}
		}
		if n.OnFailure != "" {
			if _, exists := ids[n.OnFailure]; !exists {
				return fmt.Errorf("node %q has unknown on_failure target %q", n.ID, n.OnFailure)
			}
		}
	}

	return nil
}

func loadAgentConfig(path string) (AgentConfig, error) {
	cfg := AgentConfig{}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func applyWorkflowAgentDefaults(cfg *AgentConfig) {
	base := filepath.Base(strings.TrimSpace(cfg.Command))
	switch {
	case strings.HasPrefix(base, "opencode"):
		// OpenCode requires an explicit prompt flag in non-interactive mode.
		if cfg.PromptFlag == "" {
			cfg.PromptFlag = "--prompt"
		}
	case strings.HasPrefix(base, "cursor-agent"),
		strings.HasPrefix(base, "coding-agent"):
		// Cursor agent accepts prompt as positional argument by default.
	}
}

func runLegacyCmd(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	resumePrompt := fs.Bool("resume", false, `also send "continue if not completed" prompt`)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unknown argument(s): %s", strings.Join(fs.Args(), " "))
	}
	return launchLegacy(true, *resumePrompt)
}

func launchLegacy(doRun, doResume bool) error {
	if err := checkLegacyDeps(); err != nil {
		return err
	}

	configs, err := listTmuxinatorConfigs()
	if err != nil {
		return err
	}
	if len(configs) == 0 {
		return fmt.Errorf("no tmuxinator configs found in %s", tmuxinatorConfigDir())
	}

	for _, config := range configs {
		if err := processProject(config, doRun, doResume); err != nil {
			return err
		}
	}
	return nil
}

func addLegacyCmd(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: workmuxinator add <dir>")
	}
	dir, err := resolveExistingDir(args[0])
	if err != nil {
		return err
	}

	name := filepath.Base(dir)
	configDir := tmuxinatorConfigDir()
	configPath := filepath.Join(configDir, name+".yml")
	if _, err := os.Stat(configPath); err == nil {
		return fmt.Errorf("config already exists: %s", configPath)
	}
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return err
	}

	content := fmt.Sprintf("name: %s\nroot: %s\n\nwindows:\n  - editor:\n", name, dir)
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "[workmuxinator] INFO  created tmuxinator config: %s\n", configPath)
	return nil
}

func rmLegacyCmd(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: workmuxinator rm <dir>")
	}
	dir, err := resolveExistingDir(args[0])
	if err != nil {
		return err
	}

	configs, err := listTmuxinatorConfigs()
	if err != nil {
		return err
	}
	if len(configs) == 0 {
		return fmt.Errorf("no tmuxinator configs found in %s", tmuxinatorConfigDir())
	}

	var matched string
	for _, config := range configs {
		root, err := tmuxinatorRoot(config)
		if err != nil {
			continue
		}
		if root == dir {
			matched = config
			break
		}
	}

	if matched == "" {
		candidate := filepath.Join(tmuxinatorConfigDir(), filepath.Base(dir)+".yml")
		if _, err := os.Stat(candidate); err == nil {
			matched = candidate
		}
	}
	if matched == "" {
		return fmt.Errorf("no tmuxinator config found for: %s", dir)
	}

	if err := os.Remove(matched); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "[workmuxinator] INFO  removed tmuxinator config: %s\n", matched)
	return nil
}

func checkLegacyDeps() error {
	required := []string{"tmuxinator", "workmux", "tmux"}
	for _, name := range required {
		if _, err := exec.LookPath(name); err != nil {
			return fmt.Errorf("required command not found: %s", name)
		}
	}
	return nil
}

func tmuxinatorConfigDir() string {
	if v := os.Getenv("TMUXINATOR_CONFIG_DIR"); v != "" {
		return expandTilde(v)
	}
	return filepath.Join(os.Getenv("HOME"), ".config", "tmuxinator")
}

func listTmuxinatorConfigs() ([]string, error) {
	pattern := filepath.Join(tmuxinatorConfigDir(), "*.yml")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if filepath.Base(m) == "--help.yml" {
			continue
		}
		out = append(out, m)
	}
	sort.Strings(out)
	return out, nil
}

func processProject(configPath string, doRun, doResume bool) error {
	projectName := strings.TrimSuffix(filepath.Base(configPath), ".yml")
	root, err := tmuxinatorRoot(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[workmuxinator] WARN  skipping %q: %v\n", projectName, err)
		return nil
	}
	if stat, err := os.Stat(root); err != nil || !stat.IsDir() {
		fmt.Fprintf(os.Stderr, "[workmuxinator] WARN  skipping %q: root not found: %s\n", projectName, root)
		return nil
	}

	fmt.Fprintf(os.Stderr, "[workmuxinator] INFO  project: %s (root: %s)\n", projectName, root)
	if err := ensureProjectSession(projectName, root); err != nil {
		return err
	}

	worktrees, err := listWorktrees(root)
	if err != nil {
		return err
	}
	if len(worktrees) == 0 {
		fmt.Fprintf(os.Stderr, "[workmuxinator] WARN  no worktrees found for %q\n", projectName)
		return nil
	}

	agent := workmuxAgent(root)
	commands := []string{"cd " + shellEscape(root)}
	for _, wt := range worktrees {
		fmt.Fprintf(os.Stderr, "[workmuxinator] INFO  queueing worktree: %s\n", wt)
		commands = append(commands, worktreeOpenCmd(wt))
		if doRun {
			commands = append(commands, worktreeRunCmd(wt, agent, doResume))
		}
	}
	return runInSession(projectName, strings.Join(commands, "; "))
}

func tmuxinatorRoot(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var data map[string]any
	if err := yaml.Unmarshal(b, &data); err != nil {
		return "", err
	}
	rootRaw, ok := data["root"]
	if !ok {
		return "", errors.New("missing root")
	}
	root, ok := rootRaw.(string)
	if !ok || strings.TrimSpace(root) == "" {
		return "", errors.New("invalid root")
	}
	return expandTilde(strings.TrimSpace(root)), nil
}

func ensureProjectSession(name, root string) error {
	if err := exec.Command("tmux", "has-session", "-t", name).Run(); err == nil {
		fmt.Fprintf(os.Stderr, "[workmuxinator] INFO  session %q already exists\n", name)
		return nil
	}
	cmd := exec.Command("tmux", "new-session", "-d", "-s", name, "-c", root)
	return cmd.Run()
}

func listWorktrees(root string) ([]string, error) {
	cmd := exec.Command("workmux", "list")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, nil
	}
	lines := strings.Split(string(out), "\n")
	worktrees := make([]string, 0)
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if i == 0 || fields[0] == "BRANCH" {
			continue
		}
		worktrees = append(worktrees, fields[0])
	}
	return worktrees, nil
}

func workmuxAgent(root string) string {
	localPath := filepath.Join(root, ".workmux.yaml")
	if a := readAgentFromConfig(localPath); a != "" {
		return a
	}
	globalPath := filepath.Join(os.Getenv("HOME"), ".config", "workmux", "config.yaml")
	if a := readAgentFromConfig(globalPath); a != "" {
		return a
	}
	return "claude"
}

func readAgentFromConfig(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var data map[string]any
	if err := yaml.Unmarshal(b, &data); err != nil {
		return ""
	}
	agentRaw, ok := data["agent"]
	if !ok {
		return ""
	}
	agent, ok := agentRaw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(agent)
}

func worktreeOpenCmd(name string) string {
	n := shellEscape(name)
	return fmt.Sprintf("workmux open %s --new 2>/dev/null || workmux add %s --background", n, n)
}

func worktreeRunCmd(name, agent string, doResume bool) string {
	flags := agentResumeFlags(agent)
	fullCmd := strings.TrimSpace(strings.TrimSpace(agent) + " " + flags)
	if doResume {
		prompt := agentResumePrompt(agent)
		if prompt != "" {
			promptFlag := agentPromptFlag(agent)
			if promptFlag != "" {
				fullCmd += " " + promptFlag + " " + shellEscape(prompt)
			} else {
				fullCmd += " " + shellEscape(prompt)
			}
		}
	}
	return fmt.Sprintf("workmux run %s -- bash -lc %s", shellEscape(name), shellEscape(fullCmd))
}

func agentResumeFlags(agent string) string {
	base := filepath.Base(strings.TrimSpace(agent))
	switch {
	case strings.HasPrefix(base, "claude"):
		return "--resume --continue"
	case strings.HasPrefix(base, "opencode"):
		return "--continue"
	default:
		return ""
	}
}

func agentResumePrompt(agent string) string {
	base := filepath.Base(strings.TrimSpace(agent))
	switch {
	case strings.HasPrefix(base, "claude"),
		strings.HasPrefix(base, "opencode"),
		strings.HasPrefix(base, "cursor-agent"),
		strings.HasPrefix(base, "coding-agent"):
		return "continue if not completed"
	default:
		return ""
	}
}

func agentPromptFlag(agent string) string {
	base := filepath.Base(strings.TrimSpace(agent))
	switch {
	case strings.HasPrefix(base, "opencode"):
		return "--prompt"
	default:
		return ""
	}
}

func runInSession(session, command string) error {
	target, err := tmuxFirstPane(session)
	if err != nil {
		return err
	}
	if err := exec.Command("tmux", "send-keys", "-l", "-t", target, command).Run(); err != nil {
		return err
	}
	return exec.Command("tmux", "send-keys", "-t", target, "Enter").Run()
}

func tmuxFirstPane(session string) (string, error) {
	winsOut, err := exec.Command("tmux", "list-windows", "-t", session, "-F", "#{window_index}").Output()
	if err != nil {
		return "", err
	}
	winIdx, err := minIndex(strings.Fields(string(winsOut)))
	if err != nil {
		return "", fmt.Errorf("resolve tmux window: %w", err)
	}

	panesOut, err := exec.Command("tmux", "list-panes", "-t", fmt.Sprintf("%s:%d", session, winIdx), "-F", "#{pane_index}").Output()
	if err != nil {
		return "", err
	}
	paneIdx, err := minIndex(strings.Fields(string(panesOut)))
	if err != nil {
		return "", fmt.Errorf("resolve tmux pane: %w", err)
	}
	return fmt.Sprintf("%s:%d.%d", session, winIdx, paneIdx), nil
}

func minIndex(values []string) (int, error) {
	if len(values) == 0 {
		return 0, errors.New("no indices found")
	}
	min := int(^uint(0) >> 1)
	for _, v := range values {
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			continue
		}
		if n < min {
			min = n
		}
	}
	if min == int(^uint(0)>>1) {
		return 0, errors.New("no numeric index found")
	}
	return min, nil
}

func resolveExistingDir(path string) (string, error) {
	p := expandTilde(path)
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		abs = resolved
	}
	stat, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("not a directory: %s", abs)
	}
	if !stat.IsDir() {
		return "", fmt.Errorf("not a directory: %s", abs)
	}
	return abs, nil
}

func shellEscape(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func expandTilde(path string) string {
	if path == "~" {
		return os.Getenv("HOME")
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(os.Getenv("HOME"), strings.TrimPrefix(path, "~/"))
	}
	return path
}

func oneLine(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
}

func printHelp() {
	fmt.Println(`workmuxinator - legacy workmux/tmuxinator + DAG workflow runner

Usage:
  workmuxinator                    Open all worktrees for tmuxinator projects (legacy default)
  workmuxinator run [--resume]     Open + resume agent in each worktree (legacy)
  workmuxinator open               Same as default legacy launch
  workmuxinator add <dir>          Create tmuxinator config for directory
  workmuxinator rm <dir>           Remove tmuxinator config by root (or basename fallback)

  workmuxinator workflow run [-f workflow.yaml] [--agent codex] [--prompt-flag --prompt]
  workmuxinator workflow validate [-f workflow.yaml]
  workmuxinator run -f ...         Shortcut for workflow run
  workmuxinator validate [-f ...]  Shortcut for workflow validate

  workmuxinator version
  workmuxinator help

Notes:
  - Legacy mode uses tmuxinator + workmux + tmux commands.
  - Legacy run --resume adds "continue if not completed" where supported.
  - Supports DAG nodes with prompt, prompt_file, bash, loop, depends_on, on_failure.
  - Agent defaults to "codex", override in .workmuxinator/agent.yaml or CLI flags.
  - Workflow defaults: opencode auto-uses --prompt; cursor-agent uses positional prompts.
  - Prompt files are resolved relative to the workflow file.`)
}
