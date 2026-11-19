package workbench

import (
	_ "embed"
	"errors"
	"fmt"
	"strings"
)

// Messages Agent Workbench itself sends to assistants (ADR 0014): the
// personal assistant's welcome after setting up, and a built-in's set-up
// conversation when it is turned on. They are real turns, labelled
// "[Workbench: <name>]" for the agent and the transcript.

//go:embed assistant/welcome.md
var welcomeOutline string

// workbenchText labels a message from the workbench.
func workbenchText(name, body string) string {
	return "[Workbench: " + name + "]\n\n" + body
}

// welcomeText asks the personal assistant to greet the user, naming the
// assistants already there.
func welcomeText(lang string, others []Assistant) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You have just been set up as the user's personal assistant; this starts your first conversation. Write the user a welcome in %s: warm, short (about 20 lines), in your own words, using your name. Cover every point below. Do not run commands or change files for this; only write the welcome, and end with one question about what they would like to do first.\n\n", languageNames[lang])
	b.WriteString(strings.TrimSpace(welcomeOutline))
	b.WriteString("\n")
	if len(others) > 0 {
		b.WriteString("- **Assistants already here** (each in its own folder; the user opens them in the sidebar):\n")
		for _, a := range others {
			fmt.Fprintf(&b, "  - %s (%s/)\n", a.Name, a.Dir)
		}
	}
	b.WriteString("- **Settings:** Settings → Assistants changes your name, your agent (Claude or Codex), the language, and where the repository is backed up (a private GitHub repository).\n")
	return workbenchText("Welcome", b.String())
}

// setUpText asks an assistant made in the page what it is for.
func setUpText(name, lang string) string {
	return workbenchText("Set up", fmt.Sprintf(`You were just made, as %q, with an empty procedure. Write to the user in %s.

Ask what you should do: the job, its inputs and outputs, when (on request or on a schedule), and which steps need their approval. Then follow the create-assistant skill (.agents/skills/create-assistant/ at the top of this repository) from step 4: write your AGENTS.md, any scripts in tools/ with tests, your routines (off until they work), and in assistant.toml your description, capabilities and examples (in the user's language: they are your home card in the page). Once everything works, switch the routines on, set ready = true, and run "$AGENT_WORKBENCH_BIN" assistant reload.

Never ask for a password, token or key in this conversation: tell the user how to save it themselves.`, name, languageNames[lang]))
}

// Welcome sends the personal assistant its welcome: to its ongoing topic,
// or as its first.
func (m *Manager) Welcome() (Session, error) {
	if _, err := m.assistant("main"); err != nil {
		return Session{}, errors.New("there is no personal assistant (main) to welcome you")
	}
	var others []Assistant
	for _, a := range m.Assistants() {
		if a.Dir != "main" && a.Error == "" {
			others = append(others, a)
		}
	}
	s, _, err := m.AssistantMessage("main", welcomeText(m.language(), others), nil, "")
	return s, err
}

// SetUpMessage starts the first conversation of an assistant made in the
// page.
func (m *Manager) SetUpMessage(dir string) (Session, error) {
	a, err := m.assistant(dir)
	if err != nil {
		return Session{}, err
	}
	s, _, err := m.AssistantMessage(dir, setUpText(a.Name, m.language()), nil, "")
	return s, err
}

// mossMeta is what the personal assistant's home card says, in the
// language set up chose (ADR 0015).
type assistantMeta struct {
	Description  string
	Capabilities []string
	Examples     []string
}

var mossMetas = map[string]assistantMeta{
	"en": {
		Description:  "Your personal assistant: one ongoing conversation that remembers what matters.",
		Capabilities: []string{"Reminders and routines, from a sentence", "Remembers what lasts in a wiki you can read and undo", "Makes assistants for jobs you repeat, after you approve", "Hands tasks to your other assistants"},
		Examples:     []string{"Remind me in 2 hours to call Ana", "Every weekday at 9:00, ask me for my top three", "Make me an assistant for my weekly report", "Which assistants do I have?", "What can you do?"},
	},
	"zh-Hans": {
		Description:  "你的个人助理：一个持续的对话，记住对你重要的事。",
		Capabilities: []string{"一句话设置提醒和定时任务", "把长期有用的信息记进可查看、可撤销的 wiki", "经你批准后为重复的工作创建助理", "把任务转交给你的其他助理"},
		Examples:     []string{"2 小时后提醒我给 Ana 打电话", "每个工作日 9 点问我今天最重要的三件事", "帮我建一个写周报的助理", "我有哪些助理？", "你能做什么？"},
	},
	"zh-Hant": {
		Description:  "你的個人助理：一個持續的對話，記住對你重要的事。",
		Capabilities: []string{"一句話設定提醒和定時任務", "把長期有用的資訊記進可查看、可撤銷的 wiki", "經你批准後為重複的工作建立助理", "把任務轉交給你的其他助理"},
		Examples:     []string{"2 小時後提醒我給 Ana 打電話", "每個工作日 9 點問我今天最重要的三件事", "幫我建一個寫週報的助理", "我有哪些助理？", "你能做什麼？"},
	},
}

func mossMeta(lang string) assistantMeta {
	if m, ok := mossMetas[lang]; ok {
		return m
	}
	return mossMetas[defaultLanguage]
}
