package talk

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
)

const (
	defaultCleanupTimeout = 15 * time.Second

	// sipTeardownTimeout 是拆 SIP 对话(BYE)的独立预算。
	//
	// 必须单独给:**交叉 BYE**(双方在毫秒内各发一个 BYE)时对端不会回 200,
	// 我方拆除事务只能按 T1 退避一路重传(实测 52.892 / 53.893 / 55.894 /
	// 59.895 / 03.896 五次)。这一条就能把整个清理预算吃光。SIP 事务是清理链路里
	// 唯一由对端决定快慢的步骤,不能让它独占预算。
	sipTeardownTimeout = 3 * time.Second

	// mediaReleaseTimeout 是每个 ZLM 释放步骤的独立预算。
	mediaReleaseTimeout = 5 * time.Second
)

// stepBudget 给单个清理步骤开一份独立预算。
//
// 两个要点缺一不可:
//   - context.WithoutCancel:摘掉父 ctx 的取消信号。父 ctx 到期不该让**后续**
//     步骤全部短路,否则会留下没释放的媒体资源(实测 stopSendRtp / close source
//     就是这样被跳过的:Hook 侧看是"流已断",ZLM 侧其实还挂着发送会话)。
//   - 各自的短超时:任何一步都不允许吃掉整个清理预算。
func stepBudget(parent context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(parent), d)
}

type cleanupCall struct {
	done chan struct{}
	err  error
}

type cleanupRuntime struct {
	mu      sync.Mutex
	running map[string]*cleanupCall
}

func newCleanupRuntime() *cleanupRuntime {
	return &cleanupRuntime{running: make(map[string]*cleanupCall)}
}

func (s *Service) Cleanup(ctx context.Context, sessionID string, terminal models.TalkSessionState, reason string) error {
	err, _ := s.cleanupWithOwnership(ctx, sessionID, terminal, reason)
	return err
}

// cleanupWithOwnership identifies the caller that started the shared cleanup.
// Other schema callbacks wait for the same result without reporting it again.
func (s *Service) cleanupWithOwnership(ctx context.Context, sessionID string, terminal models.TalkSessionState, reason string) (error, bool) {
	if s == nil || s.repo == nil {
		return ErrTalkActivationUnavailable, true
	}
	if !terminal.IsTerminal() {
		return fmt.Errorf("%w: %s", ErrInvalidTerminalState, terminal), true
	}
	if s.cleanup == nil {
		s.cleanup = newCleanupRuntime()
	}
	call, owner := s.cleanup.begin(sessionID)
	if owner {
		go func() {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), defaultCleanupTimeout)
			defer cancel()
			err := s.executeCleanup(cleanupCtx, sessionID, terminal, reason)
			s.cleanup.complete(sessionID, call, err)
		}()
	}
	select {
	case <-ctx.Done():
		return ctx.Err(), owner
	case <-call.done:
		return call.err, owner
	}
}

func (s *Service) executeCleanup(ctx context.Context, sessionID string, terminal models.TalkSessionState, reason string) error {
	session, err := s.repo.FindBySession(ctx, sessionID)
	if err != nil || session == nil {
		return err
	}
	if session.State.IsTerminal() {
		return nil
	}
	var firstErr error
	retryRequired := false
	record := func(step string, stepErr error, retryable bool) {
		if stepErr == nil {
			return
		}
		if firstErr == nil {
			firstErr = fmt.Errorf("%s: %w", step, stepErr)
		}
		retryRequired = retryRequired || retryable
		fields := []zap.Field{
			zap.String("session_id", sessionID), zap.String("stream_id", session.SourceStream),
			zap.String("cleanup_step", step), logging.Error(stepErr),
		}
		if retryable {
			app.Log(ctx).Named("talk").Warn("对讲清理步骤失败，保留会话等待重试",
				append(fields, zap.String("event", "gb28181.talk.cleanup.step_failed"))...)
		} else {
			app.Log(ctx).Named("talk").Info("对讲清理步骤未确认，继续释放资源",
				append(fields, zap.String("event", "gb28181.talk.cleanup.step_unconfirmed"))...)
		}
	}
	if session.State != models.TalkSessionStopping {
		changed, transitionErr := s.repo.Transition(ctx, sessionID, session.State, models.TalkSessionStopping, TransitionPatch{})
		record("mark stopping", transitionErr, true)
		if transitionErr == nil && !changed {
			reloaded, reloadErr := s.repo.FindBySession(ctx, sessionID)
			record("reload cleanup state", reloadErr, true)
			if reloaded == nil {
				record("reload cleanup state", ErrTalkSessionNotFound, true)
			} else if reloaded.State.IsTerminal() {
				return firstErr
			} else {
				session = reloaded
				if session.State != models.TalkSessionStopping {
					// CAS 落空说明**另一路并发推进了状态**，典型是设备 ACK 把 inviting 推到
					// active（实测 ACK 与 BYE 相隔 0.36ms）。这是良性竞争而非故障：用最新状态
					// 再抢一次即可。原来直接记 conflict，会把假错误写进会话 error 列，看着像故障。
					retried, retryErr := s.repo.Transition(ctx, sessionID, session.State, models.TalkSessionStopping, TransitionPatch{})
					record("mark stopping", retryErr, true)
					if retryErr == nil && !retried {
						record("cleanup state conflict", fmt.Errorf("state=%s", session.State), true)
					}
				}
			}
		}
	}
	if session.CallID != "" && s.activation != nil {
		sipCtx, cancelSIP := stepBudget(ctx, sipTeardownTimeout)
		if session.Mode == models.TalkSessionModeBroadcast && s.activation.deps.BroadcastDialogs != nil {
			record("Broadcast BYE", s.activation.deps.BroadcastDialogs.ByeBroadcast(sipCtx, session.CallID), false)
		} else if session.Mode == models.TalkSessionModeTalk && s.activation.deps.Inviter != nil {
			record("TALK BYE", s.activation.deps.Inviter.ByeTalk(sipCtx, session.CallID), false)
		}
		cancelSIP()
	}
	var client TalkMediaClient
	if s.activation == nil || s.activation.deps.ClientFor == nil || s.nodes == nil {
		record("ZLM client", ErrTalkActivationUnavailable, true)
	} else if mediaNode, ok := s.nodes.Get(session.NodeID); !ok || mediaNode == nil {
		record("ZLM node", ErrTalkNodeUnavailable, true)
	} else {
		client = s.activation.deps.ClientFor(mediaNode)
		if client == nil {
			record("ZLM client", ErrTalkActivationUnavailable, true)
		}
	}
	if client != nil && session.SourceStream != "" && session.SSRC != "" {
		stopCtx, cancelStop := stepBudget(ctx, mediaReleaseTimeout)
		stopErr := client.StopSendRtp(stopCtx, defaultTalkVHost, session.App, session.SourceStream, session.SSRC)
		cancelStop()
		// ⛔ 只有**我们确实建过发送会话**时，stopSendRtp 的失败才值得重试。
		//
		// local_port 是 answering 阶段 startSendRtpPassive/StartBroadcastSendRtp 成功后写下的
		// （talk/activation.go、talk/broadcast_activation.go）。设备从未应答 ⇒ local_port 仍为 0
		// ⇒ 本次会话压根没建过发送会话，"停掉"这个诉求其实已经达成。
		//
		// 这一层是**兜底**：ZLM 侧"流在但无该 ssrc 发送器"现在已在 client.StopSendRtp 里
		// 按幂等处理（回 -1 other_failed，见 zlm/client_talk.go）。留这里是因为幂等只覆盖
		// 已定义的返回码；传输层错误、-400 exception 等仍会返回 error，而 local_port=0 时
		// 这些同样不该把租约吊在 stopping（否则 DELETE 变 500「语音对讲操作失败」，
		// 2026-10-03 实测两条会话都卡到租约过期）。失败仍要**记进 error 列**，只是不拦收尾。
		record("stopSendRtp", stopErr, session.LocalPort > 0)
	}
	if client != nil && session.SourceStream != "" {
		closeCtx, cancelClose := stepBudget(ctx, mediaReleaseTimeout)
		record("close source", client.CloseTalkSource(closeCtx, defaultTalkVHost, session.App, session.SourceStream), true)
		cancelClose()
	}
	if retryRequired {
		return firstErr
	}
	message := strings.TrimSpace(reason)
	if firstErr != nil {
		if message != "" {
			message += "; "
		}
		message += firstErr.Error()
	}
	if len(message) > 500 {
		message = message[:500]
	}
	finishCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, finishErr := s.repo.FinishAndReleaseLease(finishCtx, sessionID, terminal, message, s.now().UTC()); finishErr != nil {
		record("finish session", finishErr, true)
		return errors.Join(firstErr, finishErr)
	}
	return firstErr
}

func (s *Service) SweepExpired(ctx context.Context) error {
	if s == nil || s.repo == nil {
		return ErrTalkActivationUnavailable
	}
	sessions, err := s.repo.ListExpired(ctx, s.now().UTC())
	if err != nil {
		return err
	}
	return s.cleanupSessions(ctx, sessions, models.TalkSessionExpired, "talk lease expired")
}

func (s *Service) Recover(ctx context.Context) error {
	if s == nil || s.repo == nil {
		return ErrTalkActivationUnavailable
	}
	sessions, err := s.repo.ListNonterminal(ctx)
	if err != nil {
		return err
	}
	return s.cleanupSessions(ctx, sessions, models.TalkSessionExpired, "service restart recovery")
}

func (s *Service) Shutdown(ctx context.Context) error {
	if s == nil || s.repo == nil {
		return nil
	}
	sessions, err := s.repo.ListNonterminal(ctx)
	if err != nil {
		return err
	}
	return s.cleanupSessions(ctx, sessions, models.TalkSessionEnded, "service shutdown")
}

func (s *Service) cleanupSessions(ctx context.Context, sessions []models.GbTalkSession, terminal models.TalkSessionState, reason string) error {
	var wg sync.WaitGroup
	errs := make(chan error, len(sessions))
	for i := range sessions {
		wg.Add(1)
		go func(sessionID string) {
			defer wg.Done()
			if err := s.Cleanup(ctx, sessionID, terminal, reason); err != nil {
				errs <- err
			}
		}(sessions[i].SessionID)
	}
	wg.Wait()
	close(errs)
	var result error
	for err := range errs {
		result = errors.Join(result, err)
	}
	return result
}

type CleanupWorker struct {
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
	mu     sync.RWMutex
	err    error
}

func (s *Service) StartCleanupWorker(interval time.Duration) *CleanupWorker {
	if interval <= 0 {
		interval = time.Second
	}
	ctx, cancel := context.WithCancel(context.Background())
	worker := &CleanupWorker{cancel: cancel, done: make(chan struct{})}
	ready := make(chan struct{})
	go func() {
		defer close(worker.done)
		worker.setError(s.SweepExpired(ctx))
		close(ready)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				worker.setError(s.SweepExpired(ctx))
			}
		}
	}()
	<-ready
	return worker
}

func (w *CleanupWorker) Err() error {
	if w == nil {
		return nil
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.err
}

func (w *CleanupWorker) setError(err error) {
	if err == nil {
		return
	}
	w.mu.Lock()
	w.err = err
	w.mu.Unlock()
}

func (w *CleanupWorker) Stop() {
	if w == nil {
		return
	}
	w.once.Do(w.cancel)
	<-w.done
}

func (r *cleanupRuntime) begin(sessionID string) (*cleanupCall, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if call := r.running[sessionID]; call != nil {
		return call, false
	}
	call := &cleanupCall{done: make(chan struct{})}
	r.running[sessionID] = call
	return call, true
}

func (r *cleanupRuntime) complete(sessionID string, call *cleanupCall, err error) {
	r.mu.Lock()
	call.err = err
	delete(r.running, sessionID)
	close(call.done)
	r.mu.Unlock()
}
