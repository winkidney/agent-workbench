# ADR 0002: An approved plan continues in the mode the session left for it

- Date: 2026-11-14
- Status: accepted

## Context

Claude Code asks how to go on when a plan is approved. A session that was in auto mode before planning is offered auto mode again, first; the other choices are to auto-accept edits or to approve edits one by one. Auto-accepting edits applies file edits without asking, but commands still ask.

The workbench offered auto mode only when the session's own workbench settings said auto. A session can run in auto mode without that: one started in a terminal, or before the workbench had defaults, has no permission setting, and Claude Code then goes on in the mode its record holds. Such a session, planned with `/plan`, was not offered auto mode. Its user picked "auto-accept edits", expecting automatic approvals, and every command after the plan asked.

The plan request itself does not say which mode the session left: there are no permission suggestions with it.

## Decision

1. **The mode before the plan is the one the session actually ran in**, taken from the first of:
   1. the session's workbench setting, which is what the user chose here;
   2. the last permission mode Claude Code recorded for the session, other than plan, which is what it resumes in when the workbench names none;
   3. the workbench's default for new Claude sessions (auto unless changed).
2. **When that mode is auto, the plan card offers auto mode first**, as Claude Code does.
3. **Each choice says what it means**: auto mode lets Claude decide what is safe to run; auto-accepting edits still asks before commands; approving edits asks for each.

## Consequences

- Reading the record costs one read of its last 256 KB when a plan asks for approval, and only when the session has no permission setting.
- A permission chosen in the workbench always wins over the record.

## Related

- `internal/workbench/manager.go` (`approvalData`, `planModeBefore`), `internal/workbench/native.go`
- `web/src/approval/ApprovalCard.tsx`
