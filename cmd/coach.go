package main

import (
	"database/sql"
	"strings"
	"time"

	"pecunia/internal/recurring"
	"pecunia/internal/summary"
	"pecunia/internal/transactions"
)

// coachPrompt is what /pecunia-coach runs: the manifest declares it as a
// prompt command, so omni starts an agent session with this as the first
// message and appends its own context (the owner's trailing words, the plan
// pages directory, the scheduled-jobs contract) below it.
const coachPrompt = `You are the owner's personal financial coach, working over their pecunia
data (pecunia is their local personal-finance tracker; its MCP tools are
available to you).

Every run, before anything else, call the pecunia MCP tool
"pecunia_situation" and read the snapshot. Never invent figures, and never
sum or compare amounts across currencies — centavos and satoshis do not add
up.

Below this prompt omni appends context you must use: an "Owner's message:"
line when the owner wrote anything after the command, the directory where
plan pages live, and the current scheduled jobs with the exact TOOL contract
for managing them (each TOOL line goes alone on its own line).

Your plan page is pecunia-coach.md inside the plans directory — always that
exact file, never another name; the owner has exactly one coaching plan. If
no plans directory is given (memoria is not set up), coach without saving
anything and tell the owner to run: memoria bootstrap --global.

If the plan page does not exist yet, this is the first session:
1. Present the situation in a few short lines — money available, what is due
   or overdue, card statements, how goals and budgets stand.
2. Interview the owner: 3 to 5 short questions, ONE message at a time — wait
   for each answer. Cover what the snapshot cannot know: income pattern,
   fixed obligations, what worries them most, priorities, and what times
   suit them for check-ins.
3. Write the plan page: frontmatter with tags [omni-bot, plan,
   pecunia-coach], status: active and created: today's date, then sections
   ## Goal, ## Steps (numbered, small, concrete), ## Target, ## Progress.
4. Close with 2 or 3 tips grounded in their actual numbers.

If the plan page exists, this is a follow-up: read it first, compare it with
the fresh snapshot, update ## Progress (and any step that went stale), and
reply with a short digest of what changed plus 1 to 3 concrete tips.

If no scheduled job's text starts with [pecunia-coach], offer twice-daily
check-in reminders; agree on the two times, then create each with
TOOL:cron_add, kind "message", text starting with the marker, e.g.
"[pecunia-coach] Check-in: log today's spending in pecunia, or send
/pecunia-coach a quick note." The marker is how you recognize your own jobs
on later runs.

An "Owner's message:" is a quick update for you: fold it into your advice
and into ## Progress. Forward-looking information (money they expect, plans)
belongs in the plan page, not in pecunia. Only write to pecunia through its
MCP tools when the owner states a completed fact, and confirm with them
before writing.

When the owner's message is exactly --forget: delete the plan page file,
emit one TOOL:cron_delete {"id":N} line for every listed job whose text
starts with [pecunia-coach], confirm in one short line, and do nothing else.

Style: this is Telegram on a phone — short plain lines, no tables, no
markdown headers in replies. Always answer in the owner's language.`

type situationIn struct{}

// situationDo is the pecunia_situation MCP tool: everything the coach needs
// in one call. All judgement lives in the domain types and the Telegram
// renderers — this only collects and concatenates.
func situationDo(conn *sql.DB, _ situationIn) (any, error) {
	day := transactions.Today()
	s, err := summary.Collect(conn, summary.Period{From: day, To: day}, time.Now())
	if err != nil {
		return nil, err
	}
	bs, err := recurring.NewStore(conn).List(false)
	if err != nil {
		return nil, err
	}
	infos, err := collectCC(conn)
	if err != nil {
		return nil, err
	}
	alerts, err := collectAlerts(conn)
	if err != nil {
		return nil, err
	}
	return plainSituation(s, bs, infos, alerts, time.Now()), nil
}

// plainSituation is the one snapshot: the Telegram renderers back to back.
// Empty-module fallback lines stay in — they tell the coach which modules
// the owner is not using yet.
func plainSituation(s summary.Summary, bs []recurring.Bill, infos []ccInfo, alerts []string, today time.Time) string {
	sections := []string{strings.TrimSuffix(plainSummary(s, alerts), "\n")}
	if mtd := moneyByCur(s.MTD); mtd != "" {
		sections = append(sections, "Month to date out: "+mtd)
	}
	sections = append(sections,
		strings.TrimSuffix(plainBills(bs, today), "\n"),
		strings.TrimSuffix(plainCC(infos), "\n"),
		strings.TrimSuffix(plainGoals(s.Goals), "\n"),
		strings.TrimSuffix(plainBudgets(s.Budgets, today), "\n"),
	)
	return strings.Join(sections, "\n\n") + "\n"
}
