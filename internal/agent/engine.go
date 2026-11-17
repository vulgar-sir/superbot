package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"superbot/internal/agentchat"
	"superbot/internal/browser"
	"superbot/internal/caldav"
	"superbot/internal/chat"
	"superbot/internal/config"
	"superbot/internal/connectors"
	"superbot/internal/devices"
	"superbot/internal/events"
	"superbot/internal/fetch"
	"superbot/internal/goals"
	"superbot/internal/imagegen"
	libindex "superbot/internal/library"
	"superbot/internal/llm"
	"superbot/internal/mailbox"
	"superbot/internal/mcpservice"
	"superbot/internal/memory"
	"superbot/internal/officetext"
	"superbot/internal/phone"
	"superbot/internal/routines"
	"superbot/internal/sandbox"
	"superbot/internal/search"
	"superbot/internal/secrets"
	"superbot/internal/skills"
	"superbot/internal/store"
	"superbot/internal/tools"
	"superbot/internal/vault"
)

const (
	defaultMaxSteps      = 15
	computerMaxSteps     = 20
	maxMaxSteps          = 40
	defaultMaxOutput     = 32768
	maxToolResultBytes   = 16 * 1024
	deltaFlushInterval   = 150 * time.Millisecond
	leaseCheckInterval   = 5 * time.Second
	defaultContextWindow = 128_000
)

var (
	errLeaseLost = errors.New("cycle lease lost")
	errCanceled  = errors.New("run canceled by user")
)

// Engine executes one claimed cycle: a direct bot turn, a team orchestration,
// a bot-to-bot thread reply or a subagent task.
type Engine struct {
	db         *store.DB
	providers  *llm.Factory
	bus        *events.Bus
	cfg        config.Config
	sandbox    *sandbox.Manager
	box        *secrets.Box
	fetcher    *fetch.Fetcher
	search     *search.Service
	skills     *skills.Service
	mcp        *mcpservice.Service
	agentChat  *agentchat.Service
	devices    *devices.Service
	memory     *memory.Service
	connectors *connectors.Service
	mailbox    *mailbox.Service
	caldav     *caldav.Service
	imagegen   *imagegen.Service
	chatSvc    *chat.Service
	routineSvc *routines.Service
	goals      *goals.Service
	logger     *slog.Logger

	browserMu       sync.Mutex
	browserSessions map[uuid.UUID]*browser.Session
	libraryIndexer  *libindex.Indexer

	seenMu       sync.Mutex
	seenOrdinals map[uuid.UUID]int64
	lastPrune    time.Time
}

func NewEngine(db *store.DB, providers *llm.Factory, bus *events.Bus, cfg config.Config, sandboxManager *sandbox.Manager, box *secrets.Box, logger *slog.Logger) *Engine {
	return &Engine{
		db:              db,
		providers:       providers,
		bus:             bus,
		cfg:             cfg,
		sandbox:         sandboxManager,
		box:             box,
		fetcher:         fetch.New(logger),
		search:          search.NewService(db, box, logger),
		skills:          skills.NewService(db, cfg.DataDir, logger),
		mcp:             mcpservice.NewService(db, box, logger),
		agentChat:       agentchat.NewService(db, bus, logger),
		devices:         devices.NewService(db, cfg.DataDir),
		memory:          memory.NewService(db, cfg.EmbeddingAPIKey, cfg.EmbeddingBaseURL, cfg.EmbeddingModel),
		connectors:      connectors.NewService(db, box, logger),
		mailbox:         mailbox.NewService(),
		caldav:          caldav.NewService(),
		imagegen:        imagegen.New(cfg.ImageAPIKey, cfg.ImageBaseURL, cfg.ImageModel),
		chatSvc:         chat.NewService(db, bus, logger),
		routineSvc:      routines.NewService(db, bus, logger),
		goals:           goals.NewService(db),
		logger:          logger,
		browserSessions: make(map[uuid.UUID]*browser.Session),
		seenOrdinals:    make(map[uuid.UUID]int64),
		libraryIndexer:  libindex.NewIndexer(db, cfg.DataDir, logger),
	}
}

type runOutcome struct {
	ConversationID uuid.UUID
	CycleID        uuid.UUID
	RunID          uuid.UUID
	FinalMessageID *uuid.UUID
	FinalText      string
	FinalStatus    string
	RunsFinalized  bool
	Step           int32
	UsageIn        int64
	UsageOut       int64
	CachedIn       int64
	CostMicros     int64
	Estimated      bool
	Warning        string
}

type llmTurn struct {
	Text         string
	Reasoning    string
	ToolCalls    []llm.ToolCall
	Usage        *llm.Usage
	FinishReason string
}

var errApprovalPending = errors.New("tool approval pending")

type runSeed struct {
	Messages         []llm.Message
	Pinned           int
	Step             int
	PendingCalls     []llm.ToolCall
	ApprovalDecision string
	Continuation     []llm.Message
}

type botParams struct {
	Temperature *float64 `json:"temperature"`
	MaxTokens   int      `json:"max_tokens"`
	MaxSteps    int      `json:"max_steps"`
}

type toolPolicy struct {
	Disabled []string `json:"disabled_tools"`
}

func (e *Engine) ProcessCycle(ctx context.Context, cycle store.Cycle, leaseID uuid.UUID) (err error) {
	if cycle.LeaseID == nil || *cycle.LeaseID != leaseID {
		return errLeaseLost
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = e.recoverCycle(ctx, cycle, leaseID, recovered)
		}
	}()
	defer e.takeSeenOrdinal(cycle.ID)
	if cycle.CancelRequested {
		e.setCycleStatus(context.WithoutCancel(ctx), cycle, leaseID, "canceled", "")
		return nil
	}

	guard := e.startLeaseGuard(ctx, cycle.ID, leaseID)
	defer guard.Stop()
	runCtx := guard.ctx

	_ = e.db.MarkCycleRunsInterrupted(ctx, cycle.ID)

	conv, err := e.db.GetConversation(runCtx, cycle.ConversationID)
	if err != nil {
		e.failCycle(ctx, cycle, leaseID, fmt.Errorf("load conversation: %w", err))
		return err
	}

	var outcome *runOutcome
	if cycle.ResumeCheckpointID != nil {
		outcome, err = e.resumeFromCheckpoint(runCtx, conv, cycle, leaseID)
	} else {
		switch conv.Kind {
		case "direct":
			outcome, err = e.runDirect(runCtx, conv, cycle, leaseID)
		case "team":
			outcome, err = e.runTeam(runCtx, conv, cycle, leaseID)
		case "agent":
			outcome, err = e.runAgent(runCtx, conv, cycle, leaseID)
		case "subagent":
			outcome, err = e.runSubagent(runCtx, conv, cycle, leaseID)
		default:
			err = fmt.Errorf("unsupported conversation kind %q", conv.Kind)
		}
	}

	if err != nil {
		cause := context.Cause(runCtx)
		switch {
		case errors.Is(err, errLeaseLost), errors.Is(cause, errLeaseLost):
			return errLeaseLost
		case errors.Is(cause, errCanceled):
			if outcome != nil && !outcome.RunsFinalized {
				e.finishRun(ctx, outcome, "canceled", "用户停止了本轮协作", "")
			}
			e.setCycleStatus(context.WithoutCancel(ctx), cycle, leaseID, "canceled", "")
			return nil
		case errors.Is(err, errApprovalPending):
			e.setCycleStatus(context.WithoutCancel(ctx), cycle, leaseID, "waiting_approval", "")
			return nil
		case ctx.Err() != nil:
			// Process is shutting down: hand the cycle back right away so the
			// next worker resumes it without waiting out the lease.
			releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			_, _ = e.db.ReleaseCycleLease(releaseCtx, store.ReleaseCycleLeaseParams{ID: cycle.ID, LeaseID: &leaseID})
			cancel()
			return ctx.Err()
		case isPermanent(err):
			if outcome != nil && !outcome.RunsFinalized {
				e.finishRun(ctx, outcome, "failed", "", err.Error())
			}
			e.closeProgress(ctx, cycle.ID)
			e.emitError(ctx, conv.ID, &cycle.ID, runIDOf(outcome), err)
			e.setCycleStatus(context.WithoutCancel(ctx), cycle, leaseID, "failed", err.Error())
			return err
		default:
			if outcome != nil && !outcome.RunsFinalized {
				e.finishRun(ctx, outcome, "failed", "", err.Error())
			}
			e.emitError(ctx, conv.ID, &cycle.ID, runIDOf(outcome), err)
			e.failCycle(ctx, cycle, leaseID, err)
			return err
		}
	}

	if outcome == nil {
		err := fmt.Errorf("cycle finished without a run")
		e.failCycle(ctx, cycle, leaseID, err)
		return err
	}
	if !outcome.RunsFinalized {
		e.finishRun(ctx, outcome, "succeeded", outcome.Warning, "")
	}
	status := outcome.FinalStatus
	if status == "" {
		status = "done"
	}
	e.setCycleStatus(context.WithoutCancel(ctx), cycle, leaseID, status, "")
	// A decision made while this cycle was busy takes priority over late
	// messages: the paused run already holds the user's earlier request.
	if e.resumeDeferredApproval(ctx, conv) {
		return nil
	}
	if conv.Kind == "direct" || conv.Kind == "team" {
		if e.scheduleFollowUp(ctx, conv, cycle, e.takeSeenOrdinal(cycle.ID)) {
			return nil
		}
	}
	if status == "done" {
		e.driveGoal(ctx, conv)
	}
	return nil
}

func runIDOf(outcome *runOutcome) *uuid.UUID {
	if outcome == nil {
		return nil
	}
	return &outcome.RunID
}

func (e *Engine) runDirect(ctx context.Context, conv store.Conversation, cycle store.Cycle, leaseID uuid.UUID) (*runOutcome, error) {
	if conv.DirectBotID == nil {
		return nil, fmt.Errorf("direct conversation has no bot")
	}
	bot, err := e.db.GetBot(ctx, store.GetBotParams{ID: *conv.DirectBotID, OwnerID: conv.CreatedBy})
	if err != nil {
		return nil, fmt.Errorf("load bot: %w", err)
	}
	return e.runBotTurn(ctx, conv, cycle, leaseID, bot, "", false)
}

// runBotTurn executes one bot turn. It is reused by direct chats and by the
// team roundtable orchestrator.
func (e *Engine) runBotTurn(ctx context.Context, conv store.Conversation, cycle store.Cycle, leaseID uuid.UUID, bot store.Bot, instruction string, isolated bool) (*runOutcome, error) {
	return e.runBotTurnSeeded(ctx, conv, cycle, leaseID, bot, instruction, isolated, nil)
}

func (e *Engine) runBotTurnSeeded(ctx context.Context, conv store.Conversation, cycle store.Cycle, leaseID uuid.UUID, bot store.Bot, instruction string, isolated bool, seed *runSeed) (*runOutcome, error) {
	// Mark the bot active before acquiring its computer so auto-sleep cannot
	// stop a workspace that is about to be used.
	if e.sandbox != nil && e.libraryIndexer != nil {
		defer func() {
			created := e.libraryIndexer.IndexWorkspace(context.WithoutCancel(ctx), bot.OwnerID, bot.ID, e.sandbox.WorkspacePath(bot.ID))
			if created > 0 {
				e.logger.Info("auto-indexed workspace artifacts", "bot_id", bot.ID, "count", created)
			}
		}()
	}
	_ = e.db.TouchBotActivity(ctx, store.TouchBotActivityParams{ID: bot.ID, OwnerID: bot.OwnerID})
	if err := e.checkWeeklyBudget(ctx, bot.OwnerID); err != nil {
		return nil, permanent(err)
	}
	if bot.ProviderID == nil {
		return nil, permanent(fmt.Errorf("bot %q has no provider configured", bot.Name))
	}
	providerRow, err := e.db.GetProvider(ctx, store.GetProviderParams{ID: *bot.ProviderID, OwnerID: conv.CreatedBy})
	if err != nil {
		return nil, fmt.Errorf("load provider: %w", err)
	}
	if !providerRow.Enabled {
		return nil, permanent(fmt.Errorf("provider %q is disabled", providerRow.Name))
	}
	provider, err := e.providers.Build(providerRow, e.cfg.LLMTimeout)
	if err != nil {
		return nil, permanent(err)
	}

	var modelRow *store.Model
	row, err := e.db.GetModelByName(ctx, store.GetModelByNameParams{ProviderID: providerRow.ID, Name: bot.Model})
	switch {
	case err == nil:
		modelRow = &row
	case errors.Is(err, pgx.ErrNoRows):
		modelRow = nil
	default:
		return nil, fmt.Errorf("load model: %w", err)
	}

	run, err := e.db.CreateRun(ctx, store.CreateRunParams{
		CycleID:        cycle.ID,
		ConversationID: conv.ID,
		BotID:          bot.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("create run: %w", err)
	}
	outcome := &runOutcome{
		ConversationID: conv.ID,
		CycleID:        cycle.ID,
		RunID:          run.ID,
	}
	if err := e.db.MarkRunRunning(ctx, run.ID); err != nil {
		return outcome, fmt.Errorf("mark run running: %w", err)
	}
	if err := e.appendEvent(ctx, conv.ID, &cycle.ID, &run.ID, events.TypeStatus, map[string]any{
		"run_id": run.ID.String(),
		"bot_id": bot.ID.String(),
		"status": "running",
	}); err != nil {
		return outcome, err
	}

	workspaceDir := filepath.Join(e.cfg.DataDir, "workspaces", bot.ID.String())
	if err := e.ensureDirs(ctx, workspaceDir); err != nil {
		return outcome, fmt.Errorf("create workspace: %w", err)
	}
	root, err := os.OpenRoot(workspaceDir)
	if err != nil {
		return outcome, fmt.Errorf("open workspace: %w", err)
	}
	defer root.Close()

	var sharedRoot *os.Root
	sharedHostPath := ""
	sharedContainerPath := ""
	if conv.TeamID != nil {
		sharedHostPath = filepath.Join(e.cfg.DataDir, "teams", conv.TeamID.String(), "shared")
		if err := e.ensureDirs(ctx, sharedHostPath); err != nil {
			return outcome, fmt.Errorf("create team shared dir: %w", err)
		}
		sharedRoot, err = os.OpenRoot(sharedHostPath)
		if err != nil {
			return outcome, fmt.Errorf("open team shared dir: %w", err)
		}
		defer sharedRoot.Close()
		sharedContainerPath = "/teams/" + conv.TeamID.String() + "/shared"
	}

	// Every bot of the account shares one drive, mounted at /shared in its
	// computer, so files can move between bots outside of group chats.
	var driveRoot *os.Root
	driveHostPath := filepath.Join(e.cfg.DataDir, "shared", bot.OwnerID.String())
	if err := e.ensureDirs(ctx, driveHostPath); err != nil {
		e.logger.Warn("prepare shared drive failed", "bot_id", bot.ID, "error", err)
	} else if driveRoot, err = os.OpenRoot(driveHostPath); err != nil {
		e.logger.Warn("open shared drive failed", "bot_id", bot.ID, "error", err)
	} else {
		defer driveRoot.Close()
	}

	registry, closeRegistry := e.buildRegistry(ctx, root, bot, sharedRoot, conv, cycle)
	if driveRoot != nil {
		for _, tool := range tools.NewDriveTools(driveRoot).All() {
			registry.Register(tool)
		}
	}
	defer closeRegistry()
	// A reclaimed cycle continues from the last finished tool round instead
	// of replaying tools that already ran. In a team cycle the checkpoint
	// belongs to whichever member was cut off, and only that member picks it
	// up when the resumed orchestration reaches it.
	if seed == nil {
		seed = e.loadProgressSeed(ctx, cycle, bot)
	}
	var messages []llm.Message
	startStep := 1
	pinned := 1
	lastSeenOrdinal := int64(0)
	if seed != nil {
		messages = seed.Messages
		startStep = seed.Step + 1
		pinned = seed.Pinned
		if pinned < 1 {
			pinned = 1
		}
		for index, pendingCall := range seed.PendingCalls {
			var result tools.Result
			switch {
			case index == 0 && seed.ApprovalDecision == "allowed":
				result = e.executeTool(ctx, conv, cycle, run, bot, registry, pendingCall)
			case index == 0:
				result = tools.Result{Content: "用户拒绝了该工具调用。", IsError: true}
			case e.toolNeedsApproval(ctx, bot, pendingCall):
				// The remaining calls of the same step each get their own
				// decision instead of being silently dropped.
				if err := e.pauseForApproval(ctx, conv, cycle, run, bot, seed.PendingCalls[index:], messages, seed.Step); err != nil {
					return outcome, err
				}
				return outcome, errApprovalPending
			default:
				result = e.executeTool(ctx, conv, cycle, run, bot, registry, pendingCall)
			}
			messages = append(messages, llm.ToolResultMessage(pendingCall.ID, pendingCall.Name, result.Content, result.IsError))
		}
		messages = append(messages, seed.Continuation...)
		lastSeenOrdinal, _ = e.db.GetLatestMessageOrdinal(ctx, conv.ID)
	} else {
		if !isolated {
			e.maybeCompact(ctx, conv, cycle, bot, &coordinatorRuntime{
				provider: provider, providerRow: providerRow, modelRow: modelRow, modelName: bot.Model,
			})
		}
		messages, pinned, lastSeenOrdinal, err = e.buildMessages(ctx, bot, conv, modelRow, registry, instruction, sharedHostPath, sharedContainerPath, isolated, root)
		if err != nil {
			return outcome, err
		}
	}
	toolDefs := registry.Definitions()
	if modelRow != nil && !modelRow.SupportsTools {
		toolDefs = nil
	}
	budget := promptBudget(modelRow) - EstimateToolDefsTokens(toolDefs)
	if budget < 2000 {
		budget = 2000
	}

	params := parseBotParams(bot.Params)
	maxSteps := params.MaxSteps
	if maxSteps <= 0 {
		maxSteps = defaultMaxSteps
		if bot.ComputerProfile != "none" {
			maxSteps = computerMaxSteps
		}
	}
	if maxSteps > maxMaxSteps {
		maxSteps = maxMaxSteps
	}
	maxTokens := params.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxOutput
	}
	if modelRow != nil && modelRow.MaxOutput > 0 && maxTokens > int(modelRow.MaxOutput) {
		maxTokens = int(modelRow.MaxOutput)
	}

	var textAll, reasoningAll strings.Builder
	var estimated bool
	usedTools := make([]string, 0, 4)
	seenTools := make(map[string]bool)
	overflowRetries := 0
	streamRetries := 0
	flattened := false
	continuations := 0
	idsChecked := false
	stoppedEarly := false
	for step := startStep; step <= maxSteps; step++ {
		if err := ctx.Err(); err != nil {
			return outcome, context.Cause(ctx)
		}
		// Anything the user (or a teammate) said while this bot was working
		// reaches it before its next model call, so it can change course
		// without waiting for the turn to end.
		if step > startStep || seed != nil {
			var delivered []string
			messages, lastSeenOrdinal, delivered = e.injectArrivals(ctx, conv, messages, lastSeenOrdinal)
			if len(delivered) > 0 {
				_ = e.appendEvent(ctx, conv.ID, &cycle.ID, &run.ID, events.TypeStatus, map[string]any{
					"run_id":                 run.ID.String(),
					"bot_id":                 bot.ID.String(),
					"status":                 "running",
					"interjection_delivered": delivered,
				})
			}
		}
		e.noteSeenOrdinal(cycle.ID, lastSeenOrdinal)
		if err := e.db.UpdateRunStep(ctx, store.UpdateRunStepParams{ID: run.ID, Step: int32(step)}); err != nil {
			e.logger.Warn("update run step failed", "run_id", run.ID, "error", err)
		}
		// The tool set can grow mid-turn (computer_enable adds the browser
		// tools), so it is read again before every model call.
		if modelRow == nil || modelRow.SupportsTools {
			toolDefs = registry.Definitions()
		}
		if fitted, changed := fitLoopMessages(messages, pinned, budget); changed {
			e.logger.Info("tool loop context compacted", "run_id", run.ID, "step", step,
				"before", EstimateMessagesTokens(messages), "after", EstimateMessagesTokens(fitted))
			messages = fitted
		}
		req := llm.Request{
			Model:       bot.Model,
			Messages:    messages,
			Tools:       toolDefs,
			Temperature: params.Temperature,
			MaxTokens:   maxTokens,
		}
		reserved, err := e.reserve(ctx, conv.ID, cycle.ID, modelRow, req)
		if err != nil {
			return outcome, permanent(err)
		}
		turn, err := e.streamTurn(ctx, provider, req, conv, cycle, run, bot)
		if err != nil {
			e.release(ctx, conv.ID, cycle.ID, reserved)
			if llm.IsContextOverflow(err) && overflowRetries < 2 {
				// Token estimates are approximate; when the provider says the
				// prompt is too large, shrink harder and retry this step.
				overflowRetries++
				budget = budget * 2 / 3
				messages, _ = fitLoopMessages(messages, pinned, budget)
				step--
				continue
			}
			if llm.IsContextOverflow(err) {
				return outcome, permanent(fmt.Errorf("上下文超出模型窗口，压缩后仍无法继续：%w", err))
			}
			if isReasoningProtocolError(err) && !flattened {
				// The provider reads the transcript as an unfinished
				// thinking step. Restating earlier turns as a plain record
				// removes every ambiguity about where the turn starts.
				if flat, ok := flattenTurns(messages, pinned); ok {
					flattened = true
					e.logger.Warn("provider rejected the transcript shape; retrying with a flattened history",
						"run_id", run.ID, "step", step, "error", err)
					messages = flat
					step--
					continue
				}
			}
			if llm.IsRetryable(err) && streamRetries < 2 && ctx.Err() == nil {
				// The stream broke before any tool ran in this step, so the
				// model call can simply be repeated instead of failing the
				// cycle and replaying earlier work.
				streamRetries++
				e.logger.Warn("model stream failed, retrying step", "run_id", run.ID, "step", step, "error", err)
				select {
				case <-time.After(time.Duration(streamRetries) * 2 * time.Second):
				case <-ctx.Done():
					return outcome, context.Cause(ctx)
				}
				step--
				continue
			}
			return outcome, err
		}
		usage := turn.Usage
		if usage == nil {
			usage = &llm.Usage{
				PromptTokens:     EstimateMessagesTokens(req.Messages),
				CompletionTokens: EstimateTokens(turn.Text),
			}
			estimated = true
		}
		cost := computeCost(modelRow, usage)
		e.settle(ctx, conv.ID, cycle.ID, reserved, cost)
		outcome.UsageIn += int64(usage.PromptTokens)
		outcome.UsageOut += int64(usage.CompletionTokens)
		outcome.CachedIn += int64(usage.CachedTokens)
		outcome.CostMicros += cost
		outcome.Estimated = estimated
		outcome.Step = int32(step)

		textAll.WriteString(turn.Text)
		reasoningAll.WriteString(turn.Reasoning)

		if len(turn.ToolCalls) == 0 {
			if turn.FinishReason == llm.FinishLength && continuations < 2 && step < maxSteps {
				// The reply hit the output limit mid-sentence; ask for the
				// rest instead of presenting a cut-off answer as complete.
				continuations++
				messages = append(messages,
					llm.AssistantMessage(turn.Text, turn.Reasoning, nil),
					llm.TextMessage(llm.RoleUser, "【系统】上一段输出因长度上限被截断。请从中断处直接接着写，不要重复已经写过的内容，也不要道歉。"))
				continue
			}
			if !idsChecked {
				// A reply that cites an id nothing produced is claiming a
				// document or file that does not exist. Give the model one
				// chance to create it for real or to correct the reply.
				if ids := fabricatedIDs(turn.Text, messages); len(ids) > 0 {
					idsChecked = true
					e.logger.Warn("reply cites ids no tool produced; asking the model to verify",
						"run_id", run.ID, "bot_id", bot.ID, "ids", ids)
					messages = append(messages,
						llm.AssistantMessage(turn.Text, turn.Reasoning, nil),
						llm.TextMessage(llm.RoleUser, fabricatedIDsNotice(ids)))
					textAll.Reset()
					if step >= maxSteps {
						maxSteps = step + 2
					}
					continue
				}
			}
			if turn.FinishReason == llm.FinishLength {
				outcome.Warning = "回复因输出长度上限被截断"
			}
			stoppedEarly = true
			break
		}
		for i := range turn.ToolCalls {
			if turn.ToolCalls[i].ID == "" {
				turn.ToolCalls[i].ID = "call_" + uuid.NewString()
			}
		}
		messages = append(messages, llm.AssistantMessage(turn.Text, turn.Reasoning, turn.ToolCalls))
		for i := range turn.ToolCalls {
			call := turn.ToolCalls[i]
			if truncatedToolCall(call, turn.FinishReason) {
				e.logger.Warn("tool call arguments cut off by the output limit; not running it",
					"run_id", run.ID, "tool", call.Name, "args_bytes", len(call.Arguments))
				messages = append(messages, llm.ToolResultMessage(call.ID, call.Name, truncatedToolCallNotice, true))
				continue
			}
			// A tool the model made up is not run, so there is nothing to
			// approve; it gets the unknown-tool error and a hint instead.
			if registry.Has(call.Name) && e.toolNeedsApproval(ctx, bot, call) {
				e.closeProgress(ctx, cycle.ID)
				if err := e.pauseForApproval(ctx, conv, cycle, run, bot, turn.ToolCalls[i:], messages, step); err != nil {
					return outcome, err
				}
				return outcome, errApprovalPending
			}
			result := e.executeTool(ctx, conv, cycle, run, bot, registry, call)
			messages = append(messages, llm.ToolResultMessage(call.ID, call.Name, result.Content, result.IsError))
			if !seenTools[call.Name] {
				seenTools[call.Name] = true
				usedTools = append(usedTools, call.Name)
			}
		}
		e.saveProgress(ctx, conv, cycle, run, bot, messages, pinned, step)
		if step == maxSteps {
			outcome.Warning = "达到工具调用步数上限"
		}
	}
	if !stoppedEarly && outcome.Warning == "达到工具调用步数上限" {
		// The step budget ran out mid-task. Rather than ending on an empty
		// placeholder, ask once more, without tools, for a progress report
		// the user can act on.
		if summary := e.stepLimitSummary(ctx, provider, conv, cycle, run, bot, modelRow, messages, toolDefs, params, maxTokens, outcome); summary != "" {
			textAll.WriteString("\n\n")
			textAll.WriteString(summary)
		}
	}
	e.closeProgress(ctx, cycle.ID)

	finalText := llm.StripArtifacts(strings.TrimSpace(textAll.String()))
	if finalText == "" {
		if outcome.Warning == "达到工具调用步数上限" {
			finalText = "（达到工具调用步数上限，模型未生成最终回复）"
		} else {
			finalText = "（模型未返回文本）"
		}
	}
	metadata, _ := json.Marshal(map[string]any{
		"reasoning":   strings.TrimSpace(reasoningAll.String()),
		"warning":     outcome.Warning,
		"estimated":   outcome.Estimated,
		"cost_micros": outcome.CostMicros,
		"tools_used":  usedTools,
		// seen_ordinal is how far into the conversation the bot had read
		// when it wrote this reply; anything newer arrived too late for it.
		"seen_ordinal": lastSeenOrdinal,
	})

	finalMessage, err := e.insertFinalMessage(ctx, conv.ID, cycle.ID, leaseID, bot.ID, run.ID, finalText, metadata)
	if err != nil {
		return outcome, err
	}
	outcome.FinalMessageID = &finalMessage.ID
	outcome.FinalText = finalText
	if e.agentChat != nil && conv.Kind != "agent" {
		if err := e.agentChat.RecordUpdate(ctx, conv.CreatedBy, bot.ID, conv.ID, finalMessage.ID, finalText); err != nil {
			e.logger.Warn("record bot activity failed", "bot_id", bot.ID, "error", err)
		}
	}
	if _, err := e.db.CreateUsageRecord(ctx, store.CreateUsageRecordParams{
		UserID:           conv.CreatedBy,
		BotID:            &bot.ID,
		ProviderID:       &providerRow.ID,
		Model:            bot.Model,
		CycleID:          &cycle.ID,
		RunID:            &run.ID,
		PromptTokens:     outcome.UsageIn,
		CachedTokens:     outcome.CachedIn,
		CompletionTokens: outcome.UsageOut,
		CostMicros:       outcome.CostMicros,
		Estimated:        outcome.Estimated,
	}); err != nil {
		e.logger.Warn("record usage failed", "run_id", run.ID, "error", err)
	}
	if err := e.appendEvent(ctx, conv.ID, &cycle.ID, &run.ID, events.TypeUsage, map[string]any{
		"run_id":      run.ID.String(),
		"usage_in":    outcome.UsageIn,
		"usage_out":   outcome.UsageOut,
		"cost_micros": outcome.CostMicros,
		"estimated":   outcome.Estimated,
	}); err != nil {
		return outcome, err
	}
	return outcome, nil
}

func (e *Engine) checkWeeklyBudget(ctx context.Context, accountID uuid.UUID) error {
	account, err := e.db.GetAccountByID(ctx, accountID)
	if err != nil || account.WeeklyBudgetMicros <= 0 {
		return nil
	}
	spent, err := e.db.GetWeeklyUsageCost(ctx, accountID)
	if err != nil {
		return nil
	}
	if spent >= account.WeeklyBudgetMicros {
		return fmt.Errorf("账户本周用量已达到预算上限，请调整预算或等待重置")
	}
	return nil
}

// documentIndexer keeps a nil indexer nil as an interface, so tools can test
// for it instead of calling through a nil pointer.
func documentIndexer(indexer *libindex.Indexer) tools.DocumentIndexer {
	if indexer == nil {
		return nil
	}
	return indexer
}

func (e *Engine) buildRegistry(ctx context.Context, root *os.Root, bot store.Bot, sharedRoot *os.Root, conv store.Conversation, cycle store.Cycle) (*tools.Registry, func()) {
	teamID := conv.TeamID
	registry := tools.NewRegistry()
	policy := toolPolicy{}
	_ = json.Unmarshal(bot.ToolPolicy, &policy)
	disabled := make(map[string]bool, len(policy.Disabled))
	for _, name := range policy.Disabled {
		disabled[name] = true
	}
	if conv.Kind == "subagent" {
		for _, name := range []string{
			"connector_add", "connector_remove", "local_exec", "local_screenshot",
			"local_clipboard", "local_file", "local_app", "local_location", "local_audio",
		} {
			disabled[name] = true
		}
	}
	phoneService := phone.New(phone.Config{
		AccountSID: e.cfg.TwilioSID,
		AuthToken:  e.cfg.TwilioToken,
		FromNumber: e.cfg.TwilioFrom,
	})
	all := tools.NewWorkspaceTools(root).All()
	if sharedRoot != nil {
		all = append(all, tools.NewSharedTools(sharedRoot).All()...)
	}
	if e.sandbox != nil {
		all = append(all,
			tools.NewShellTool(e.sandbox, bot.OwnerID, bot.ID, bot.ComputerProfile),
			tools.NewPythonTool(e.sandbox, bot.OwnerID, bot.ID, bot.ComputerProfile),
		)
		if bot.ComputerProfile == "desktop" {
			all = append(all, tools.NewDesktopTools(e.sandbox, bot.OwnerID, bot.ID)...)
		}
		if bot.ComputerProfile == "browser" || bot.ComputerProfile == "desktop" {
			all = append(all, tools.NewVaultFillTool(e.sandbox, vault.NewService(e.db, e.box), e.db, bot.OwnerID, bot.ID))
		}
	}
	all = append(all,
		tools.NewFetchTool(e.fetcher),
		tools.NewSearchTool(e.search, bot.OwnerID),
		tools.NewSkillListTool(e.skills, bot.OwnerID, bot.ID),
		tools.NewSkillReadTool(e.skills, bot.OwnerID, bot.ID),
		tools.NewExtensionProposeTool(e.skills, bot.OwnerID),
		tools.NewChatHistoryTool(e.db, conv.ID),
		tools.NewSaveToLibraryTool(e.db, bot.OwnerID, bot.ID, conv.ID),
		tools.NewImageGenerateTool(e.imagegen, e.db, bot.OwnerID, bot.ID, e.cfg.DataDir),
		tools.NewListLibraryTool(e.db, bot.OwnerID, bot.ID),
		tools.NewReadLibraryTool(e.db, bot.OwnerID, root),
		tools.NewLocalExecTool(e.devices, bot.OwnerID),
		tools.NewLocalScreenshotTool(e.devices, bot.OwnerID),
		tools.NewLocalClipboardTool(e.devices, bot.OwnerID),
		tools.NewLocalFileTool(e.devices, bot.OwnerID),
		tools.NewLocalAppTool(e.devices, bot.OwnerID),
		tools.NewLocalLocationTool(e.devices, bot.OwnerID),
		tools.NewLocalAudioTool(e.devices, bot.OwnerID),
		tools.NewLocalHTTPTool(e.devices, bot.OwnerID),
		tools.NewGitHubAPITool(e.connectors, bot.OwnerID),
		tools.NewSendSMSTool(phoneService),
		tools.NewCallPhoneTool(phoneService),
		tools.NewSlackPostTool(e.connectors, bot.OwnerID),
		tools.NewNotionSearchTool(e.connectors, bot.OwnerID),
		tools.NewSendEmailTool(e.connectors, e.mailbox, bot.OwnerID),
		tools.NewReadEmailTool(e.connectors, e.mailbox, bot.OwnerID),
		tools.NewConnectorRequestTool(e.connectors, bot.OwnerID),
		tools.NewPostgresQueryTool(e.connectors, bot.OwnerID),
		tools.NewConnectorListTool(e.connectors, bot.OwnerID),
		tools.NewConnectorAddTool(e.connectors, bot.OwnerID),
		tools.NewConnectorTestTool(e.connectors, bot.OwnerID),
		tools.NewConnectorRemoveTool(e.connectors, bot.OwnerID),
		tools.NewCalendarEventTool(e.db, bot.OwnerID, bot.ID, conv.ID),
		tools.NewCalendarListTool(e.db, bot.OwnerID),
		tools.NewCalDAVListTool(e.connectors, e.caldav, bot.OwnerID),
		tools.NewCalDAVCreateTool(e.connectors, e.caldav, bot.OwnerID),
	)
	if bot.MemoryEnabled {
		all = append(all,
			tools.NewMemorySaveTool(e.memory, bot.OwnerID, bot.ID, teamID),
			tools.NewMemorySearchTool(e.memory, bot.OwnerID, bot.ID, teamID),
			tools.NewMemoryForgetTool(e.memory, bot.OwnerID, bot.ID, teamID),
		)
	}
	proactiveConversation := uuid.Nil
	if conv.Kind == "direct" && conv.DirectBotID != nil && *conv.DirectBotID == bot.ID {
		proactiveConversation = conv.ID
	}
	if conv.Kind != "subagent" {
		all = append(all,
			tools.NewPollTool(e.db, e.bus, conv.ID, bot.ID, cycle.ID),
			tools.NewSendFileTool(e.db, e.bus, bot.OwnerID, conv.ID, bot.ID, cycle.ID, root),
			tools.NewProactiveMessageTool(e.db, e.bus, bot.OwnerID, bot.ID, bot.Name, proactiveConversation),
			tools.NewProactiveSettingTool(e.db, bot.OwnerID, bot.ID),
			tools.NewGitHubReviewTool(e.connectors, bot.OwnerID),
			tools.NewXDraftTool(e.db, e.bus, conv.ID, bot.ID, cycle.ID),
			tools.NewTeamReviewTool(e.db, bot.OwnerID, bot.ID, bot.Kind),
		)
		if account, err := e.db.GetAccountByID(ctx, bot.OwnerID); err == nil &&
			account.PrimaryBotID != nil && *account.PrimaryBotID == bot.ID {
			all = append(all,
				tools.NewAdminCreateAgentTool(e.db, bot.OwnerID, bot.ID, bot.ProviderID, bot.Model),
				tools.NewAdminListAgentsTool(e.db, bot.OwnerID),
				tools.NewAdminCreateGroupTool(e.db, e.chatSvc, bot.OwnerID),
				tools.NewAdminCreateRoutineTool(e.db, e.routineSvc, bot.OwnerID, bot.ID),
				tools.NewAdminSetPrimaryBotTool(e.db, bot.OwnerID, bot.ID),
			)
		}
	}
	if e.agentChat != nil {
		all = append(all, &agentMessageTool{
			chat:       e.agentChat,
			ownerID:    bot.OwnerID,
			senderID:   bot.ID,
			originConv: conv.ID,
			hop:        e.triggerHop(ctx, conv, cycle),
		})
	}
	var parentTaskID *uuid.UUID
	subagentDepth := int32(0)
	if conv.Kind == "subagent" {
		task, err := e.db.GetSubagentTaskByChildCycle(ctx, &cycle.ID)
		if err != nil {
			task, err = e.db.GetSubagentTaskByChildConversation(ctx, conv.ID)
		}
		if err == nil {
			parentTaskID = &task.ID
			subagentDepth = task.Depth + 1
		}
	}
	all = append(all,
		tools.NewSubagentSpawnTool(e.db, bot.OwnerID, bot.ID, conv.ID, cycle.ID, parentTaskID, subagentDepth),
		tools.NewSubagentCheckTool(e.db, bot.OwnerID),
		tools.NewSubagentStopTool(e.db, bot.OwnerID),
		tools.NewSubagentMessageTool(e.db, bot.OwnerID),
	)
	if conv.Kind != "subagent" {
		all = append(all,
			tools.NewTaskCreateTool(e.db, bot.OwnerID, bot.ID),
			tools.NewTaskListTool(e.db, bot.OwnerID),
			tools.NewTaskUpdateTool(e.db, bot.OwnerID),
			tools.NewProjectShareTool(e.db, bot.OwnerID),
			tools.NewProjectJoinTool(e.db, bot.OwnerID),
			tools.NewDocumentCreateTool(e.db, bot.OwnerID, bot.ID, documentIndexer(e.libraryIndexer)),
			tools.NewDocumentReadTool(e.db, bot.OwnerID),
		)
	}
	if goalTool := e.goalTool(ctx, conv, cycle, bot); goalTool != nil {
		all = append(all, goalTool)
	}
	for _, tool := range all {
		if !disabled[tool.Name()] {
			registry.Register(tool)
		}
	}
	cleanup := func() {}
	if e.mcp != nil {
		toolset, err := e.mcp.LoadToolset(ctx, bot.OwnerID, bot.ID)
		if err != nil {
			e.logger.Warn("load mcp toolset failed", "bot_id", bot.ID, "error", err)
		} else if toolset != nil {
			for _, tool := range toolset.Tools() {
				if !disabled[tool.Name()] {
					registry.Register(tool)
				}
			}
			cleanup = toolset.Close
		}
	}
	if bot.ComputerProfile == "none" && e.sandbox != nil && !disabled["computer_enable"] {
		own := bot
		registry.Register(&computerEnableTool{engine: e, bot: &own, registry: registry})
	}
	if (bot.ComputerProfile == "browser" || bot.ComputerProfile == "desktop") && e.sandbox != nil {
		// The computer boots on the first browser call, not with every turn:
		// a plain chat must not wait for (or pay for) a running computer.
		own := bot
		open := func(ctx context.Context) (*browser.Session, error) { return e.browserSession(ctx, own) }
		for _, tool := range browser.LazyTools(open) {
			if !disabled[tool.Name()] {
				registry.Register(tool)
			}
		}
	}
	return registry, cleanup
}

// browserSession reuses one CDP tab per bot across cycles. Approvals resume a
// run in a new cycle; creating a fresh tab each time would lose the page and
// every snapshot ref, which made browser_type/click/screenshot operate on
// about:blank.
func (e *Engine) browserSession(ctx context.Context, bot store.Bot) (*browser.Session, error) {
	e.browserMu.Lock()
	defer e.browserMu.Unlock()
	if session := e.browserSessions[bot.ID]; session != nil {
		if session.Alive() {
			return session, nil
		}
		session.Close()
		delete(e.browserSessions, bot.ID)
	}
	session, err := browser.NewSession(ctx, e.sandbox, bot.OwnerID, bot.ID, bot.ComputerProfile)
	if err != nil {
		return nil, err
	}
	e.browserSessions[bot.ID] = session
	return session, nil
}

func userMessageWithImages(content string, images []llm.Part) llm.Message {
	parts := []llm.Part{{Type: llm.PartText, Text: content}}
	parts = append(parts, images...)
	return llm.Message{Role: llm.RoleUser, Parts: parts}
}

func (e *Engine) buildMessages(ctx context.Context, bot store.Bot, conv store.Conversation, modelRow *store.Model, registry *tools.Registry, instruction, sharedHostPath, sharedContainerPath string, isolated bool, root *os.Root) ([]llm.Message, int, int64, error) {
	names := map[uuid.UUID]string{bot.ID: bot.Name}
	if conv.TeamID != nil {
		teamBots, err := e.db.ListTeamBots(ctx, *conv.TeamID)
		if err == nil {
			for _, tb := range teamBots {
				names[tb.Bot.ID] = tb.Bot.Name
			}
		}
	}
	if conv.Kind == "agent" {
		if thread, err := e.db.GetAgentThreadByConversation(ctx, conv.ID); err == nil {
			if a, err := e.db.GetBot(ctx, store.GetBotParams{ID: thread.BotAID, OwnerID: conv.CreatedBy}); err == nil {
				names[a.ID] = a.Name
			}
			if b, err := e.db.GetBot(ctx, store.GetBotParams{ID: thread.BotBID, OwnerID: conv.CreatedBy}); err == nil {
				names[b.ID] = b.Name
			}
		}
	}

	toolNames := make([]string, 0)
	for _, def := range registry.Definitions() {
		toolNames = append(toolNames, def.Name)
	}
	systemText := BuildSystemPrompt(
		bot,
		filepath.Join(e.cfg.DataDir, "workspaces", bot.ID.String()),
		sharedContainerPath,
		sharedHostPath,
		toolNames,
	)
	botSkills, _ := e.skills.ListForBot(ctx, bot.ID)
	if builtins, err := e.skills.ListBuiltins(ctx, bot.OwnerID); err == nil {
		seen := make(map[uuid.UUID]bool, len(botSkills))
		for _, skill := range botSkills {
			seen[skill.ID] = true
		}
		for _, skill := range builtins {
			if !seen[skill.ID] {
				botSkills = append(botSkills, skill)
			}
		}
	}
	if len(botSkills) > 0 {
		var sb strings.Builder
		sb.WriteString("\n## 已启用技能\n")
		for _, skill := range botSkills {
			fmt.Fprintf(&sb, "- %s: %s（用 skill_read 读取完整说明）\n", skill.Name, skill.Description)
		}
		systemText += sb.String()
	}
	system := llm.TextMessage(llm.RoleSystem, systemText)

	recent, err := e.db.ListRecentMessages(ctx, store.ListRecentMessagesParams{
		ConversationID: conv.ID,
		Limit:          historyLoadLimit,
	})
	if err != nil {
		return nil, 0, 0, fmt.Errorf("load recent messages: %w", err)
	}
	lastOrdinal := int64(0)
	if len(recent) > 0 {
		lastOrdinal = recent[0].Ordinal
	}
	// Query returns newest-first; reverse for chronological context.
	for i, j := 0, len(recent)-1; i < j; i, j = i+1, j-1 {
		recent[i], recent[j] = recent[j], recent[i]
	}

	attachmentNotes := make(map[uuid.UUID]string)
	imagesByMessage := make(map[uuid.UUID][]llm.Part)
	// Files a bot sent with send_file: the transcript names them on that
	// bot's message instead of treating them as user uploads.
	sentFiles := make(map[uuid.UUID][]string)
	if len(recent) > 0 {
		ids := make([]uuid.UUID, 0, len(recent))
		fromBot := make(map[uuid.UUID]bool, len(recent))
		for _, msg := range recent {
			ids = append(ids, msg.ID)
			fromBot[msg.ID] = msg.SenderType == "bot"
		}
		metas, err := e.db.ListAttachmentMetaForMessages(ctx, ids)
		if err == nil {
			for _, meta := range metas {
				if meta.MessageID == nil {
					continue
				}
				if fromBot[*meta.MessageID] {
					sentFiles[*meta.MessageID] = append(sentFiles[*meta.MessageID], meta.Filename)
					continue
				}
				note := fmt.Sprintf("【附件：%s（%s，%d 字节）】", meta.Filename, meta.MimeType, meta.SizeBytes)
				// Every attachment is also placed in the bot's computer, so
				// a Word template or a spreadsheet can be opened and filled
				// in with the right library instead of only being described.
				placed := ""
				if root != nil && meta.SizeBytes > 0 && meta.SizeBytes <= 25<<20 {
					if att, err := e.db.GetAttachmentForOwner(ctx, store.GetAttachmentForOwnerParams{
						ID: meta.ID, OwnerID: meta.OwnerID,
					}); err == nil {
						if rel, err := tools.PlaceInWorkspace(root, meta.Filename, att.Content); err == nil {
							placed = rel
						} else {
							e.logger.Warn("place attachment in workspace failed", "attachment_id", meta.ID, "error", err)
						}
						if officetext.Supported(meta.Filename) {
							if text, cut, err := officetext.Extract(meta.Filename, att.Content, 6000); err == nil && text != "" {
								note += "\n正文（自动提取，排版和图片已省略）：\n" + text
								if cut {
									note += "\n…（正文较长，已截断；完整内容在电脑里的原文件中）"
								}
							}
						}
					}
				}
				if placed != "" {
					note += "\n原文件已放到你的电脑：/workspace/" + placed + "（工作区路径 " + placed + "）。需要保留原格式（例如按模板填写）时，在电脑里用 python 处理这个文件。"
				}
				if officetext.Supported(meta.Filename) {
					// Text and the file location are already in the note.
				} else if strings.HasPrefix(meta.MimeType, "text/") || meta.MimeType == "application/json" {
					att, err := e.db.GetAttachmentForOwner(ctx, store.GetAttachmentForOwnerParams{
						ID:      meta.ID,
						OwnerID: meta.OwnerID,
					})
					if err == nil {
						content := strings.ToValidUTF8(string(att.Content), "")
						if len(content) > 4000 {
							content = content[:4000] + "\n…（附件内容已截断）"
						}
						note += "\n" + content
					}
				} else if strings.HasPrefix(meta.MimeType, "image/") &&
					modelRow != nil && modelRow.SupportsVision {
					att, err := e.db.GetAttachmentForOwner(ctx, store.GetAttachmentForOwnerParams{
						ID:      meta.ID,
						OwnerID: meta.OwnerID,
					})
					if err == nil && len(att.Content) > 0 && len(att.Content) <= 4<<20 &&
						len(imagesByMessage[*meta.MessageID]) < 4 {
						imagesByMessage[*meta.MessageID] = append(
							imagesByMessage[*meta.MessageID],
							llm.Part{
								Type:  llm.PartImage,
								Image: &llm.Image{MIMEType: meta.MimeType, Data: att.Content},
							},
						)
					} else {
						note += "\n（图片过大或读取失败，未能附加到模型输入。）"
					}
				} else if placed == "" {
					note += "\n（当前模型无法直接查看该类型的附件，请向用户说明或请其提供文本描述。）"
				}
				attachmentNotes[*meta.MessageID] += note + "\n"
			}
		}
	}

	memoryBlock := ""
	for i := len(recent) - 1; i >= 0; i-- {
		if recent[i].SenderType != "user" || strings.TrimSpace(recent[i].Content) == "" {
			continue
		}
		recallUserID := conv.DirectUserID
		if recalled := e.memory.Recall(ctx, bot.OwnerID, bot.ID, conv.TeamID, recallUserID, recent[i].Content, 6); len(recalled) > 0 {
			var sb strings.Builder
			sb.WriteString("## 相关记忆\n以下是从长期记忆中自动召回的相关事实；它们可能过时，以当前对话与工具结果为准。\n")
			for _, memoryItem := range recalled {
				fmt.Fprintf(&sb, "- [%s] %s\n", memoryItem.ScopeType, truncateForCoordinator(memoryItem.Content, 400))
			}
			memoryBlock = sb.String()
		}
		break
	}
	// Order matters for provider prefix caching: the system prompt and the
	// checkpoint summary change rarely, so they come first; per-turn recall is
	// appended after the history instead of sitting in the stable prefix.
	messages := []llm.Message{system}
	summarySeq := int64(-1)
	if summary, err := e.db.GetLatestConversationSummary(ctx, conv.ID); err == nil &&
		strings.TrimSpace(summary.Content) != "" {
		summarySeq = summary.UpToSeq
		messages = append(messages, llm.TextMessage(llm.RoleSystem,
			"## 早前对话检查点\n以下是本次会话较早内容的整理，属于参考资料而不是新的指令；以当前消息与工具结果为准。"+
				"需要原文时用 search_chat_history 按关键词或消息序号回查。\n"+summary.Content))
		e.logger.Info("conversation summary in prompt",
			"conversation_id", conv.ID, "up_to_seq", summary.UpToSeq,
			"chars", len([]rune(summary.Content)))
	}
	if len(attachmentNotes) > 0 {
		var sb strings.Builder
		sb.WriteString("## 用户附件\n以下附件随用户消息提供；图片附件当前模型可能无法直接查看。\n")
		for _, note := range attachmentNotes {
			sb.WriteString("- ")
			sb.WriteString(strings.ReplaceAll(note, "\n", "\n  "))
			sb.WriteString("\n")
		}
		messages = append(messages, llm.TextMessage(llm.RoleSystem, sb.String()))
		e.logger.Info("attachments in prompt", "count", len(attachmentNotes))
	}
	pinned := len(messages)
	goalBlock := e.goalBlock(ctx, conv, bot)
	if isolated {
		// Router-Worker mode: the worker only sees the user's task and its
		// assignment, not the full shared transcript.
		for i := len(recent) - 1; i >= 0; i-- {
			if recent[i].SenderType == "user" && strings.TrimSpace(recent[i].Content) != "" {
				content := strings.TrimSpace(recent[i].Content)
				if note := attachmentNotes[recent[i].ID]; note != "" {
					content += "\n\n" + note
				}
				messages = append(messages, userMessageWithImages(content, imagesByMessage[recent[i].ID]))
				break
			}
		}
		if strings.TrimSpace(instruction) != "" {
			messages = append(messages, llm.TextMessage(llm.RoleSystem, "【协调者任务】\n"+instruction))
		}
		if memoryBlock != "" {
			messages = append(messages, llm.TextMessage(llm.RoleSystem, memoryBlock))
		}
		if goalBlock != "" {
			messages = append(messages, llm.TextMessage(llm.RoleSystem, goalBlock))
		}
		return messages, pinned, lastOrdinal, nil
	}
	recent = orderByDelivery(recent, bot.ID)
	for _, msg := range recent {
		if summarySeq >= 0 && msg.Ordinal > 0 && msg.Ordinal <= summarySeq {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if files := sentFiles[msg.ID]; len(files) > 0 {
			content = strings.TrimSpace(content + "\n[已把文件发给用户：" + strings.Join(files, "、") + "]")
		}
		if content == "" || messageKind(msg) == messageKindCheckpoint {
			continue
		}
		switch msg.SenderType {
		case "user":
			if note := attachmentNotes[msg.ID]; note != "" {
				content += "\n\n" + note
			}
			messages = append(messages, userMessageWithImages(content, imagesByMessage[msg.ID]))
		case "system":
			// Notices enter the prompt in the user role. Providers define a
			// turn by its user message; a system message in the middle of
			// the transcript makes thinking models treat the assistant reply
			// after it as an unfinished step and reject the request.
			if text := messageInjectText(msg); text != "" {
				// A folded card shows the user one line; the model gets
				// the result the card stands for.
				content = text
			} else if messageKind(msg) == "agent_thread" {
				// Display-only: the tool result already told the bot.
				continue
			}
			messages = append(messages, llm.TextMessage(llm.RoleUser, "【系统通知】\n"+content))
		case "bot":
			if msg.SenderBotID != nil && *msg.SenderBotID == bot.ID {
				// Earlier turns keep only their final text; naming the tools
				// that produced it stops the model from assuming it never
				// touched the workspace.
				if used := messageToolsUsed(msg); len(used) > 0 {
					content += "\n\n[该轮使用过的工具：" + strings.Join(used, ", ") + "；产物在工作区 artifacts 目录]"
				}
				messages = append(messages, llm.TextMessage(llm.RoleAssistant, content))
			} else {
				name := "队友"
				if msg.SenderBotID != nil {
					if n, ok := names[*msg.SenderBotID]; ok {
						name = n
					}
				}
				messages = append(messages, llm.TextMessage(llm.RoleUser, fmt.Sprintf("[%s]: %s", name, content)))
			}
		}
	}

	// Leave room for the trailing recall and instruction blocks so trimming
	// history never has to be repeated after they are appended.
	tail := EstimateTokens(memoryBlock) + EstimateTokens(goalBlock) + EstimateTokens(instruction) + 24
	messages = trimHistory(messages, pinned, promptBudget(modelRow)-tail, 6)
	messages = endWithUserTurn(messages)
	if memoryBlock != "" {
		messages = append(messages, llm.TextMessage(llm.RoleSystem, memoryBlock))
	}
	if goalBlock != "" {
		messages = append(messages, llm.TextMessage(llm.RoleSystem, goalBlock))
	}
	if strings.TrimSpace(instruction) != "" {
		messages = append(messages, llm.TextMessage(llm.RoleSystem, "【协调者指令】\n"+instruction))
	}
	return messages, pinned, lastOrdinal, nil
}

func (e *Engine) streamTurn(ctx context.Context, provider llm.Provider, req llm.Request, conv store.Conversation, cycle store.Cycle, run store.Run, bot store.Bot) (llmTurn, error) {
	stream, err := provider.Stream(ctx, req)
	if err != nil {
		return llmTurn{}, err
	}
	defer stream.Close()

	ticker := time.NewTicker(deltaFlushInterval)
	defer ticker.Stop()

	var turn llmTurn
	var pendingText, pendingReasoning strings.Builder
	flush := func() error {
		if pendingText.Len() > 0 {
			if err := e.appendEvent(ctx, conv.ID, &cycle.ID, &run.ID, events.TypeTextDelta, map[string]any{
				"run_id": run.ID.String(),
				"bot_id": bot.ID.String(),
				"text":   pendingText.String(),
			}); err != nil {
				return err
			}
			pendingText.Reset()
		}
		if pendingReasoning.Len() > 0 {
			if err := e.appendEvent(ctx, conv.ID, &cycle.ID, &run.ID, events.TypeReasoningDelta, map[string]any{
				"run_id": run.ID.String(),
				"bot_id": bot.ID.String(),
				"text":   pendingReasoning.String(),
			}); err != nil {
				return err
			}
			pendingReasoning.Reset()
		}
		return nil
	}

loop:
	for {
		select {
		case ev, ok := <-stream.Events():
			if !ok {
				break loop
			}
			switch ev.Type {
			case llm.EventTextDelta:
				turn.Text += ev.Text
				pendingText.WriteString(ev.Text)
				if pendingText.Len() >= 4*1024 {
					if err := flush(); err != nil {
						return llmTurn{}, err
					}
				}
			case llm.EventReasoningDelta:
				turn.Reasoning += ev.Text
				pendingReasoning.WriteString(ev.Text)
			case llm.EventToolCalls:
				if err := flush(); err != nil {
					return llmTurn{}, err
				}
				turn.ToolCalls = append(turn.ToolCalls, ev.ToolCalls...)
			case llm.EventUsage:
				turn.Usage = ev.Usage
			case llm.EventError:
				return llmTurn{}, ev.Err
			case llm.EventDone:
				turn.FinishReason = ev.FinishReason
			}
		case <-ticker.C:
			if err := flush(); err != nil {
				return llmTurn{}, err
			}
		case <-ctx.Done():
			return llmTurn{}, context.Cause(ctx)
		}
	}
	if err := flush(); err != nil {
		return llmTurn{}, err
	}
	if err := ctx.Err(); err != nil {
		return llmTurn{}, context.Cause(ctx)
	}
	return turn, nil
}

func (e *Engine) toolNeedsApproval(ctx context.Context, bot store.Bot, call llm.ToolCall) bool {
	autoReview, err := e.db.GetUserAutoReview(ctx, bot.OwnerID)
	if err != nil {
		// Fail closed: an unreadable policy must not silently allow tools.
		return true
	}
	if !autoReview {
		// Auto-review off means "ask for every operation", not "skip checks".
		return true
	}
	rules, err := e.db.ListPermissionRules(ctx, bot.OwnerID)
	if err != nil {
		return true
	}
	return approvalRequired(autoReview, rules, call)
}

func maskApprovalArgs(raw json.RawMessage) json.RawMessage {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return json.RawMessage(`{}`)
	}
	maskSensitiveValue(value)
	encoded, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return encoded
}

func maskSensitiveValue(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			lower := strings.ToLower(key)
			if strings.Contains(lower, "token") || strings.Contains(lower, "secret") ||
				strings.Contains(lower, "password") || strings.Contains(lower, "api_key") ||
				strings.Contains(lower, "apikey") || lower == "authorization" || lower == "cookie" {
				typed[key] = "***"
				continue
			}
			maskSensitiveValue(item)
		}
	case []any:
		for _, item := range typed {
			maskSensitiveValue(item)
		}
	}
}

func (e *Engine) executeTool(ctx context.Context, conv store.Conversation, cycle store.Cycle, run store.Run, bot store.Bot, registry *tools.Registry, call llm.ToolCall) (result tools.Result) {
	// A panicking tool must cost one tool call, not the whole worker.
	defer func() {
		if recovered := recover(); recovered != nil {
			e.logger.Error("tool panicked", "tool", call.Name, "run_id", run.ID, "panic", recovered, "stack", string(debug.Stack()))
			result = tools.Result{Content: fmt.Sprintf("工具 %s 内部出错（panic），本次调用未完成。", call.Name), IsError: true}
		}
	}()
	// Tool activity keeps a long browser/computer run from being auto-slept.
	_ = e.db.TouchBotActivity(ctx, store.TouchBotActivityParams{ID: bot.ID, OwnerID: bot.OwnerID})
	if len(call.Arguments) > 0 && !json.Valid(call.Arguments) {
		// A long tool argument can be cut off by the model's output limit.
		// Do not try to persist invalid JSON into a jsonb column; tell the
		// model to split the work instead.
		content := "工具参数不是合法 JSON，可能被输出长度截断。请减小单次写入的内容，或拆成多次工具调用后重试。"
		_ = e.appendEvent(ctx, conv.ID, &cycle.ID, &run.ID, events.TypeToolCall, map[string]any{
			"run_id":        run.ID.String(),
			"call_id":       call.ID,
			"name":          call.Name,
			"arguments_raw": string(call.Arguments),
			"valid":         false,
		})
		_ = e.appendEvent(ctx, conv.ID, &cycle.ID, &run.ID, events.TypeToolResult, map[string]any{
			"run_id":   run.ID.String(),
			"call_id":  call.ID,
			"name":     call.Name,
			"content":  content,
			"is_error": true,
			"artifact": "",
		})
		return tools.Result{Content: content, IsError: true}
	}

	maskedArgs := maskApprovalArgs(call.Arguments)

	if err := e.appendEvent(ctx, conv.ID, &cycle.ID, &run.ID, events.TypeToolCall, map[string]any{
		"run_id":    run.ID.String(),
		"call_id":   call.ID,
		"name":      call.Name,
		"arguments": maskedArgs,
	}); err != nil {
		e.logger.Warn("append tool_call event failed", "run_id", run.ID, "error", err)
	}

	invocation, err := e.db.CreateToolInvocation(ctx, store.CreateToolInvocationParams{
		RunID:  run.ID,
		CallID: call.ID,
		Name:   call.Name,
		Args:   maskedArgs,
	})
	if err != nil {
		e.logger.Warn("create tool invocation failed", "run_id", run.ID, "call_id", call.ID, "error", err)
		return tools.Result{Content: "tool invocation could not be recorded: " + err.Error(), IsError: true}
	}
	_ = e.db.MarkToolInvocationRunning(ctx, invocation.ID)

	result = registry.Execute(ctx, call.Name, call.Arguments)
	artifactPath := e.writeArtifact(bot.ID, run.ID, call.ID, result.Content)

	status := "done"
	errText := ""
	if result.IsError {
		status = "failed"
		errText = result.Content
	}
	if _, err := e.db.FinishToolInvocation(ctx, store.FinishToolInvocationParams{
		ID:             invocation.ID,
		Status:         status,
		ResultArtifact: artifactPath,
		Error:          errText,
	}); err != nil {
		e.logger.Warn("finish tool invocation failed", "run_id", run.ID, "call_id", call.ID, "error", err)
	}

	content := result.Content
	if len(content) > maxToolResultBytes {
		content = content[:maxToolResultBytes] + "\n\n[truncated; full result: " + artifactPath + "]"
	}
	payload := map[string]any{
		"run_id":   run.ID.String(),
		"call_id":  call.ID,
		"name":     call.Name,
		"content":  content,
		"is_error": result.IsError,
		"artifact": artifactPath,
	}
	if result.WidgetHTML != "" {
		payload["widget_html"] = result.WidgetHTML
	}
	if err := e.appendEvent(ctx, conv.ID, &cycle.ID, &run.ID, events.TypeToolResult, payload); err != nil {
		e.logger.Warn("append tool_result event failed", "run_id", run.ID, "error", err)
	}
	return tools.Result{Content: content, IsError: result.IsError, ArtifactPath: artifactPath, WidgetHTML: result.WidgetHTML}
}

func (e *Engine) writeArtifact(botID, runID uuid.UUID, callID, content string) string {
	if content == "" {
		return ""
	}
	dir := filepath.Join(e.cfg.DataDir, "workspaces", botID.String(), "artifacts", runID.String())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		e.logger.Warn("create artifact dir failed", "error", err)
		return ""
	}
	name := safeFileName(callID)
	if name == "" {
		name = uuid.NewString()
	}
	filePath := filepath.Join(dir, name+".txt")
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		e.logger.Warn("write artifact failed", "error", err)
		return ""
	}
	return filePath
}

var unsafeFileChars = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func safeFileName(s string) string {
	name := unsafeFileChars.ReplaceAllString(s, "_")
	if len(name) > 80 {
		name = name[:80]
	}
	return name
}

func (e *Engine) insertFinalMessage(ctx context.Context, convID, cycleID uuid.UUID, leaseID, botID, runID uuid.UUID, content string, metadata json.RawMessage) (*store.Message, error) {
	tx, err := e.db.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin final message: %w", err)
	}
	defer tx.Rollback(ctx)
	q := e.db.WithTx(tx)

	if _, err := q.LockCycleLease(ctx, store.LockCycleLeaseParams{ID: cycleID, LeaseID: &leaseID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errLeaseLost
		}
		return nil, fmt.Errorf("lock cycle lease: %w", err)
	}

	msg, err := q.CreateMessage(ctx, store.CreateMessageParams{
		ConversationID: convID,
		CycleID:        &cycleID,
		SenderType:     "bot",
		SenderBotID:    &botID,
		Content:        content,
		Metadata:       metadata,
		RunID:          &runID,
	})
	if err != nil {
		return nil, fmt.Errorf("create bot message: %w", err)
	}
	if _, err := events.Append(ctx, q, convID, &cycleID, &runID, events.TypeMessageCreated, map[string]any{"message": msg}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit final message: %w", err)
	}
	if err := e.bus.Notify(ctx, convID); err != nil {
		e.logger.Warn("notify failed", "conversation_id", convID, "error", err)
	}
	return &msg, nil
}

func (e *Engine) finishRun(ctx context.Context, outcome *runOutcome, status, warning, errMsg string) {
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_, err := e.db.FinishRun(writeCtx, store.FinishRunParams{
		ID:         outcome.RunID,
		Status:     status,
		Warning:    warning,
		Error:      errMsg,
		UsageIn:    outcome.UsageIn,
		UsageOut:   outcome.UsageOut,
		CostMicros: outcome.CostMicros,
		Step:       outcome.Step,
	})
	if err != nil {
		e.logger.Warn("finish run failed", "run_id", outcome.RunID, "error", err)
		return
	}
	if err := e.appendEvent(writeCtx, outcome.ConversationID, &outcome.CycleID, &outcome.RunID, events.TypeStatus, map[string]any{
		"run_id": outcome.RunID.String(),
		"status": status,
		"error":  errMsg,
	}); err != nil {
		e.logger.Warn("append run status event failed", "run_id", outcome.RunID, "error", err)
	}
}

func (e *Engine) failCycle(ctx context.Context, cycle store.Cycle, leaseID uuid.UUID, cause error) {
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	row, err := e.db.RetryOrFailCycle(writeCtx, store.RetryOrFailCycleParams{
		ID:      cycle.ID,
		LeaseID: &leaseID,
		Error:   cause.Error(),
	})
	if err != nil {
		e.logger.Warn("retry/fail cycle failed", "cycle_id", cycle.ID, "error", err)
		return
	}
	if err := e.appendEvent(writeCtx, cycle.ConversationID, &cycle.ID, nil, events.TypeCycleUpdated, map[string]any{
		"cycle_id":     cycle.ID.String(),
		"status":       row.Status,
		"error":        row.Error,
		"attempt":      row.Attempt,
		"max_attempts": row.MaxAttempts,
	}); err != nil {
		e.logger.Warn("append retry status event failed", "cycle_id", cycle.ID, "error", err)
	}
}

func (e *Engine) setCycleStatus(ctx context.Context, cycle store.Cycle, leaseID uuid.UUID, status, errMsg string) {
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	var n int64
	var err error
	switch status {
	case "done":
		n, err = e.db.SetCycleDone(writeCtx, store.SetCycleDoneParams{ID: cycle.ID, LeaseID: &leaseID})
	case "waiting_user":
		n, err = e.db.SetCycleWaitingUser(writeCtx, store.SetCycleWaitingUserParams{ID: cycle.ID, LeaseID: &leaseID})
	case "waiting_approval":
		n, err = e.db.SetCycleWaitingApproval(writeCtx, store.SetCycleWaitingApprovalParams{ID: cycle.ID, LeaseID: &leaseID})
	case "canceled":
		n, err = e.db.SetCycleCanceled(writeCtx, store.SetCycleCanceledParams{ID: cycle.ID, LeaseID: &leaseID})
	case "failed":
		n, err = e.db.SetCycleFailed(writeCtx, store.SetCycleFailedParams{ID: cycle.ID, LeaseID: &leaseID, Error: errMsg})
	default:
		return
	}
	if err != nil {
		e.logger.Warn("set cycle status failed", "cycle_id", cycle.ID, "status", status, "error", err)
		return
	}
	if n == 0 {
		e.logger.Warn("cycle lease was lost before status write", "cycle_id", cycle.ID, "status", status)
		return
	}
	if err := e.appendEvent(writeCtx, cycle.ConversationID, &cycle.ID, nil, events.TypeCycleUpdated, map[string]any{
		"cycle_id": cycle.ID.String(),
		"status":   status,
		"error":    errMsg,
	}); err != nil {
		e.logger.Warn("append cycle status event failed", "cycle_id", cycle.ID, "error", err)
	}
}

func (e *Engine) emitError(ctx context.Context, convID uuid.UUID, cycleID, runID *uuid.UUID, cause error) {
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	payload := map[string]any{"error": cause.Error()}
	if runID != nil {
		payload["run_id"] = runID.String()
	}
	_ = e.appendEvent(writeCtx, convID, cycleID, runID, events.TypeError, payload)
}

func (e *Engine) appendEvent(ctx context.Context, convID uuid.UUID, cycleID, runID *uuid.UUID, typ string, payload any) error {
	if convID == uuid.Nil {
		return nil
	}
	if _, err := events.Append(ctx, e.db.Queries, convID, cycleID, runID, typ, payload); err != nil {
		return err
	}
	if err := e.bus.Notify(ctx, convID); err != nil {
		e.logger.Warn("notify failed", "conversation_id", convID, "error", err)
	}
	return nil
}

type leaseGuard struct {
	ctx    context.Context
	cancel context.CancelCauseFunc
	done   chan struct{}
}

func (e *Engine) startLeaseGuard(parent context.Context, cycleID, leaseID uuid.UUID) *leaseGuard {
	ctx, cancel := context.WithCancelCause(parent)
	guard := &leaseGuard{ctx: ctx, cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(guard.done)
		ticker := time.NewTicker(leaseCheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				hbCtx, hbCancel := context.WithTimeout(ctx, 5*time.Second)
				_, err := e.db.HeartbeatCycle(hbCtx, store.HeartbeatCycleParams{ID: cycleID, LeaseID: &leaseID})
				hbCancel()
				if err != nil {
					cancel(errLeaseLost)
					return
				}
				checkCtx, checkCancel := context.WithTimeout(ctx, 5*time.Second)
				cycle, err := e.db.GetCycle(checkCtx, cycleID)
				checkCancel()
				if err != nil {
					cancel(errLeaseLost)
					return
				}
				if cycle.CancelRequested || cycle.Status != "running" {
					cancel(errCanceled)
					return
				}
			}
		}
	}()
	return guard
}

func (g *leaseGuard) Stop() {
	g.cancel(nil)
	<-g.done
}

func (e *Engine) reserve(ctx context.Context, convID, cycleID uuid.UUID, modelRow *store.Model, req llm.Request) (int64, error) {
	amount := estimateRequestCost(modelRow, req)
	if amount <= 0 {
		return 0, nil
	}
	n, err := e.db.ReserveConversationBudget(ctx, store.ReserveConversationBudgetParams{ID: convID, ReservedMicros: amount})
	if err != nil {
		return 0, fmt.Errorf("reserve conversation budget: %w", err)
	}
	if n == 0 {
		return 0, fmt.Errorf("会话预算不足，已停止本轮协作")
	}
	n, err = e.db.ReserveCycleBudget(ctx, store.ReserveCycleBudgetParams{ID: cycleID, ReservedMicros: amount})
	if err != nil {
		_ = e.db.ReleaseConversationReservation(ctx, store.ReleaseConversationReservationParams{ID: convID, ReservedMicros: amount})
		return 0, fmt.Errorf("reserve cycle budget: %w", err)
	}
	if n == 0 {
		_ = e.db.ReleaseConversationReservation(ctx, store.ReleaseConversationReservationParams{ID: convID, ReservedMicros: amount})
		return 0, fmt.Errorf("本轮预算不足，已停止协作")
	}
	return amount, nil
}

func (e *Engine) release(ctx context.Context, convID, cycleID uuid.UUID, amount int64) {
	if amount <= 0 {
		return
	}
	if err := e.db.ReleaseConversationReservation(ctx, store.ReleaseConversationReservationParams{ID: convID, ReservedMicros: amount}); err != nil {
		e.logger.Warn("release conversation reservation failed", "error", err)
	}
	if err := e.db.ReleaseCycleReservation(ctx, store.ReleaseCycleReservationParams{ID: cycleID, ReservedMicros: amount}); err != nil {
		e.logger.Warn("release cycle reservation failed", "error", err)
	}
}

func (e *Engine) settle(ctx context.Context, convID, cycleID uuid.UUID, reserved, actual int64) {
	if err := e.db.SettleConversationUsage(ctx, store.SettleConversationUsageParams{ID: convID, ReservedMicros: reserved, SpentMicros: actual}); err != nil {
		e.logger.Warn("settle conversation usage failed", "error", err)
	}
	if err := e.db.SettleCycleUsage(ctx, store.SettleCycleUsageParams{ID: cycleID, ReservedMicros: reserved, SpentMicros: actual}); err != nil {
		e.logger.Warn("settle cycle usage failed", "error", err)
	}
}

func estimateRequestCost(modelRow *store.Model, req llm.Request) int64 {
	if modelRow == nil {
		return 0
	}
	promptTokens := int64(EstimateMessagesTokens(req.Messages))
	outputTokens := int64(req.MaxTokens)
	if modelRow.PriceInMicrosPerMtok == 0 && modelRow.PriceOutMicrosPerMtok == 0 {
		return 0
	}
	return promptTokens*modelRow.PriceInMicrosPerMtok/1_000_000 + outputTokens*modelRow.PriceOutMicrosPerMtok/1_000_000
}

func computeCost(modelRow *store.Model, usage *llm.Usage) int64 {
	if modelRow == nil || usage == nil {
		return 0
	}
	prompt := int64(usage.PromptTokens)
	cached := int64(usage.CachedTokens)
	if cached > prompt {
		cached = prompt
	}
	uncached := prompt - cached
	cost := uncached*modelRow.PriceInMicrosPerMtok/1_000_000 +
		cached*modelRow.PriceCacheMicrosPerMtok/1_000_000 +
		int64(usage.CompletionTokens)*modelRow.PriceOutMicrosPerMtok/1_000_000
	return cost
}

func parseBotParams(raw json.RawMessage) botParams {
	var params botParams
	_ = json.Unmarshal(raw, &params)
	return params
}

// ensureDirs creates mount directories through the sandbox manager, which
// falls back to the Docker proxy where the services may not create them.
func (e *Engine) ensureDirs(ctx context.Context, dirs ...string) error {
	if e.sandbox != nil {
		return e.sandbox.EnsureDirs(ctx, dirs...)
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o777); err != nil {
			return err
		}
	}
	return nil
}
