package workbench

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// mergeOptions overlays the user's overrides on the values the CLI reports;
// empty overrides keep the CLI value.
func mergeOptions(base, override Options) Options {
	if override.Model != "" {
		base.Model = override.Model
	}
	if override.Effort != "" {
		base.Effort = override.Effort
	}
	if override.Speed != "" {
		base.Speed = override.Speed
	}
	if override.Mode != "" {
		base.Mode = override.Mode
	}
	if override.Permissions != "" {
		base.Permissions = override.Permissions
	}
	return base
}

// codexProfile maps a Codex sandbox policy (camelCase over the app-server,
// kebab-case in rollout files) to the permission profile id the workbench
// offers.
func codexProfile(sandbox map[string]any) string {
	switch str(sandbox["type"]) {
	case "readOnly", "read-only":
		return ":read-only"
	case "workspaceWrite", "workspace-write":
		return ":workspace"
	case "dangerFullAccess", "danger-full-access":
		return ":danger-full-access"
	}
	return ""
}

// codexAutoReview is the workbench's permission choice for the Codex TUI's
// "Approve for me": the workspace sandbox, with approval requests reviewed
// by Codex's auto-review agent instead of the user.
const codexAutoReview = "auto-review"

// codexPermissions is the permission choice a sandbox and an approvals
// reviewer amount to.
func codexPermissions(sandbox map[string]any, reviewer string) string {
	profile := codexProfile(sandbox)
	if profile == ":workspace" && (reviewer == "auto_review" || reviewer == "guardian_subagent") {
		return codexAutoReview
	}
	return profile
}

// builtinDefaults are the settings new sessions start with when the user saved
// none: approvals in each CLI's automatic mode (Claude Code's auto mode,
// Codex's auto-review).
func builtinDefaults(agent string) Options {
	if agent == "codex" {
		return Options{Permissions: codexAutoReview}
	}
	return Options{Permissions: "auto"}
}

// newSessionDefaults are the built-in defaults with the user's saved ones on top.
func (m *Manager) newSessionDefaults(agent string) Options {
	var saved Options
	m.store.readSetting("defaults:"+agent, &saved)
	return mergeOptions(builtinDefaults(agent), saved)
}

// codexTurnOptions reads the settings a Codex turn ran with from a rollout
// turn_context record.
func codexTurnOptions(p map[string]any) Options {
	o := Options{
		Model:       str(p["model"]),
		Effort:      str(p["effort"]),
		Speed:       str(p["service_tier"]),
		Mode:        str(obj(p["collaboration_mode"])["mode"]),
		Permissions: codexPermissions(obj(p["sandbox_policy"]), str(p["approvals_reviewer"])),
	}
	if o.Mode == "" {
		o.Mode = "default"
	}
	return o
}

// codexSettingsOptions reads the settings of a thread/settings/updated
// notification.
func codexSettingsOptions(ts map[string]any) Options {
	o := Options{
		Model:       str(ts["model"]),
		Effort:      str(ts["effort"]),
		Speed:       str(ts["serviceTier"]),
		Mode:        str(obj(ts["collaborationMode"])["mode"]),
		Permissions: codexPermissions(obj(ts["sandboxPolicy"]), str(ts["approvalsReviewer"])),
	}
	if o.Speed == "" {
		o.Speed = "default"
	}
	if o.Mode == "" {
		o.Mode = "default"
	}
	return o
}

// codexThreadOptions reads the effective settings from a thread/start or
// thread/resume result.
func codexThreadOptions(result map[string]any) Options {
	o := Options{
		Model:       str(result["model"]),
		Effort:      str(result["reasoningEffort"]),
		Speed:       str(result["serviceTier"]),
		Mode:        str(obj(result["collaborationMode"])["mode"]),
		Permissions: codexPermissions(obj(result["sandbox"]), str(result["approvalsReviewer"])),
	}
	if o.Speed == "" {
		o.Speed = "default"
	}
	if o.Mode == "" {
		o.Mode = "default"
	}
	return o
}

// codexDefaults resolves what a new Codex turn uses without overrides: the
// config for the directory, then the default model's own defaults. The
// workbench always starts threads with the workspace-write sandbox.
func codexDefaults(config map[string]any, models []any) Options {
	o := Options{
		Model:       str(config["model"]),
		Effort:      str(config["model_reasoning_effort"]),
		Speed:       str(config["service_tier"]),
		Mode:        "default",
		Permissions: ":workspace",
	}
	var chosen map[string]any
	for _, raw := range models {
		m := obj(raw)
		id := str(m["model"])
		if id == "" {
			id = str(m["id"])
		}
		if (o.Model != "" && id == o.Model) || (o.Model == "" && m["isDefault"] == true) {
			chosen = m
			if o.Model == "" {
				o.Model = id
			}
			break
		}
	}
	if o.Effort == "" && chosen != nil {
		o.Effort = str(chosen["defaultReasoningEffort"])
	}
	if o.Speed == "" {
		o.Speed = "default"
	}
	return o
}

// claudePermissionOptions splits a Claude permission mode into the
// workbench's Mode and Permissions settings.
func claudePermissionOptions(mode string) (string, string) {
	switch mode {
	case "plan":
		return "plan", "manual"
	case "", "default", "manual":
		return "default", "manual"
	}
	return "default", mode
}

// claudePermissionMode is the permission mode applyOptions sends for the
// chosen settings; empty sends nothing.
func claudePermissionMode(o Options) string {
	if o.Mode == "plan" {
		return "plan"
	}
	if o.Permissions == "" && o.Mode == "default" {
		return "manual"
	}
	return o.Permissions
}

// claudeApplied is what a Claude session uses after applyOptions: a cleared
// model, effort or speed falls back to the CLI's own value (reported again
// later), a sent permission mode replaces mode and permissions, and the
// overrides win.
func claudeApplied(effective, previous, o Options) Options {
	if previous.Model != "" && o.Model == "" {
		effective.Model = ""
	}
	if previous.Effort != "" && o.Effort == "" {
		effective.Effort = ""
	}
	if previous.Speed != "" && o.Speed == "" {
		effective.Speed = ""
	}
	if p := claudePermissionMode(o); p != "" {
		effective.Mode, effective.Permissions = claudePermissionOptions(p)
	}
	return mergeOptions(effective, o)
}

// claudeInitOptions reads the model, permission mode and fast mode from a
// Claude system init message.
func claudeInitOptions(o Options, v map[string]any) Options {
	if model := str(v["model"]); model != "" {
		o.Model = model
	}
	if mode := str(v["permissionMode"]); mode != "" {
		o.Mode, o.Permissions = claudePermissionOptions(mode)
	}
	switch str(v["fast_mode_state"]) {
	case "off":
		o.Speed = "standard"
	case "on", "cooldown":
		o.Speed = "fast"
	}
	return o
}

// claudeSettingsDefaults reads the effective defaults from Claude's settings
// layers (user, project, local; later wins). Only these keys are decoded.
func claudeSettingsDefaults(home, cwd string) Options {
	var model, effort, mode string
	fast := false
	for _, path := range []string{
		filepath.Join(home, "settings.json"),
		filepath.Join(cwd, ".claude", "settings.json"),
		filepath.Join(cwd, ".claude", "settings.local.json"),
	} {
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var v struct {
			Model       *string `json:"model"`
			EffortLevel *string `json:"effortLevel"`
			FastMode    *bool   `json:"fastMode"`
			Permissions struct {
				DefaultMode *string `json:"defaultMode"`
			} `json:"permissions"`
		}
		if json.Unmarshal(b, &v) != nil {
			continue
		}
		if v.Model != nil {
			model = *v.Model
		}
		if v.EffortLevel != nil {
			effort = *v.EffortLevel
		}
		if v.FastMode != nil {
			fast = *v.FastMode
		}
		if v.Permissions.DefaultMode != nil {
			mode = *v.Permissions.DefaultMode
		}
	}
	o := Options{Model: model, Effort: effort, Speed: "standard"}
	if o.Model == "" {
		o.Model = "default"
	}
	if fast {
		o.Speed = "fast"
	}
	o.Mode, o.Permissions = claudePermissionOptions(mode)
	return o
}
